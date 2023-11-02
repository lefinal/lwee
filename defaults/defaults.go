package defaults

import _ "embed"

var (
	//go:embed action.yaml
	Action []byte
	//go:embed flow.yaml
	FlowFile []byte
)
