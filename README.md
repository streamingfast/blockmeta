blockmeta serves block metadata to the world
--------------------------------------------

Depends on:
* live block stream
* blocks file
* eosdb (to determine last written HEAD and LIB and where to start)

Testing the service with `grpcui`
---------------------------------

    go get github.com/fullstorydev/grpcui/cmd/grpcui

    grpcui -plaintext -port 60001 localhost:50001


Installing protoc
-----------------

The simplest way to do this is to download pre-compiled binaries for
your platform(protoc-<version>-<platform>.zip) from here:
https://github.com/google/protobuf/releases


* add `protoc` to your path
* `go get -u github.com/golang/protobuf/protoc-gen-go`
* use `go generate` to update blockmeta.pb.go from blockmeta.proto
* see example_client/ for details on how to use...
