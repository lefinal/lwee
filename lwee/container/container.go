package container

type EngineType string

const (
	EngineTypeDocker EngineType = "docker"
	EngineTypePodman EngineType = "podman"
)

func SetEngine(newEngineType EngineType) error {
	switch newEngineType {
	case EngineTypeDocker:
	// TODO: Setup and test engine.
	case EngineTypePodman:
		// TODO: Setup and test engine.
	}
	return nil
}
