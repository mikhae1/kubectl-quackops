package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	quacksession "github.com/mikhae1/kubectl-quackops/pkg/session"
	"github.com/tmc/langchaingo/llms"
)

func TestProcessUserPromptResumeSlashCommand(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.SessionsDir = t.TempDir()

	source := createInteractiveTestConfig()
	source.SessionsDir = cfg.SessionsDir
	source.CurrentSessionID = "ses_resume"
	source.CurrentSessionCreatedAt = time.Unix(1700000000, 0).UTC()
	source.LastTextPrompt = "resume me"
	source.UserMsgCount = 1
	source.SessionOutgoingTokens = 321
	source.SessionIncomingTokens = 123
	source.ChatMessages = []llms.ChatMessage{
		llms.HumanChatMessage{Content: "How are my pods?"},
		llms.AIChatMessage{Content: "They look healthy."},
	}
	source.SessionHistory = []config.SessionEvent{
		{
			Timestamp:  time.Unix(1700000010, 0).UTC(),
			UserPrompt: "How are my pods?",
			AIResponse: "They look healthy.",
		},
	}
	if err := quacksession.NewStore(cfg.SessionsDir).Save(quacksession.FromConfig(source)); err != nil {
		t.Fatalf("save source session failed: %v", err)
	}

	if err := processUserPrompt(cfg, "/resume ses_resume", "", 0); err != nil {
		t.Fatalf("processUserPrompt failed: %v", err)
	}

	if cfg.CurrentSessionID != "ses_resume" {
		t.Fatalf("expected current session id ses_resume, got %q", cfg.CurrentSessionID)
	}
	if len(cfg.ChatMessages) != 2 {
		t.Fatalf("expected 2 chat messages, got %d", len(cfg.ChatMessages))
	}
	if cfg.SessionOutgoingTokens != 321 || cfg.SessionIncomingTokens != 123 {
		t.Fatalf("expected session token totals to restore, got out=%d in=%d", cfg.SessionOutgoingTokens, cfg.SessionIncomingTokens)
	}
	if cfg.ChatMessages[1].GetContent() != "They look healthy." {
		t.Fatalf("unexpected restored assistant message: %q", cfg.ChatMessages[1].GetContent())
	}
}

func TestProcessUserPromptSessionsInteractiveSelection(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.SessionsDir = t.TempDir()

	source := createInteractiveTestConfig()
	source.SessionsDir = cfg.SessionsDir
	source.CurrentSessionID = "ses_pick"
	source.CurrentSessionCreatedAt = time.Unix(1700000100, 0).UTC()
	source.ChatMessages = []llms.ChatMessage{
		llms.HumanChatMessage{Content: "Investigate DNS"},
		llms.AIChatMessage{Content: "Checking DNS diagnostics."},
	}
	source.SessionHistory = []config.SessionEvent{
		{
			Timestamp:  time.Unix(1700000110, 0).UTC(),
			UserPrompt: "Investigate DNS",
			AIResponse: "Checking DNS diagnostics.",
		},
	}
	if err := quacksession.NewStore(cfg.SessionsDir).Save(quacksession.FromConfig(source)); err != nil {
		t.Fatalf("save source session failed: %v", err)
	}

	restoreStdin := replaceStdin(t, "1\n")
	defer restoreStdin()

	if err := processUserPrompt(cfg, "/sessions", "", 0); err != nil {
		t.Fatalf("processUserPrompt failed: %v", err)
	}

	if cfg.CurrentSessionID != "ses_pick" {
		t.Fatalf("expected selected session to load, got %q", cfg.CurrentSessionID)
	}
	if len(cfg.SessionHistory) != 1 || cfg.SessionHistory[0].UserPrompt != "Investigate DNS" {
		t.Fatalf("expected restored session history after interactive load")
	}
}

func TestProcessUserPromptSessionsInteractiveSelectionAutocompleteFormattedEntry(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.SessionsDir = t.TempDir()

	source := createInteractiveTestConfig()
	source.SessionsDir = cfg.SessionsDir
	source.CurrentSessionID = "ses_pick_fmt"
	source.CurrentSessionCreatedAt = time.Unix(1700000100, 0).UTC()
	source.SessionHistory = []config.SessionEvent{{Timestamp: time.Unix(1700000110, 0).UTC(), UserPrompt: "Investigate DNS failures in kube-system"}}
	if err := quacksession.NewStore(cfg.SessionsDir).Save(quacksession.FromConfig(source)); err != nil {
		t.Fatalf("save source session failed: %v", err)
	}

	restoreStdin := replaceStdin(t, "ses_pick_fmt · Investigate DNS failures in kube-system\n")
	defer restoreStdin()

	if err := processUserPrompt(cfg, "/sessions", "", 0); err != nil {
		t.Fatalf("processUserPrompt failed: %v", err)
	}

	if cfg.CurrentSessionID != "ses_pick_fmt" {
		t.Fatalf("expected selected session to load, got %q", cfg.CurrentSessionID)
	}
}

func TestProcessUserPromptSessionsInteractiveSelectionNavigation(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.SessionsDir = t.TempDir()

	store := quacksession.NewStore(cfg.SessionsDir)
	for i := 1; i <= 12; i++ {
		sessionID := fmt.Sprintf("ses_nav_%02d", i)
		source := createInteractiveTestConfig()
		source.SessionsDir = cfg.SessionsDir
		source.CurrentSessionID = sessionID
		source.CurrentSessionCreatedAt = time.Now().Add(-time.Duration(20-i) * time.Second).UTC()
		source.SessionHistory = []config.SessionEvent{
			{Timestamp: time.Now().Add(-time.Duration(20-i) * time.Second).UTC(), UserPrompt: fmt.Sprintf("prompt %d", i)},
		}
		if err := store.Save(quacksession.FromConfig(source)); err != nil {
			t.Fatalf("save source session failed: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	sessions, err := listSavedSessions(cfg)
	if err != nil {
		t.Fatalf("list saved sessions failed: %v", err)
	}
	targetID := sessions[10].ID // first row on page 2 (global index 11)

	restoreStdin := replaceStdin(t, "n\n11\n")
	defer restoreStdin()

	if err := processUserPrompt(cfg, "/sessions", "", 0); err != nil {
		t.Fatalf("processUserPrompt failed: %v", err)
	}

	if cfg.CurrentSessionID != targetID {
		t.Fatalf("expected selected session %q, got %q", targetID, cfg.CurrentSessionID)
	}
}

func TestProcessUserPromptNewSlashCommand(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.CurrentSessionID = "ses_active"
	cfg.CurrentSessionCreatedAt = time.Now().UTC()
	cfg.LastTextPrompt = "stale"
	cfg.UserMsgCount = 3
	cfg.SessionOutgoingTokens = 999
	cfg.SessionIncomingTokens = 888
	cfg.ChatMessages = []llms.ChatMessage{
		llms.HumanChatMessage{Content: "hello"},
		llms.AIChatMessage{Content: "world"},
	}
	cfg.SessionHistory = []config.SessionEvent{
		{Timestamp: time.Now().UTC(), UserPrompt: "hello", AIResponse: "world"},
	}

	if err := processUserPrompt(cfg, "/new", "", 0); err != nil {
		t.Fatalf("processUserPrompt failed: %v", err)
	}

	if cfg.CurrentSessionID != "" {
		t.Fatalf("expected session id to be cleared, got %q", cfg.CurrentSessionID)
	}
	if len(cfg.ChatMessages) != 0 || len(cfg.SessionHistory) != 0 {
		t.Fatalf("expected in-memory session state to be cleared")
	}
	if cfg.LastTextPrompt != "" || cfg.UserMsgCount != 0 {
		t.Fatalf("expected prompt tracking state to be reset")
	}
	if cfg.SessionOutgoingTokens != 0 || cfg.SessionIncomingTokens != 0 {
		t.Fatalf("expected session token totals to reset for new session")
	}
}

func TestSessionCommandListAndDelete(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.SessionsDir = t.TempDir()

	source := createInteractiveTestConfig()
	source.SessionsDir = cfg.SessionsDir
	source.CurrentSessionID = "ses_cli"
	source.CurrentSessionCreatedAt = time.Unix(1700000200, 0).UTC()
	source.ChatMessages = []llms.ChatMessage{
		llms.HumanChatMessage{Content: "cli list"},
	}
	if err := quacksession.NewStore(cfg.SessionsDir).Save(quacksession.FromConfig(source)); err != nil {
		t.Fatalf("save source session failed: %v", err)
	}

	sessionCmd := newSessionCommand(cfg)
	listOutput := captureStdout(t, func() {
		sessionCmd.SetArgs([]string{"list", "--format", "json"})
		if err := sessionCmd.Execute(); err != nil {
			t.Fatalf("list command failed: %v", err)
		}
	})
	if !strings.Contains(listOutput, "ses_cli") {
		t.Fatalf("expected list output to include session id, got %q", listOutput)
	}

	deleteOutput := captureStdout(t, func() {
		sessionCmd.SetArgs([]string{"delete", "ses_cli"})
		if err := sessionCmd.Execute(); err != nil {
			t.Fatalf("delete command failed: %v", err)
		}
	})
	if !strings.Contains(deleteOutput, "Deleted session ses_cli") {
		t.Fatalf("unexpected delete output: %q", deleteOutput)
	}
	if _, err := quacksession.NewStore(cfg.SessionsDir).Load("ses_cli"); err == nil {
		t.Fatalf("expected session to be deleted")
	}
}

func TestFormatSavedSessionsTableUsesFirstPromptHeader(t *testing.T) {
	rows := []quacksession.Info{{ID: "ses_a", Summary: "first user prompt", UpdatedAt: time.Now().UTC()}}
	table := formatSavedSessionsTable(rows)
	if !strings.Contains(table, "First Prompt") {
		t.Fatalf("expected First Prompt header, got %q", table)
	}
}

func TestFormatSessionCompletionIncludesSummary(t *testing.T) {
	entry := formatSessionCompletion(quacksession.Info{ID: "ses_a", Summary: "first prompt in session"})
	if !strings.Contains(entry, "ses_a") || !strings.Contains(entry, "first prompt") {
		t.Fatalf("unexpected completion format: %q", entry)
	}
}

func TestPersistCurrentSessionRespectsMaxSavedSessions(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.SessionsDir = t.TempDir()
	cfg.MaxSavedSessions = 2

	ids := []string{"ses_one", "ses_two", "ses_three"}
	for _, id := range ids {
		cfg.CurrentSessionID = id
		cfg.CurrentSessionCreatedAt = time.Now().UTC()
		cfg.LastTextPrompt = id
		cfg.UserMsgCount = 1
		if err := persistCurrentSession(cfg); err != nil {
			t.Fatalf("persistCurrentSession failed for %s: %v", id, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	store := quacksession.NewStore(cfg.SessionsDir)
	sessions, err := store.List()
	if err != nil {
		t.Fatalf("list sessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions after pruning, got %d", len(sessions))
	}
	if sessions[0].ID != "ses_three" || sessions[1].ID != "ses_two" {
		t.Fatalf("unexpected sessions after pruning: %#v", sessions)
	}
	if _, err := store.Load("ses_one"); err == nil {
		t.Fatalf("expected oldest session to be pruned")
	}
}

func replaceStdin(t *testing.T, input string) func() {
	t.Helper()
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe failed: %v", err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatalf("write stdin input failed: %v", err)
	}
	_ = writer.Close()
	os.Stdin = reader
	return func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe failed: %v", err)
	}
	os.Stdout = writer

	fn()

	_ = writer.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("read stdout failed: %v", err)
	}
	_ = reader.Close()
	return buf.String()
}

func TestExitMessageWithSessionID(t *testing.T) {
	cfg := createInteractiveTestConfig()
	cfg.CurrentSessionID = "ses_exit"

	got := exitMessageWithSessionID("Exiting...", cfg)
	if !strings.Contains(got, "Session ID: ses_exit") {
		t.Fatalf("expected session id in exit message, got %q", got)
	}

	got = exitMessageWithSessionID("", cfg)
	if got != "Session ID: ses_exit" {
		t.Fatalf("expected session-only message, got %q", got)
	}

	cfg.CurrentSessionID = ""
	got = exitMessageWithSessionID("Exiting...", cfg)
	if got != "Exiting..." {
		t.Fatalf("expected unchanged exit message without session id, got %q", got)
	}
}
