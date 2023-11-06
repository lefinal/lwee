package runinfo

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
	"os/exec"
	"sync"
	"time"
)

type Duration time.Duration

func (dur Duration) MarshalYAML() (any, error) {
	return time.Duration(dur).String(), nil
}

type Recorder struct {
	logger *zap.Logger
	flow   FlowInfo
	m      sync.Mutex
}

func NewCollector(logger *zap.Logger) *Recorder {
	return &Recorder{
		logger: logger,
		flow: FlowInfo{
			Actions: make(map[string]ActionInfo),
			IO: IOInfo{
				Writes: make(map[string]IOWriteInfo),
			},
		},
	}
}

func (collector *Recorder) RecordFlowName(name string) {
	collector.m.Lock()
	defer collector.m.Unlock()
	collector.flow.Name = name
}

func (collector *Recorder) RecordFlowStart(t time.Time) {
	collector.m.Lock()
	defer collector.m.Unlock()
	collector.flow.Start = t
}

func (collector *Recorder) RecordFlowEnd(t time.Time) {
	collector.m.Lock()
	defer collector.m.Unlock()
	collector.flow.End = t
}

func (collector *Recorder) assureAction(actionName string) {
	collector.m.Lock()
	defer collector.m.Unlock()
	_, ok := collector.flow.Actions[actionName]
	if ok {
		return
	}
	collector.flow.Actions[actionName] = ActionInfo{
		Info: make(map[string]string),
	}
}

func (collector *Recorder) RecordActionStart(actionName string, t time.Time) {
	collector.assureAction(actionName)
	collector.m.Lock()
	defer collector.m.Unlock()
	action := collector.flow.Actions[actionName]
	action.Start = t
	collector.flow.Actions[actionName] = action
}

func (collector *Recorder) RecordActionEnd(actionName string, t time.Time) {
	collector.assureAction(actionName)
	collector.m.Lock()
	defer collector.m.Unlock()
	action := collector.flow.Actions[actionName]
	action.End = t
	collector.flow.Actions[actionName] = action
}

func (collector *Recorder) RecordActionInfo(actionName string, info map[string]string) {
	collector.assureAction(actionName)
	collector.m.Lock()
	defer collector.m.Unlock()
	action := collector.flow.Actions[actionName]
	action.Info = info
	collector.flow.Actions[actionName] = action
}

func (collector *Recorder) assureIOWrite(writeName string) {
	collector.m.Lock()
	defer collector.m.Unlock()
	_, ok := collector.flow.IO.Writes[writeName]
	if ok {
		return
	}
	collector.flow.IO.Writes[writeName] = IOWriteInfo{}
}

func (collector *Recorder) RecordIOWriteInfo(writeName string, info IOWriteInfo) {
	collector.assureIOWrite(writeName)
	collector.m.Lock()
	defer collector.m.Unlock()
	collector.flow.IO.Writes[writeName] = info
}

func (collector *Recorder) bake() {
	collector.m.Lock()
	defer collector.m.Unlock()
	collector.flow.Took = Duration(collector.flow.End.Sub(collector.flow.Start))
	// Bake actions.
	for actionName, actionInfo := range collector.flow.Actions {
		actionInfo.Took = Duration(actionInfo.End.Sub(actionInfo.Start))
		collector.flow.Actions[actionName] = actionInfo
	}
	// Bake IO.
	totalWrittenBytes := int64(0)
	for writeName, writeInfo := range collector.flow.IO.Writes {
		writeInfo.WaitForOpenTook = Duration(writeInfo.WaitForOpenEnd.Sub(writeInfo.WaitForOpenStart))
		writeInfo.WriteTook = Duration(writeInfo.WriteEnd.Sub(writeInfo.WriteStart))
		writeInfo.Written = logging.FormatByteCountDecimal(writeInfo.WrittenBytes)
		writeInfo.CopyBufferSize = logging.FormatByteCountDecimal(writeInfo.CopyBufferSizeBytes)
		totalWrittenBytes += writeInfo.WrittenBytes
		collector.flow.IO.Writes[writeName] = writeInfo
	}
	collector.flow.IO.TotalWrittenBytes = totalWrittenBytes
	collector.flow.IO.TotalWritten = logging.FormatByteCountDecimal(collector.flow.IO.TotalWrittenBytes)
}

func (collector *Recorder) Result() ([]byte, error) {
	collector.bake()
	collector.m.Lock()
	defer collector.m.Unlock()
	result, err := yaml.Marshal(collector.flow)
	if err != nil {
		return nil, meh.NewInternalErrFromErr(err, "marshal flow result", meh.Details{"was": fmt.Sprintf("%+v", collector.flow)})
	}
	return result, nil
}

type FlowInfo struct {
	Name    string                `yaml:"flowName"`
	Start   time.Time             `yaml:"start"`
	End     time.Time             `yaml:"end"`
	Took    Duration              `yaml:"took"`
	Actions map[string]ActionInfo `yaml:"actions"`
	IO      IOInfo                `yaml:"io"`
}

type ActionInfo struct {
	Start time.Time         `yaml:"start"`
	End   time.Time         `yaml:"end"`
	Took  Duration          `yaml:"took"`
	Info  map[string]string `yaml:"info"`
}

type IOInfo struct {
	TotalWritten      string                 `yaml:"totalWritten"`
	TotalWrittenBytes int64                  `yaml:"totalWrittenBytes"`
	Writes            map[string]IOWriteInfo `yaml:"writes"`
}

type IOWriteInfo struct {
	Requesters                        []string          `yaml:"requesters"`
	WaitForOpenStart                  time.Time         `yaml:"waitForOpenStart"`
	WaitForOpenEnd                    time.Time         `yaml:"waitForOpenEnd"`
	WaitForOpenTook                   Duration          `yaml:"waitForOpenTook"`
	WriteStart                        time.Time         `yaml:"writeStart"`
	WriteEnd                          time.Time         `yaml:"writeEnd"`
	WriteTook                         Duration          `yaml:"writeTook"`
	Written                           string            `yaml:"written"`
	WrittenBytes                      int64             `yaml:"writtenBytes"`
	CopyBufferSize                    string            `yaml:"copyBufferSize"`
	CopyBufferSizeBytes               int64             `yaml:"copyBufferSizeBytes"`
	MinWriteTime                      Duration          `yaml:"minWriteTime"`
	MaxWriteTime                      Duration          `yaml:"maxWriteTime"`
	AvgWriteTime                      Duration          `yaml:"avgWriteTime"`
	TotalWaitForNextP                 Duration          `yaml:"totalWaitForNextP"`
	TotalDistributeP                  Duration          `yaml:"totalDistributeP"`
	TotalWaitForWritesAfterDistribute Duration          `yaml:"totalWaitForWritesAfterDistribute"`
	WriteTimes                        map[string]string `yaml:"writeTimes"`
}

// Provider allows evaluating run information using Eval.
type Provider interface {
	Eval(ctx context.Context) (string, error)
}

// NewProvider creates a new Provider from the given
// lweeflowfile.ActionRunnerCommandRunInfo.
func NewProvider(logDetails lweeflowfile.ActionRunnerCommandRunInfo) (Provider, error) {
	if len(logDetails.Run) == 0 || logDetails.Run[0] == "" {
		return nil, meh.NewBadInputErr("missing run command", nil)
	}
	return &commandProvider{
		run: logDetails.Run,
	}, nil
}

// EvalAll evaluates all providers in the given map and returns the corresponding
// map with the results.
func EvalAll(ctx context.Context, providers map[string]Provider) (map[string]string, error) {
	eg, ctx := errgroup.WithContext(ctx)
	runInfo := make(map[string]string)
	var runInfoMutex sync.Mutex
	for infoName, provider := range providers {
		infoName := infoName
		provider := provider
		eg.Go(func() error {
			result, err := provider.Eval(ctx)
			if err != nil {
				return meh.Wrap(err, fmt.Sprintf("eval %q", infoName), nil)
			}
			runInfoMutex.Lock()
			runInfo[infoName] = result
			runInfoMutex.Unlock()
			return nil
		})
	}
	err := eg.Wait()
	if err != nil {
		return nil, err
	}
	return runInfo, nil
}

// commandProvider is a Provider that returns a command's output.
type commandProvider struct {
	run []string
}

func (c *commandProvider) Eval(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, c.run[0], c.run[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", meh.NewInternalErrFromErr(err, "run command", meh.Details{"command": c.run})
	}
	return string(out), nil
}
