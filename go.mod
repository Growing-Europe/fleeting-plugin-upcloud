module github.com/Growing-Europe/fleeting-plugin-upcloud

go 1.26.0

// v0.1.0's release pipeline produced no signed artifacts (a broken cosign config);
// it is superseded by v0.1.1 and later. Retracting steers `go get` away from it.
retract v0.1.0

require github.com/UpCloudLtd/upcloud-go-api/v8 v8.38.0

require pgregory.net/rapid v1.3.0

require (
	github.com/hashicorp/go-hclog v1.6.3
	github.com/pelletier/go-toml/v2 v2.3.1
	gitlab.com/gitlab-org/fleeting/fleeting v0.0.0-20260501120451-629e3db24357
	golang.org/x/crypto v0.56.0
)

require (
	github.com/fatih/color v1.18.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-plugin v1.7.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/oklog/run v1.2.0 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260120221211-b8f7ae30c516 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

require (
	go.yaml.in/yaml/v4 v4.0.0-rc.3 // indirect
	gopkg.in/dnaeon/go-vcr.v4 v4.0.6
)
