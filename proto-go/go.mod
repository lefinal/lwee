module github.com/lefinal/lwee/proto-go

go 1.21

replace github.com/lefinal/lwee/shared-go => ../shared-go

require (
	github.com/gofrs/uuid v4.4.0+incompatible
	github.com/golang/protobuf v1.5.3
	github.com/lefinal/meh v1.10.0
	github.com/stretchr/testify v1.8.4
	google.golang.org/grpc v1.56.2
	google.golang.org/protobuf v1.31.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sys v0.8.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
