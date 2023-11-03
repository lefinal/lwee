package scheduler

import (
	"context"
	"errors"
	"fmt"
	"github.com/lefinal/lwee/lwee/action"
	"github.com/lefinal/lwee/lwee/actionio"
	"github.com/lefinal/lwee/lwee/logging"
	"github.com/lefinal/meh"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"sync"
	"time"
)

// scheduledAction is a wrapper for action.Action that the Scheduler deals with.
// This holds fields like inputs for keeping track of when to schedule the action
// for start, etc.
type scheduledAction struct {
	action       action.Action
	inputs       []*input
	outputs      []*output
	start        time.Time
	currentPhase action.Phase
}

// inputIngestionsNotDoneForPhase returns a list of all input ingestions for the
// given action.Phase or earlier that are not done.
func (scheduledAction *scheduledAction) inputIngestionsNotDoneForPhase(phase action.Phase) []*input {
	notDone := make([]*input, 0)
	for _, input := range scheduledAction.inputs {
		if !input.done && input.request.IngestionPhase <= phase {
			notDone = append(notDone, input)
		}
	}
	return notDone
}

// outputProvidersNotDoneForPhase returns a list of all outputs for the given
// action.Phase or earlier that are not done.
func (scheduledAction *scheduledAction) outputProvidersNotDoneForPhase(phase action.Phase) []*output {
	notDone := make([]*output, 0)
	for _, output := range scheduledAction.outputs {
		if !output.done && output.offer.OutputPhase <= phase {
			notDone = append(notDone, output)
		}
	}
	return notDone
}

type input struct {
	request action.InputIngestionRequest
	ready   bool
	done    bool
	source  actionio.SourceReader
}

type output struct {
	offer  action.OutputOffer
	done   bool
	source actionio.SourceWriter
}

type Scheduler struct {
	ctx              context.Context
	cancel           context.CancelCauseFunc
	logger           *zap.Logger
	scheduledActions []*scheduledAction
	remainingActions int
	m                sync.Mutex
}

func scheduledActionFromAction(logger *zap.Logger, actionToSchedule action.Action, ioSupplier actionio.Supplier) (*scheduledAction, error) {
	scheduledAction := &scheduledAction{
		action:       actionToSchedule,
		inputs:       make([]*input, 0),
		outputs:      make([]*output, 0),
		currentPhase: action.PhasePreStart,
	}
	// Setup inputs
	for _, inputIngestionRequest := range actionToSchedule.InputIngestionRequests() {
		logger.Debug(fmt.Sprintf("request source %q for input %q of action %q",
			inputIngestionRequest.SourceName, inputIngestionRequest.InputName, actionToSchedule.Name()))
		input := &input{
			request: inputIngestionRequest,
			source:  ioSupplier.RequestSource(inputIngestionRequest.SourceName),
		}
		scheduledAction.inputs = append(scheduledAction.inputs, input)
	}
	// Setup outputs.
	for _, outputOffer := range actionToSchedule.OutputOffers() {
		logger.Debug(fmt.Sprintf("offer output %q of action %q",
			outputOffer.OutputName, actionToSchedule.Name()))
		sourceName := fmt.Sprintf("action.%s.out.%s", actionToSchedule.Name(), outputOffer.OutputName)
		source, err := ioSupplier.RegisterSourceProvider(sourceName)
		if err != nil {
			return nil, meh.Wrap(err, "register source provider", meh.Details{
				"source_name":  sourceName,
				"output_name":  outputOffer.OutputName,
				"output_phase": outputOffer.OutputPhase,
			})
		}
		output := &output{
			offer:  outputOffer,
			source: source,
		}
		scheduledAction.outputs = append(scheduledAction.outputs, output)
	}
	return scheduledAction, nil
}

// New creates a new Scheduler and registers IO for the given action.Action list.
func New(runCtx context.Context, logger *zap.Logger, ioSupplier actionio.Supplier, actions []action.Action) (*Scheduler, error) {
	ctx, cancel := context.WithCancelCause(runCtx)
	scheduler := &Scheduler{
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
		scheduledActions: make([]*scheduledAction, 0),
		remainingActions: len(actions),
	}
	for _, actionToSchedule := range actions {
		scheduledAction, err := scheduledActionFromAction(logger, actionToSchedule, ioSupplier)
		if err != nil {
			return nil, meh.Wrap(err, "scheduled action from action", meh.Details{"action_name": actionToSchedule.Name()})
		}
		scheduler.scheduledActions = append(scheduler.scheduledActions, scheduledAction)
	}
	return scheduler, nil
}

func (scheduler *Scheduler) isCanceled() bool {
	select {
	case <-scheduler.ctx.Done():
		return true
	default:
		return false
	}
}

func (scheduler *Scheduler) schedule() {
	scheduler.m.Lock()
	defer scheduler.m.Unlock()
	for _, scheduledAction := range scheduler.scheduledActions {
		reschedule := true
		var err error
		for reschedule {
			reschedule, err = scheduler.scheduleAction(scheduler.logger.Named("action").Named(logging.WrapName(scheduledAction.action.Name())), scheduledAction)
			if err != nil {
				scheduler.cancel(meh.Wrap(err, "schedule action", meh.Details{"action_name": scheduledAction.action.Name()}))
			}
		}
	}
	if scheduler.remainingActions == 0 {
		scheduler.cancel(nil)
	}
}

func (scheduler *Scheduler) scheduleAction(logger *zap.Logger, scheduledAction *scheduledAction) (bool, error) {
	switch scheduledAction.currentPhase {
	case action.PhasePreStart:
		// Check if all pre-start input ingestion requests are done.
		inputIngestionsNotDoneForPhase := scheduledAction.inputIngestionsNotDoneForPhase(action.PhasePreStart)
		if len(inputIngestionsNotDoneForPhase) > 0 {
			waitingForSources := make([]string, 0)
			for _, input := range inputIngestionsNotDoneForPhase {
				waitingForSources = append(waitingForSources, input.request.SourceName)
			}
			logger.Debug("waiting for action input ingestions in pre-start phase to finish",
				zap.Strings("waiting_for_sources", waitingForSources))
			return false, nil
		}
		logger.Debug("no action input ingestions to wait for. now waiting for live input.")
		scheduledAction.currentPhase = action.PhaseWaitForLiveInput
		return true, nil
	case action.PhaseWaitForLiveInput:
		// Check if we have any input ingestion requests for when the application is
		// running. If so, we wait for the first source to be ready.
		liveInputsBeingReady := 0
		liveInputsNotBeingReady := 0
		for _, actionInput := range scheduledAction.inputs {
			if actionInput.request.IngestionPhase < action.PhaseRunning {
				continue
			}
			if actionInput.ready {
				liveInputsBeingReady++
			} else {
				liveInputsNotBeingReady++
			}
		}
		if liveInputsNotBeingReady > 0 && liveInputsBeingReady == 0 {
			return false, nil
		}
		// Ready.
		logger.Debug("action now ready")
		scheduledAction.currentPhase = action.PhaseReady
		return true, nil
	case action.PhaseReady:
		// Start the action.
		logger.Debug("start action")
		scheduledAction.currentPhase = action.PhaseStarting
		go func() {
			defer func() {
				scheduler.m.Lock()
				scheduledAction.currentPhase = action.PhaseStopped
				scheduler.remainingActions--
				remainingActionNames := make([]string, 0)
				for _, scheduledAction := range scheduler.scheduledActions {
					if scheduledAction.currentPhase != action.PhaseDone {
						remainingActionNames = append(remainingActionNames, scheduledAction.action.Name())
					}
				}
				scheduler.m.Unlock()
				scheduler.logger.Info(fmt.Sprintf("finished action %d/%d",
					len(scheduler.scheduledActions)-scheduler.remainingActions, len(scheduler.scheduledActions)),
					zap.String("action_name", scheduledAction.action.Name()),
					zap.Duration("action_took", time.Since(scheduledAction.start)))
				scheduler.schedule()
			}()
			scheduledAction.start = time.Now()
			done, err := scheduledAction.action.Start(scheduler.ctx)
			if err != nil {
				scheduler.cancel(meh.Wrap(err, fmt.Sprintf("start action %q", scheduledAction.action.Name()), nil))
				return
			}
			defer func() { _ = scheduledAction.action.Stop(context.Background()) }()
			logger.Debug("action now running")
			scheduler.m.Lock()
			scheduledAction.currentPhase = action.PhaseRunning
			scheduler.m.Unlock()
			scheduler.schedule()
			eg, ctx := errgroup.WithContext(scheduler.ctx)
			// Provide outputs.
			for _, output := range scheduledAction.outputs {
				output := output
				eg.Go(func() error {
					logger := logger.Named("output").Named(logging.WrapName(output.offer.OutputName))
					defer func() {
						scheduler.m.Lock()
						output.done = true
						scheduler.m.Unlock()
						scheduler.schedule()
					}()
					err := scheduledAction.action.ProvideOutput(scheduler.ctx, output.offer.OutputName, output.source)
					if err != nil {
						return meh.Wrap(err, "provide output", meh.Details{"output_name": output.offer.OutputName})
					}
					logger.Debug("output done")
					return nil
				})
			}
			// Wait for action to stop.
			eg.Go(func() error {
				select {
				case <-ctx.Done():
					return meh.NewInternalErrFromErr(ctx.Err(), "wait for action to stop", nil)
				case err = <-done:
				}
				return nil
			})
			err = eg.Wait()
			if err != nil {
				scheduler.cancel(meh.Wrap(err, fmt.Sprintf("run action %q", scheduledAction.action.Name()), nil))
				return
			}
			logger.Debug("action done")
		}()
		return true, nil
	case action.PhaseStarting,
		action.PhaseRunning:
		return false, nil
	case action.PhaseStopped:
		// Check if all post-running output providers are done.
		outputProvidersNotDoneForPhase := scheduledAction.outputProvidersNotDoneForPhase(action.PhaseStopped)
		if len(outputProvidersNotDoneForPhase) > 0 {
			waitingForOutputs := make([]string, 0)
			for _, output := range outputProvidersNotDoneForPhase {
				waitingForOutputs = append(waitingForOutputs, output.offer.OutputName)
			}
			logger.Debug("waiting for action output in stopped phase to finish",
				zap.Strings("waiting_for_outputs", waitingForOutputs))
			return false, nil
		}
		scheduledAction.currentPhase = action.PhaseDone
		return false, nil
	case action.PhaseDone:
		logger.Debug("reached phase done")
		return false, nil
	default:
		return false, meh.NewInternalErr(fmt.Sprintf("unexpected phase: %v", scheduledAction.currentPhase), nil)
	}
}

func (scheduler *Scheduler) Run() error {
	defer func() { scheduler.logger.Debug("done") }()
	// Start input reading (waiting and ingestion).
	go func() {
		err := scheduler.readActionInputs(scheduler.ctx)
		if err != nil {
			scheduler.cancel(meh.Wrap(err, "read inputs", nil))
			return
		}
	}()
	scheduler.schedule()
	<-scheduler.ctx.Done()
	scheduler.m.Lock()
	defer scheduler.m.Unlock()
	if scheduler.remainingActions == 0 && errors.Is(scheduler.ctx.Err(), context.Canceled) {
		return nil
	}
	return context.Cause(scheduler.ctx)
}

func (scheduler *Scheduler) readActionInputs(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	scheduler.m.Lock()
	for _, scheduledAction := range scheduler.scheduledActions {
		for _, input := range scheduledAction.inputs {
			scheduledAction := scheduledAction
			input := input
			eg.Go(func() error {
				err := scheduler.readActionInput(ctx, scheduledAction, input)
				if err != nil {
					return meh.Wrap(err, "read action input", meh.Details{
						"action_name": scheduledAction.action.Name(),
						"input_name":  input.request.InputName,
						"source_name": input.source.Name,
					})
				}
				return nil
			})
		}
	}
	scheduler.m.Unlock()
	return eg.Wait()
}

func (scheduler *Scheduler) readActionInput(ctx context.Context, scheduledAction *scheduledAction, input *input) error {
	logger := scheduler.logger.Named("action").Named(logging.WrapName(scheduledAction.action.Name())).
		Named("input").Named(logging.WrapName(input.request.InputName))
	// Wait for source ready.
	select {
	case <-ctx.Done():
		return meh.NewInternalErrFromErr(ctx.Err(), "wait for source ready", nil)
	case <-input.source.Open:
	}
	scheduler.m.Lock()
	input.ready = true
	scheduler.m.Unlock()
	// Schedule for handling ready inputs (for startup delay).
	scheduler.schedule()
	// Ingest the actual input.
	logger.Debug("input ingestion started")
	err := scheduledAction.action.IngestInput(scheduler.ctx, input.request.InputName, input.source.Reader)
	if err != nil {
		return meh.Wrap(err, "ingest input", nil)
	}
	logger.Debug("input ingestion done")
	scheduler.m.Lock()
	input.done = true
	scheduler.m.Unlock()
	// Schedule for handling done inputs.
	scheduler.schedule()
	return nil
}

// TODO: deadlock erkennung? im scheduler vermutlich, also schauen, ob alle in der phase für warten sind. aber concurrency probleme?
