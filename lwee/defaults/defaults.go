// Package defaults holds embedded defaults.
package defaults

import _ "embed"

var (
	// FlowFile is the default flow file for newly initialized projects.
	//
	//go:embed flow.yaml
	FlowFile []byte
)
