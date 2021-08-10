module github.com/streamingfast/blockmeta

go 1.15

require (
	github.com/dfuse-io/bstream v0.0.2-0.20210810112308-458fdfd5441b
	github.com/dfuse-io/dbin v0.0.0-20200406215642-ec7f22e794eb // indirect
	github.com/dfuse-io/dgrpc v0.0.0-20210128133958-db1ca95920e4
	github.com/dfuse-io/dmetrics v0.0.0-20200406214800-499fc7b320ab
	github.com/dfuse-io/dstore v0.1.0 // indirect
	github.com/dfuse-io/jsonpb v0.0.0-20200406211248-c5cf83f0e0c0 // indirect
	github.com/dfuse-io/logging v0.0.0-20210109005628-b97a57253f70
	github.com/dfuse-io/opaque v0.0.0-20210108174126-bc02ec905d48 // indirect
	github.com/dfuse-io/pbgo v0.0.6-0.20210429181308-d54fc7723ad3
	github.com/dfuse-io/shutter v1.4.1
	github.com/golang/protobuf v1.3.5
	github.com/gorilla/mux v1.7.3 // indirect
	github.com/kr/pretty v0.2.0 // indirect
	github.com/rs/xid v1.2.1 // indirect
	github.com/streamingfast/derr v0.0.0-20210810022442-32249850a4fb
	github.com/streamingfast/dstore v0.1.1-0.20210810110932-928f221474e4 // indirect
	github.com/streamingfast/kvdb v0.0.2-0.20210809203849-c1762028eb64
	github.com/streamingfast/opaque v0.0.0-20210809210154-b964592beb5d // indirect
	github.com/stretchr/testify v1.4.0
	github.com/tidwall/gjson v1.5.0 // indirect
	go.uber.org/atomic v1.6.0
	go.uber.org/zap v1.15.0
	google.golang.org/grpc v1.29.1
)

replace github.com/blendle/zapdriver => github.com/karixtech/zapdriver v1.1.7-0.20190304072941-7b5d38c10286

// This is required to fix build where 0.1.0 version is not considered a valid version because a v0 line does not exists
// We replace with same commit, simply tricking go and tell him that's it's actually version 0.0.3
replace github.com/census-instrumentation/opencensus-proto v0.1.0-0.20181214143942-ba49f56771b8 => github.com/census-instrumentation/opencensus-proto v0.0.3-0.20181214143942-ba49f56771b8
