module github.com/lefinal/lwee/lwee

go 1.21

replace (
	github.com/lefinal/lwee/proto-go => ../proto-go
	github.com/lefinal/lwee/shared-go => ../shared-go
)

require (
	github.com/lefinal/lwee/shared-go v0.0.0-00010101000000-000000000000
	github.com/lefinal/meh v1.10.0
	go.uber.org/zap v1.21.0
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/pkg/errors v0.9.1 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
)
