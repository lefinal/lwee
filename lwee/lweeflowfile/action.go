package lweeflowfile

import (
	"github.com/lefinal/lwee/lwee/lweefile"
	"github.com/lefinal/meh"
)

type Action struct {
	Description string             `json:"description"`
	Inputs      ActionInputs       `json:"in"`
	Runner      ActionRunnerHolder `json:"run"`
	Outputs     ActionOutputs      `json:"out"`
}

type ActionInputType string

const (
	ActionInputTypeContainerWorkspaceFile ActionInputType = "container-workspace-file"
	ActionInputTypeFile                   ActionInputType = "file"
	ActionInputTypeStdin                  ActionInputType = "stdin"
	ActionInputTypeStream                 ActionInputType = "stream"
)

type ActionInput interface {
	SourceName() string
	Type() string
}

type ActionInputs map[string]ActionInput

func actionInputConstructor[T ActionInput](t T) ActionInput {
	return t
}

func (in *ActionInputs) UnmarshalJSON(data []byte) error {
	var err error
	*in, err = lweefile.ParseMapBasedOnType[ActionInputType, ActionInput](data, map[ActionInputType]lweefile.Unmarshaller[ActionInput]{
		ActionInputTypeContainerWorkspaceFile: lweefile.UnmarshallerFn[ActionInputContainerWorkspaceFile](actionInputConstructor[ActionInputContainerWorkspaceFile]),
		ActionInputTypeFile:                   lweefile.UnmarshallerFn[ActionInputFile](actionInputConstructor[ActionInputFile]),
		ActionInputTypeStdin:                  lweefile.UnmarshallerFn[ActionInputStdin](actionInputConstructor[ActionInputStdin]),
		ActionInputTypeStream:                 lweefile.UnmarshallerFn[ActionInputStream](actionInputConstructor[ActionInputStream]),
	}, "provideAs")
	if err != nil {
		return meh.Wrap(err, "parse action input map based on type", nil)
	}
	return nil
}

type ActionInputBase struct {
	Source string `json:"source"`
}

func (base ActionInputBase) SourceName() string {
	return base.Source
}

type ActionInputContainerWorkspaceFile struct {
	ActionInputBase
	Filename string `json:"filename"`
}

func (input ActionInputContainerWorkspaceFile) Type() string {
	return string(ActionInputTypeContainerWorkspaceFile)
}

type ActionInputFile struct {
	ActionInputBase
	Filename string `json:"filename"`
}

func (input ActionInputFile) Type() string {
	return string(ActionInputTypeFile)
}

type ActionInputStdin struct {
	ActionInputBase
}

func (input ActionInputStdin) Type() string {
	return string(ActionInputTypeStdin)
}

type ActionInputStream struct {
	ActionInputBase
	StreamName string `json:"streamName"`
}

func (input ActionInputStream) Type() string {
	return string(ActionInputTypeStream)
}

type ActionRunnerType string

const (
	ActionRunnerTypeCommand       ActionRunnerType = "command"
	ActionRunnerTypeImage         ActionRunnerType = "image"
	ActionRunnerTypeProjectAction ActionRunnerType = "proj-action"
)

type ActionRunner interface {
	Type() string
}

func actionRunnerConstructor[T ActionRunner](t T) ActionRunner {
	return t
}

type ActionRunnerHolder struct {
	Runner ActionRunner
}

func (runner *ActionRunnerHolder) UnmarshalJSON(data []byte) error {
	var err error
	runner.Runner, err = lweefile.ParseBasedOnType[ActionRunnerType, ActionRunner](data, map[ActionRunnerType]lweefile.Unmarshaller[ActionRunner]{
		ActionRunnerTypeCommand:       lweefile.UnmarshallerFn[ActionRunnerCommand](actionRunnerConstructor[ActionRunnerCommand]),
		ActionRunnerTypeImage:         lweefile.UnmarshallerFn[ActionRunnerImage](actionRunnerConstructor[ActionRunnerImage]),
		ActionRunnerTypeProjectAction: lweefile.UnmarshallerFn[ActionRunnerProjectAction](actionRunnerConstructor[ActionRunnerProjectAction]),
	}, "type")
	if err != nil {
		return meh.Wrap(err, "parse action runner based on type", nil)
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

func (runner ActionRunnerCommand) Type() string {
	return string(ActionRunnerTypeCommand)
}

type ActionRunnerImage struct {
	ActionRunnerBase
	Image   string `json:"image"`
	Command string `json:"command"`
}

func (runner ActionRunnerImage) Type() string {
	return string(ActionRunnerTypeImage)
}

type ActionRunnerProjectAction struct {
	ActionRunnerBase
	Name   string `json:"name"`
	Config string `json:"config"`
	Args   string `json:"args"`
}

func (runner ActionRunnerProjectAction) Type() string {
	return string(ActionRunnerTypeProjectAction)
}

type ActionOutputType string

const (
	ActionOutputTypeContainerWorkspaceFile ActionOutputType = "container-workspace-file"
	ActionOutputTypeStdout                 ActionOutputType = "stdout"
	ActionOutputTypeStream                 ActionOutputType = "stream"
)

type ActionOutput interface {
	Type() string
}

func actionOutputConstructor[T ActionOutput](t T) ActionOutput {
	return t
}

type ActionOutputs map[string]ActionOutput

func (out *ActionOutputs) UnmarshalJSON(data []byte) error {
	var err error
	*out, err = lweefile.ParseMapBasedOnType[ActionOutputType, ActionOutput](data, map[ActionOutputType]lweefile.Unmarshaller[ActionOutput]{
		ActionOutputTypeContainerWorkspaceFile: lweefile.UnmarshallerFn[ActionOutputContainerWorkspaceFile](actionOutputConstructor[ActionOutputContainerWorkspaceFile]),
		ActionOutputTypeStdout:                 lweefile.UnmarshallerFn[ActionOutputStdout](actionOutputConstructor[ActionOutputStdout]),
		ActionOutputTypeStream:                 lweefile.UnmarshallerFn[ActionOutputStream](actionOutputConstructor[ActionOutputStream]),
	}, "providedAs")
	if err != nil {
		return meh.Wrap(err, "parse action output based on type", nil)
	}
	return nil
}

type ActionOutputBase struct{}

type ActionOutputContainerWorkspaceFile struct {
	ActionOutputBase
	Filename string `json:"filename"`
}

func (output ActionOutputContainerWorkspaceFile) Type() string {
	return string(ActionOutputTypeContainerWorkspaceFile)
}

type ActionOutputStdout struct {
	ActionOutputBase
}

func (output ActionOutputStdout) Type() string {
	return string(ActionOutputTypeStdout)
}

type ActionOutputStream struct {
	ActionOutputBase
	StreamName string `json:"streamName"`
}

func (output ActionOutputStream) Type() string {
	return string(ActionOutputTypeStream)
}
