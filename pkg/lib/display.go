package lib

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// KubeCtxInfo shows the user which Kubernetes context is currently active
func KubeCtxInfo(cfg *config.Config) error {
	// Execute the context command directly without going through the normal flow
	// that shows output to the user
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	// Get current context
	contextCmd := exec.CommandContext(ctx, cfg.KubectlBinaryPath, "config", "current-context")
	contextOutput, err := contextCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error getting current context: %w", err)
	}
	ctxName := strings.TrimSpace(string(contextOutput))

	// Get cluster info
	clusterCmd := exec.CommandContext(ctx, cfg.KubectlBinaryPath, "cluster-info")
	clusterOutput, err := clusterCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error getting cluster info: %w", err)
	}

	info := strings.TrimSpace(string(clusterOutput))
	if info == "" {
		fmt.Println(color.HiRedString("Current Kubernetes context is empty or not set."))
	} else {
		infoLines := strings.Split(info, "\n")
		fmt.Printf(color.HiYellowString("Using Kubernetes context")+": %s\n%s", ctxName, strings.Join(infoLines[:len(infoLines)-1], "\n"))
	}

	return nil
}
