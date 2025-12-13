package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/ergochat/readline"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"golang.org/x/term"
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
			// Treat as a user cancel so callers can uniformly print "(canceled)" and continue.
			return "", lib.NewUserCancelError("canceled by user")
		case "setplan":
			editedPlan, perr := parsePlan(feedback)
			if perr != nil {
				return "", fmt.Errorf("failed to parse edited plan: %w", perr)
			}
			currentPlan = editedPlan
			fmt.Println(config.Colors.Dim.Sprint("Edited plan ready for review:"))
			continue
		case "replan":
			if strings.TrimSpace(feedback) == "" {
				continue
			}
			currentPlan, err = GeneratePlan(ctx, cfg, prompt, feedback)
			if err != nil {
				return "", err
			}
			// Make the replan visible before showing the approval UI again.
			fmt.Println(config.Colors.Dim.Sprint("Updated plan ready for review:"))
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

// RunPlanFlowFunc is a test-hookable entrypoint for plan flows.
// cmd package should call this instead of RunPlanFlow directly.
var RunPlanFlowFunc = RunPlanFlow

// GeneratePlan requests a structured plan from the LLM and parses it. Optional adjustments guide replans.
func GeneratePlan(ctx context.Context, cfg *config.Config, prompt string, adjustments string) (PlanResult, error) {
	userPrompt := fmt.Sprintf("Task: %s\nReturn JSON: {\"steps\":[{\"step_number\":1,\"action\":\"...\",\"reasoning\":\"...\",\"required_tools\":[\"kubectl\"]}]} Only JSON.", strings.TrimSpace(prompt))
	if strings.TrimSpace(adjustments) != "" {
		userPrompt += "\nAdjustments: " + strings.TrimSpace(adjustments)
	}

	// Codex-style plan-mode spinner message + minimal checklist while the plan is generated.
	origSpinnerOverride := cfg.SpinnerMessageOverride
	cfg.SpinnerMessageOverride = "Asking clarifying questions…"
	spinnerManager := lib.GetSpinnerManager(cfg)
	spinnerManager.SetDetailsLines([]string{
		"  └ ☐ Drafting plan",
	})
	defer func() {
		cfg.SpinnerMessageOverride = origSpinnerOverride
		spinnerManager.ClearDetailsLines()
	}()

	// Plan generation returns structured JSON that is only useful for debugging.
	// Suppress printing in normal mode so users only see the interactive plan UI.
	origSuppress := cfg.SuppressContentPrint
	origSuppressTools := cfg.SuppressToolPrint
	if !logger.DEBUG {
		cfg.SuppressContentPrint = true
		cfg.SuppressToolPrint = true
	}
	raw, err := RequestWithSystem(cfg, planSystemPrompt, userPrompt, false, false)
	cfg.SuppressContentPrint = origSuppress
	cfg.SuppressToolPrint = origSuppressTools
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

	renderer := newPlanProgressRenderer(cfg, plan.Steps)
	restoreSink := SetPlanProgressSink(renderer.Handle)
	defer restoreSink()
	renderer.Init()
	defer renderer.Clear()

	origSpinnerOverride := cfg.SpinnerMessageOverride
	cfg.SpinnerMessageOverride = "Executing plan…"
	defer func() { cfg.SpinnerMessageOverride = origSpinnerOverride }()

	origHideToolBlocksWhenHidden := cfg.HideToolBlocksWhenDetailsHidden
	cfg.HideToolBlocksWhenDetailsHidden = true
	defer func() { cfg.HideToolBlocksWhenDetailsHidden = origHideToolBlocksWhenHidden }()

	systemPrompt := buildExecutionSystemPrompt(originalPrompt, plan)
	stepOutputs := make(map[int]string, len(plan.Steps))

	for _, step := range plan.Steps {
		emitPlanProgress(PlanProgressEvent{
			Kind:       PlanProgressStepStarted,
			StepNumber: step.StepNumber,
			StepAction: step.Action,
		})
		stepPrompt := fmt.Sprintf("Execute step %d: %s\nReason: %s\nOriginal request: %s", step.StepNumber, strings.TrimSpace(step.Action), strings.TrimSpace(step.Reasoning), originalPrompt)

		// We render step output inside the plan spinner (and then replay it once at the end),
		// so suppress all Chat printing for this call.
		origSuppressContent := cfg.SuppressContentPrint
		cfg.SuppressContentPrint = true
		resp, err := RequestWithSystem(cfg, systemPrompt, stepPrompt, false, true)
		cfg.SuppressContentPrint = origSuppressContent
		if err != nil {
			emitPlanProgress(PlanProgressEvent{
				Kind:       PlanProgressStepFailed,
				StepNumber: step.StepNumber,
				StepAction: step.Action,
				Err:        err.Error(),
			})
			if lib.IsUserCancel(err) {
				return "", err
			}
			return "", fmt.Errorf("step %d failed: %w", step.StepNumber, err)
		}

		resp = strings.TrimSpace(resp)
		stepOutputs[step.StepNumber] = resp

		emitPlanProgress(PlanProgressEvent{
			Kind:       PlanProgressStepCompleted,
			StepNumber: step.StepNumber,
			StepAction: step.Action,
		})

		emitPlanProgress(PlanProgressEvent{
			Kind:       PlanProgressStepOutput,
			StepNumber: step.StepNumber,
			StepAction: step.Action,
			Output:     resp,
		})
	}

	// Final message: replay all step results for scrollback, then provide a concise wrap-up.
	var replay strings.Builder
	replay.WriteString("## Results\n\n")
	for _, step := range plan.Steps {
		action := strings.TrimSpace(step.Action)
		replay.WriteString(fmt.Sprintf("%d. %s\n\n", step.StepNumber, action))
		out := strings.TrimSpace(stepOutputs[step.StepNumber])
		if out == "" {
			out = "(no output)"
		}
		replay.WriteString(out)
		replay.WriteString("\n\n")
	}

	finalPrompt := fmt.Sprintf(
		"Original request:\n%s\n\nStep results:\n%s\n\nNow think hard and provide a final answer.\nConstraints:\n- Do NOT restate the plan.\n- You may reference step results, but do not paste them verbatim.\n- If something failed or is ambiguous, say what and suggest the next best command/check.\n",
		strings.TrimSpace(originalPrompt),
		replay.String(),
	)

	origSuppressContent := cfg.SuppressContentPrint
	cfg.SuppressContentPrint = true
	finalResp, err := RequestWithSystem(cfg, systemPrompt, finalPrompt, false, true)
	cfg.SuppressContentPrint = origSuppressContent
	if err != nil {
		logger.Log("warn", "Failed to generate final plan wrap-up: %v", err)
		return replay.String(), nil
	}
	if strings.TrimSpace(finalResp) == "" {
		return replay.String(), nil
	}

	var out strings.Builder
	out.WriteString(replay.String())
	out.WriteString("## Summary\n\n")
	out.WriteString(strings.TrimSpace(finalResp))
	out.WriteString("\n")
	return out.String(), nil
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

	// If we aren't interactive, avoid ANSI/cursor control sequences and use a simple typed selector.
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return selectPlanStepsNonTTY(cfg, plan, input)
	}

	// Hide cursor while the interactive plan UI is active (prevents a distracting cursor on the last line).
	fmt.Fprint(os.Stdout, "\x1b[?25l")
	defer fmt.Fprint(os.Stdout, "\x1b[?25h")

	selected := make([]bool, len(plan.Steps))
	for i := range selected {
		selected[i] = true
	}
	cursor := 0

	visibleLen := func(s string) int {
		inEscape := false
		length := 0
		for _, r := range s {
			if r == '\033' {
				inEscape = true
				continue
			}
			if inEscape {
				if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
					inEscape = false
				}
				continue
			}
			length++
		}
		return length
	}

	countPhysicalLines := func(lines []string) int {
		width := 80
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
			width = w
		}
		total := 0
		for _, ln := range lines {
			vlen := visibleLen(ln)
			if vlen <= 0 {
				total++
				continue
			}
			wrapped := (vlen - 1) / width
			total += 1 + wrapped
		}
		return total
	}

	renderedPhysicalLines := 0
	renderBlock := func(lines []string) {
		// Repaint only this block in-place (preserves scrollback/history above it).
		// We count physical lines to handle wrapped content.
		if renderedPhysicalLines > 0 {
			fmt.Printf("\033[%dA", renderedPhysicalLines)
			// Clear the full previously-rendered physical block.
			for i := 0; i < renderedPhysicalLines; i++ {
				fmt.Print("\r\033[2K\n")
			}
			fmt.Printf("\033[%dA", renderedPhysicalLines)
		}
		for _, ln := range lines {
			fmt.Print("\r\033[2K")
			fmt.Print(ln)
			fmt.Print("\n")
		}
		renderedPhysicalLines = countPhysicalLines(lines)
	}
	clearRenderedBlock := func() {
		if renderedPhysicalLines <= 0 {
			return
		}
		fmt.Printf("\033[%dA", renderedPhysicalLines)
		for i := 0; i < renderedPhysicalLines; i++ {
			fmt.Print("\r\033[2K\n")
		}
		renderedPhysicalLines = 0
	}

	redraw := func() {
		lines := renderPlanPickerLines(cfg, plan, selected, cursor)
		renderBlock(lines)
	}

	redraw()

	for {
		key := lib.ReadKey("")
		switch key {
		case "esc":
			return nil, "cancel", "", nil
		case "enter":
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
		case "e":
			clearRenderedBlock()
			// Show cursor while handing control to the editor.
			fmt.Fprint(os.Stdout, "\x1b[?25h")
			editedRaw, canceled, err := editPlanInEditor(cfg, plan)
			if err != nil {
				return nil, "cancel", "", err
			}
			if canceled {
				return nil, "cancel", "", nil
			}
			return nil, "setplan", editedRaw, nil
		case "r":
			// Clear current plan UI so the adjustments prompt and next plan render cleanly.
			clearRenderedBlock()
			// Show cursor for the prompt input.
			fmt.Fprint(os.Stdout, "\x1b[?25h")
			feedback, err := readLineWithHistory(cfg, "Describe plan adjustments (blank keeps current): ")
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

func selectPlanStepsNonTTY(cfg *config.Config, plan PlanResult, input io.Reader) ([]PlanStep, string, string, error) {
	renderPlan(cfg, plan)

	for {
		raw, err := readLinePrompt("Select steps [all] (e.g. 1,3-5 | all | none | e edit | r replan | c cancel): ", input)
		if err != nil {
			return nil, "cancel", "", err
		}
		s := strings.TrimSpace(strings.ToLower(raw))
		if s == "" || s == "all" {
			return plan.Steps, "execute", "", nil
		}
		if s == "none" {
			return nil, "execute", "", nil
		}
		if s == "c" || s == "cancel" {
			return nil, "cancel", "", nil
		}
		if s == "r" || s == "replan" {
			feedback, err := readLinePrompt("Describe plan adjustments (blank keeps current): ", input)
			if err != nil {
				return nil, "cancel", "", err
			}
			return nil, "replan", strings.TrimSpace(feedback), nil
		}
		if s == "e" || s == "edit" {
			editedRaw, canceled, err := editPlanInEditor(cfg, plan)
			if err != nil {
				return nil, "cancel", "", err
			}
			if canceled {
				return nil, "cancel", "", nil
			}
			return nil, "setplan", editedRaw, nil
		}

		selected, parseErr := parseStepSelection(s, len(plan.Steps))
		if parseErr != nil {
			fmt.Println(config.Colors.Warn.Sprintf("Invalid selection: %v", parseErr))
			continue
		}

		var steps []PlanStep
		for i, st := range plan.Steps {
			if selected[i] {
				steps = append(steps, st)
			}
		}
		return steps, "execute", "", nil
	}
}

func parseStepSelection(s string, n int) ([]bool, error) {
	if n <= 0 {
		return nil, errors.New("no steps")
	}
	out := make([]bool, n)
	parts := strings.Split(s, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "-") {
			bounds := strings.SplitN(p, "-", 2)
			if len(bounds) != 2 {
				return nil, fmt.Errorf("bad range %q", p)
			}
			a, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("bad range %q", p)
			}
			if a > b {
				a, b = b, a
			}
			if a < 1 || b < 1 || a > n || b > n {
				return nil, fmt.Errorf("range %d-%d out of bounds (1-%d)", a, b, n)
			}
			for i := a; i <= b; i++ {
				out[i-1] = true
			}
			continue
		}
		i, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("bad step %q", p)
		}
		if i < 1 || i > n {
			return nil, fmt.Errorf("step %d out of bounds (1-%d)", i, n)
		}
		out[i-1] = true
	}
	return out, nil
}

func readLineWithHistory(cfg *config.Config, prompt string) (string, error) {
	// If history is disabled or unavailable, fall back to simple prompt.
	if cfg == nil || cfg.DisableHistory || strings.TrimSpace(cfg.HistoryFile) == "" {
		return readLinePrompt(prompt, nil)
	}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:      config.Colors.Info.Sprint(prompt),
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		HistoryFile: cfg.HistoryFile,
	})
	if err != nil {
		return readLinePrompt(prompt, nil)
	}
	defer rl.Close()

	line, err := rl.Readline()
	if err != nil && !errors.Is(err, io.EOF) && err != readline.ErrInterrupt {
		return "", err
	}

	line = strings.TrimRight(line, "\r\n")

	return line, nil
}

func editPlanInEditor(cfg *config.Config, plan PlanResult) (string, bool, error) {
	tmp, err := os.CreateTemp("", "quackops-plan-*.json")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(tmp.Name())

	payload := struct {
		Steps []PlanStep `json:"steps"`
	}{Steps: plan.Steps}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		tmp.Close()
		return "", false, err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return "", false, err
	}
	if err := tmp.Close(); err != nil {
		return "", false, err
	}

	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}

	fmt.Println(config.Colors.Dim.Sprintf("Opening %s to edit the plan. Save and close to continue.", editor))
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{editor}
	}
	cmd := exec.Command(parts[0], append(parts[1:], tmp.Name())...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", false, err
	}

	editedBytes, err := os.ReadFile(tmp.Name())
	if err != nil {
		return "", false, err
	}
	edited := strings.TrimSpace(string(editedBytes))
	if edited == "" {
		return "", true, nil
	}

	return edited, false, nil
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
