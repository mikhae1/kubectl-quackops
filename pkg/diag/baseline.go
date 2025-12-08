package diag

import (
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// BaselineCommands returns a curated set of safe, read-only diagnostic commands
// to quickly capture high-signal cluster state.
// The list will be analyzed by Analyzers to trim it for the first prompt context.
// Supports three levels: minimal (default), standard (+ workloads), comprehensive (+ metrics/policies)
func BaselineCommands(cfg *config.Config) []string {
	// honor a simple toggle; default is enabled in config layer
	if cfg == nil || !cfg.EnableBaseline {
		return nil
	}

	// Normalize level to lowercase
	level := strings.ToLower(strings.TrimSpace(cfg.BaselineLevel))
	if level == "" {
		level = "minimal"
	}

	// Minimal commands (always included)
	c := []string{
		// API server basic health (raw endpoints)
		"kubectl get --raw='/readyz?verbose'",
		"kubectl get --raw='/livez?verbose'",

		// Core inventory
		"kubectl get nodes -o json",
		"kubectl get pods -A -o json",
		"kubectl get deployments -A -o json",
		"kubectl get services -A -o json",
		"kubectl get ingress -A -o json",

		// Endpoints/EndpointSlices to correlate Service→Pod connectivity
		"kubectl get endpoints -A -o json",
		"kubectl get endpointslices -A -o json",

		// Events for recent warnings
		"kubectl get events -A -o json",

		// HPAs for autoscaling diagnostics
		"kubectl get hpa -A -o json",

		// Storage diagnostics
		"kubectl get pvc -A -o json",
		"kubectl get pv -A -o json",
	}

	// Standard level: add StatefulSets, DaemonSets, Jobs, CronJobs
	if level == "standard" || level == "comprehensive" {
		c = append(c,
			"kubectl get statefulsets -A -o json",
			"kubectl get daemonsets -A -o json",
			"kubectl get jobs -A -o json",
			"kubectl get cronjobs -A -o json",
		)
	}

	// Comprehensive level: add metrics, network policies
	if level == "comprehensive" {
		if cfg.BaselineIncludeMetrics {
			c = append(c,
				"kubectl get --raw '/apis/metrics.k8s.io/v1beta1/nodes'",
				"kubectl get --raw '/apis/metrics.k8s.io/v1beta1/pods'",
			)
		}
		c = append(c, "kubectl get networkpolicies -A -o json")
	}

	// Filter defensively against accidental duplicates if callers append us repeatedly
	uniq := make([]string, 0, len(c))
	seen := map[string]bool{}
	for _, cmd := range c {
		key := strings.TrimSpace(cmd)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, key)
	}
	return uniq
}
