package lweeflowfile

import (
	"github.com/lefinal/lwee/lwee/fileparse"
	"github.com/lefinal/lwee/lwee/templaterender"
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
	ActionInputTypeWorkspaceFile ActionInputType = "workspace-file"
	ActionInputTypeFile          ActionInputType = "file"
	ActionInputTypeStdin         ActionInputType = "stdin"
	ActionInputTypeStream        ActionInputType = "stream"
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
	*in, err = fileparse.ParseMapBasedOnType[ActionInputType, ActionInput](data, map[ActionInputType]fileparse.Unmarshaller[ActionInput]{
		ActionInputTypeWorkspaceFile: fileparse.UnmarshallerFn[ActionInputWorkspaceFile](actionInputConstructor[ActionInputWorkspaceFile]),
		ActionInputTypeFile:          fileparse.UnmarshallerFn[ActionInputFile](actionInputConstructor[ActionInputFile]),
		ActionInputTypeStdin:         fileparse.UnmarshallerFn[ActionInputStdin](actionInputConstructor[ActionInputStdin]),
		ActionInputTypeStream:        fileparse.UnmarshallerFn[ActionInputStream](actionInputConstructor[ActionInputStream]),
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

type ActionInputWorkspaceFile struct {
	ActionInputBase
	Filename string `json:"filename"`
}

func (input ActionInputWorkspaceFile) Type() string {
	return string(ActionInputTypeWorkspaceFile)
}

type ActionInputFile struct {
	ActionInputBase
	Filename string `json:"filename"`
}

func (input ActionInputFile) Type() string {
	return string(ActionInputTypeFile)
}

func (input ActionInputFile) Render(renderer *templaterender.Renderer) error {
	err := renderer.RenderString(&input.Filename)
	if err != nil {
		return meh.Wrap(err, "render filename", nil)
	}
	return nil
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
	Render(renderer *templaterender.Renderer) error
}

func actionRunnerConstructor[T ActionRunner](t T) ActionRunner {
	return t
}

type ActionRunnerHolder struct {
	Runner ActionRunner
}

func (runner *ActionRunnerHolder) UnmarshalJSON(data []byte) error {
	var err error
	runner.Runner, err = fileparse.ParseBasedOnType[ActionRunnerType, ActionRunner](data, map[ActionRunnerType]fileparse.Unmarshaller[ActionRunner]{
		ActionRunnerTypeCommand:       fileparse.UnmarshallerFn[ActionRunnerCommand](actionRunnerConstructor[ActionRunnerCommand]),
		ActionRunnerTypeImage:         fileparse.UnmarshallerFn[ActionRunnerImage](actionRunnerConstructor[ActionRunnerImage]),
		ActionRunnerTypeProjectAction: fileparse.UnmarshallerFn[ActionRunnerProjectAction](actionRunnerConstructor[ActionRunnerProjectAction]),
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
	Command    []string                                `json:"command"`
	Assertions map[string]ActionRunnerCommandAssertion `json:"assert"`
}

type ActionRunnerCommandAssertion struct {
	// Run is the command to run that returns the assertion value that will be
	// checked using the method described in Should.
	Run []string `json:"run"`
	// Should is the comparison-method to use.
	Should string `json:"should"`
	// Target is used for equality comparison.
	Target string `json:"target"`
}

func (runner ActionRunnerCommand) Type() string {
	return string(ActionRunnerTypeCommand)
}

func (runner ActionRunnerCommand) Render(renderer *templaterender.Renderer) error {
	err := renderer.RenderStrings(runner.Command)
	if err != nil {
		return meh.Wrap(err, "render command", nil)
	}
	return nil
}

type ActionRunnerImage struct {
	ActionRunnerBase
	Image   string   `json:"image"`
	Command []string `json:"command"`
}

func (runner ActionRunnerImage) Type() string {
	return string(ActionRunnerTypeImage)
}

func (runner ActionRunnerImage) Render(renderer *templaterender.Renderer) error {
	err := renderer.RenderString(&runner.Image)
	if err != nil {
		return meh.Wrap(err, "render image", nil)
	}
	err = renderer.RenderStrings(runner.Command)
	if err != nil {
		return meh.Wrap(err, "render command", nil)
	}
	return nil
}

type ActionRunnerProjectAction struct {
	ActionRunnerBase
	Name    string   `json:"name"`
	Config  string   `json:"config"`
	Command []string `json:"command"`
}

func (runner ActionRunnerProjectAction) Type() string {
	return string(ActionRunnerTypeProjectAction)
}

func (runner ActionRunnerProjectAction) Render(renderer *templaterender.Renderer) error {
	err := renderer.RenderString(&runner.Config)
	if err != nil {
		return meh.Wrap(err, "render config name", nil)
	}
	err = renderer.RenderStrings(runner.Command)
	if err != nil {
		return meh.Wrap(err, "render command", nil)
	}
	return nil
}

type ActionOutputType string

const (
	ActionOutputTypeWorkspaceFile ActionOutputType = "workspace-file"
	ActionOutputTypeStdout        ActionOutputType = "stdout"
	ActionOutputTypeStream        ActionOutputType = "stream"
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
	*out, err = fileparse.ParseMapBasedOnType[ActionOutputType, ActionOutput](data, map[ActionOutputType]fileparse.Unmarshaller[ActionOutput]{
		ActionOutputTypeWorkspaceFile: fileparse.UnmarshallerFn[ActionOutputWorkspaceFile](actionOutputConstructor[ActionOutputWorkspaceFile]),
		ActionOutputTypeStdout:        fileparse.UnmarshallerFn[ActionOutputStdout](actionOutputConstructor[ActionOutputStdout]),
		ActionOutputTypeStream:        fileparse.UnmarshallerFn[ActionOutputStream](actionOutputConstructor[ActionOutputStream]),
	}, "providedAs")
	if err != nil {
		return meh.Wrap(err, "parse action output based on type", nil)
	}
	return nil
}

type ActionOutputBase struct {
}

type ActionOutputWorkspaceFile struct {
	ActionOutputBase
	Filename string `json:"filename"`
}

func (output ActionOutputWorkspaceFile) Type() string {
	return string(ActionOutputTypeWorkspaceFile)
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
