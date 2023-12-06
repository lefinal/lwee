package lweeflowfile

import (
	"github.com/lefinal/lwee/lwee/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type validatable interface {
	Validate(path *validate.Path) *validate.Report
}

type validatablePrivate interface {
	validate(path *validate.Path) *validate.Report
}

type validatablePrivateToExported struct {
	v validatablePrivate
}

func (v validatablePrivateToExported) Validate(path *validate.Path) *validate.Report {
	return v.v.validate(path)
}

func newValidatable(valToCast any) (validatable, bool) {
	if val, ok := valToCast.(validatable); ok {
		return val, true
	}
	if val, ok := valToCast.(validatablePrivate); ok {
		return validatablePrivateToExported{v: val}, true
	}
	return nil, false
}

type validateTest[T any] struct {
	name     string
	valid    bool
	warnings bool
	mutate   func(val *T)
}

func (tt validateTest[T]) run(t *testing.T, constructor func() T) {
	val := constructor()
	tt.mutate(&val)
	validatable, ok := newValidatable(val)
	require.Truef(t, ok, "value should be validatable but was: %T", val)
	report := validatable.Validate(field.NewPath(""))
	if tt.valid {
		assert.Empty(t, report.Errors, "should not report errors")
		if tt.warnings {
			assert.NotEmpty(t, report.Warnings, "should report warnings")
		} else {
			assert.Empty(t, report.Warnings, "should not report warnings")
		}
	} else {
		assert.NotEmpty(t, report.Errors, "should report errors")
	}
}

func validFlow() Flow {
	return Flow{
		Name:        "My Simple Flow",
		Description: "My description.",
		Inputs: map[string]FlowInput{
			"hello": FlowInputFile{
				Filename: "src/hello.txt",
			},
		},
		Actions: map[string]Action{
			"fileInfo": {
				Description: "My action.",
				Inputs: map[string]ActionInput{
					"entries": ActionInputWorkspaceFile{
						ActionInputBase: ActionInputBase{
							Source: "flow.in.hello",
						},
						Filename: "weather-data.txt",
					},
				},
				Runner: ActionRunnerHolder{
					Runner: ActionRunnerImage{
						Image: "ubuntu:latest",
						Command: []string{"echo",
							"Host container workspace dir: {{ .Action.Extras.WorkspaceHostDir }}\nContainer workspace mount dir: {{ .Action.Extras.WorkspaceMountDir }}"},
					},
				},
				Outputs: map[string]ActionOutput{
					"echo": ActionOutputStdout{},
				},
			},

			"copyFromStdin": {
				Inputs: map[string]ActionInput{
					"workspaceInfo": ActionInputStdin{
						ActionInputBase{
							Source: "action.getContainerWorkspace.out.echo",
						},
					},
				},
				Runner: ActionRunnerHolder{
					Runner: ActionRunnerCommand{
						ActionRunnerBase: ActionRunnerBase{},
						Command: []string{
							"cp",
							"/dev/stdin",
							"workspace-info-copy.txt",
						},
						Assertions: nil,
						RunInfo:    nil,
					},
				},
				Outputs: map[string]ActionOutput{
					"copy": ActionOutputWorkspaceFile{
						Filename: "workspace-info-copy.txt",
					},
				},
			},

			"randomAction": {
				Runner: ActionRunnerHolder{
					Runner: ActionRunnerCommand{
						Command: []string{
							"python3",
							"--version",
						},
						Assertions: map[string]ActionRunnerCommandAssertion{
							"versionMatches": {
								Run: []string{
									"python3",
									"--version",
								},
								Should: "equal", // TODO
								Target: "Python 3.10.12",
							},
						},
						RunInfo: map[string]ActionRunnerCommandRunInfo{
							"pythonVersion": {
								Run: []string{
									"python3",
									"--version",
								},
							},
						},
					},
				},
			},
		},
		Outputs: map[string]FlowOutput{
			"entryCount": FlowOutputFile{
				FlowOutputBase: FlowOutputBase{
					Source: "action.fileInfo.out.fileInfo",
				},
				Filename: "out/file-info.txt",
			},
			"containerWorkspaceDir": FlowOutputFile{
				FlowOutputBase: FlowOutputBase{
					Source: "action.getContainerWorkspace.out.echo",
				},
				Filename: "out/container-workspace-dir.txt",
			},
			"containerWorkspaceDirCopy": FlowOutputFile{
				FlowOutputBase: FlowOutputBase{
					Source: "action.copyFromStdin.out.copy",
				},
				Filename: "out/container-workspace-dir-copy.txt",
			},
		},
	}
}

func TestFlowInputFile_Validate(t *testing.T) {
	t.Parallel()

	// Create temporary file.
	filename := filepath.Join(t.TempDir(), "my.txt")
	err := os.WriteFile(filename, []byte("Hello World!"), 0600)
	require.NoError(t, err, "create temporary file should not fail")
	// We prepend a few times "../" to avoid the warning for absolute paths.
	filenameNonAbsolute := filepath.Join(strings.Repeat("../", 512), filename)

	tests := []validateTest[FlowInputFile]{
		{
			name:     "ok",
			valid:    true,
			warnings: false,
			mutate:   func(val *FlowInputFile) {},
		},

		{
			name:     "filename ok",
			valid:    true,
			warnings: false,
			mutate: func(val *FlowInputFile) {
				val.Filename = filenameNonAbsolute
			},
		},
		{
			name:  "filename not set",
			valid: false,
			mutate: func(val *FlowInputFile) {
				val.Filename = ""
			},
		},
		{
			name:     "filename absolute",
			valid:    true,
			warnings: true,
			mutate: func(val *FlowInputFile) {
				val.Filename = filename
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() FlowInputFile {
				return FlowInputFile{
					Filename: "my/file.txt",
				}
			})
		})
	}
}

func TestFlowOutputBase_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[FlowOutputBase]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *FlowOutputBase) {},
		},

		{
			name:  "source ok",
			valid: true,
			mutate: func(val *FlowOutputBase) {
				val.Source = "action.abc.out.xyz"
			},
		},
		{
			name:  "source not set",
			valid: false,
			mutate: func(val *FlowOutputBase) {
				val.Source = ""
			},
		},
		{
			name:     "source with spaces",
			valid:    true,
			warnings: true,
			mutate: func(val *FlowOutputBase) {
				val.Source = "action.a b c.out.xyz"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() FlowOutputBase {
				return FlowOutputBase{
					Source: "action.abc.out.xyz",
				}
			})
		})
	}
}

func TestFlowOutputFile_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[FlowOutputFile]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *FlowOutputFile) {},
		},

		{
			name:  "base not ok",
			valid: false,
			mutate: func(val *FlowOutputFile) {
				val.Source = ""
			},
		},

		{
			name:  "filename ok",
			valid: true,
			mutate: func(val *FlowOutputFile) {
				val.Filename = "hello/world.txt"
			},
		},
		{
			name:  "filename not set",
			valid: false,
			mutate: func(val *FlowOutputFile) {
				val.Filename = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() FlowOutputFile {
				return FlowOutputFile{
					FlowOutputBase: FlowOutputBase{
						Source: "flow.in.what",
					},
					Filename: "hello/world.txt",
				}
			})
		})
	}
}
