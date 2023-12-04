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

// Action represents an action with description, inputs, runner, and outputs.
type Action struct {
	Description string             `json:"description"`
	Inputs      ActionInputs       `json:"in"`
	Runner      ActionRunnerHolder `json:"run"`
	Outputs     ActionOutputs      `json:"out"`
}

// Validate validates an Action and returns a Report.
//
// It validates each input, runner, and output of the Action. The validation is
// performed by adding the validation reports for each input, runner, and output
// to the reporter. Finally, the function returns the generated validate.Report.
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

// ActionInputType represents the type of action input.
type ActionInputType string

const (
	// ActionInputTypeWorkspaceFile for ActionInputWorkspaceFile.
	ActionInputTypeWorkspaceFile ActionInputType = "workspace-file"
	// ActionInputTypeStdin for ActionInputStdin.
	ActionInputTypeStdin ActionInputType = "stdin"
	// ActionInputTypeStream for ActionInputStream.
	ActionInputTypeStream ActionInputType = "stream"
)

// ActionInput describes an input request for an action.
type ActionInput interface {
	SourceName() string
	Type() string
	Validate(path *validate.Path) *validate.Report
}

// ActionInputs uses custom JSON unmarshalling logic for a map with ActionInput.
type ActionInputs map[string]ActionInput

func actionInputConstructor[T ActionInput](t T) ActionInput {
	return t
}

// UnmarshalJSON parses JSON data into an ActionInputs instance. It uses the
// provided type mapping and type field name to parse the input data into the
// appropriate ActionInput based on the type field value.
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

// ActionInputBase holds common fields as base for ActionInput.
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

// SourceName returns the name of the specified data source.
func (base ActionInputBase) SourceName() string {
	return base.Source
}

// ActionInputWorkspaceFile is used for input requests in actions that are
// provided as workspace file.
type ActionInputWorkspaceFile struct {
	ActionInputBase
	Filename string `json:"filename"`
}

// Type of ActionInputWorkspaceFile
func (input ActionInputWorkspaceFile) Type() string {
	return string(ActionInputTypeWorkspaceFile)
}

// Validate the options.
func (input ActionInputWorkspaceFile) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(input.ActionInputBase.validate(path))
	validate.ForField(reporter, path.Child("filename"), input.Filename,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

// ActionInputStdin is used for input requests in actions that are provided via
// stdin.
type ActionInputStdin struct {
	ActionInputBase
}

// Type of ActionInputStdin.
func (input ActionInputStdin) Type() string {
	return string(ActionInputTypeStdin)
}

// Validate the options.
func (input ActionInputStdin) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(input.ActionInputBase.validate(path))
	return reporter.Report()
}

// ActionInputStream is used for input requests in actions that are provided as
// stream.
type ActionInputStream struct {
	ActionInputBase
	StreamName string `json:"streamName"`
}

// Type of ActionInputStream.
func (input ActionInputStream) Type() string {
	return string(ActionInputTypeStream)
}

// Validate the options.
func (input ActionInputStream) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(input.ActionInputBase.validate(path))
	validate.ForField(reporter, path.Child("streamName"), input.StreamName,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

// ActionRunnerType describes the type of ActionRunner.
type ActionRunnerType string

const (
	// ActionRunnerTypeCommand for ActionRunnerCommand.
	ActionRunnerTypeCommand ActionRunnerType = "command"
	// ActionRunnerTypeImage for ActionRunnerImage.
	ActionRunnerTypeImage ActionRunnerType = "image"
	// ActionRunnerTypeProjectAction for ActionRunnerProjectAction.
	ActionRunnerTypeProjectAction ActionRunnerType = "proj-action"
)

// ActionRunner describes how an operation to run on data.
type ActionRunner interface {
	Type() string
	// Render option values with the given templaterender.Renderer.
	Render(renderer *templaterender.Renderer) error
	Validate(path *validate.Path) *validate.Report
}

func actionRunnerConstructor[T ActionRunner](t T) ActionRunner {
	return t
}

// ActionRunnerHolder holds an ActionRunner and provides custom JSON
// unmarshalling functionality based on the runner type with UnmarshalJSON.
type ActionRunnerHolder struct {
	Runner ActionRunner
}

// UnmarshalJSON unmarshals the given JSON data to Runner based on the type.
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

// ActionRunnerBase holds common fields for ActionRunner.
type ActionRunnerBase struct {
}

func (base ActionRunnerBase) validate(_ *validate.Path) *validate.Report {
	return validate.NewReport()
}

// ActionRunnerCommand describes an action operation that runs a locally
// available application.
type ActionRunnerCommand struct {
	ActionRunnerBase
	// Command to run.
	Command []string `json:"command"`
	// Assertions to check before running the command.
	Assertions map[string]ActionRunnerCommandAssertion `json:"assert"`
	// RunInfo to log.
	RunInfo map[string]ActionRunnerCommandRunInfo `json:"log"`
}

// Type of ActionRunnerCommand.
func (runner ActionRunnerCommand) Type() string {
	return string(ActionRunnerTypeCommand)
}

// Render the option values using the given templaterender.Renderer.
func (runner ActionRunnerCommand) Render(renderer *templaterender.Renderer) error {
	err := renderer.RenderStrings(runner.Command)
	if err != nil {
		return meh.Wrap(err, "render command", nil)
	}
	return nil
}

// Validate the options.
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

// ActionRunnerCommandAssertion describes an assertion in ActionRunnerCommand.
// This is used, for example, to check an application's version, conditions, etc.
type ActionRunnerCommandAssertion struct {
	// Run is the command to run that returns the assertion value that will be
	// checked using the method described in Should.
	Run []string `json:"run"`
	// Should is the comparison-method to use.
	Should string `json:"should"`
	// Target is used for equality comparison.
	Target string `json:"target"`
}

// Validate the options.
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

// ActionRunnerCommandRunInfo describes a snippet of information to log for an
// ActionRunnerCommand-operation.
type ActionRunnerCommandRunInfo struct {
	// Run is the command to run. The stdout and stderr result will be logged for the
	// run.
	Run []string `json:"run"`
}

// Validate the options.
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

// ActionRunnerImage describes an operation for an action that runs an external
// container image.
type ActionRunnerImage struct {
	ActionRunnerBase
	// Image to run.
	Image string `json:"image"`
	// Command overwrite.
	Command []string `json:"command"`
	// Env holds environment variables to set for the container.
	Env map[string]string `json:"env"`
}

// Type of ActionRunnerImage.
func (runner ActionRunnerImage) Type() string {
	return string(ActionRunnerTypeImage)
}

// Render option values using the given templaterender.Renderer.
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

// Validate the options.
func (runner ActionRunnerImage) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(runner.ActionRunnerBase.validate(path))
	validate.ForField(reporter, path.Child("image"), runner.Image,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

// ActionRunnerProjectAction describes an operation for an action that runs a
// project action.
type ActionRunnerProjectAction struct {
	ActionRunnerBase
	// Name of the project action. Matches the directory name where the project
	// action is located within the actions-directory.
	Name string `json:"name"`
	// Config to use.
	Config string `json:"config"`
	// Command overwrite.
	Command []string `json:"command"`
	// Env holds environment variables to set for the container.
	Env map[string]string `json:"env"`
}

// Type of ActionRunnerProjectAction.
func (runner ActionRunnerProjectAction) Type() string {
	return string(ActionRunnerTypeProjectAction)
}

// Render option values using the given templaterender.Renderer.
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

// Validate the options.
func (runner ActionRunnerProjectAction) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	reporter.AddReport(runner.ActionRunnerBase.validate(path))
	validate.ForField(reporter, path.Child("name"), runner.Name,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

// ActionOutputType describes an action's provided output type.
type ActionOutputType string

const (
	// ActionOutputTypeWorkspaceFile for ActionOutputWorkspaceFile.
	ActionOutputTypeWorkspaceFile ActionOutputType = "workspace-file"
	// ActionOutputTypeStdout for ActionOutputStdout.
	ActionOutputTypeStdout ActionOutputType = "stdout"
	// ActionOutputTypeStream for ActionOutputStream.
	ActionOutputTypeStream ActionOutputType = "stream"
)

// ActionOutput describes an action's provided output.
type ActionOutput interface {
	Type() string
	Validate(path *validate.Path) *validate.Report
}

func actionOutputConstructor[T ActionOutput](t T) ActionOutput {
	return t
}

// ActionOutputs is a map for ActionOutput with custom JSON unmarshalling logic
// based on the output's type.
type ActionOutputs map[string]ActionOutput

// UnmarshalJSON unmarshals the JSON representation of ActionOutputs. It uses the
// provided type mapping to determine the appropriate concrete type for each
// ActionOutput. The type is determined based on the value of the provided field
// name in the JSON.
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

// ActionOutputBase holds common fields for ActionOutput.
type ActionOutputBase struct {
}

// ActionOutputWorkspaceFile is used for providing outputs in actions from a
// workspace file.
type ActionOutputWorkspaceFile struct {
	ActionOutputBase
	Filename string `json:"filename"`
}

// Type of ActionOutputWorkspaceFile.
func (output ActionOutputWorkspaceFile) Type() string {
	return string(ActionOutputTypeWorkspaceFile)
}

// Validate the options.
func (output ActionOutputWorkspaceFile) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	validate.ForField(reporter, path.Child("filename"), output.Filename,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}

// ActionOutputStdout is used for providing outputs in actions from stdout.
type ActionOutputStdout struct {
	ActionOutputBase
}

// Type of ActionOutputStdout.
func (output ActionOutputStdout) Type() string {
	return string(ActionOutputTypeStdout)
}

// Validate the options.
func (output ActionOutputStdout) Validate(_ *validate.Path) *validate.Report {
	return validate.NewReporter().Report()
}

// ActionOutputStream is used for providing outputs in actions from a stream.
type ActionOutputStream struct {
	ActionOutputBase
	StreamName string `json:"streamName"`
}

// Type of ActionOutputStream.
func (output ActionOutputStream) Type() string {
	return string(ActionOutputTypeStream)
}

// Validate the options.
func (output ActionOutputStream) Validate(path *validate.Path) *validate.Report {
	reporter := validate.NewReporter()
	validate.ForField(reporter, path.Child("streamName"), output.StreamName,
		validate.AssertNotEmpty[string]())
	return reporter.Report()
}
