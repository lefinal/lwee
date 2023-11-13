package lweeflowfile

import (
	"testing"
)

func TestAction_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[Action]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *Action) {},
		},

		{
			name:  "in invalid",
			valid: false,
			mutate: func(val *Action) {
				val.Inputs["hello"] = ActionInputWorkspaceFile{
					Filename: "", // Missing filename.
				}
			},
		},
		{
			name:  "run invalid",
			valid: false,
			mutate: func(val *Action) {
				val.Runner = ActionRunnerHolder{
					Runner: ActionRunnerImage{
						Image: "", // Missing image.
					},
				}
			},
		},
		{
			name:  "out invalid",
			valid: false,
			mutate: func(val *Action) {
				val.Outputs["hello"] = ActionOutputWorkspaceFile{
					Filename: "", // Missing filename.
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() Action {
				return Action{
					Description: "My description.",
					Inputs: map[string]ActionInput{
						"entries": ActionInputStdin{
							ActionInputBase{
								Source: "flow.in.hello",
							},
						},
					},
					Runner: ActionRunnerHolder{
						Runner: ActionRunnerImage{
							ActionRunnerBase: ActionRunnerBase{},
							Image:            "hello-world:latest",
							Command:          nil,
						},
					},
					Outputs: map[string]ActionOutput{
						"helloWorld": ActionOutputStdout{
							ActionOutputBase{},
						},
					},
				}
			})
		})
	}
}

func TestActionInputBase_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionInputBase]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionInputBase) {},
		},

		{
			name:  "source ok",
			valid: true,
			mutate: func(val *ActionInputBase) {
				val.Source = "my.source"
			},
		},
		{
			name:  "source not set",
			valid: false,
			mutate: func(val *ActionInputBase) {
				val.Source = ""
			},
		},
		{
			name:     "source with spaces",
			valid:    true,
			warnings: true,
			mutate: func(val *ActionInputBase) {
				val.Source = "my.source name"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionInputBase {
				return ActionInputBase{
					Source: "flow.in.x",
				}
			})
		})
	}
}

func TestActionInputWorkspaceFile_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionInputWorkspaceFile]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionInputWorkspaceFile) {},
		},

		{
			name:  "base invalid",
			valid: false,
			mutate: func(val *ActionInputWorkspaceFile) {
				val.Source = ""
			},
		},

		{
			name:  "filename name ok",
			valid: true,
			mutate: func(val *ActionInputWorkspaceFile) {
				val.Filename = "my/file.txt"
			},
		},
		{
			name:  "filename not set",
			valid: false,
			mutate: func(val *ActionInputWorkspaceFile) {
				val.Filename = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionInputWorkspaceFile {
				return ActionInputWorkspaceFile{
					ActionInputBase: ActionInputBase{
						Source: "flow.in.hello",
					},
					Filename: "my/file.txt",
				}
			})
		})
	}
}

func TestActionInputFile_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionInputFile]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionInputFile) {},
		},

		{
			name:  "base invalid",
			valid: false,
			mutate: func(val *ActionInputFile) {
				val.Source = ""
			},
		},

		{
			name:  "filename name ok",
			valid: true,
			mutate: func(val *ActionInputFile) {
				val.Filename = "my/file.txt"
			},
		},
		{
			name:  "filename not set",
			valid: false,
			mutate: func(val *ActionInputFile) {
				val.Filename = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionInputFile {
				return ActionInputFile{
					ActionInputBase: ActionInputBase{
						Source: "flow.in.hello",
					},
					Filename: "my/file.txt",
				}
			})
		})
	}
}

func TestActionInputStdin_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionInputStdin]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionInputStdin) {},
		},

		{
			name:  "base invalid",
			valid: false,
			mutate: func(val *ActionInputStdin) {
				val.Source = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionInputStdin {
				return ActionInputStdin{
					ActionInputBase: ActionInputBase{
						Source: "flow.in.hello",
					},
				}
			})
		})
	}
}

func TestActionInputStream_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionInputStream]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionInputStream) {},
		},

		{
			name:  "base invalid",
			valid: false,
			mutate: func(val *ActionInputStream) {
				val.Source = ""
			},
		},

		{
			name:  "stream name ok",
			valid: true,
			mutate: func(val *ActionInputStream) {
				val.StreamName = "my-stream"
			},
		},
		{
			name:  "stream name not set",
			valid: false,
			mutate: func(val *ActionInputStream) {
				val.StreamName = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionInputStream {
				return ActionInputStream{
					ActionInputBase: ActionInputBase{
						Source: "flow.in.hello",
					},
					StreamName: "my-stream",
				}
			})
		})
	}
}

func TestActionRunnerCommand_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionRunnerCommand]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionRunnerCommand) {},
		},

		{
			name:  "command ok",
			valid: true,
			mutate: func(val *ActionRunnerCommand) {
				val.Command = []string{
					"echo",
					"Hello World!",
				}
			},
		},
		{
			name:  "command not set",
			valid: false,
			mutate: func(val *ActionRunnerCommand) {
				val.Command = nil
			},
		},
		{
			name:  "command not set 2",
			valid: false,
			mutate: func(val *ActionRunnerCommand) {
				val.Command = []string{}
			},
		},
		{
			name:  "command first element empty",
			valid: false,
			mutate: func(val *ActionRunnerCommand) {
				val.Command = []string{""}
			},
		},
		{
			name:     "command not found",
			valid:    true,
			warnings: true,
			mutate: func(val *ActionRunnerCommand) {
				val.Command = []string{
					"my-unknown-program-that-should-not-exist",
				}
			},
		},

		{
			name:  "assertions invalid",
			valid: false,
			mutate: func(val *ActionRunnerCommand) {
				val.Assertions = map[string]ActionRunnerCommandAssertion{
					"hello": {
						Run:    []string{},
						Should: "",
						Target: "",
					},
				}
			},
		},
		{
			name:  "run info invalid",
			valid: false,
			mutate: func(val *ActionRunnerCommand) {
				val.RunInfo = map[string]ActionRunnerCommandRunInfo{
					"hello": {
						Run: nil,
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionRunnerCommand {
				return ActionRunnerCommand{
					Command: []string{
						"echo",
						"Hello World!",
					},
					Assertions: map[string]ActionRunnerCommandAssertion{
						"versionMatches": {
							Run: []string{
								"echo",
								"v1.2.3",
							},
							Should: "equal",
							Target: "v1.2.3",
						},
					},
					RunInfo: map[string]ActionRunnerCommandRunInfo{
						"pythonVersion": {
							Run: []string{
								"echo",
								"my.important.version",
							},
						},
					},
				}
			})
		})
	}
}

func TestActionRunnerCommandAssertion_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionRunnerCommandAssertion]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionRunnerCommandAssertion) {},
		},

		{
			name:  "run not set",
			valid: false,
			mutate: func(val *ActionRunnerCommandAssertion) {
				val.Run = nil
			},
		},
		{
			name:  "run not set 2",
			valid: false,
			mutate: func(val *ActionRunnerCommandAssertion) {
				val.Run = []string{}
			},
		},
		{
			name:  "run first element empty",
			valid: false,
			mutate: func(val *ActionRunnerCommandAssertion) {
				val.Run = []string{""}
			},
		},
		{
			name:     "run not found",
			valid:    true,
			warnings: true,
			mutate: func(val *ActionRunnerCommandAssertion) {
				val.Run = []string{
					"my-unknown-program-that-should-not-exist",
				}
			},
		},

		{
			name:     "assertion fail",
			valid:    true,
			warnings: true,
			mutate: func(val *ActionRunnerCommandAssertion) {
				val.Run = []string{
					"echo",
					"Hello World!",
				}
				val.Should = "equal"
				val.Target = "Goodbye World!"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionRunnerCommandAssertion {
				return ActionRunnerCommandAssertion{
					Run: []string{
						"echo",
						"v1.2.3",
					},
					Should: "equal",
					Target: "v1.2.3",
				}
			})
		})
	}
}

func TestActionRunnerCommandRunInfo_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionRunnerCommandRunInfo]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionRunnerCommandRunInfo) {},
		},

		{
			name:  "run not set",
			valid: false,
			mutate: func(val *ActionRunnerCommandRunInfo) {
				val.Run = nil
			},
		},
		{
			name:  "run not set 2",
			valid: false,
			mutate: func(val *ActionRunnerCommandRunInfo) {
				val.Run = []string{}
			},
		},
		{
			name:  "run first element empty",
			valid: false,
			mutate: func(val *ActionRunnerCommandRunInfo) {
				val.Run = []string{""}
			},
		},
		{
			name:     "run not found",
			valid:    true,
			warnings: true,
			mutate: func(val *ActionRunnerCommandRunInfo) {
				val.Run = []string{
					"my-unknown-program-that-should-not-exist",
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionRunnerCommandRunInfo {
				return ActionRunnerCommandRunInfo{
					Run: []string{
						"echo",
						"v1.2.3",
					},
				}
			})
		})
	}
}

func TestActionRunnerImage_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionRunnerImage]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionRunnerImage) {},
		},

		{
			name:  "image ok",
			valid: true,
			mutate: func(val *ActionRunnerImage) {
				val.Image = "my/image:latest"
			},
		},
		{
			name:  "image not set",
			valid: false,
			mutate: func(val *ActionRunnerImage) {
				val.Image = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionRunnerImage {
				return ActionRunnerImage{
					Image: "hello-world:latest",
				}
			})
		})
	}
}

func TestActionRunnerProjectAction_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionRunnerProjectAction]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionRunnerProjectAction) {},
		},

		{
			name:  "name ok",
			valid: true,
			mutate: func(val *ActionRunnerProjectAction) {
				val.Name = "my-action"
			},
		},
		{
			name:  "name not set",
			valid: false,
			mutate: func(val *ActionRunnerProjectAction) {
				val.Name = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionRunnerProjectAction {
				return ActionRunnerProjectAction{
					Name: "my-action",
				}
			})
		})
	}
}

func TestActionOutputWorkspaceFile_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionOutputWorkspaceFile]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionOutputWorkspaceFile) {},
		},

		{
			name:  "filename name ok",
			valid: true,
			mutate: func(val *ActionOutputWorkspaceFile) {
				val.Filename = "my/file.txt"
			},
		},
		{
			name:  "filename not set",
			valid: false,
			mutate: func(val *ActionOutputWorkspaceFile) {
				val.Filename = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionOutputWorkspaceFile {
				return ActionOutputWorkspaceFile{
					ActionOutputBase: ActionOutputBase{},
					Filename:         "my/file.txt",
				}
			})
		})
	}
}

func TestActionOutputStdout_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionOutputStdout]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionOutputStdout) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionOutputStdout {
				return ActionOutputStdout{
					ActionOutputBase: ActionOutputBase{},
				}
			})
		})
	}
}

func TestActionOutputStream_Validate(t *testing.T) {
	t.Parallel()

	tests := []validateTest[ActionOutputStream]{
		{
			name:   "ok",
			valid:  true,
			mutate: func(val *ActionOutputStream) {},
		},

		{
			name:  "stream name name ok",
			valid: true,
			mutate: func(val *ActionOutputStream) {
				val.StreamName = "my-stream"
			},
		},
		{
			name:  "stream name not set",
			valid: false,
			mutate: func(val *ActionOutputStream) {
				val.StreamName = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, func() ActionOutputStream {
				return ActionOutputStream{
					ActionOutputBase: ActionOutputBase{},
					StreamName:       "my-stream",
				}
			})
		})
	}
}
