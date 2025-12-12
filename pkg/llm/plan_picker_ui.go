package llm

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"golang.org/x/term"
)

var planPickerANSIRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func planPickerStripANSI(s string) string { return planPickerANSIRe.ReplaceAllString(s, "") }

func planPickerVisibleLen(s string) int { return len([]rune(planPickerStripANSI(s))) }

func planPickerIndent(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func planPickerTermWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return 80
}

func planPickerWrapWords(s string, width int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if width <= 8 {
		return []string{s}
	}

	// Preserve explicit line breaks; wrap each paragraph.
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		words := strings.Fields(para)
		if len(words) == 0 {
			continue
		}
		cur := words[0]
		for _, w := range words[1:] {
			if planPickerVisibleLen(cur)+1+planPickerVisibleLen(w) <= width {
				cur += " " + w
				continue
			}
			out = append(out, cur)
			cur = w
		}
		if strings.TrimSpace(cur) != "" {
			out = append(out, cur)
		}
	}
	return out
}

// renderPlanPickerLines builds the interactive (TTY) plan picker screen as a set of lines,
// pre-wrapped to the current terminal width to avoid unexpected wrapping/flicker.
func renderPlanPickerLines(cfg *config.Config, plan PlanResult, selected []bool, cursor int) []string {
	width := planPickerTermWidth()
	if width < 40 {
		width = 40
	}
	// Safety margin to reduce edge wrapping on some terminals.
	maxWidth := width - 1
	if maxWidth < 30 {
		maxWidth = width
	}

	selectedCount := 0
	for _, v := range selected {
		if v {
			selectedCount++
		}
	}

	// Compute display width for step numbers (may be sparse / non-sequential).
	maxStepNo := 0
	for _, st := range plan.Steps {
		if st.StepNumber > maxStepNo {
			maxStepNo = st.StepNumber
		}
	}
	numWidth := len(strconv.Itoa(maxStepNo))
	if numWidth < 1 {
		numWidth = 1
	}

	lines := make([]string, 0, len(plan.Steps)*3+4)

	header := config.Colors.Accent.Sprint("PLAN") + config.Colors.Dim.Sprintf(" (%d/%d selected)", selectedCount, len(plan.Steps))
	hint := config.Colors.Dim.Sprint("↑/↓ move  Space toggle  a all  e edit  r replan  Enter approve  Esc cancel  1-9 toggle")
	lines = append(lines, header)
	lines = append(lines, hint)
	lines = append(lines, "")

	for i, step := range plan.Steps {
		pointerRaw := "  "
		pointerColored := pointerRaw
		if i == cursor {
			pointerRaw = "› "
			pointerColored = config.Colors.Command.Sprint(pointerRaw)
		}

		box := "[ ]"
		if i >= 0 && i < len(selected) && selected[i] {
			box = "[✔]"
		}

		num := fmt.Sprintf("%*d.", numWidth, step.StepNumber)
		prefixRaw := pointerRaw + box + " " + num + " "
		prefixLen := len([]rune(prefixRaw))
		indent := planPickerIndent(prefixLen)

		action := strings.TrimSpace(step.Action)
		actionWidth := maxWidth - prefixLen
		actionLines := planPickerWrapWords(action, actionWidth)
		if len(actionLines) == 0 {
			actionLines = []string{""}
		}

		// First action line.
		firstLine := pointerColored + box + " " + num + " " + actionLines[0]
		if i == cursor {
			// Subtle emphasis on current row.
			firstLine = pointerColored + box + " " + num + " " + config.Colors.Accent.Sprint(actionLines[0])
		}
		lines = append(lines, firstLine)

		// Continuation action lines.
		for j := 1; j < len(actionLines); j++ {
			if i == cursor {
				lines = append(lines, indent+config.Colors.Accent.Sprint(actionLines[j]))
			} else {
				lines = append(lines, indent+actionLines[j])
			}
		}

		// Details (tools, reasoning) in dim style, aligned under the action column.
		if len(step.RequiredTools) > 0 {
			labelText := "tools:"
			labelStyled := config.Colors.Label.Sprint(labelText)
			valueText := strings.Join(step.RequiredTools, ", ")
			valueWidth := maxWidth - prefixLen - (len([]rune(labelText)) + 1)
			for j, tl := range planPickerWrapWords(valueText, valueWidth) {
				if j == 0 {
					lines = append(lines, indent+labelStyled+" "+config.Colors.Dim.Sprint(tl))
					continue
				}
				lines = append(lines, indent+planPickerIndent(len([]rune(labelText))+1)+config.Colors.Dim.Sprint(tl))
			}
		}

		if reason := strings.TrimSpace(step.Reasoning); reason != "" {
			labelText := "why:"
			labelStyled := config.Colors.Label.Sprint(labelText)
			valueWidth := maxWidth - prefixLen - (len([]rune(labelText)) + 1)
			for j, rl := range planPickerWrapWords(reason, valueWidth) {
				if j == 0 {
					lines = append(lines, indent+labelStyled+" "+config.Colors.Dim.Sprint(rl))
					continue
				}
				lines = append(lines, indent+planPickerIndent(len([]rune(labelText))+1)+config.Colors.Dim.Sprint(rl))
			}
		}
	}

	return lines
}
