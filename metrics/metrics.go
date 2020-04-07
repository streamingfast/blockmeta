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

package metrics

import (
	"github.com/dfuse-io/dmetrics"
)

var metrics = dmetrics.NewSet()
var ErrorCount = metrics.NewCounter("error_count", "number of requests resulting in error (including not_found)")
var RequestCount = metrics.NewCounter("request_count", "number of requests made to this service")
var MapSize = metrics.NewGauge("map_size", "size of live blocks map")
var HeadTimeDrift = metrics.NewHeadTimeDrift("blockmeta")
var HeadBlockNumber = metrics.NewHeadBlockNumber("blockmeta")

func init() {
	dmetrics.Register(metrics)
}

func ServeMetrics() {
	dmetrics.Serve(":9102")
}
