package lweeflowfile

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/commandassert"
	"github.com/lefinal/lwee/lwee/fileparse"
	"github.com/lefinal/lwee/lwee/templaterender"
	"github.com/lefinal/lwee/lwee/validate"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"os/exec"
	"strings"
)

type Action struct {
	Description string             `json:"description"`
	Inputs      ActionInputs       `json:"in"`
	Runner      ActionRunnerHolder `json:"run"`
	Outputs     ActionOutputs      `json:"out"`
}

func (action Action) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	for inputName, input := range action.Inputs {
		reporter.AddReport(input.Validate(path.Child("in").Key(inputName)))
	}
	reporter.AddReport(action.Runner.Runner.Validate(path.Child("run")))
	for outputName, output := range action.Outputs {
		reporter.AddReport(output.Validate(path.Child("out").Key(outputName)))
	}
	return reporter.Report()
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
	Validate(path *validate.Path) *validate.Report
}

type ActionInputs map[string]ActionInput

func actionInputConstructor[T ActionInput](t T) ActionInput {
	return t
}

func (in *ActionInputs) UnmarshalJSON(data []byte) error {
	var err error
	*in, err = fileparse.ParseMapBasedOnType[ActionInputType, ActionInput](data, map[ActionInputType]fileparse.Unmarshaller[ActionInput]{
		ActionInputTypeWorkspaceFile: fileparse.UnmarshallerFn[ActionInputWorkspaceFile](actionInputConstructor[ActionInputWorkspaceFile]),
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

func (base ActionInputBase) validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.NextField(path.Child("source"), base.Source)
	validate.ForReporter(reporter, base.Source,
		validate.AssertNotEmpty[string]())
	if strings.Contains(base.Source, " ") {
		reporter.Warn("spaces in source may lead to confusion in log output. consider renaming your source entities.")
	}

	return reporter.Report()
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

func (input ActionInputWorkspaceFile) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(input.ActionInputBase.validate(path))
	validate.ForField(reporter, path.Child("filename"), input.Filename,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

type ActionInputStdin struct {
	ActionInputBase
}

func (input ActionInputStdin) Type() string {
	return string(ActionInputTypeStdin)
}

func (input ActionInputStdin) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(input.ActionInputBase.validate(path))
	return reporter.Report()
}

type ActionInputStream struct {
	ActionInputBase
	StreamName string `json:"streamName"`
}

func (input ActionInputStream) Type() string {
	return string(ActionInputTypeStream)
}

func (input ActionInputStream) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(input.ActionInputBase.validate(path))
	validate.ForField(reporter, path.Child("streamName"), input.StreamName,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
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
	Validate(path *validate.Path) *validate.Report
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

func (base ActionRunnerBase) validate(_ *validate.Path) *validate.Report {
	return validate.NewReport()
}

type ActionRunnerCommand struct {
	ActionRunnerBase
	Command    []string                                `json:"command"`
	Assertions map[string]ActionRunnerCommandAssertion `json:"assert"`
	RunInfo    map[string]ActionRunnerCommandRunInfo   `json:"log"`
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

func (runner ActionRunnerCommand) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(runner.ActionRunnerBase.validate(path))

	reporter.NextField(path.Child("command"), runner.Command)
	if len(runner.Command) == 0 || runner.Command[0] == "" {
		reporter.Error("required")
	} else {
		// Check whether the command exists.
		_, err := exec.LookPath(runner.Command[0])
		if err != nil {
			reporter.Warn(fmt.Sprintf("look up command %q: %s", runner.Command[0], err))
		}
	}

	for assertionName, assertion := range runner.Assertions {
		reporter.AddReport(assertion.Validate(path.Child("assert").Key(assertionName)))
	}

	for infoName, info := range runner.RunInfo {
		reporter.AddReport(info.Validate(path.Child("log").Key(infoName)))
	}

	return reporter.Report()
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

func (assertion ActionRunnerCommandAssertion) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.NextField(path.Child("run"), assertion.Run)
	if len(assertion.Run) == 0 || assertion.Run[0] == "" {
		reporter.Error("required")
	} else {
		// Run the assertion.
		assertion, err := commandassert.New(commandassert.Options{
			Logger: zap.NewNop(),
			Run:    assertion.Run,
			Should: commandassert.ShouldType(assertion.Should),
			Target: assertion.Target,
		})
		if err != nil {
			reporter.Warn(err.Error())
		} else if err = assertion.Assert(context.Background()); err != nil {
			reporter.Warn(err.Error())
		}
	}
	return reporter.Report()
}

type ActionRunnerCommandRunInfo struct {
	// Run is the command to run. The stdout and stderr result will be logged for the
	// run.
	Run []string `json:"run"`
}

func (runInfo ActionRunnerCommandRunInfo) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.NextField(path.Child("run"), runInfo.Run)
	if len(runInfo.Run) == 0 || runInfo.Run[0] == "" {
		reporter.Error("required")
	} else {
		// Check whether the command exists.
		_, err := exec.LookPath(runInfo.Run[0])
		if err != nil {
			reporter.Warn(fmt.Sprintf("look up command %q: %s", runInfo.Run[0], err))
		}
	}
	return reporter.Report()
}

type ActionRunnerImage struct {
	ActionRunnerBase
	Image   string            `json:"image"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
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
	err = renderer.RenderStringStringMap(runner.Env)
	if err != nil {
		return meh.Wrap(err, "render env", nil)
	}
	return nil
}

func (runner ActionRunnerImage) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(runner.ActionRunnerBase.validate(path))
	validate.ForField(reporter, path.Child("image"), runner.Image,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

type ActionRunnerProjectAction struct {
	ActionRunnerBase
	Name    string            `json:"name"`
	Config  string            `json:"config"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
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
	err = renderer.RenderStringStringMap(runner.Env)
	if err != nil {
		return meh.Wrap(err, "render env", nil)
	}
	return nil
}

func (runner ActionRunnerProjectAction) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(runner.ActionRunnerBase.validate(path))
	validate.ForField(reporter, path.Child("name"), runner.Name,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

type ActionOutputType string

const (
	ActionOutputTypeWorkspaceFile ActionOutputType = "workspace-file"
	ActionOutputTypeStdout        ActionOutputType = "stdout"
	ActionOutputTypeStream        ActionOutputType = "stream"
)

type ActionOutput interface {
	Type() string
	Validate(path *validate.Path) *validate.Report
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

func (output ActionOutputWorkspaceFile) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	validate.ForField(reporter, path.Child("filename"), output.Filename,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

type ActionOutputStdout struct {
	ActionOutputBase
}

func (output ActionOutputStdout) Type() string {
	return string(ActionOutputTypeStdout)
}

func (output ActionOutputStdout) Validate(_ *validate.Path) *validate.Report {
	return validate.NewReporter().Report()
}

type ActionOutputStream struct {
	ActionOutputBase
	StreamName string `json:"streamName"`
}

func (output ActionOutputStream) Type() string {
	return string(ActionOutputTypeStream)
}

func (output ActionOutputStream) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	validate.ForField(reporter, path.Child("streamName"), output.StreamName,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}
