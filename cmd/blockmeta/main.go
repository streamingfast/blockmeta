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

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	blockmetaapp "github.com/dfuse-io/blockmeta/app/blockmeta"
	"github.com/dfuse-io/blockmeta/metrics"
	_ "github.com/dfuse-io/bstream/codecs/deos"
	"github.com/dfuse-io/derr"
	"github.com/dfuse-io/logging"
	pbbstream "github.com/dfuse-io/pbgo/dfuse/bstream/v1"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var flagKvdbDSN = flag.String("kvdb-dsn", "bigtable://dev.dev/kvdb", "Kvdb database connection information")
var flagBlocksStore = flag.String("source-store", "file:///tmp/blocksstore", "URL to source store")
var flagBlockStreamAddr = flag.String("block-stream-addr", ":9001", "Websocket endpoint to get a real-time blocks feed")
var flagEOSAPIUpstreamAddr = flag.String("eos-api-upstream-addr", "", "EOS API address to fetch info from running chain, must be in-sync")
var flagEOSAPIExtraAddr = flag.String("eos-api-extra-addr", "", "Additional EOS API address for ID lookups (valid even if it is out of sync or read-only)")
var flagListenAddr = flag.String("listen-addr", ":9000", "GRPC listen on this port")
var flagProtocol = flag.String("protocol", "", "Protocol that this service will handle")
var flagLiveSource = flag.Bool("live-source", true, "Wheter we want to connect to a live block source or not, defaults to true")

func main() {
	setupTracing()

	flag.Parse()
	zlog.Info("starting blockmeta", logging.FlagFields()...)

	protocol := mustGetProtocol(*flagProtocol)

	go metrics.ServeMetrics()

	derr.Check("validating protocol-exclusive flags", checkEOSAPIAddr(*flagEOSAPIUpstreamAddr, protocol))
	derr.Check("validating protocol-exclusive flags", checkEOSAPIAddr(*flagEOSAPIExtraAddr, protocol))

	config := &blockmetaapp.Config{
		KvdbDSN:                 *flagKvdbDSN,
		BlocksStore:             *flagBlocksStore,
		BlockStreamAddr:         *flagBlockStreamAddr,
		ListenAddr:              *flagListenAddr,
		Protocol:                protocol,
		LiveSource:              *flagLiveSource,
		EOSAPIUpstreamAddresses: []string{*flagEOSAPIUpstreamAddr},
		EOSAPIExtraAddresses:    []string{*flagEOSAPIExtraAddr},
	}
	app := blockmetaapp.New(config)
	derr.Check("failed to run app", app.Run())

	select {
	case sig := <-derr.SetupSignalHandler(viper.GetDuration("global-shutdown-drain-delay")):
		zlog.Info("terminating through system signal", zap.Reflect("sig", sig))
		app.Shutdown(nil)
	case <-app.Terminated():
		zlog.Info("Blockmeta is done", zap.Error(app.Err()))
	}

	return
}

func checkEOSAPIAddr(flagEOSAPIAddr string, protocol pbbstream.Protocol) error {
	if protocol != pbbstream.Protocol_EOS && flagEOSAPIAddr != "" {
		return fmt.Errorf("cannot set flag eos-api-addr on another protocol than EOS")
	}
	return nil
}

func mustGetProtocol(blockKindString string) pbbstream.Protocol {
	if blockKindString == "" {
		fmt.Println("You must pass the 'protocol' flag, none received")
		os.Exit(1)
	}

	protocol := pbbstream.Protocol_value[blockKindString]
	if protocol == 0 {
		derr.Check("invalid protocol", errors.New("invalid protocol"))
	}

	return pbbstream.Protocol(protocol)
}
