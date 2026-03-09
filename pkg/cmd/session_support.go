package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ergochat/readline"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	quacksession "github.com/mikhae1/kubectl-quackops/pkg/session"
	"github.com/spf13/cobra"
)

func newSessionCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage saved sessions",
	}
	cmd.AddCommand(newSessionListCommand(cfg))
	cmd.AddCommand(newSessionDeleteCommand(cfg))
	return cmd
}

func newSessionListCommand(cfg *config.Config) *cobra.Command {
	var maxCount int
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions, err := listSavedSessions(cfg)
			if err != nil {
				return err
			}
			if maxCount > 0 && len(sessions) > maxCount {
				sessions = sessions[:maxCount]
			}

			switch outputFormat {
			case "json":
				payload, err := json.MarshalIndent(sessions, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal sessions: %w", err)
				}
				fmt.Println(string(payload))
			default:
				if len(sessions) == 0 {
					fmt.Println("No saved sessions.")
					return nil
				}
				fmt.Println(formatSavedSessionsTable(sessions))
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&maxCount, "max-count", "n", 0, "Limit to N most recent sessions")
	cmd.Flags().StringVar(&outputFormat, "format", "table", "Output format: table or json")
	return cmd
}

func newSessionDeleteCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a saved session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := requireSessionStore(cfg)
			if err != nil {
				return err
			}
			if err := store.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted session %s\n", args[0])
			return nil
		},
	}
}

func sessionStore(cfg *config.Config) *quacksession.Store {
	if cfg == nil || strings.TrimSpace(cfg.SessionsDir) == "" {
		return nil
	}
	return quacksession.NewStore(cfg.SessionsDir)
}

func requireSessionStore(cfg *config.Config) (*quacksession.Store, error) {
	store := sessionStore(cfg)
	if store == nil {
		return nil, fmt.Errorf("sessions directory is not configured")
	}
	return store, nil
}

func persistCurrentSession(cfg *config.Config) error {
	store := sessionStore(cfg)
	if store == nil {
		return nil
	}

	snapshot := quacksession.FromConfig(cfg)
	if snapshot.IsEmpty() {
		return nil
	}

	if strings.TrimSpace(snapshot.ID) == "" {
		sessionID, err := quacksession.NewSessionID()
		if err != nil {
			return err
		}
		cfg.CurrentSessionID = sessionID
		snapshot.ID = sessionID
	}
	if cfg.CurrentSessionCreatedAt.IsZero() {
		cfg.CurrentSessionCreatedAt = time.Now()
	}
	snapshot.CreatedAt = cfg.CurrentSessionCreatedAt

	if err := store.Save(snapshot); err != nil {
		return err
	}

	return enforceMaxSavedSessions(cfg, store)
}

func enforceMaxSavedSessions(cfg *config.Config, store *quacksession.Store) error {
	if cfg == nil || store == nil || cfg.MaxSavedSessions <= 0 {
		return nil
	}

	sessions, err := store.List()
	if err != nil {
		return err
	}
	if len(sessions) <= cfg.MaxSavedSessions {
		return nil
	}

	for _, stale := range sessions[cfg.MaxSavedSessions:] {
		if err := store.Delete(stale.ID); err != nil {
			return fmt.Errorf("prune session %s: %w", stale.ID, err)
		}
	}

	return nil
}

func resetConversationContext(cfg *config.Config) {
	cfg.ChatMessages = nil
	cfg.StoredUserCmdResults = nil
	cfg.LastOutgoingTokens = 0
	cfg.LastIncomingTokens = 0
	cfg.LastTextPrompt = ""
	cfg.UserMsgCount = 0
	cfg.SelectedPrompt = ""
	cfg.MCPPromptServer = ""
}

func startFreshSession(cfg *config.Config) {
	resetConversationContext(cfg)
	cfg.SessionOutgoingTokens = 0
	cfg.SessionIncomingTokens = 0
	cfg.SessionHistory = nil
	cfg.CurrentSessionID = ""
	cfg.CurrentSessionCreatedAt = time.Time{}
}

func listSavedSessions(cfg *config.Config) ([]quacksession.Info, error) {
	store, err := requireSessionStore(cfg)
	if err != nil {
		return nil, err
	}
	return store.List()
}

func loadSavedSession(cfg *config.Config, sessionID string) error {
	store, err := requireSessionStore(cfg)
	if err != nil {
		return err
	}
	snapshot, err := store.Load(strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	return snapshot.Apply(cfg)
}

func promptForSessionSelection(cfg *config.Config) (bool, error) {
	sessions, err := listSavedSessions(cfg)
	if err != nil {
		return false, err
	}
	if len(sessions) == 0 {
		fmt.Println(config.Colors.Dim.Sprint("No saved sessions found."))
		return false, nil
	}

	const pageSize = 10
	page := 0
	maxPage := (len(sessions) - 1) / pageSize
	reader := bufio.NewReader(os.Stdin)
	completer := &sessionSelectionCompleter{sessions: sessions}
	var rl *readline.Instance
	if selector, rlErr := readline.NewFromConfig(&readline.Config{
		Prompt:       config.Colors.Info.Sprint("Select session") + " (Tab to autocomplete): ",
		AutoComplete: completer,
		EOFPrompt:    "cancel",
	}); rlErr == nil {
		rl = selector
		defer rl.Close()
		fmt.Println(config.Colors.Dim.Sprint("Tip: press Tab to autocomplete session IDs and navigation commands."))
	}
	idIndex := make(map[string]string, len(sessions))
	for _, s := range sessions {
		idIndex[strings.ToLower(strings.TrimSpace(s.ID))] = s.ID
	}

	for {
		start := page * pageSize
		fmt.Println(formatSavedSessionsTablePage(sessions, start, pageSize))
		fmt.Printf(
			"Select session number or ID (%d-%d of %d) [%s]: ",
			start+1,
			minInt(start+pageSize, len(sessions)),
			len(sessions),
			"n next, p prev, q cancel",
		)

		var selection string
		if rl != nil {
			rl.SetPrompt(config.Colors.Info.Sprint("session> "))
			line, readErr := rl.ReadLine()
			if readErr != nil {
				if errors.Is(readErr, io.EOF) || errors.Is(readErr, readline.ErrInterrupt) {
					fmt.Println(config.Colors.Dim.Sprint("Session selection cancelled."))
					return false, nil
				}
				return false, fmt.Errorf("read session selection: %w", readErr)
			}
			selection = line
		} else {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return false, fmt.Errorf("read session selection: %w", readErr)
			}
			selection = line
		}
		selection = strings.TrimSpace(selection)
		lower := strings.ToLower(selection)

		switch lower {
		case "", "q", "quit", "exit":
			fmt.Println(config.Colors.Dim.Sprint("Session selection cancelled."))
			return false, nil
		case "n", "next", "j":
			if page < maxPage {
				page++
			} else {
				fmt.Println(config.Colors.Dim.Sprint("Already at newest/last page."))
			}
			continue
		case "p", "prev", "previous", "k":
			if page > 0 {
				page--
			} else {
				fmt.Println(config.Colors.Dim.Sprint("Already at first page."))
			}
			continue
		}

		sessionID := selection
		if index, convErr := strconv.Atoi(selection); convErr == nil {
			if index < 1 || index > len(sessions) {
				fmt.Printf("%s\n", config.Colors.Warn.Sprintf("Invalid session number %d", index))
				continue
			}
			sessionID = sessions[index-1].ID
		} else if canonical, ok := idIndex[strings.ToLower(strings.TrimSpace(selection))]; ok {
			sessionID = canonical
		} else if embeddedID := findSessionIDInSelection(sessions, selection); embeddedID != "" {
			sessionID = embeddedID
		} else {
			candidates := closeSessionIDs(sessions, selection, 3)
			if len(candidates) > 0 {
				fmt.Printf("%s %s\n", config.Colors.Warn.Sprint("Unknown session ID."), config.Colors.Dim.Sprintf("Did you mean: %s", strings.Join(candidates, ", ")))
			} else {
				fmt.Printf("%s\n", config.Colors.Warn.Sprint("Unknown session ID."))
			}
			continue
		}

		if err := loadSavedSession(cfg, sessionID); err != nil {
			return false, err
		}

		fmt.Printf("%s %s\n", config.Colors.Info.Sprint("Loaded session"), config.Colors.Accent.Sprint(sessionID))
		return true, nil
	}
}

func formatSavedSessionsTable(sessions []quacksession.Info) string {
	return formatSavedSessionsTablePage(sessions, 0, len(sessions))
}

func formatSavedSessionsTablePage(sessions []quacksession.Info, start int, count int) string {
	if len(sessions) == 0 {
		return ""
	}
	if start < 0 {
		start = 0
	}
	if count <= 0 {
		count = len(sessions)
	}
	end := start + count
	if end > len(sessions) {
		end = len(sessions)
	}
	if start >= end {
		return ""
	}
	visible := sessions[start:end]

	maxID := len("Session ID")
	maxSummary := len("First Prompt")
	for _, item := range visible {
		if len(item.ID) > maxID {
			maxID = len(item.ID)
		}
		if len(item.Summary) > maxSummary {
			maxSummary = len(item.Summary)
		}
	}

	var lines []string
	header := fmt.Sprintf("#  %-*s  %-*s  %s", maxID, "Session ID", maxSummary, "First Prompt", "Updated")
	lines = append(lines, header)
	lines = append(lines, strings.Repeat("-", len(header)))
	for i, item := range visible {
		idx := start + i + 1
		lines = append(lines, fmt.Sprintf("%-2d %-*s  %-*s  %s",
			idx,
			maxID, item.ID,
			maxSummary, item.Summary,
			item.UpdatedAt.Local().Format(time.RFC3339)))
	}
	return strings.Join(lines, "\n")
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func closeSessionIDs(sessions []quacksession.Info, query string, max int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || max <= 0 {
		return nil
	}
	candidates := make([]string, 0, max)
	for _, s := range sessions {
		id := strings.ToLower(strings.TrimSpace(s.ID))
		if strings.HasPrefix(id, query) {
			candidates = append(candidates, s.ID)
			if len(candidates) == max {
				return candidates
			}
		}
	}
	if len(candidates) > 0 {
		return candidates
	}
	for _, s := range sessions {
		id := strings.ToLower(strings.TrimSpace(s.ID))
		if strings.Contains(id, query) {
			candidates = append(candidates, s.ID)
			if len(candidates) == max {
				break
			}
		}
	}
	sort.Strings(candidates)
	return candidates
}

type sessionSelectionCompleter struct {
	sessions []quacksession.Info
}

func (sc *sessionSelectionCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if sc == nil {
		return nil, 0
	}
	input := strings.TrimSpace(string(line[:pos]))
	lower := strings.ToLower(input)

	completions := make([][]rune, 0, 20)
	appendMatch := func(candidate string) {
		if len(completions) >= 20 {
			return
		}
		candidateLower := strings.ToLower(candidate)
		if lower == "" || strings.HasPrefix(candidateLower, lower) {
			suffix := candidate
			if lower != "" && strings.HasPrefix(candidateLower, lower) {
				suffix = candidate[len(input):]
			}
			if suffix != "" {
				completions = append(completions, []rune(suffix))
			}
		}
	}

	for _, s := range sc.sessions {
		appendMatch(formatSessionCompletion(s))
	}

	return completions, len(input)
}

func formatSessionCompletion(s quacksession.Info) string {
	id := strings.TrimSpace(s.ID)
	summary := strings.TrimSpace(s.Summary)
	if summary == "" {
		return id
	}
	if len(summary) > 42 {
		summary = strings.TrimSpace(summary[:39]) + "..."
	}
	return fmt.Sprintf("%s · %s", id, summary)
}

func findSessionIDInSelection(sessions []quacksession.Info, selection string) string {
	clean := strings.ToLower(strings.TrimSpace(selection))
	if clean == "" {
		return ""
	}
	var matched string
	for _, s := range sessions {
		id := strings.ToLower(strings.TrimSpace(s.ID))
		if id != "" && strings.Contains(clean, id) {
			if matched == "" || len(id) > len(strings.TrimSpace(matched)) {
				matched = s.ID
			}
		}
	}
	return matched
}

func splitSlashCommand(input string) (string, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", ""
	}
	command := parts[0]
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, command))
	return command, rest
}

func exitMessageWithSessionID(message string, cfg *config.Config) string {
	sessionID := ""
	if cfg != nil {
		sessionID = strings.TrimSpace(cfg.CurrentSessionID)
	}
	if sessionID == "" {
		return message
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Sprintf("Session ID: %s", sessionID)
	}
	return fmt.Sprintf("%s\nSession ID: %s", message, sessionID)
}
