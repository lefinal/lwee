package defaults

import _ "embed"

var (
	//go:embed flow.yaml
	FlowFile []byte
)
