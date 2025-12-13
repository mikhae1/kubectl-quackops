package llm

import (
	"fmt"
	"strings"
	"sync"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
)

type planStepStatus int

const (
	planStepNotStarted planStepStatus = iota
	planStepInProgress
	planStepCompleted
	planStepFailed
)

type planToolStatus int

const (
	planToolNone planToolStatus = iota
	planToolRunning
	planToolDone
	planToolFailed
)

type planToolRecord struct {
	Name string
	Iter int
	Max  int
	Kind planToolStatus
	Err  string
}

type planProgressRenderer struct {
	cfg *config.Config

	mu sync.Mutex

	steps       []PlanStep
	stepIdxByNo map[int]int
	stepStatus  []planStepStatus

	activeStepNo int

	stepOutputs map[int]string
	stepTools   map[int][]planToolRecord

	lastToolName      string
	lastToolIter      int
	lastToolMax       int
	lastToolStatus    planToolStatus
	lastToolErrString string
}

func newPlanProgressRenderer(cfg *config.Config, steps []PlanStep) *planProgressRenderer {
	stepIdxByNo := make(map[int]int, len(steps))
	for i, st := range steps {
		stepIdxByNo[st.StepNumber] = i
	}
	return &planProgressRenderer{
		cfg:         cfg,
		steps:       append([]PlanStep(nil), steps...),
		stepIdxByNo: stepIdxByNo,
		stepStatus:  make([]planStepStatus, len(steps)),
		stepOutputs: make(map[int]string, len(steps)),
		stepTools:   make(map[int][]planToolRecord, len(steps)),
	}
}

func (r *planProgressRenderer) Init() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderLocked()
}

func (r *planProgressRenderer) Clear() {
	sm := lib.GetSpinnerManager(r.cfg)
	sm.ClearDetailsLines()
}

func (r *planProgressRenderer) Handle(ev PlanProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch ev.Kind {
	case PlanProgressStepStarted:
		r.activeStepNo = ev.StepNumber
		if idx, ok := r.stepIdxByNo[ev.StepNumber]; ok {
			r.stepStatus[idx] = planStepInProgress
		}
		r.lastToolStatus = planToolNone
		r.lastToolName = ""
		r.lastToolErrString = ""
	case PlanProgressStepCompleted:
		if idx, ok := r.stepIdxByNo[ev.StepNumber]; ok {
			r.stepStatus[idx] = planStepCompleted
		}
		r.lastToolStatus = planToolNone
		r.lastToolName = ""
		r.lastToolErrString = ""
	case PlanProgressStepFailed:
		if idx, ok := r.stepIdxByNo[ev.StepNumber]; ok {
			r.stepStatus[idx] = planStepFailed
		}
		r.lastToolStatus = planToolNone
		r.lastToolName = ""
		r.lastToolErrString = ev.Err
	case PlanProgressStepOutput:
		if strings.TrimSpace(ev.Output) != "" {
			r.stepOutputs[ev.StepNumber] = strings.TrimSpace(ev.Output)
		}
	case PlanProgressToolStarted:
		r.lastToolStatus = planToolRunning
		r.lastToolName = ev.ToolName
		r.lastToolIter = ev.Iteration
		r.lastToolMax = ev.MaxIterations
		r.lastToolErrString = ""
	case PlanProgressToolCompleted:
		r.lastToolStatus = planToolDone
		r.lastToolName = ev.ToolName
		r.lastToolIter = ev.Iteration
		r.lastToolMax = ev.MaxIterations
		r.lastToolErrString = ""
		if r.activeStepNo != 0 && strings.TrimSpace(ev.ToolName) != "" {
			r.stepTools[r.activeStepNo] = append(r.stepTools[r.activeStepNo], planToolRecord{
				Name: strings.TrimSpace(ev.ToolName),
				Iter: ev.Iteration,
				Max:  ev.MaxIterations,
				Kind: planToolDone,
			})
		}
	case PlanProgressToolFailed:
		r.lastToolStatus = planToolFailed
		r.lastToolName = ev.ToolName
		r.lastToolIter = ev.Iteration
		r.lastToolMax = ev.MaxIterations
		r.lastToolErrString = ev.Err
		if r.activeStepNo != 0 && strings.TrimSpace(ev.ToolName) != "" {
			r.stepTools[r.activeStepNo] = append(r.stepTools[r.activeStepNo], planToolRecord{
				Name: strings.TrimSpace(ev.ToolName),
				Iter: ev.Iteration,
				Max:  ev.MaxIterations,
				Kind: planToolFailed,
				Err:  strings.TrimSpace(ev.Err),
			})
		}
	}

	r.renderLocked()
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max <= 1 {
		return string(rs[:max])
	}
	return string(rs[:max-1]) + "…"
}

func (r *planProgressRenderer) renderLocked() {
	sm := lib.GetSpinnerManager(r.cfg)

	lines := make([]string, 0, len(r.steps)+2)

	firstPrefix := "  └ "
	otherPrefix := "    "

	for i, st := range r.steps {
		prefix := otherPrefix
		if i == 0 {
			prefix = firstPrefix
		}

		action := strings.TrimSpace(st.Action)
		label := fmt.Sprintf("Task %d: %s", i+1, action)

		var mark string
		var text string
		switch r.stepStatus[i] {
		case planStepCompleted:
			mark = config.Colors.Ok.Sprint("✓")
			text = config.Colors.Dim.Sprint(label)
		case planStepFailed:
			mark = config.Colors.Error.Sprint("✗")
			text = config.Colors.Error.Sprint(label)
		case planStepInProgress:
			mark = "☐"
			text = config.Colors.Accent.Sprint(label)
		default:
			mark = "☐"
			text = label
		}

		lines = append(lines, prefix+mark+" "+text)

		// Show tool call history + step output for completed steps (dim, nested under the task).
		if r.stepStatus[i] == planStepCompleted || r.stepStatus[i] == planStepFailed {
			stepNo := st.StepNumber

			if tools := r.stepTools[stepNo]; len(tools) > 0 {
				lines = append(lines, otherPrefix+config.Colors.Dim.Sprint("└ tool calls"))
				maxTools := 8
				for ti, tr := range tools {
					if ti >= maxTools {
						lines = append(lines, otherPrefix+config.Colors.Dim.Sprint("└ …"))
						break
					}
					iter := ""
					if tr.Max > 0 && tr.Iter > 0 {
						iter = fmt.Sprintf(" (%d/%d)", tr.Iter, tr.Max)
					}
					var toolMark string
					switch tr.Kind {
					case planToolDone:
						toolMark = config.Colors.Ok.Sprint("✓")
					case planToolFailed:
						toolMark = config.Colors.Error.Sprint("✗")
					default:
						toolMark = "☐"
					}
					lines = append(lines, otherPrefix+config.Colors.Dim.Sprint("└ ")+toolMark+config.Colors.Dim.Sprint(" "+tr.Name+iter))
					if strings.TrimSpace(tr.Err) != "" {
						lines = append(lines, otherPrefix+config.Colors.Dim.Sprint("   ")+config.Colors.Error.Sprint(tr.Err))
					}
				}
			}

			if out := strings.TrimSpace(r.stepOutputs[stepNo]); out != "" {
				maxLines := 8
				maxCols := 140
				outLines := strings.Split(out, "\n")
				shown := 0
				for _, ol := range outLines {
					ol = strings.TrimRight(ol, "\r")
					olTrim := strings.TrimSpace(ol)
					if olTrim == "" {
						continue
					}
					lines = append(lines, otherPrefix+config.Colors.Dim.Sprint("└ ")+config.Colors.Dim.Sprint(truncateRunes(olTrim, maxCols)))
					shown++
					if shown >= maxLines {
						if len(outLines) > shown {
							lines = append(lines, otherPrefix+config.Colors.Dim.Sprint("└ …"))
						}
						break
					}
				}
			}
		}

		// Tool progress under the active step.
		if st.StepNumber == r.activeStepNo && r.lastToolStatus != planToolNone && r.lastToolName != "" {
			toolPrefix := otherPrefix
			var toolMark string
			switch r.lastToolStatus {
			case planToolRunning:
				toolMark = "☐"
			case planToolDone:
				toolMark = config.Colors.Ok.Sprint("✓")
			case planToolFailed:
				toolMark = config.Colors.Error.Sprint("✗")
			default:
				toolMark = "☐"
			}
			iter := ""
			if r.lastToolMax > 0 && r.lastToolIter > 0 {
				iter = fmt.Sprintf(" (%d/%d)", r.lastToolIter, r.lastToolMax)
			}
			toolLine := fmt.Sprintf("%s%s %s%s", toolPrefix, config.Colors.Dim.Sprint("└"), toolMark, config.Colors.Dim.Sprint(" "+r.lastToolName+iter))
			lines = append(lines, toolLine)
			if r.lastToolErrString != "" {
				lines = append(lines, toolPrefix+config.Colors.Dim.Sprint("   ")+config.Colors.Error.Sprint(r.lastToolErrString))
			}
		}
	}

	sm.SetDetailsLines(lines)
}
