package diag

import (
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// BaselineCommands returns a curated set of safe, read-only diagnostic commands
// to quickly capture high-signal cluster state. All commands are read-only.
// The list intentionally favors JSON output for machine analysis.
func BaselineCommands(cfg *config.Config) []string {
	// honor a simple toggle; default is enabled in config layer
	if cfg == nil || !cfg.EnableBaseline {
		return nil
	}

	c := []string{
		// API server basic health (raw endpoints)
		"kubectl get --raw /readyz?verbose",
		"kubectl get --raw /livez?verbose",
		// Core inventory (JSON for analyzers)
		"kubectl get nodes -o json",
		"kubectl get pods -A -o json",
		"kubectl get deployments -A -o json",
		"kubectl get services -A -o json",
		"kubectl get ingress -A -o json",
		// Endpoints/EndpointSlices to correlate Service→Pod connectivity
		"kubectl get endpoints -A -o json",
		"kubectl get endpointslices -A -o json",
		// Events for recent warnings (windowing applied in analyzer)
		"kubectl get events -A -o json",
		// HPAs for autoscaling diagnostics
		"kubectl get hpa -A -o json",
		// Storage diagnostics
		"kubectl get pvc -A -o json",
		"kubectl get pv -A -o json",
		// Try metrics-server if available (non-fatal if missing)
		"kubectl get --raw /apis/metrics.k8s.io/v1beta1/nodes",
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
