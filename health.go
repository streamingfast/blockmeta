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
	"time"

	pbhealth "google.golang.org/grpc/health/grpc_health_v1"
)

func (s *server) Check(ctx context.Context, in *pbhealth.HealthCheckRequest) (*pbhealth.HealthCheckResponse, error) {
	return &pbhealth.HealthCheckResponse{
		Status: s.healthStatus(),
	}, nil
}

func (s *server) Watch(req *pbhealth.HealthCheckRequest, stream pbhealth.Health_WatchServer) error {
	currentStatus := pbhealth.HealthCheckResponse_SERVICE_UNKNOWN
	waitTime := 0 * time.Second

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-time.After(waitTime):
			newStatus := s.healthStatus()
			waitTime = 5 * time.Second

			if newStatus != currentStatus {
				currentStatus = newStatus

				if err := stream.Send(&pbhealth.HealthCheckResponse{Status: currentStatus}); err != nil {
					return err
				}
			}
		}
	}
}

func (s *server) healthStatus() pbhealth.HealthCheckResponse_ServingStatus {
	status := pbhealth.HealthCheckResponse_NOT_SERVING
	if s.ready.Load() && !s.IsTerminating() {
		status = pbhealth.HealthCheckResponse_SERVING
	}

	return status
}
