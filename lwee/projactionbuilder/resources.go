package projactionbuilder

import (
	"embed"
	_ "embed"
)

var (
	//go:embed resources/action.yaml
	actionYAML []byte
	//go:embed resources
	templateResources embed.FS
)
