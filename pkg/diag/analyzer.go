package diag

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Finding represents a concise diagnostic signal extracted from JSON outputs.
type Finding struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Severity string `json:"severity"` // info|warn|error
	Priority int    `json:"priority"` // 1-10, higher = more urgent (0 = not set)
	Summary  string `json:"summary"`
}

// assignPriority calculates priority score (1-10) based on severity and kind
// Higher scores = more urgent
func assignPriority(severity string, kind string, issue string) int {
	base := 0
	switch strings.ToLower(severity) {
	case "error":
		base = 7
	case "warn":
		base = 4
	case "info":
		base = 2
	}

	// Boost priority for critical issues
	if strings.Contains(strings.ToLower(issue), "crashloopbackoff") {
		return 10 // Highest priority
	}
	if strings.Contains(strings.ToLower(issue), "imagepull") {
		return 9
	}
	if kind == "Node" && strings.Contains(issue, "NotReady") {
		return 9 // Node failures are critical
	}
	if kind == "Service" && strings.Contains(issue, "zero endpoints") {
		return 8 // Service connectivity issues are high priority
	}
	if kind == "APIServer" {
		return 8 // API server health issues are critical
	}

	return base
}

// AnalyzePods inspects `kubectl get pods -A -o json` output for common issues.
func AnalyzePods(podsJSON string) []Finding {
	type Pod struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			Phase             string `json:"phase"`
			Reason            string `json:"reason"`
			Message           string `json:"message"`
			ContainerStatuses []struct {
				Name         string         `json:"name"`
				RestartCount int            `json:"restartCount"`
				State        map[string]any `json:"state"`
				LastState    map[string]any `json:"lastState"`
			} `json:"containerStatuses"`
		} `json:"status"`
	}
	type list struct {
		Items []Pod `json:"items"`
	}
	var l list
	if err := json.Unmarshal([]byte(podsJSON), &l); err != nil {
		return nil
	}
	var findings []Finding
	for _, p := range l.Items {
		nsName := fmt.Sprintf("%s/%s", p.Metadata.Namespace, p.Metadata.Name)
		// High restarts
		totalRestarts := 0
		for _, cs := range p.Status.ContainerStatuses {
			totalRestarts += cs.RestartCount
			// CrashLoopBackOff detection via state
			if st, ok := cs.State["waiting"].(map[string]any); ok {
				if r, _ := st["reason"].(string); strings.EqualFold(r, "CrashLoopBackOff") {
					findings = append(findings, Finding{
						Kind:     "Pod",
						ID:       nsName,
						Severity: "error",
						Summary:  fmt.Sprintf("container %s in CrashLoopBackOff", cs.Name),
					})
				}
				if r, _ := st["reason"].(string); strings.Contains(strings.ToLower(r), "imagepull") {
					findings = append(findings, Finding{
						Kind:     "Pod",
						ID:       nsName,
						Severity: "error",
						Summary:  fmt.Sprintf("container %s has image pull issue: %s", cs.Name, r),
					})
				}
			}
		}
		if totalRestarts >= 5 {
			findings = append(findings, Finding{
				Kind:     "Pod",
				ID:       nsName,
				Severity: "warn",
				Summary:  fmt.Sprintf("high restart count: %d", totalRestarts),
			})
		}
		// Pending / scheduling problems
		if strings.EqualFold(p.Status.Phase, "Pending") && p.Status.Reason != "" {
			findings = append(findings, Finding{
				Kind:     "Pod",
				ID:       nsName,
				Severity: "warn",
				Summary:  fmt.Sprintf("pending: %s", p.Status.Reason),
			})
		}
	}
	return findings
}

// AnalyzeServices inspects `kubectl get services -A -o json` and `endpoints/endpointSlices` JSON
// to detect Services with zero endpoints or selector mismatches.
func AnalyzeServices(servicesJSON, endpointsJSON, endpointSlicesJSON string) []Finding {
	type Service struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			Selector map[string]string `json:"selector"`
		} `json:"spec"`
	}
	type Endpoints struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Subsets []struct {
			Addresses []any `json:"addresses"`
		} `json:"subsets"`
	}
	type EndpointSlice struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
		Endpoints []struct{} `json:"endpoints"`
	}
	type listS struct {
		Items []Service `json:"items"`
	}
	type listE struct {
		Items []Endpoints `json:"items"`
	}
	type listES struct {
		Items []EndpointSlice `json:"items"`
	}
	// optional: we may extend to read pods JSON in future for selector checks

	var svcs listS
	var eps listE
	var es listES
	if servicesJSON != "" {
		_ = json.Unmarshal([]byte(servicesJSON), &svcs)
	}
	if endpointsJSON != "" {
		_ = json.Unmarshal([]byte(endpointsJSON), &eps)
	}
	if endpointSlicesJSON != "" {
		_ = json.Unmarshal([]byte(endpointSlicesJSON), &es)
	}

	// Build quick lookup of endpoints and slices by namespace/name
	hasAddresses := map[string]bool{}
	for _, ep := range eps.Items {
		key := ep.Metadata.Namespace + "/" + ep.Metadata.Name
		for _, s := range ep.Subsets {
			if len(s.Addresses) > 0 {
				hasAddresses[key] = true
				break
			}
		}
	}

	// Also mark via EndpointSlices (k8s >=1.21)
	for _, slice := range es.Items {
		// Endpointslice label kubernetes.io/service-name has the service name
		svcName := ""
		if slice.Metadata.Labels != nil {
			svcName = slice.Metadata.Labels["kubernetes.io/service-name"]
		}
		if svcName != "" && len(slice.Endpoints) > 0 {
			key := slice.Metadata.Namespace + "/" + svcName
			hasAddresses[key] = true
		}
	}

	var findings []Finding
	for _, s := range svcs.Items {
		nsName := s.Metadata.Namespace + "/" + s.Metadata.Name
		if len(s.Spec.Selector) == 0 {
			// Headless or externalName services are legitimate; skip them
			continue
		}
		if !hasAddresses[nsName] {
			summary := "service has zero endpoints; check selectors and pod readiness"
			findings = append(findings, Finding{
				Kind:     "Service",
				ID:       nsName,
				Severity: "error",
				Priority: assignPriority("error", "Service", summary),
				Summary:  summary,
			})
		}
	}
	return findings
}

// AnalyzeNodes inspects `kubectl get nodes -o json` for NotReady and pressure conditions.
func AnalyzeNodes(nodesJSON string) []Finding {
	type Node struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	}
	type list struct {
		Items []Node `json:"items"`
	}
	var l list
	if err := json.Unmarshal([]byte(nodesJSON), &l); err != nil {
		return nil
	}
	var f []Finding
	for _, n := range l.Items {
		var notReady, memPressure, diskPressure bool
		for _, c := range n.Status.Conditions {
			switch c.Type {
			case "Ready":
				if strings.ToLower(c.Status) != "true" {
					notReady = true
				}
			case "MemoryPressure":
				if strings.ToLower(c.Status) == "true" {
					memPressure = true
				}
			case "DiskPressure":
				if strings.ToLower(c.Status) == "true" {
					diskPressure = true
				}
			}
		}
		if notReady {
			summary := "NotReady"
			f = append(f, Finding{
				Kind:     "Node",
				ID:       n.Metadata.Name,
				Severity: "error",
				Priority: assignPriority("error", "Node", summary),
				Summary:  summary,
			})
		}
		if memPressure {
			summary := "MemoryPressure"
			f = append(f, Finding{
				Kind:     "Node",
				ID:       n.Metadata.Name,
				Severity: "warn",
				Priority: assignPriority("warn", "Node", summary),
				Summary:  summary,
			})
		}
		if diskPressure {
			summary := "DiskPressure"
			f = append(f, Finding{
				Kind:     "Node",
				ID:       n.Metadata.Name,
				Severity: "warn",
				Priority: assignPriority("warn", "Node", summary),
				Summary:  summary,
			})
		}
	}
	return f
}

// AnalyzeHPAs inspects `kubectl get hpa -A -o json` for failing conditions.
func AnalyzeHPAs(hpaJSON string) []Finding {
	type HPA struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
				Reason string `json:"reason"`
			} `json:"conditions"`
		} `json:"status"`
	}
	type list struct {
		Items []HPA `json:"items"`
	}
	var l list
	if err := json.Unmarshal([]byte(hpaJSON), &l); err != nil {
		return nil
	}
	var f []Finding
	for _, h := range l.Items {
		id := h.Metadata.Namespace + "/" + h.Metadata.Name
		for _, c := range h.Status.Conditions {
			if strings.ToLower(c.Status) == "false" && c.Type != "AbleToScale" {
				msg := c.Type
				if c.Reason != "" {
					msg += ": " + c.Reason
				}
				f = append(f, Finding{
					Kind:     "HPA",
					ID:       id,
					Severity: "warn",
					Priority: assignPriority("warn", "HPA", msg),
					Summary:  msg,
				})
			}
		}
	}
	return f
}

// AnalyzeAPIServerHealth inspects the output of /readyz or /livez for failing checks.
func AnalyzeAPIServerHealth(raw string, kind string) []Finding {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	// Lines with "[-]" denote failing checks in verbose mode
	lines := strings.Split(raw, "\n")
	var failing []string
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "[-]") {
			failing = append(failing, strings.TrimSpace(ln))
		}
	}
	if len(failing) == 0 {
		return nil
	}
	summary := strings.Join(failing, "; ")
	return []Finding{{
		Kind:     "APIServer",
		ID:       kind,
		Severity: "warn",
		Priority: assignPriority("warn", "APIServer", summary),
		Summary:  summary,
	}}
}

// FormatFindings returns a compact human-readable list for inclusion in RAG prompts.
func FormatFindings(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("-")
		b.WriteString(" [")
		b.WriteString(strings.ToUpper(f.Severity))
		b.WriteString("] ")
		b.WriteString(f.Kind)
		b.WriteString(" ")
		b.WriteString(f.ID)
		b.WriteString(": ")
		b.WriteString(f.Summary)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// AnalyzeDeployments inspects deployments for rollout issues.
func AnalyzeDeployments(deploymentsJSON string) []Finding {
	type Deployment struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Status struct {
			Replicas          int `json:"replicas"`
			AvailableReplicas int `json:"availableReplicas"`
			UpdatedReplicas   int `json:"updatedReplicas"`
			Conditions        []struct {
				Type    string `json:"type"`
				Status  string `json:"status"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"conditions"`
		} `json:"status"`
	}
	type list struct {
		Items []Deployment `json:"items"`
	}
	var l list
	if err := json.Unmarshal([]byte(deploymentsJSON), &l); err != nil {
		return nil
	}
	var f []Finding
	for _, d := range l.Items {
		id := d.Metadata.Namespace + "/" + d.Metadata.Name
		if d.Status.Replicas > 0 && d.Status.AvailableReplicas == 0 {
			f = append(f, Finding{Kind: "Deployment", ID: id, Severity: "warn", Summary: "no available replicas"})
		}
		for _, c := range d.Status.Conditions {
			if c.Type == "Available" && strings.ToLower(c.Status) == "false" {
				msg := c.Type
				if c.Reason != "" {
					msg += ": " + c.Reason
				}
				f = append(f, Finding{Kind: "Deployment", ID: id, Severity: "warn", Summary: msg})
			}
			if c.Type == "Progressing" && strings.ToLower(c.Status) == "false" {
				msg := c.Type
				if c.Reason != "" {
					msg += ": " + c.Reason
				}
				f = append(f, Finding{Kind: "Deployment", ID: id, Severity: "warn", Summary: msg})
			}
		}
	}
	return f
}

// AnalyzeServiceSelectorMatches warns when a Service selector matches zero Pods.
func AnalyzeServiceSelectorMatches(servicesJSON, podsJSON string) []Finding {
	type Service struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			Selector map[string]string `json:"selector"`
		} `json:"spec"`
	}
	type Pod struct {
		Metadata struct {
			Name      string            `json:"name"`
			Namespace string            `json:"namespace"`
			Labels    map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	type listS struct {
		Items []Service `json:"items"`
	}
	type listP struct {
		Items []Pod `json:"items"`
	}
	var svcs listS
	var pods listP
	if err := json.Unmarshal([]byte(servicesJSON), &svcs); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(podsJSON), &pods); err != nil {
		return nil
	}
	var f []Finding
	for _, s := range svcs.Items {
		if len(s.Spec.Selector) == 0 {
			continue
		}
		id := s.Metadata.Namespace + "/" + s.Metadata.Name
		matches := 0
		for _, p := range pods.Items {
			if p.Metadata.Namespace != s.Metadata.Namespace {
				continue
			}
			if selectorMatches(s.Spec.Selector, p.Metadata.Labels) {
				matches++
			}
		}
		if matches == 0 {
			f = append(f, Finding{Kind: "Service", ID: id, Severity: "warn", Summary: "no pods match the service selector"})
		}
	}
	return f
}

// AnalyzeIngress validates that referenced backend services exist (basic check).
func AnalyzeIngress(ingressJSON, servicesJSON string) []Finding {
	type Ingress struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			Rules []struct {
				HTTP struct {
					Paths []struct {
						Backend struct {
							Service struct {
								Name string `json:"name"`
							} `json:"service"`
						} `json:"backend"`
					} `json:"paths"`
				} `json:"http"`
			} `json:"rules"`
		} `json:"spec"`
	}
	type Service struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	type listIng struct {
		Items []Ingress `json:"items"`
	}
	type listSvc struct {
		Items []Service `json:"items"`
	}
	var ings listIng
	var svcs listSvc
	if err := json.Unmarshal([]byte(ingressJSON), &ings); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(servicesJSON), &svcs); err != nil {
		return nil
	}
	svcExists := map[string]bool{}
	for _, s := range svcs.Items {
		svcExists[s.Metadata.Namespace+"/"+s.Metadata.Name] = true
	}
	var f []Finding
	for _, ing := range ings.Items {
		id := ing.Metadata.Namespace + "/" + ing.Metadata.Name
		for _, r := range ing.Spec.Rules {
			for _, p := range r.HTTP.Paths {
				name := p.Backend.Service.Name
				if name == "" {
					continue
				}
				key := ing.Metadata.Namespace + "/" + name
				if !svcExists[key] {
					f = append(f, Finding{Kind: "Ingress", ID: id, Severity: "error", Summary: "backend service not found: " + name})
				}
			}
		}
	}
	return f
}

// AnalyzePVCsPVs surfaces unbound PVCs and failed PVs.
func AnalyzePVCsPVs(pvcJSON, pvJSON string) []Finding {
	type PVC struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	type PV struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	type listPVC struct {
		Items []PVC `json:"items"`
	}
	type listPV struct {
		Items []PV `json:"items"`
	}
	var pvcs listPVC
	var pvs listPV
	if err := json.Unmarshal([]byte(pvcJSON), &pvcs); err != nil {
		return nil
	}
	if err := json.Unmarshal([]byte(pvJSON), &pvs); err != nil {
		return nil
	}
	var f []Finding
	for _, c := range pvcs.Items {
		if strings.ToLower(c.Status.Phase) == "pending" {
			f = append(f, Finding{Kind: "PVC", ID: c.Metadata.Namespace + "/" + c.Metadata.Name, Severity: "warn", Summary: "pending"})
		}
	}
	for _, v := range pvs.Items {
		ph := strings.ToLower(v.Status.Phase)
		if ph == "failed" || ph == "released" {
			f = append(f, Finding{Kind: "PV", ID: v.Metadata.Name, Severity: "warn", Summary: "phase: " + v.Status.Phase})
		}
	}
	return f
}

func selectorMatches(sel, labels map[string]string) bool {
	if len(sel) == 0 {
		return true
	}
	if labels == nil {
		return false
	}
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}
