package llm

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

func TestGeneratePlanParsesJSON(t *testing.T) {
	orig := RequestWithSystem
	t.Cleanup(func() { RequestWithSystem = orig })

	RequestWithSystem = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		return `{"steps":[{"step_number":1,"action":"inspect pods","reasoning":"check status"}]}`, nil
	}

	cfg := config.LoadConfig()
	plan, err := GeneratePlan(context.Background(), cfg, "inspect cluster", "")
	if err != nil {
		t.Fatalf("GeneratePlan returned error: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Action != "inspect pods" {
		t.Fatalf("unexpected action: %s", plan.Steps[0].Action)
	}
}

func TestRunPlanFlowConfirmYesExecutesSteps(t *testing.T) {
	orig := RequestWithSystem
	origSelector := SelectPlanSteps
	t.Cleanup(func() { RequestWithSystem = orig })
	t.Cleanup(func() { SelectPlanSteps = origSelector })

	callCount := 0
	RequestWithSystem = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		callCount++
		if strings.Contains(systemPrompt, "Plan Generator") {
			return `{"steps":[{"step_number":1,"action":"inspect pods","reasoning":"check status"}]}`, nil
		}
		return "done", nil
	}

	SelectPlanSteps = func(cfg *config.Config, plan PlanResult, input io.Reader) ([]PlanStep, string, string, error) {
		return plan.Steps, "execute", "", nil
	}

	cfg := config.LoadConfig()
	result, err := RunPlanFlow(context.Background(), cfg, "inspect cluster", strings.NewReader("y\n"))
	if err != nil {
		t.Fatalf("RunPlanFlow returned error: %v", err)
	}
	if !strings.Contains(result, "## Step results") || !strings.Contains(result, "### Step 1:") {
		t.Fatalf("expected final output to include replayed step results, got: %q", result)
	}
	if !strings.Contains(result, "## Final") {
		t.Fatalf("expected final output to include wrap-up section, got: %q", result)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 calls to RequestWithSystem (plan + step + final), got %d", callCount)
	}
}

func TestRunPlanFlowGuidedReplan(t *testing.T) {
	orig := RequestWithSystem
	origSelector := SelectPlanSteps
	t.Cleanup(func() { RequestWithSystem = orig })
	t.Cleanup(func() { SelectPlanSteps = origSelector })

	call := 0
	RequestWithSystem = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		call++
		if strings.Contains(systemPrompt, "Plan Generator") {
			if call == 1 {
				return `{"steps":[{"step_number":1,"action":"old","reasoning":"r"}]}`, nil
			}
			return `{"steps":[{"step_number":1,"action":"new","reasoning":"r"}]}`, nil
		}
		return "done", nil
	}

	SelectPlanSteps = func(cfg *config.Config, plan PlanResult, input io.Reader) ([]PlanStep, string, string, error) {
		if plan.Steps[0].Action == "old" {
			return nil, "replan", "add logs", nil
		}
		return plan.Steps, "execute", "", nil
	}

	cfg := config.LoadConfig()
	res, err := RunPlanFlow(context.Background(), cfg, "inspect cluster", strings.NewReader(""))
	if err != nil {
		t.Fatalf("RunPlanFlow returned error: %v", err)
	}
	if !strings.Contains(res, "## Step results") || !strings.Contains(res, "### Step 1:") {
		t.Fatalf("expected final output to include replayed step results, got: %q", res)
	}
	if !strings.Contains(res, "## Final") {
		t.Fatalf("expected final output to include wrap-up section, got: %q", res)
	}
	if call != 4 {
		t.Fatalf("expected 4 calls to RequestWithSystem (plan + replan + step + final), got %d", call)
	}
}

func TestRunPlanFlowManualEditReapprovesEditedPlan(t *testing.T) {
	orig := RequestWithSystem
	origSelector := SelectPlanSteps
	t.Cleanup(func() { RequestWithSystem = orig })
	t.Cleanup(func() { SelectPlanSteps = origSelector })

	genCalls := 0
	RequestWithSystem = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		if strings.Contains(systemPrompt, "Plan Generator") {
			genCalls++
			return `{"steps":[{"step_number":1,"action":"old","reasoning":"r"}]}`, nil
		}
		return "done", nil
	}

	SelectPlanSteps = func(cfg *config.Config, plan PlanResult, input io.Reader) ([]PlanStep, string, string, error) {
		if plan.Steps[0].Action == "old" {
			return nil, "setplan", `{"steps":[{"step_number":1,"action":"edited","reasoning":"r"}]}`, nil
		}
		return plan.Steps, "execute", "", nil
	}

	cfg := config.LoadConfig()
	res, err := RunPlanFlow(context.Background(), cfg, "inspect cluster", strings.NewReader(""))
	if err != nil {
		t.Fatalf("RunPlanFlow returned error: %v", err)
	}
	if !strings.Contains(res, "## Step results") || !strings.Contains(res, "### Step 1:") {
		t.Fatalf("expected final output to include replayed step results, got: %q", res)
	}
	if !strings.Contains(res, "## Final") {
		t.Fatalf("expected final output to include wrap-up section, got: %q", res)
	}
	if genCalls != 1 {
		t.Fatalf("expected 1 plan generation call, got %d", genCalls)
	}
}
