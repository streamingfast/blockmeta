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
	"time"

	pbbstream "github.com/dfuse-io/pbgo/dfuse/bstream/v1"
	pbheadinfo "github.com/dfuse-io/pbgo/dfuse/headinfo/v1"
	"github.com/eoscanada/eos-go"
	"github.com/golang/protobuf/ptypes"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// GetHeadInfo can be called even when not ready, it will simply relay info from where it can
func (s *server) GetHeadInfo(ctx context.Context, req *pbheadinfo.HeadInfoRequest) (*pbheadinfo.HeadInfoResponse, error) {
	switch req.Source {
	case pbheadinfo.HeadInfoRequest_STREAM:
		if s.ready.Load() {
			return s.headInfoFromLocal()
		}
		return headInfoFromBlockstream(ctx, s.blockstreamConn, append(s.upstreamEOSAPIs, s.extraEOSAPIs...))

	case pbheadinfo.HeadInfoRequest_NETWORK:
		if s.protocol != pbbstream.Protocol_EOS {
			return nil, fmt.Errorf("unimplemented headinfo source for this protocol: %d", s.protocol)
		}
		return headInfoFromAPI(ctx, s.upstreamEOSAPIs)

	default:
		return nil, fmt.Errorf("unimplemented headinfo source")
	}

}

func (s *server) headInfoFromLocal() (*pbheadinfo.HeadInfoResponse, error) {
	s.headLock.RLock()
	head := s.headBlock
	s.headLock.RUnlock()
	s.libLock.RLock()
	lib := s.lib
	s.libLock.RUnlock()

	headTimestamp, err := ptypes.TimestampProto(head.Time())
	if err != nil {
		zlog.Error("invalid timestamp conversion from head block", zap.Error(err))
		return nil, err
	}

	return &pbheadinfo.HeadInfoResponse{
		LibNum:   lib.Num(),
		LibID:    lib.ID(),
		HeadNum:  head.Num(),
		HeadID:   head.ID(),
		HeadTime: headTimestamp,
	}, nil
}

func headInfoFromAPI(ctx context.Context, eosAPIs []*eos.API) (*pbheadinfo.HeadInfoResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	respChan := make(chan *pbheadinfo.HeadInfoResponse)
	errChan := make(chan error)
	for _, a := range eosAPIs {
		api := a
		go func() {
			info, err := api.GetInfo(ctx)
			if err != nil {
				select {
				case errChan <- err:
				case <-ctx.Done():
				}
				return
			}
			headTimestamp, err := ptypes.TimestampProto(info.HeadBlockTime.Time)
			if err != nil {
				zlog.Error("invalid timestamp conversion from head block", zap.Error(err))
				select {
				case errChan <- err:
				case <-ctx.Done():
				}
				return

			}

			resp := &pbheadinfo.HeadInfoResponse{
				LibNum:   uint64(info.LastIrreversibleBlockNum),
				LibID:    info.LastIrreversibleBlockID.String(),
				HeadNum:  uint64(info.HeadBlockNum),
				HeadID:   info.HeadBlockID.String(),
				HeadTime: headTimestamp,
			}

			select {
			case respChan <- resp:
			case <-ctx.Done():
			}

		}()
	}
	var errors []error
	for {
		if len(errors) == len(eosAPIs) {
			return nil, fmt.Errorf("all APIs failed with errors: %v", errors)
		}
		select {
		case resp := <-respChan:
			return resp, nil
		case err := <-errChan:
			errors = append(errors, err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func headInfoFromBlockstream(ctx context.Context, conn *grpc.ClientConn, apis []*eos.API) (*pbheadinfo.HeadInfoResponse, error) {
	headinfoCli := pbheadinfo.NewHeadInfoClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	head, err := headinfoCli.GetHeadInfo(ctx, &pbheadinfo.HeadInfoRequest{}, grpc.WaitForReady(false))
	if err != nil {
		return nil, err
	}

	if head.LibID == "" {
		id, err := eosNumToIDFromAPI(ctx, head.LibNum, apis)
		if err != nil {
			return nil, err
		}
		head.LibID = id
	}
	return head, nil
}

func eosNumToIDFromAPI(ctx context.Context, blockNum uint64, eosAPIs []*eos.API) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if blockNum < 2 {
		return "", fmt.Errorf("trying to get block ID below block 2 on EOS")
	}
	respChan := make(chan string)
	errChan := make(chan error)
	for _, a := range eosAPIs {
		api := a
		go func() {
			blk, err := api.GetBlockByNum(ctx, uint32(blockNum))
			if err != nil || blk == nil {
				select {
				case errChan <- err:
				case <-ctx.Done():
				}
				return
			}

			id, err := blk.BlockID()
			if err != nil {
				select {
				case errChan <- err:
				case <-ctx.Done():
				}
				return

			}

			select {
			case respChan <- id.String():
			case <-ctx.Done():
			}

		}()
	}
	var errors []error
	for {
		if len(errors) == len(eosAPIs) {
			return "", fmt.Errorf("all EOS APIs failed with errors: %v", errors)
		}
		select {
		case resp := <-respChan:
			return resp, nil
		case err := <-errChan:
			errors = append(errors, err)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func Timestamp(ts *tspb.Timestamp) time.Time {
	t, _ := ptypes.Timestamp(ts)
	return t
}

func TimestampProto(t time.Time) *tspb.Timestamp {
	out, _ := ptypes.TimestampProto(t)
	return out
}
