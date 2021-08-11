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

	pbhealth "github.com/streamingfast/pbgo/grpc/health/v1"
)

func (s *server) Check(ctx context.Context, in *pbhealth.HealthCheckRequest) (*pbhealth.HealthCheckResponse, error) {
	status := pbhealth.HealthCheckResponse_NOT_SERVING
	if s.ready.Load() && !s.IsTerminating() {
		status = pbhealth.HealthCheckResponse_SERVING
	}

	return &pbhealth.HealthCheckResponse{
		Status: status,
	}, nil
}
