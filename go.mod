module github.com/dfuse-io/blockmeta

require (
	github.com/dfuse-io/bstream v0.0.0-20200427161155-5bc64e86c005
	github.com/dfuse-io/derr v0.0.0-20200406214256-c690655246a1
	github.com/dfuse-io/dgrpc v0.0.0-20200406214416-6271093e544c
	github.com/dfuse-io/dmetrics v0.0.0-20200406214800-499fc7b320ab
	github.com/dfuse-io/dstore v0.1.0
	github.com/dfuse-io/kvdb v0.0.0-20200520211319-cbf4776ac2a3
	github.com/dfuse-io/logging v0.0.0-20200407175011-14021b7a79af
	github.com/dfuse-io/pbgo v0.0.6-0.20200602163822-677d66ead419
	github.com/dfuse-io/shutter v1.4.1-0.20200319040708-c809eec458e6
	github.com/eoscanada/eos-go v0.9.1-0.20200415144303-2adb25bcdeca
	github.com/golang/protobuf v1.4.2
	github.com/gorilla/mux v1.7.3 // indirect
	github.com/kr/pretty v0.2.0 // indirect
	github.com/stretchr/testify v1.4.0
	go.uber.org/atomic v1.6.0
	go.uber.org/zap v1.14.0
	google.golang.org/grpc v1.26.0
)

replace github.com/blendle/zapdriver => github.com/karixtech/zapdriver v1.1.7-0.20190304072941-7b5d38c10286

// This is required to fix build where 0.1.0 version is not considered a valid version because a v0 line does not exists
// We replace with same commit, simply tricking go and tell him that's it's actually version 0.0.3
replace github.com/census-instrumentation/opencensus-proto v0.1.0-0.20181214143942-ba49f56771b8 => github.com/census-instrumentation/opencensus-proto v0.0.3-0.20181214143942-ba49f56771b8

go 1.13
