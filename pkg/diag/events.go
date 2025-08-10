package diag

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// K8sEvent is a minimal subset of corev1.Event for summarization without importing k8s api.
type K8sEvent struct {
	Type           string    `json:"type"`
	Reason         string    `json:"reason"`
	Message        string    `json:"message"`
	Count          int       `json:"count"`
	LastTimestamp  time.Time `json:"lastTimestamp"`
	FirstTimestamp time.Time `json:"firstTimestamp"`
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"involvedObject"`
}

// SummarizeEvents reduces a JSON response from `kubectl get events -A -o json` into
// a concise list of recent Warning/Normal items, grouped and sorted by recency.
func SummarizeEvents(eventsJSON string, warnOnly bool, window time.Duration, maxItems int) string {
	type list struct {
		Items []K8sEvent `json:"items"`
	}
	if strings.TrimSpace(eventsJSON) == "" {
		return ""
	}
	var l list
	if err := json.Unmarshal([]byte(eventsJSON), &l); err != nil {
		return ""
	}

	// Filter by window and type
	cutoff := time.Now().Add(-window)
	filtered := make([]K8sEvent, 0, len(l.Items))
	for _, e := range l.Items {
		t := e.LastTimestamp
		if t.IsZero() {
			t = e.FirstTimestamp
		}
		if !t.IsZero() && t.Before(cutoff) {
			continue
		}
		if warnOnly && strings.ToUpper(e.Type) != "WARNING" {
			continue
		}
		filtered = append(filtered, e)
	}

	sort.Slice(filtered, func(i, j int) bool {
		ti := filtered[i].LastTimestamp
		if ti.IsZero() {
			ti = filtered[i].FirstTimestamp
		}
		tj := filtered[j].LastTimestamp
		if tj.IsZero() {
			tj = filtered[j].FirstTimestamp
		}
		return ti.After(tj)
	})

	if maxItems <= 0 || maxItems > len(filtered) {
		maxItems = len(filtered)
	}

	var b strings.Builder
	for idx := 0; idx < maxItems; idx++ {
		e := filtered[idx]
		ts := e.LastTimestamp
		if ts.IsZero() {
			ts = e.FirstTimestamp
		}
		// One-line compact entry per event
		b.WriteString("[")
		b.WriteString(strings.ToUpper(e.Type))
		b.WriteString("] ")
		b.WriteString(ts.Format(time.RFC3339))
		b.WriteString(" ")
		b.WriteString(e.InvolvedObject.Kind)
		if e.InvolvedObject.Namespace != "" {
			b.WriteString("/")
			b.WriteString(e.InvolvedObject.Namespace)
		}
		b.WriteString("/")
		b.WriteString(e.InvolvedObject.Name)
		if e.Reason != "" {
			b.WriteString(" ")
			b.WriteString(e.Reason)
		}
		if e.Count > 1 {
			b.WriteString(" (x")
			b.WriteString(strconv.Itoa(e.Count))
			b.WriteString(")")
		}
		if e.Message != "" {
			b.WriteString(" — ")
			b.WriteString(e.Message)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
