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
	"strings"
	"time"

	"github.com/dfuse-io/bstream"
	"github.com/dfuse-io/dgrpc"
	"github.com/dfuse-io/dmetrics"
	"github.com/dfuse-io/dstore"
	pbbstream "github.com/dfuse-io/pbgo/dfuse/bstream/v1"
	pbhealth "github.com/dfuse-io/pbgo/grpc/health/v1"
	"github.com/dfuse-io/shutter"
	"github.com/streamingfast/blockmeta"
	"github.com/streamingfast/blockmeta/metrics"
	"go.uber.org/zap"
)

var StartupAborted = fmt.Errorf("blockmeta start aborted by terminating signal")

type Config struct {
	KVDBDSN         string
	BlocksStoreURL  string
	BlockStreamAddr string
	GRPCListenAddr  string
	Protocol        pbbstream.Protocol
	LiveSource      bool
}

type App struct {
	*shutter.Shutter
	config         *Config
	ReadyFunc      func()
	readinessProbe pbhealth.HealthClient
	db             blockmeta.BlockmetaDB
}

func New(config *Config, db blockmeta.BlockmetaDB) *App {
	return &App{
		Shutter:   shutter.New(),
		config:    config,
		ReadyFunc: func() {},
		db:        db,
	}
}

func (a *App) Run() error {
	dmetrics.Register(metrics.MetricSet)

	blocksStore, err := dstore.NewDBinStore(a.config.BlocksStoreURL)
	if err != nil {
		return fmt.Errorf("failed setting up blocks store: %w", err)
	}

	s := blockmeta.NewServer(
		a.config.GRPCListenAddr,
		a.config.BlockStreamAddr,
		blocksStore,
		a.db,
		a.config.Protocol,
	)

	a.OnTerminating(func(err error) {
		s.Shutdown(err)
	})

	go func() {
		a.Shutdown(s.BootstrapAndLaunch())
	}()

	// Move this to where it fits
	a.ReadyFunc()

	gs, err := dgrpc.NewInternalClient(a.config.GRPCListenAddr)
	if err != nil {
		return fmt.Errorf("cannot create readiness probe")
	}
	a.readinessProbe = pbhealth.NewHealthClient(gs)

	return nil
}

func (a *App) explodeDatabaseConnectionInfo(connectionInfo string) (project, instance, prefix string, err error) {
	parts := strings.Split(connectionInfo, ":")
	if len(parts) != 3 {
		err = fmt.Errorf("database connection info should be <project>:<instance>:<prefix>")
		return
	}

	return parts[0], parts[1], parts[2], nil
}

func (a *App) getLastWrittenBlock(ctx context.Context, db blockmeta.BlockmetaDB) (string, error) {
	zlog.Info("getting last written block in kvdb")
	for {
		lastWrittenBlockID, err := db.GetLastWrittenBlockID(ctx)
		if err == nil {
			zlog.Info("fetched last written block", zap.String("last_written_block_id", lastWrittenBlockID))
			return lastWrittenBlockID, nil
		}
		zlog.Info("failed getting last written block", zap.Error(err))

		select {
		case <-time.After(5 * time.Second):
		case <-a.Shutter.Terminating():
			zlog.Info("getting last written block aborted by terminating signal")
			return "", StartupAborted
		}
	}
}

func (a *App) getIrrBlockAtNum(ctx context.Context, lastWrittenBlockID string, db blockmeta.BlockmetaDB) (bstream.BlockRef, error) {
	zlog.Info("get irr block num at")
	for {
		libRef, err := db.GetIrreversibleIDAtBlockID(ctx, lastWrittenBlockID)
		if err == nil {
			zlog.Info("fetched irr block", zap.String("irr_block_id", libRef.ID()), zap.Uint64("irr_block_num", libRef.Num()))
			return libRef, nil
		}
		zlog.Info("cannot get LIB", zap.Error(err))
		select {
		case <-time.After(5 * time.Second):
		case <-a.Shutter.Terminating():
			zlog.Info("getting irr block aboard by terminating signal")
			return nil, StartupAborted
		}

	}
}

func (a *App) OnReady(f func()) {
	a.ReadyFunc = f
}

func (a *App) IsReady() bool {
	if a.readinessProbe == nil {
		return false
	}

	resp, err := a.readinessProbe.Check(context.Background(), &pbhealth.HealthCheckRequest{})
	if err != nil {
		zlog.Info("merger readiness probe error", zap.Error(err))
		return false
	}

	if resp.Status == pbhealth.HealthCheckResponse_SERVING {
		return true
	}

	return false
}
