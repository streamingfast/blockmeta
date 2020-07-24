// Copyright 2019 dfuse Platform Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package blockmeta

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/dfuse-io/blockmeta/metrics"
	"github.com/dfuse-io/bstream"
	"github.com/dfuse-io/bstream/blockstream"
	"github.com/dfuse-io/bstream/forkable"
	"github.com/dfuse-io/derr"
	"github.com/dfuse-io/dgrpc"
	"github.com/dfuse-io/dstore"
	"github.com/dfuse-io/kvdb"
	"github.com/dfuse-io/logging"
	pbblockmeta "github.com/dfuse-io/pbgo/dfuse/blockmeta/v1"
	pbbstream "github.com/dfuse-io/pbgo/dfuse/bstream/v1"
	pbheadinfo "github.com/dfuse-io/pbgo/dfuse/headinfo/v1"
	pbhealth "github.com/dfuse-io/pbgo/grpc/health/v1"
	"github.com/dfuse-io/shutter"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	forkDBRef *forkable.ForkDB

	headBlock *bstream.Block
	headLock  sync.RWMutex
	lib       *bstream.Block
	libLock   sync.RWMutex

	keepDurationAfterLib time.Duration

	blockTimes map[string]time.Time
	mapLock    sync.Mutex

	protocol pbbstream.Protocol

	*shutter.Shutter
	db BlockmetaDB
	gs *grpc.Server

	blocksStore         dstore.Store
	blockstreamAddr     string
	blockstreamConn     *grpc.ClientConn
	src                 bstream.Source
	initialStartBlockID string // for readiness

	addr  string
	ready *atomic.Bool
}

var GetBlockNumFromID func(ctx context.Context, id string) (uint64, error)

func NewServer(
	addr string,
	blockstreamAddr string,
	blocksStore dstore.Store,
	db BlockmetaDB,
	protocol pbbstream.Protocol) *server {
	return &server{
		addr:                 addr,
		keepDurationAfterLib: 5 * time.Minute,
		blockstreamAddr:      blockstreamAddr,
		blocksStore:          blocksStore,
		db:                   db,
		protocol:             protocol,

		blockTimes: make(map[string]time.Time),
		ready:      atomic.NewBool(false),
		Shutter:    shutter.New(),
	}
}

func (s *server) getInitialStartBlock() (bstream.BlockRef, error) {
	ctx := context.Background()
	retryDelay := 0 * time.Millisecond
	for {
		select {
		case <-time.After(retryDelay):
			retryDelay = 500 * time.Millisecond
		case <-s.Terminating():
			return nil, fmt.Errorf("terminating with %w", s.Err())
		}
		headInfo, err := s.GetHeadInfo(ctx, &pbheadinfo.HeadInfoRequest{Source: pbheadinfo.HeadInfoRequest_STREAM})
		if err != nil {
			zlog.Info("cannot get stream's headinfo, retrying", zap.Error(err))
			continue
		}
		return bstream.NewBlockRef(headInfo.LibID, headInfo.LibNum), nil
	}
}

func (s *server) setupSource(initialStartBlock bstream.BlockRef) {

	zlog.Info("setting up source, waiting to see block", zap.Stringer("block", initialStartBlock))
	sf := bstream.SourceFromRefFactory(func(startBlockRef bstream.BlockRef, h bstream.Handler) bstream.Source {
		if startBlockRef.ID() == "" {
			startBlockRef = initialStartBlock
		}

		fileSourceFactory := bstream.SourceFactory(func(h bstream.Handler) bstream.Source {
			src := bstream.NewFileSource(s.blocksStore, startBlockRef.Num(), 2, nil, h)
			return src
		})

		zlog.Info("new live joining source", zap.Stringer("start_block", startBlockRef))
		liveSourceFactory := bstream.SourceFactory(func(subHandler bstream.Handler) bstream.Source {
			return blockstream.NewSource(
				context.Background(),
				s.blockstreamAddr,
				200,
				subHandler,
				blockstream.WithRequester("blockmeta"),
			)
		})

		js := bstream.NewJoiningSource(
			fileSourceFactory,
			liveSourceFactory,
			h,
			bstream.JoiningSourceLogger(zlog),
			bstream.JoiningSourceTargetBlockID(startBlockRef.ID()),
			bstream.JoiningSourceTargetBlockNum(bstream.GetProtocolFirstStreamableBlock),
		)
		return js
	})

	handler := bstream.Handler(s)
	forkable := forkable.New(handler, forkable.WithLogger(zlog), forkable.WithInclusiveLIB(initialStartBlock))
	s.src = bstream.NewEternalSource(sf, forkable, bstream.EternalSourceWithLogger(zlog))
}

func (s *server) ProcessBlock(block *bstream.Block, obj interface{}) error {
	blockID := block.ID()
	blockTime := block.Time()

	fObj := obj.(*forkable.ForkableObject)
	if s.forkDBRef == nil {
		s.forkDBRef = fObj.ForkDB
	}

	switch fObj.Step {
	case forkable.StepNew, forkable.StepRedo:
		s.headLock.Lock()
		s.headBlock = block
		s.headLock.Unlock()
		s.mapLock.Lock()
		defer s.mapLock.Unlock()
		zlog.Debug("processing new/redo block", zap.Uint64("block_num", block.Num()), zap.String("step", fObj.Step.String()))
		metrics.MapSize.Inc()
		metrics.HeadBlockNumber.SetUint64(block.Num())
		metrics.HeadTimeDrift.SetBlockTime(blockTime)

		s.blockTimes[blockID] = blockTime

		if !s.ready.Load() {
			if blockID == s.initialStartBlockID {
				zlog.Info("seen initial start block (as new, but assuming irreversible), setting ready")
				s.libLock.Lock()
				s.lib = block
				s.libLock.Unlock()
				s.ready.Store(true)
			} else if block.Num() == bstream.GetProtocolFirstStreamableBlock && block.PreviousID() == s.initialStartBlockID {
				zlog.Info("starting on first streamable block with LIB set to genesis block")
				s.libLock.Lock()
				s.lib = &bstream.Block{
					Id:     s.initialStartBlockID,
					Number: block.Num() - 1,
				}
				s.libLock.Unlock()
				s.ready.Store(true)
			}

		}

	case forkable.StepUndo:
		s.mapLock.Lock()
		defer s.mapLock.Unlock()

		metrics.MapSize.Dec()
		delete(s.blockTimes, blockID)

	case forkable.StepIrreversible:
		zlog.Debug("processing irreversible block", zap.Uint64("block_num", block.Num()), zap.String("step", fObj.Step.String()))
		if !s.ready.Load() && blockID == s.initialStartBlockID {
			zlog.Info("seen initial start block, setting ready")
			s.ready.Store(true)
		}

		s.libLock.Lock()
		s.lib = block
		s.libLock.Unlock()
		s.purgeOldIrrBlocksFromMap()
	}
	return nil
}

func (s *server) BootstrapAndLaunch() error {
	blockstreamConn, err := dgrpc.NewInternalClient(s.blockstreamAddr)
	if err != nil {
		return fmt.Errorf("cannot connect to blockmeta server: %w", err)
	}
	s.blockstreamConn = blockstreamConn

	initialStartBlock, err := s.getInitialStartBlock()
	if err != nil {
		return err
	}
	s.initialStartBlockID = initialStartBlock.ID()
	s.setupSource(initialStartBlock)
	return s.launch()
}

func (s *server) launch() error {
	zlog.Info("starting blockmeta server")

	s.gs = dgrpc.NewServer(dgrpc.WithLogger(zlog))
	s.OnTerminating(func(err error) {
		s.gs.Stop()
		s.src.Shutdown(err)
	})
	s.src.OnTerminating(func(err error) { s.Shutdown(err) }) // make sure source shutdown will kill grpc server too

	go s.src.Run()

	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}

	pbhealth.RegisterHealthServer(s.gs, s)
	zlog.Info("ready to serve")

	pbblockmeta.RegisterBlockIDServer(s.gs, s)
	pbblockmeta.RegisterTimeToIDServer(s.gs, s)
	pbblockmeta.RegisterChainDiscriminatorServer(s.gs, s)
	pbblockmeta.RegisterForksServer(s.gs, s)
	pbheadinfo.RegisterHeadInfoServer(s.gs, s)

	return s.gs.Serve(lis)

}

func (s *server) checkReady() error {
	if !s.ready.Load() {
		return status.Errorf(codes.Unavailable, "service not ready")
	}
	return nil
}

func (s *server) LibTime() (time.Time, error) {
	s.libLock.RLock()
	defer s.libLock.RUnlock()
	if s.lib == nil {
		return time.Time{}, fmt.Errorf("no lib found yet")
	}
	return s.lib.Time(), nil
}

func (s *server) purgeOldIrrBlocksFromMap() {
	libtime, err := s.LibTime()
	if err != nil {
		return
	}
	truncateBefore := libtime.Add(-s.keepDurationAfterLib)
	s.mapLock.Lock()
	defer s.mapLock.Unlock()
	for id, t := range s.blockTimes {
		if t.Before(truncateBefore) {
			zlog.Debug("deleting from map", zap.String("id", id), zap.Time("time", t))
			metrics.MapSize.Dec()
			delete(s.blockTimes, id)
		}
	}
}

func (s *server) lowestTime() (lowestTime time.Time) {
	s.mapLock.Lock()
	for _, t := range s.blockTimes {
		if lowestTime.IsZero() {
			lowestTime = t
		}
		if t.Before(lowestTime) {
			lowestTime = t
		}
	}
	s.mapLock.Unlock()
	return lowestTime
}

func (s *server) NumToID(ctx context.Context, in *pbblockmeta.NumToIDRequest) (*pbblockmeta.BlockIDResponse, error) {
	zlog.Info("num to id called", zap.Uint64("block_num", in.BlockNum))
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	out := &pbblockmeta.BlockIDResponse{}
	if s.forkDBRef.IsBehindLIB(in.BlockNum) {
		zlog.Info("behind lib", zap.Uint64("block_num", in.BlockNum))
		id, err := s.numToIDFromEosDB(ctx, in.BlockNum)
		if err != nil {
			return out, err
		}
		out.Id = id
		out.Irreversible = true
		return out, nil
	}
	s.headLock.RLock()
	b := s.forkDBRef.BlockInCurrentChain(s.headBlock, in.BlockNum)
	s.headLock.RUnlock()
	out.Id = b.ID()
	out.Irreversible = false
	return out, nil
}

func (s *server) Resolve(ctx context.Context, in *pbblockmeta.ForkResolveRequest) (*pbblockmeta.ForkResolveResponse, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	out := &pbblockmeta.ForkResolveResponse{}

	var topfork bstream.BlockRef
	if in.Block.BlockNum == 0 && s.protocol == pbbstream.Protocol_EOS {
		topfork = bstream.NewBlockRefFromID(in.Block.BlockID)
	} else {
		topfork = bstream.NewBlockRef(in.Block.BlockID, in.Block.BlockNum)
	}

	refs, err := s.db.GetForkPreviousBlocks(ctx, topfork)
	if err != nil {
		return out, err
	}
	for _, ref := range refs {
		out.ForkedBlockRefs = append(out.ForkedBlockRefs, &pbblockmeta.BlockRef{
			BlockNum: ref.Num(),
			BlockID:  ref.ID(),
		})
	}
	return out, nil
}

func (s *server) LIBID(ctx context.Context, in *pbblockmeta.LIBRequest) (*pbblockmeta.BlockIDResponse, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return &pbblockmeta.BlockIDResponse{Id: s.forkDBRef.LIBID()}, nil
}

func (s *server) numToIDFromEosDB(ctx context.Context, blockNum uint64) (id string, err error) {
	if blockNum > s.forkDBRef.LIBNum() { // ensure this doesn't happen by gating this with forkable.IsBehindLIB(blockNum)
		return "", fmt.Errorf("cannot look up blocks after lib in here")
	}

	// as of the interfaces-refactor the this call has become INCLUSIVE no need to increment
	irrBlockRef, err := s.db.GetIrreversibleIDAtBlockNum(ctx, blockNum)
	if err != nil {
		return "", err
	}

	if irrBlockRef.Num() != blockNum {
		zlog.Info("database out of sync", zap.Uint64("wanted_block_num", blockNum), zap.Uint64("found block num", irrBlockRef.Num()))
		id, err := BlockNumToIDFromAPI(ctx, blockNum)
		if err != nil {
			return "", err
		}
		zlog.Info("got id from api", zap.Uint64("block_num", blockNum), zap.String("block_id", id))
		return id, nil
	}

	return irrBlockRef.ID(), nil
}

func (s *server) getIrreversibleFromDB(ctx context.Context, blockID string) (isIrreversible bool, err error) {
	blockNum, err := GetBlockNumFromID(ctx, blockID)
	if err != nil {
		return false, err
	}
	if blockNum > s.forkDBRef.LIBNum() { // ensure this doesn't happen by gating this with forkable.IsBehindLIB(blockNum)
		return false, fmt.Errorf("cannot look up blocks after lib in here")
	}

	// as of the interfaces-refactor the this call has become INCLUSIVE no need to increment
	irrBlockID, err := s.db.GetIrreversibleIDAtBlockNum(ctx, blockNum)
	if err != nil {
		return false, err
	}

	if irrBlockID.Num() != blockNum {
		return false, ErrDBOutOfSync
	}

	if irrBlockID.ID() != blockID {
		return false, nil
	}

	return true, nil
}

func (s *server) GetBlockInLongestChain(ctx context.Context, in *pbblockmeta.GetBlockInLongestChainRequest) (*pbblockmeta.GetBlockInLongestChainResponse, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	zlogger := logging.Logger(ctx, zlog)

	out := &pbblockmeta.GetBlockInLongestChainResponse{
		BlockNum: in.BlockNum,
	}
	if s.forkDBRef.IsBehindLIB(in.BlockNum) {
		zlogger.Debug("get block in longest chain requested with block behind lib", zap.Uint64("block_num", in.BlockNum))

		id, err := s.numToIDFromEosDB(ctx, in.BlockNum) // fallback on eosDB
		if err != nil {
			return nil, err
		}

		out.BlockNum = in.BlockNum
		out.BlockId = id
		return out, nil
	}

	s.headLock.RLock()
	b := s.forkDBRef.BlockInCurrentChain(s.headBlock, in.BlockNum)
	s.headLock.RUnlock()

	out.BlockId = b.ID()
	return out, nil

}

func (s *server) InLongestChain(ctx context.Context, in *pbblockmeta.InLongestChainRequest) (*pbblockmeta.InLongestChainResponse, error) {
	blockNum, err := GetBlockNumFromID(ctx, in.BlockID)
	if err != nil {
		return nil, err
	}
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	out := &pbblockmeta.InLongestChainResponse{}

	zlogger := logging.Logger(ctx, zlog)
	if s.protocol != pbbstream.Protocol_EOS {
		zlogger.Error("InLongestChain only implemented on EOS")
		return out, ErrNotImplemented
	}

	if s.forkDBRef.IsBehindLIB(blockNum) {
		zlogger.Debug("InLongestChain requested with block behind lib", zap.Uint64("block_num", blockNum))

		s.mapLock.Lock()
		_, found := s.blockTimes[in.BlockID] // from local irreversible buffer
		s.mapLock.Unlock()

		if found {
			zlogger.Debug("inlongestChain found in recent irreversible blocks", zap.String("block_id", in.BlockID))
			out.InLongestChain = true
			out.Irreversible = true
			return out, nil
		}

		found, err := s.getIrreversibleFromDB(ctx, in.BlockID) // fallback on eosDB
		if err != nil {
			return nil, err
		}

		if found {
			out.InLongestChain = true
			out.Irreversible = true
		}
		return out, nil
	}

	s.headLock.RLock()
	b := s.forkDBRef.BlockInCurrentChain(s.headBlock, blockNum)
	s.headLock.RUnlock()

	if b.ID() == in.BlockID {
		out.InLongestChain = true
	}
	return out, nil
}

func (s *server) At(ctx context.Context, in *pbblockmeta.TimeRequest) (*pbblockmeta.BlockResponse, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	zlogger := logging.Logger(ctx, zlog)

	metrics.RequestCount.Inc()
	reqTime := Timestamp(in.Time)
	var foundID string
	var err error

	s.mapLock.Lock()
	for id, t := range s.blockTimes {
		if t.Equal(reqTime) {
			foundID = id
			break
		}
	}
	s.mapLock.Unlock()

	if foundID != "" {
		irreversible := false
		libTime, err := s.LibTime()
		if err != nil {
			return &pbblockmeta.BlockResponse{}, err
		}
		if reqTime.Before(libTime) || reqTime.Equal(libTime) {
			irreversible = true
		}
		return &pbblockmeta.BlockResponse{Id: foundID, Time: in.Time, Irreversible: irreversible}, nil
	}

	if reqTime.After(s.lowestTime()) {
		metrics.ErrorCount.Inc()
		return nil, status.Error(codes.NotFound, "block id was not found") // skip database lookup for current or future blocks
	}

	foundID, err = s.db.BlockIDAt(ctx, reqTime)
	if err != nil {
		metrics.ErrorCount.Inc()
		if err == kvdb.ErrNotFound {
			return nil, derr.Status(codes.NotFound, "block id not found")
		}

		zlogger.Error("kvdb error", zap.Error(err))
		return nil, derr.Status(codes.Internal, "backend error fetching block ID")
	}

	return &pbblockmeta.BlockResponse{Id: foundID, Time: in.Time, Irreversible: true}, nil
}

func (s *server) After(ctx context.Context, in *pbblockmeta.RelativeTimeRequest) (*pbblockmeta.BlockResponse, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	metrics.RequestCount.Inc()
	reqTime := Timestamp(in.Time)

	var foundID string
	var foundBlockTime time.Time
	s.mapLock.Lock()
	for id, t := range s.blockTimes {
		if foundBlockTime.IsZero() || t.Before(foundBlockTime) {
			if t.After(reqTime) || (in.Inclusive && t.Equal(reqTime)) {
				foundBlockTime = t
				foundID = id
			}
		}
	}
	s.mapLock.Unlock()

	lowestTimeInMap := s.lowestTime()
	if foundID != "" && !foundBlockTime.Equal(lowestTimeInMap) { // we never return the lowest value in the map, we might be missing something, so we lookup in DB
		irreversible := false
		libTime, err := s.LibTime()
		if err != nil {
			return &pbblockmeta.BlockResponse{}, err
		}
		if foundBlockTime.Before(libTime) || foundBlockTime.Equal(libTime) {
			irreversible = true
		}

		return &pbblockmeta.BlockResponse{Id: foundID, Time: TimestampProto(foundBlockTime), Irreversible: irreversible}, nil
	}

	if reqTime.After(lowestTimeInMap) || reqTime.Equal(lowestTimeInMap) {
		metrics.ErrorCount.Inc()
		return nil, status.Error(codes.NotFound, "block id was not found") // skip database lookup for current or future blocks
	}

	id, bt, err := s.db.BlockIDAfter(ctx, reqTime, in.Inclusive)
	if err != nil {
		metrics.ErrorCount.Inc()
		return nil, err
	}
	return &pbblockmeta.BlockResponse{Id: id, Time: TimestampProto(bt), Irreversible: true}, nil
}

func (s *server) Before(ctx context.Context, in *pbblockmeta.RelativeTimeRequest) (*pbblockmeta.BlockResponse, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	metrics.RequestCount.Inc()
	reqTime := Timestamp(in.Time)

	var foundID string
	var foundBlockTime time.Time
	s.mapLock.Lock()
	for id, t := range s.blockTimes {
		if t.After(foundBlockTime) {
			if t.Before(reqTime) || (in.Inclusive && t.Equal(reqTime)) {
				foundBlockTime = t
				foundID = id
			}
		}
	}
	s.mapLock.Unlock()

	if foundID != "" {
		irreversible := false
		libTime, err := s.LibTime()
		if err != nil {
			return &pbblockmeta.BlockResponse{}, err
		}
		if foundBlockTime.Before(libTime) || foundBlockTime.Equal(libTime) {
			irreversible = true
		}
		return &pbblockmeta.BlockResponse{Id: foundID, Time: TimestampProto(foundBlockTime), Irreversible: irreversible}, nil
	}

	id, bt, err := s.db.BlockIDBefore(ctx, reqTime, in.Inclusive)
	if err != nil {
		metrics.ErrorCount.Inc()
		return nil, err
	}

	return &pbblockmeta.BlockResponse{Id: id, Time: TimestampProto(bt), Irreversible: true}, nil
}
