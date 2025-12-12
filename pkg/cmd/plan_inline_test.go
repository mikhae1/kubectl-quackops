package cmd

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
)

func TestProcessUserPromptInlinePlanUsesTrailingQuery(t *testing.T) {
	orig := llm.RunPlanFlowFunc
	t.Cleanup(func() { llm.RunPlanFlowFunc = orig })

	var got string
	llm.RunPlanFlowFunc = func(ctx context.Context, cfg *config.Config, prompt string, r io.Reader) (string, error) {
		got = prompt
		return "", nil
	}

	cfg := config.LoadConfig()
	err := processUserPrompt(cfg, "please help /plan check node pressure", "", 1)
	if err != nil {
		t.Fatalf("processUserPrompt error: %v", err)
	}
	if !strings.Contains(got, "check node pressure") {
		t.Fatalf("expected trailing plan query, got %q", got)
	}
}

func TestProcessUserPromptInlinePlanWithoutTrailingUsesFullWithoutToken(t *testing.T) {
	orig := llm.RunPlanFlowFunc
	t.Cleanup(func() { llm.RunPlanFlowFunc = orig })

	var got string
	llm.RunPlanFlowFunc = func(ctx context.Context, cfg *config.Config, prompt string, r io.Reader) (string, error) {
		got = prompt
		return "", nil
	}

	cfg := config.LoadConfig()
	err := processUserPrompt(cfg, "context before /plan", "", 1)
	if err != nil {
		t.Fatalf("processUserPrompt error: %v", err)
	}
	if !strings.Contains(got, "context before") {
		t.Fatalf("expected full prompt without /plan, got %q", got)
	}
}
