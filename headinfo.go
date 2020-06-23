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

	"github.com/dfuse-io/bstream"

	pbheadinfo "github.com/dfuse-io/pbgo/dfuse/headinfo/v1"
	"github.com/golang/protobuf/ptypes"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var BlockNumToIDFromAPI func(ctx context.Context, blockNum uint64) (string, error)
var GetHeadInfoFromAPI func(ctx context.Context) (*pbheadinfo.HeadInfoResponse, error)
var GetIrrIDFromAPI func(ctx context.Context, blockNum uint64, libNum uint64) (string, error)

// GetHeadInfo can be called even when not ready, it will simply relay info from where it can
func (s *server) GetHeadInfo(ctx context.Context, req *pbheadinfo.HeadInfoRequest) (*pbheadinfo.HeadInfoResponse, error) {
	switch req.Source {
	case pbheadinfo.HeadInfoRequest_STREAM:
		if s.ready.Load() {
			return s.headInfoFromLocal()
		}
		return headInfoFromBlockstream(ctx, s.blockstreamConn)

	case pbheadinfo.HeadInfoRequest_NETWORK:
		return GetHeadInfoFromAPI(ctx)

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

	hi := &pbheadinfo.HeadInfoResponse{
		LibNum:   lib.Num(),
		LibID:    lib.ID(),
		HeadNum:  head.Num(),
		HeadID:   head.ID(),
		HeadTime: headTimestamp,
	}
	zlog.Debug("head info from local returning", zap.Reflect("head_info", hi))
	return hi, nil
}

func headInfoFromBlockstream(ctx context.Context, conn *grpc.ClientConn) (*pbheadinfo.HeadInfoResponse, error) {
	headinfoCli := pbheadinfo.NewHeadInfoClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	var hi *pbheadinfo.HeadInfoResponse
	hi, err = headinfoCli.GetHeadInfo(ctx, &pbheadinfo.HeadInfoRequest{}, grpc.WaitForReady(false))
	if err != nil {
		return nil, err
	}

	zlog.Info("got head info from cli", zap.Uint64("lib_num", hi.LibNum), zap.String("lib_id", hi.LibID))

	if hi.LibID == "" {
		for {
			apiHeadInfo, err := GetHeadInfoFromAPI(ctx)
			if err != nil {
				return nil, err
			}
			if apiHeadInfo.LibNum == bstream.GetProtocolGenesisBlock {
				zlog.Debug("got genesis block as lib block. retrying")
				time.Sleep(2 * time.Second)
				continue
			}
			hi = apiHeadInfo
			break
		}
	}
	zlog.Debug("head info from stream returning", zap.Uint64("lib_block_num", hi.LibNum), zap.String("lib_id", hi.LibID))
	return hi, nil
}

func Timestamp(ts *tspb.Timestamp) time.Time {
	t, _ := ptypes.Timestamp(ts)
	return t
}

func TimestampProto(t time.Time) *tspb.Timestamp {
	out, _ := ptypes.TimestampProto(t)
	return out
}
