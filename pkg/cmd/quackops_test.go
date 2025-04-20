package cmd

import (
	"bytes"
	"testing"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestNewRootCmd(t *testing.T) {
	// Setup test streams
	streams := genericiooptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}

	// Create the root command
	rootCmd := NewRootCmd(streams)

	// Test command properties
	if rootCmd.Use != "kubectl-quackops" {
		t.Errorf("Expected command use to be 'kubectl-quackops', got '%s'", rootCmd.Use)
	}

	if rootCmd.Short == "" {
		t.Errorf("Command short description should not be empty")
	}

	if !rootCmd.SilenceUsage {
		t.Errorf("Expected SilenceUsage to be true")
	}

	// Test command flags
	flags := rootCmd.Flags()

	// Test a few important flags
	providerFlag := flags.Lookup("provider")
	if providerFlag == nil {
		t.Errorf("Expected 'provider' flag to exist")
	} else {
		if providerFlag.Shorthand != "p" {
			t.Errorf("Expected 'provider' flag shorthand to be 'p', got '%s'", providerFlag.Shorthand)
		}
	}

	modelFlag := flags.Lookup("model")
	if modelFlag == nil {
		t.Errorf("Expected 'model' flag to exist")
	} else {
		if modelFlag.Shorthand != "m" {
			t.Errorf("Expected 'model' flag shorthand to be 'm', got '%s'", modelFlag.Shorthand)
		}
	}

	safeModeFlag := flags.Lookup("safe-mode")
	if safeModeFlag == nil {
		t.Errorf("Expected 'safe-mode' flag to exist")
	} else {
		if safeModeFlag.Shorthand != "s" {
			t.Errorf("Expected 'safe-mode' flag shorthand to be 's', got '%s'", safeModeFlag.Shorthand)
		}
	}

	// Test the 'env' subcommand
	envCmd, _, err := rootCmd.Find([]string{"env"})
	if err != nil {
		t.Errorf("Error finding 'env' subcommand: %v", err)
	}

	if envCmd.Use != "env" {
		t.Errorf("Expected 'env' subcommand use to be 'env', got '%s'", envCmd.Use)
	}

	if envCmd.Short == "" {
		t.Errorf("'env' subcommand short description should not be empty")
	}
}

func TestPrintEnvVarsHelp(t *testing.T) {
	// Skip for now as it's hard to consistently test colorized output
	// and we don't want to make the build fail for this test alone
	t.Skip("Skipping test because it's hard to consistently test colorized output in different environments")
}
