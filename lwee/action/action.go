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

type Phase int

const (
	PhasePreStart Phase = iota
	PhaseWaitForLiveInput
	PhaseReady
	PhaseStarting
	PhaseRunning
	PhaseStopped
	PhaseDone
)

type Action interface {
	Name() string
	Build(ctx context.Context) error
	InputIngestionRequests() []InputIngestionRequest
	OutputOffers() []OutputOffer
	IngestInput(ctx context.Context, inputName string, data io.ReadCloser) error
	ProvideOutput(ctx context.Context, outputName string, writer actionio.SourceWriter) error
	Start(ctx context.Context) (<-chan error, error)
	Stop(ctx context.Context) error
}

type action interface {
	Action
	registerInputIngestionRequests() error
	registerOutputProviders() error
}

type InputIngestionRequest struct {
	RequireFinishUntilPhase Phase
	InputName               string
	SourceName              string
}

type inputIngestionRequestWithIngestor struct {
	request InputIngestionRequest
	ingest  actionio.Ingestor
}

type OutputOffer struct {
	RequireFinishUntilPhase Phase
	OutputName              string
}

type OutputOfferWithOutputter struct {
	offer  OutputOffer
	output actionio.Outputter
}

type Base struct {
	logger                            *zap.Logger
	actionName                        string
	fileActionInputs                  lweeflowfile.ActionInputs
	fileActionOutputs                 lweeflowfile.ActionOutputs
	inputIngestionRequestsByInputName map[string]inputIngestionRequestWithIngestor
	outputOffersByOutputName          map[string]OutputOfferWithOutputter
	ioSupplier                        actionio.Supplier
}

func (base *Base) Name() string {
	return base.actionName
}

func (base *Base) InputIngestionRequests() []InputIngestionRequest {
	requests := make([]InputIngestionRequest, 0)
	for _, request := range base.inputIngestionRequestsByInputName {
		requests = append(requests, request.request)
	}
	return requests
}

func (base *Base) IngestInput(ctx context.Context, inputName string, data io.ReadCloser) error {
	ingestionRequest, ok := base.inputIngestionRequestsByInputName[inputName]
	if !ok {
		return meh.NewInternalErr("input ingestion request for unknown input", meh.Details{"input_name": inputName})
	}
	err := ingestionRequest.ingest(ctx, data)
	if err != nil {
		return meh.Wrap(err, "ingest data", nil)
	}
	return nil
}

func (base *Base) OutputOffers() []OutputOffer {
	offers := make([]OutputOffer, 0)
	for _, offer := range base.outputOffersByOutputName {
		offers = append(offers, offer.offer)
	}
	return offers
}

func (base *Base) ProvideOutput(ctx context.Context, outputName string, writer actionio.SourceWriter) error {
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

type Factory struct {
	FlowName        string
	RawFlow         map[string]any
	Locator         *locator.Locator
	ContainerEngine container.Engine
	IOSupplier      actionio.Supplier
}

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
	base := &Base{
		logger:                            logger,
		actionName:                        actionName,
		fileActionInputs:                  fileAction.Inputs,
		fileActionOutputs:                 fileAction.Outputs,
		ioSupplier:                        factory.IOSupplier,
		inputIngestionRequestsByInputName: make(map[string]inputIngestionRequestWithIngestor),
		outputOffersByOutputName:          make(map[string]OutputOfferWithOutputter),
	}
	// Create the actual action.
	switch runner := fileAction.Runner.Runner.(type) {
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
