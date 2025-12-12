package llm

import "sync/atomic"

type PlanProgressKind string

const (
	PlanProgressStepStarted   PlanProgressKind = "step_started"
	PlanProgressStepCompleted PlanProgressKind = "step_completed"
	PlanProgressStepFailed    PlanProgressKind = "step_failed"
	PlanProgressStepOutput    PlanProgressKind = "step_output"

	PlanProgressToolStarted   PlanProgressKind = "tool_started"
	PlanProgressToolCompleted PlanProgressKind = "tool_completed"
	PlanProgressToolFailed    PlanProgressKind = "tool_failed"
)

type PlanProgressEvent struct {
	Kind PlanProgressKind

	StepNumber int
	StepAction string

	ToolName      string
	Iteration     int
	MaxIterations int

	Output string
	Err    string
}

type PlanProgressSink func(PlanProgressEvent)

var planProgressSink atomic.Value // PlanProgressSink

func SetPlanProgressSink(sink PlanProgressSink) (restore func()) {
	prev := planProgressSink.Load()
	if sink == nil {
		planProgressSink.Store((PlanProgressSink)(nil))
	} else {
		planProgressSink.Store(sink)
	}
	return func() {
		if prev == nil {
			planProgressSink.Store((PlanProgressSink)(nil))
			return
		}
		planProgressSink.Store(prev)
	}
}

func emitPlanProgress(ev PlanProgressEvent) {
	v := planProgressSink.Load()
	if v == nil {
		return
	}
	sink, ok := v.(PlanProgressSink)
	if !ok || sink == nil {
		return
	}
	sink(ev)
}
