// Package action holds actions to run along with their corresponding setup,
// tear-down, action IO setup, operation, etc.
package action

import (
	"context"
	"fmt"
	"github.com/lefinal/lwee/lwee/actionio"
	"github.com/lefinal/lwee/lwee/container"
	"github.com/lefinal/lwee/lwee/locator"
	"github.com/lefinal/lwee/lwee/lweeflowfile"
	"github.com/lefinal/lwee/lwee/templaterender"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"io"
)

// Phase describes the phase of an action.
type Phase int

const (
	// PhasePreStart for when the action is created but not started yet.
	PhasePreStart Phase = iota
	// PhaseWaitForLiveInput for when the action is waiting for the first input to
	// open. This improves resource consumption as it is only started when it can
	// actually start working right away.
	PhaseWaitForLiveInput
	// PhaseReady for when the action is ready to be started.
	PhaseReady
	// PhaseStarting for when the action is starting up.
	PhaseStarting
	// PhaseRunning for when the action is running.
	PhaseRunning
	// PhaseStopped for when the action is stopped and providing outputs or clean-up
	// are still in progress.
	PhaseStopped
	// PhaseDone for when the action is done and all of its outputs have been fully
	// consumed.
	PhaseDone
)

// Action is one of the core components in LWEE. It has a number of inputs and
// outputs and an operation to perform.
type Action interface {
	// Name of the action.
	Name() string
	// Verify validates the action's definition.
	Verify(ctx context.Context) error
	// Build the action. This may include, for example, building or pulling container
	// images.
	Build(ctx context.Context) error
	// InputIngestionRequests returns an InputIngestionRequest-list that is build
	// from all requested inputs from the action.
	InputIngestionRequests() []InputIngestionRequest
	// OutputOffers returns an OutputOffer-list that is build from all offered
	// outputs from the action.
	OutputOffers() []OutputOffer
	// IngestInput ingests the given data into the action.
	IngestInput(ctx context.Context, inputName string, data io.ReadCloser, availableOptimizations *actionio.AvailableOptimizations) error
	// ProvideOutput requests the output from the action. The data will be written to
	// the passed actionio.SourceWriter.
	ProvideOutput(ctx context.Context, outputName string, writer actionio.SourceWriter) error
	// RunInfo returns the run info for the action.
	RunInfo(ctx context.Context) (map[string]string, error)
	// Start the action. When the action is done, it will send on the returned
	// channel along with the result.
	Start(ctx context.Context) (<-chan error, error)
	Stop(ctx context.Context) error
}

type action interface {
	Action
	registerInputIngestionRequests() error
	registerOutputProviders() error
}

// InputIngestionRequest requests input from the specified source.
type InputIngestionRequest struct {
	// RequireFinishUntilPhase describes whether the input ingestion should finish
	// until a certain Phase is reached. This is used, for example, for provided
	// files - these need to be ready before the operation is started.
	RequireFinishUntilPhase Phase
	// InputName as it will be passed in Action.IngestInput.
	InputName string
	// SourceName is the name of the source to request data from.
	SourceName string
}

// OptimizeFunc performs optimizations based on the provided
// actionio.AvailableOptimizations.
type OptimizeFunc func(ctx context.Context, availableOptimizations *actionio.AvailableOptimizations)

type inputIngestionRequestWithIngestor struct {
	request  InputIngestionRequest
	optimize OptimizeFunc
	ingest   actionio.Ingestor
}

// OutputOffer provides output via the specified output name.
type OutputOffer struct {
	// RequireFinishUntilPhase describes whether data output should finish until a
	// certain Phase is reached. This is used, for example, for stdout - this needs
	// to be finished before the operation is stopped.
	RequireFinishUntilPhase Phase
	OutputName              string
}

// outputOfferWithOutputter is a simple container for OutputOffer and
// actionio.Outputter. Mainly used as syntactic sugar instead of returning
// multiple values.
type outputOfferWithOutputter struct {
	offer  OutputOffer
	output actionio.Outputter
}

// base holds common fields for Action.
type base struct {
	logger                            *zap.Logger
	actionName                        string
	fileActionInputs                  lweeflowfile.ActionInputs
	fileActionOutputs                 lweeflowfile.ActionOutputs
	inputIngestionRequestsByInputName map[string]inputIngestionRequestWithIngestor
	outputOffersByOutputName          map[string]outputOfferWithOutputter
	ioSupplier                        actionio.Supplier
}

// Name of the action.
func (base *base) Name() string {
	return base.actionName
}

// InputIngestionRequests returns an InputIngestionRequest-list for all input
// ingestion requests.
func (base *base) InputIngestionRequests() []InputIngestionRequest {
	requests := make([]InputIngestionRequest, 0)
	for _, request := range base.inputIngestionRequestsByInputName {
		requests = append(requests, request.request)
	}
	return requests
}

// IngestInput ingests the input data for a specific input name. It retrieves the
// ingestion request based on the input name. If an optimization function is
// specified in the ingestion request, it invokes the optimize function. If
// transmission is skipped based on the available optimizations, it closes the
// input data and returns. Otherwise, it performs the ingestion using the ingest
// function specified in the ingestion request. If an error occurs during
// ingestion, it wraps the error and returns.
func (base *base) IngestInput(ctx context.Context, inputName string, data io.ReadCloser, availableOptimizations *actionio.AvailableOptimizations) error {
	ingestionRequest, ok := base.inputIngestionRequestsByInputName[inputName]
	if !ok {
		return meh.NewInternalErr("input ingestion request for unknown input", meh.Details{"input_name": inputName})
	}
	if ingestionRequest.optimize != nil {
		ingestionRequest.optimize(ctx, availableOptimizations)
	}
	if availableOptimizations.IsTransmissionSkipped() {
		_ = data.Close()
		return nil
	}
	err := ingestionRequest.ingest(ctx, data)
	if err != nil {
		return meh.Wrap(err, "ingest data", nil)
	}
	return nil
}

// OutputOffers returns an OutputOffer-list for the output offers of the action.
func (base *base) OutputOffers() []OutputOffer {
	offers := make([]OutputOffer, 0)
	for _, offer := range base.outputOffersByOutputName {
		offers = append(offers, offer.offer)
	}
	return offers
}

// ProvideOutput provides the output with the specified name to the writer. It
// looks up the output offer associated with the name and calls its
// output-function.
func (base *base) ProvideOutput(ctx context.Context, outputName string, writer actionio.SourceWriter) error {
	outputOffer, ok := base.outputOffersByOutputName[outputName]
	if !ok {
		return meh.NewInternalErr(fmt.Sprintf("unknown output name: %s", outputName), nil)
	}
	err := outputOffer.output(ctx, writer.Open, writer.Writer)
	if err != nil {
		return meh.Wrap(err, "output", nil)
	}
	return nil
}

// Factory allows creating actions.
type Factory struct {
	FlowName        string
	RawFlow         map[string]any
	Locator         *locator.Locator
	ContainerEngine container.Engine
	IOSupplier      actionio.Supplier
}

// NewAction creates anew Action from the given lweeflowfile.Action.
func (factory *Factory) NewAction(logger *zap.Logger, actionName string, fileAction lweeflowfile.Action) (Action, error) {
	renderData := templaterender.Data{
		Flow: templaterender.FlowData{
			Raw: factory.RawFlow,
		},
		Action: templaterender.ActionData{
			Name: actionName,
		},
	}
	var builtAction action
	var err error
	// Build action base.
	base := &base{
		logger:                            logger,
		actionName:                        actionName,
		fileActionInputs:                  fileAction.Inputs,
		fileActionOutputs:                 fileAction.Outputs,
		ioSupplier:                        factory.IOSupplier,
		inputIngestionRequestsByInputName: make(map[string]inputIngestionRequestWithIngestor),
		outputOffersByOutputName:          make(map[string]outputOfferWithOutputter),
	}
	// Create the actual action.
	switch runner := fileAction.Runner.Runner.(type) {
	case lweeflowfile.ActionRunnerCommand:
		builtAction, err = factory.newCommandAction(base, renderData, runner)
		if err != nil {
			return nil, meh.Wrap(err, "new command action", nil)
		}
	case lweeflowfile.ActionRunnerImage:
		builtAction, err = factory.newImageAction(base, renderData, runner)
		if err != nil {
			return nil, meh.Wrap(err, "new image action", nil)
		}
	case lweeflowfile.ActionRunnerProjectAction:
		builtAction, err = factory.newProjectAction(base, renderData, runner)
		if err != nil {
			return nil, meh.Wrap(err, "new project action", nil)
		}
	default:
		return nil, meh.NewBadInputErr(fmt.Sprintf("unsupported runner type: %T", runner), nil)
	}
	// Setup inputs.
	err = builtAction.registerInputIngestionRequests()
	if err != nil {
		return nil, meh.Wrap(err, "register input ingestion requests", nil)
	}
	// Setup outputs.
	err = builtAction.registerOutputProviders()
	if err != nil {
		return nil, meh.Wrap(err, "register output providers", nil)
	}
	return builtAction, nil
}
