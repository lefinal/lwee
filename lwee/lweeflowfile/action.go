package lweeflowfile

import (
	"github.com/lefinal/lwee/lwee/lweefile"
	"github.com/lefinal/meh"
)

type Action struct {
	Description string        `json:"description"`
	Inputs      ActionInputs  `json:"in"`
	Runner      ActionRunner  `json:"run"`
	Outputs     ActionOutputs `json:"out"`
}

type ActionInputType string

const (
	ActionInputTypeContainerWorkspaceFile ActionInputType = "container-workspace-file"
	ActionInputTypeFile                   ActionInputType = "file"
	ActionInputTypeStdin                  ActionInputType = "stdin"
	ActionInputTypeStream                 ActionInputType = "stream"
)

type ActionInputs map[string]any

func (in *ActionInputs) UnmarshalJSON(data []byte) error {
	var err error
	*in, err = lweefile.ParseMapBasedOnType(data, map[ActionInputType]any{
		ActionInputTypeContainerWorkspaceFile: ActionInputContainerWorkspaceFile{},
		ActionInputTypeFile:                   ActionInputFile{},
		ActionInputTypeStdin:                  ActionInputStdin{},
		ActionInputTypeStream:                 ActionInputStream{},
	})
	if err != nil {
		return meh.Wrap(err, "parse map based on type", nil)
	}
	return nil
}

type ActionInputBase struct {
	Source string `json:"source"`
}

type ActionInputContainerWorkspaceFile struct {
	ActionInputBase
	Filename string `json:"filename"`
}

type ActionInputFile struct {
	ActionInputBase
}

type ActionInputStdin struct {
	ActionInputBase
}

type ActionInputStream struct {
	ActionInputBase
	StreamName string `json:"streamName"`
}

type ActionRunnerType string

const (
	ActionRunnerTypeCommand       ActionRunnerType = "command"
	ActionRunnerTypeImage         ActionRunnerType = "image"
	ActionRunnerTypeProjectAction ActionRunnerType = "proj-action"
)

type ActionRunner struct {
	Runner any
}

func (runner *ActionRunner) UnmarshalJSON(data []byte) error {
	var err error
	runner.Runner, err = lweefile.ParseBasedOnType(data, map[ActionRunnerType]any{
		ActionRunnerTypeCommand:       ActionRunnerCommand{},
		ActionRunnerTypeImage:         ActionRunnerImage{},
		ActionRunnerTypeProjectAction: ActionRunnerProjectAction{},
	})
	if err != nil {
		return meh.Wrap(err, "parse based on type", nil)
	}
	return nil
}

type ActionRunnerBase struct {
}

type ActionRunnerCommand struct {
	ActionRunnerBase
	Command string `json:"command"`
	Args    string `json:"args"`
	// TODO: Add assert-stuff.
}

type ActionRunnerImage struct {
	ActionRunnerBase
	Image   string `json:"image"`
	Command string `json:"command"`
}

type ActionRunnerProjectAction struct {
	ActionRunnerBase
	ActionName string `json:"actionName"`
	Args       string `json:"args"`
}

type ActionOutputType string

const (
	ActionOutputTypeContainerWorkspaceFile ActionOutputType = "container-workspace-file"
	ActionOutputTypeStdout                 ActionOutputType = "stdout"
	ActionOutputTypeStream                 ActionOutputType = "stream"
)

type ActionOutputs map[string]any

func (out *ActionOutputs) UnmarshalJSON(data []byte) error {
	var err error
	*out, err = lweefile.ParseMapBasedOnType(data, map[ActionOutputType]any{
		ActionOutputTypeContainerWorkspaceFile: ActionOutputContainerWorkspaceFile{},
		ActionOutputTypeStdout:                 ActionOutputStdout{},
		ActionOutputTypeStream:                 ActionOutputStream{},
	})
	if err != nil {
		return meh.Wrap(err, "parse based on type", nil)
	}
	return nil
}

type ActionOutputBase struct{}

type ActionOutputContainerWorkspaceFile struct {
	ActionOutputBase
	Filename string `json:"filename"`
}

type ActionOutputStdout struct {
	ActionOutputBase
}

type ActionOutputStream struct {
	ActionOutputBase
	StreamName string `json:"streamName"`
}
