package session

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms"
)

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store := NewStore(dir)

	cfg := &config.Config{
		CurrentSessionID:        "ses_roundtrip",
		CurrentSessionCreatedAt: time.Unix(1700000000, 0).UTC(),
		LastTextPrompt:          "check pods",
		UserMsgCount:            2,
		LastOutgoingTokens:      123,
		LastIncomingTokens:      456,
		SessionOutgoingTokens:   789,
		SessionIncomingTokens:   654,
		ChatMessages: []llms.ChatMessage{
			llms.SystemChatMessage{Content: "system"},
			llms.HumanChatMessage{Content: "user"},
			llms.AIChatMessage{Content: "assistant"},
		},
		SessionHistory: []config.SessionEvent{
			{
				Timestamp:  time.Unix(1700000010, 0).UTC(),
				UserPrompt: "check pods",
				ToolCalls: []config.ToolCallData{
					{Name: "kubectl", Args: map[string]any{"cmd": "get pods"}, Result: "ok"},
				},
				AIResponse: "Pods look healthy.",
			},
		},
		StoredUserCmdResults: []config.CmdRes{
			{Cmd: "kubectl get pods", Out: "pod-a", Err: errors.New("timed out")},
		},
	}

	if err := store.Save(FromConfig(cfg)); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load("ses_roundtrip")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.ID != cfg.CurrentSessionID {
		t.Fatalf("expected session id %q, got %q", cfg.CurrentSessionID, loaded.ID)
	}
	if loaded.Version != CurrentVersion {
		t.Fatalf("expected version %d, got %d", CurrentVersion, loaded.Version)
	}
	if loaded.CreatedAt.IsZero() || loaded.UpdatedAt.IsZero() {
		t.Fatalf("expected created and updated timestamps to be set")
	}

	var restored config.Config
	if err := loaded.Apply(&restored); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	if restored.CurrentSessionID != cfg.CurrentSessionID {
		t.Fatalf("expected restored id %q, got %q", cfg.CurrentSessionID, restored.CurrentSessionID)
	}
	if restored.LastTextPrompt != cfg.LastTextPrompt {
		t.Fatalf("expected last text prompt %q, got %q", cfg.LastTextPrompt, restored.LastTextPrompt)
	}
	if restored.UserMsgCount != cfg.UserMsgCount {
		t.Fatalf("expected user message count %d, got %d", cfg.UserMsgCount, restored.UserMsgCount)
	}
	if restored.SessionOutgoingTokens != cfg.SessionOutgoingTokens || restored.SessionIncomingTokens != cfg.SessionIncomingTokens {
		t.Fatalf("expected session token totals to round-trip, got out=%d in=%d", restored.SessionOutgoingTokens, restored.SessionIncomingTokens)
	}
	if len(restored.ChatMessages) != len(cfg.ChatMessages) {
		t.Fatalf("expected %d chat messages, got %d", len(cfg.ChatMessages), len(restored.ChatMessages))
	}
	if restored.ChatMessages[0].GetType() != llms.ChatMessageTypeSystem {
		t.Fatalf("expected first chat message to be system, got %s", restored.ChatMessages[0].GetType())
	}
	if restored.ChatMessages[1].GetContent() != "user" || restored.ChatMessages[2].GetContent() != "assistant" {
		t.Fatalf("restored chat contents do not match")
	}
	if len(restored.SessionHistory) != 1 || restored.SessionHistory[0].AIResponse != "Pods look healthy." {
		t.Fatalf("expected restored session history")
	}
	if len(restored.StoredUserCmdResults) != 1 || restored.StoredUserCmdResults[0].Err == nil {
		t.Fatalf("expected stored command results to round-trip with error text")
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir failed: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected dir mode 0700, got %o", got)
	}

	path, err := store.SessionPath("ses_roundtrip")
	if err != nil {
		t.Fatalf("session path failed: %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file failed: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected file mode 0600, got %o", got)
	}
}

func TestNewSessionIDIPv6LikeFormat(t *testing.T) {
	id, err := NewSessionID()
	if err != nil {
		t.Fatalf("new session id failed: %v", err)
	}

	pattern := regexp.MustCompile(`^([0-9a-f]{4}-){3}[0-9a-f]{4}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("expected IPv6-like session id, got %q", id)
	}
}

func TestStoreDelete(t *testing.T) {
	store := NewStore(t.TempDir())
	snapshot := Snapshot{
		Version:      CurrentVersion,
		ID:           "ses_delete",
		CreatedAt:    time.Now().Add(-time.Minute).UTC(),
		UpdatedAt:    time.Now().UTC(),
		ChatMessages: []Message{{Role: string(llms.ChatMessageTypeHuman), Content: "hello"}},
	}

	if err := store.Save(snapshot); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if err := store.Delete("ses_delete"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.Load("ses_delete"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound after delete, got %v", err)
	}
}

func TestStoreListOrdersMostRecentFirst(t *testing.T) {
	store := NewStore(t.TempDir())

	first := Snapshot{
		Version:      CurrentVersion,
		ID:           "ses_first",
		CreatedAt:    time.Now().Add(-2 * time.Minute).UTC(),
		UpdatedAt:    time.Now().Add(-2 * time.Minute).UTC(),
		ChatMessages: []Message{{Role: string(llms.ChatMessageTypeHuman), Content: "first"}},
	}
	second := Snapshot{
		Version:      CurrentVersion,
		ID:           "ses_second",
		CreatedAt:    time.Now().Add(-time.Minute).UTC(),
		UpdatedAt:    time.Now().Add(-time.Minute).UTC(),
		ChatMessages: []Message{{Role: string(llms.ChatMessageTypeHuman), Content: "second"}},
	}

	if err := store.Save(first); err != nil {
		t.Fatalf("save first failed: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := store.Save(second); err != nil {
		t.Fatalf("save second failed: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "ses_second" || sessions[1].ID != "ses_first" {
		t.Fatalf("unexpected order: %#v", sessions)
	}
}

func TestStoreListSummaryUsesFirstUserPrompt(t *testing.T) {
	store := NewStore(t.TempDir())
	snapshot := Snapshot{
		Version:   CurrentVersion,
		ID:        "ses_summary",
		CreatedAt: time.Now().Add(-time.Minute).UTC(),
		UpdatedAt: time.Now().UTC(),
		SessionHistory: []config.SessionEvent{
			{Timestamp: time.Now().Add(-50 * time.Second).UTC(), UserPrompt: "first prompt", AIResponse: "first answer"},
			{Timestamp: time.Now().Add(-40 * time.Second).UTC(), UserPrompt: "later prompt", AIResponse: "later answer"},
		},
		ChatMessages: []Message{{Role: string(llms.ChatMessageTypeHuman), Content: "fallback human"}},
	}

	if err := store.Save(snapshot); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].Summary != "first prompt" {
		t.Fatalf("expected summary from first user prompt, got %q", sessions[0].Summary)
	}
}

func TestStoreListSummaryExtractsUserTaskFromSystemLikePrompt(t *testing.T) {
	store := NewStore(t.TempDir())
	snapshot := Snapshot{
		Version:   CurrentVersion,
		ID:        "ses_systemlike",
		CreatedAt: time.Now().Add(-time.Minute).UTC(),
		UpdatedAt: time.Now().UTC(),
		SessionHistory: []config.SessionEvent{
			{
				Timestamp:  time.Now().Add(-30 * time.Second).UTC(),
				UserPrompt: "You are an expert Kubernetes administrator specializing in cluster diagnostics.\n\nTask: Analyze the user's issue and provide appropriate kubectl commands for diagnostics.\n\nIssue description: why is dns failing in kube-system",
			},
		},
	}

	if err := store.Save(snapshot); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].Summary != "why is dns failing in kube-system" {
		t.Fatalf("expected extracted user task summary, got %q", sessions[0].Summary)
	}
}

func TestStoreLoadInvalidJSON(t *testing.T) {
	store := NewStore(t.TempDir())
	path, err := store.SessionPath("ses_broken")
	if err != nil {
		t.Fatalf("session path failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if _, err := store.Load("ses_broken"); !errors.Is(err, ErrCorruptSessionJSON) {
		t.Fatalf("expected ErrCorruptSessionJSON, got %v", err)
	}
}

func TestStoreSaveAtomicallyReplacesExistingFile(t *testing.T) {
	store := NewStore(t.TempDir())
	original := Snapshot{
		Version:      CurrentVersion,
		ID:           "ses_atomic",
		CreatedAt:    time.Now().Add(-time.Minute).UTC(),
		UpdatedAt:    time.Now().Add(-time.Minute).UTC(),
		ChatMessages: []Message{{Role: string(llms.ChatMessageTypeHuman), Content: "old"}},
	}
	replacement := Snapshot{
		Version:        CurrentVersion,
		ID:             "ses_atomic",
		CreatedAt:      original.CreatedAt,
		UpdatedAt:      time.Now().UTC(),
		LastTextPrompt: "new prompt",
		ChatMessages: []Message{
			{Role: string(llms.ChatMessageTypeHuman), Content: "new"},
			{Role: string(llms.ChatMessageTypeAI), Content: "replacement"},
		},
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("save original failed: %v", err)
	}
	if err := store.Save(replacement); err != nil {
		t.Fatalf("save replacement failed: %v", err)
	}

	loaded, err := store.Load("ses_atomic")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded.ChatMessages) != 2 || loaded.ChatMessages[0].Content != "new" || loaded.ChatMessages[1].Content != "replacement" {
		t.Fatalf("expected replacement contents, got %#v", loaded.ChatMessages)
	}
	if loaded.LastTextPrompt != "new prompt" {
		t.Fatalf("expected replacement prompt, got %q", loaded.LastTextPrompt)
	}

	entries, err := os.ReadDir(filepath.Dir(mustSessionPath(t, store, "ses_atomic")))
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Fatalf("unexpected temp file left behind: %s", entry.Name())
		}
	}
}

func TestStoreLoadInvalidSchema(t *testing.T) {
	store := NewStore(t.TempDir())
	path := mustSessionPath(t, store, "ses_invalid")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	payload := `{"version":99,"id":"ses_invalid","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","chat_messages":[]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if _, err := store.Load("ses_invalid"); !errors.Is(err, ErrInvalidSessionSchema) {
		t.Fatalf("expected ErrInvalidSessionSchema, got %v", err)
	}
}

func mustSessionPath(t *testing.T, store *Store, sessionID string) string {
	t.Helper()
	path, err := store.SessionPath(sessionID)
	if err != nil {
		t.Fatalf("session path failed: %v", err)
	}
	return path
}
