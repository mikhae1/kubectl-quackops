package lib

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ergochat/readline"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/themes"
)

// ThemeSelector provides an interactive theme selection interface.
type ThemeSelector struct {
	names []string
}

// NewThemeSelector constructs a selector with available theme names.
func NewThemeSelector() *ThemeSelector {
	return &ThemeSelector{
		names: themes.Names(),
	}
}

// SelectTheme launches an interactive prompt and returns the chosen theme.
func (ts *ThemeSelector) SelectTheme(current string) (string, error) {
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:       config.Colors.Info.Sprint("Choose a theme") + " (Tab to autocomplete): ",
		AutoComplete: ts,
		EOFPrompt:    "exit",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create theme selector: %w", err)
	}
	defer rl.Close()

	fmt.Println()
	fmt.Printf("%s\n", config.Colors.Info.Sprint("Available themes:"))
	for i, name := range ts.names {
		marker := ""
		if strings.EqualFold(name, current) {
			marker = config.Colors.Accent.Sprintf(" (current)")
		}
		fmt.Printf("  %d. %s%s\n", i+1, name, marker)
	}
	fmt.Println(config.Colors.Dim.Sprint("Type a name or number, Enter to confirm, Ctrl+C to cancel."))

	for {
		line, err := rl.ReadLine()
		if err != nil {
			return "", fmt.Errorf("selection cancelled")
		}

		clean := strings.ToLower(strings.TrimSpace(line))
		if clean == "" {
			continue
		}

		if num, err := strconv.Atoi(clean); err == nil {
			if num >= 1 && num <= len(ts.names) {
				return ts.names[num-1], nil
			}
		}

		for _, name := range ts.names {
			if clean == strings.ToLower(name) {
				return name, nil
			}
		}

		fmt.Printf("%s %s\n", config.Colors.Warn.Sprint("Unknown theme."), config.Colors.Dim.Sprintf("Options: %s", strings.Join(ts.names, ", ")))
	}
}

// Do implements readline.AutoCompleter.
func (ts *ThemeSelector) Do(line []rune, pos int) ([][]rune, int) {
	prefix := strings.ToLower(string(line[:pos]))
	var comps [][]rune
	for _, name := range ts.names {
		if prefix == "" || strings.HasPrefix(strings.ToLower(name), prefix) {
			comps = append(comps, []rune(name))
		}
	}
	return comps, len(prefix)
}
