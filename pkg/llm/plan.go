package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// PlanStep represents a single planned action.
type PlanStep struct {
	StepNumber    int      `json:"step_number"`
	Action        string   `json:"action"`
	Reasoning     string   `json:"reasoning"`
	RequiredTools []string `json:"required_tools,omitempty"`
}

// PlanResult holds the parsed plan and the raw model output.
type PlanResult struct {
	Steps []PlanStep
	Raw   string
}

const (
	planSystemPrompt = "## Plan Generator\nCreate a concise, safe execution plan as JSON with field names: step_number (int), action (string), reasoning (string), required_tools (string array, optional). Keep steps specific and ordered."
)

// RunPlanFlow generates a plan, lets the user edit via checkboxes or guided replan, then executes selected steps.
func RunPlanFlow(ctx context.Context, cfg *config.Config, prompt string, confirmReader io.Reader) (string, error) {
	if confirmReader == nil {
		confirmReader = os.Stdin
	}

	currentPlan, err := GeneratePlan(ctx, cfg, prompt, "")
	if err != nil {
		return "", err
	}

	for {
		selectedSteps, action, feedback, selErr := SelectPlanSteps(cfg, currentPlan, confirmReader)
		if selErr != nil {
			return "", selErr
		}

		switch action {
		case "cancel":
			// Back out without error; caller can continue its flow
			return "", nil
		case "replan":
			if strings.TrimSpace(feedback) == "" {
				continue
			}
			currentPlan, err = GeneratePlan(ctx, cfg, prompt, feedback)
			if err != nil {
				return "", err
			}
			continue
		default:
			if len(selectedSteps) == 0 {
				fmt.Println(config.Colors.Warn.Sprint("No steps selected."))
				return "", nil
			}
			currentPlan.Steps = selectedSteps
			return ExecutePlan(ctx, cfg, currentPlan, prompt)
		}
	}
}

// GeneratePlan requests a structured plan from the LLM and parses it. Optional adjustments guide replans.
func GeneratePlan(ctx context.Context, cfg *config.Config, prompt string, adjustments string) (PlanResult, error) {
	userPrompt := fmt.Sprintf("Task: %s\nReturn JSON: {\"steps\":[{\"step_number\":1,\"action\":\"...\",\"reasoning\":\"...\",\"required_tools\":[\"kubectl\"]}]} Only JSON.", strings.TrimSpace(prompt))
	if strings.TrimSpace(adjustments) != "" {
		userPrompt += "\nAdjustments: " + strings.TrimSpace(adjustments)
	}

	raw, err := RequestWithSystem(cfg, planSystemPrompt, userPrompt, false, false)
	if err != nil {
		return PlanResult{}, err
	}

	plan, err := parsePlan(raw)
	if err != nil {
		logger.Log("warn", "Failed to parse plan JSON: %v", err)
		return PlanResult{}, fmt.Errorf("failed to parse plan: %w", err)
	}

	plan.Raw = raw
	return plan, nil
}

// ExecutePlan runs each plan step sequentially using the LLM.
func ExecutePlan(ctx context.Context, cfg *config.Config, plan PlanResult, originalPrompt string) (string, error) {
	if len(plan.Steps) == 0 {
		return "", errors.New("no plan steps to execute")
	}

	systemPrompt := buildExecutionSystemPrompt(originalPrompt, plan)
	var outputs []string

	for _, step := range plan.Steps {
		stepPrompt := fmt.Sprintf("Execute step %d: %s\nReason: %s\nOriginal request: %s", step.StepNumber, strings.TrimSpace(step.Action), strings.TrimSpace(step.Reasoning), originalPrompt)
		resp, err := RequestWithSystem(cfg, systemPrompt, stepPrompt, true, true)
		if err != nil {
			return "", fmt.Errorf("step %d failed: %w", step.StepNumber, err)
		}
		outputs = append(outputs, fmt.Sprintf("Step %d: %s\n%s", step.StepNumber, step.Action, strings.TrimSpace(resp)))
	}

	return strings.Join(outputs, "\n\n"), nil
}

func buildExecutionSystemPrompt(originalPrompt string, plan PlanResult) string {
	var planLines []string
	for _, s := range plan.Steps {
		planLines = append(planLines, fmt.Sprintf("%d. %s", s.StepNumber, strings.TrimSpace(s.Action)))
	}
	return fmt.Sprintf("## Plan Execution\nFollow the approved plan. Execute each step deterministically. Use tools when clearly required. Keep outputs concise.\n\nOriginal request: %s\nPlan:\n%s", strings.TrimSpace(originalPrompt), strings.Join(planLines, "\n"))
}

func renderPlan(cfg *config.Config, plan PlanResult) {
	title := config.Colors.Accent
	body := config.Colors.Info
	dim := config.Colors.Dim

	fmt.Println(title.Sprint("Plan:"))
	for _, step := range plan.Steps {
		tools := ""
		if len(step.RequiredTools) > 0 {
			tools = dim.Sprintf(" (tools: %s)", strings.Join(step.RequiredTools, ", "))
		}
		fmt.Printf("%s %s%s\n", body.Sprintf("%d.", step.StepNumber), body.Sprint(strings.TrimSpace(step.Action)), tools)
		if strings.TrimSpace(step.Reasoning) != "" {
			fmt.Println(dim.Sprintf("  reason: %s", strings.TrimSpace(step.Reasoning)))
		}
	}
	fmt.Println()
}

// SelectPlanSteps renders the plan with checkboxes and supports guided replan.
// Controls: Up/Down move, space toggle, a toggle all, Enter run, r guided replan, ESC back.
var SelectPlanSteps = selectPlanSteps

func selectPlanSteps(cfg *config.Config, plan PlanResult, input io.Reader) ([]PlanStep, string, string, error) {
	if len(plan.Steps) == 0 {
		return nil, "cancel", "", errors.New("no plan steps")
	}
	if input == nil {
		input = os.Stdin
	}

	selected := make([]bool, len(plan.Steps))
	for i := range selected {
		selected[i] = true
	}
	cursor := 0
	clearScreen := func() {
		// Clear entire screen and move cursor to home to avoid duplicated blocks.
		fmt.Print("\033[H\033[2J")
	}

	redraw := func() {
		clearScreen()
		header := config.Colors.Bold.Sprint("Plan (Up/Down move, space toggle, 1-9 select step, a all, r edit, Enter approve, ESC cancel):")
		lines := make([]string, 0, len(plan.Steps)+2)
		lines = append(lines, header)
		for i, step := range plan.Steps {
			box := "[ ]"
			if selected[i] {
				box = "[x]"
			}
			pointer := "  "
			if i == cursor {
				pointer = config.Colors.Command.Sprint("➤ ")
			}
			line := fmt.Sprintf("%s- %s %d. %s", pointer, box, step.StepNumber, strings.TrimSpace(step.Action))
			lines = append(lines, line)
			if strings.TrimSpace(step.Reasoning) != "" {
				reasonLine := config.Colors.Dim.Sprintf("    reason: %s", strings.TrimSpace(step.Reasoning))
				lines = append(lines, reasonLine)
			}
		}

		for idx, ln := range lines {
			fmt.Print("\r\033[2K")
			fmt.Println(ln)
			// Avoid extra move on last line
			if idx < len(lines)-1 {
				// nothing extra; Println already moved cursor
			}
		}
	}

	redraw()

	for {
		key := lib.ReadKey("")
		switch key {
		case "esc":
			clearScreen()
			return nil, "cancel", "", nil
		case "enter":
			clearScreen()
			var steps []PlanStep
			for i, s := range plan.Steps {
				if selected[i] {
					steps = append(steps, s)
				}
			}
			return steps, "execute", "", nil
		case "down":
			if cursor < len(plan.Steps)-1 {
				cursor++
				redraw()
			}
		case "up":
			if cursor > 0 {
				cursor--
				redraw()
			}
		case "a":
			all := true
			for _, s := range selected {
				if !s {
					all = false
					break
				}
			}
			for i := range selected {
				selected[i] = !all
			}
			redraw()
		case "space":
			selected[cursor] = !selected[cursor]
			redraw()
		case "r":
			clearScreen()
			feedback, err := readLinePrompt("Describe plan adjustments (blank keeps current): ", input)
			if err != nil {
				return nil, "cancel", "", nil
			}
			return nil, "replan", strings.TrimSpace(feedback), nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			stepNum := int(key[0] - '0')
			if stepNum > 0 && stepNum <= len(plan.Steps) {
				idx := stepNum - 1
				selected[idx] = !selected[idx]
				redraw()
			}
		default:
			// ignore
		}
	}
}

func confirmPlan(r io.Reader) (bool, error) {
	if r == nil {
		r = os.Stdin
	}
	fmt.Print(config.Colors.Info.Sprint("Execute this plan? [y/N]: "))
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}

func parsePlan(raw string) (PlanResult, error) {
	jsonBlob, err := extractJSON(raw)
	if err != nil {
		return PlanResult{}, err
	}

	var payload struct {
		Steps []PlanStep `json:"steps"`
	}
	if err := json.Unmarshal([]byte(jsonBlob), &payload); err != nil {
		return PlanResult{}, err
	}

	for i := range payload.Steps {
		if payload.Steps[i].StepNumber == 0 {
			payload.Steps[i].StepNumber = i + 1
		}
	}

	return PlanResult{Steps: payload.Steps, Raw: raw}, nil
}

func extractJSON(raw string) (string, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return "", errors.New("no JSON object found")
	}
	return raw[start : end+1], nil
}

func readLinePrompt(prompt string, r io.Reader) (string, error) {
	if r == nil {
		r = os.Stdin
	}
	fmt.Print(config.Colors.Info.Sprint(prompt))
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
