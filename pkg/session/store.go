package session

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms"
)

const CurrentVersion = 1

var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrCorruptSessionJSON   = errors.New("corrupt session json")
	ErrInvalidSessionSchema = errors.New("invalid session schema")
)

type Store struct {
	dir string
}

type CommandResult struct {
	Cmd string `json:"cmd"`
	Out string `json:"out,omitempty"`
	Err string `json:"err,omitempty"`
}

type Message struct {
	Role        string `json:"role"`
	Content     string `json:"content"`
	Name        string `json:"name,omitempty"`
	GenericRole string `json:"generic_role,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
}

type Snapshot struct {
	Version               int                   `json:"version"`
	ID                    string                `json:"id"`
	CreatedAt             time.Time             `json:"created_at"`
	UpdatedAt             time.Time             `json:"updated_at"`
	LastTextPrompt        string                `json:"last_text_prompt,omitempty"`
	UserMsgCount          int                   `json:"user_msg_count"`
	LastOutgoingTokens    int                   `json:"last_outgoing_tokens,omitempty"`
	LastIncomingTokens    int                   `json:"last_incoming_tokens,omitempty"`
	SessionOutgoingTokens int                   `json:"session_outgoing_tokens,omitempty"`
	SessionIncomingTokens int                   `json:"session_incoming_tokens,omitempty"`
	ChatMessages          []Message             `json:"chat_messages"`
	SessionHistory        []config.SessionEvent `json:"session_history,omitempty"`
	StoredUserCmdResults  []CommandResult       `json:"stored_user_cmd_results,omitempty"`
}

type Info struct {
	ID        string    `json:"id"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Path      string    `json:"path"`
}

func NewStore(dir string) *Store {
	return &Store{dir: strings.TrimSpace(dir)}
}

func NewSessionID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}

	groups := make([]string, 0, len(buf)/2)
	for i := 0; i < len(buf); i += 2 {
		groups = append(groups, fmt.Sprintf("%02x%02x", buf[i], buf[i+1]))
	}

	return strings.Join(groups, "-"), nil
}

func (s Snapshot) IsEmpty() bool {
	return len(s.ChatMessages) == 0 &&
		len(s.SessionHistory) == 0 &&
		len(s.StoredUserCmdResults) == 0 &&
		strings.TrimSpace(s.LastTextPrompt) == "" &&
		s.UserMsgCount == 0
}

func FromConfig(cfg *config.Config) Snapshot {
	if cfg == nil {
		return Snapshot{Version: CurrentVersion}
	}

	sessionHistory := append([]config.SessionEvent(nil), cfg.SessionHistory...)

	return Snapshot{
		Version:               CurrentVersion,
		ID:                    strings.TrimSpace(cfg.CurrentSessionID),
		CreatedAt:             cfg.CurrentSessionCreatedAt,
		UpdatedAt:             time.Now(),
		LastTextPrompt:        cfg.LastTextPrompt,
		UserMsgCount:          cfg.UserMsgCount,
		LastOutgoingTokens:    cfg.LastOutgoingTokens,
		LastIncomingTokens:    cfg.LastIncomingTokens,
		SessionOutgoingTokens: cfg.SessionOutgoingTokens,
		SessionIncomingTokens: cfg.SessionIncomingTokens,
		ChatMessages:          serializeChatMessages(cfg.ChatMessages),
		SessionHistory:        sessionHistory,
		StoredUserCmdResults:  serializeCommandResults(cfg.StoredUserCmdResults),
	}
}

func (s Snapshot) Apply(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("apply session: nil config")
	}
	if err := validateSnapshot(s); err != nil {
		return err
	}

	chatMessages, err := deserializeChatMessages(s.ChatMessages)
	if err != nil {
		return err
	}

	cfg.CurrentSessionID = s.ID
	cfg.CurrentSessionCreatedAt = s.CreatedAt
	cfg.LastTextPrompt = s.LastTextPrompt
	cfg.UserMsgCount = s.UserMsgCount
	cfg.LastOutgoingTokens = s.LastOutgoingTokens
	cfg.LastIncomingTokens = s.LastIncomingTokens
	cfg.SessionOutgoingTokens = s.SessionOutgoingTokens
	cfg.SessionIncomingTokens = s.SessionIncomingTokens
	cfg.ChatMessages = chatMessages
	cfg.SessionHistory = append([]config.SessionEvent(nil), s.SessionHistory...)
	cfg.StoredUserCmdResults = deserializeCommandResults(s.StoredUserCmdResults)
	cfg.SelectedPrompt = ""
	cfg.MCPPromptServer = ""
	return nil
}

func (s *Store) SessionPath(sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	if s == nil || s.dir == "" {
		return "", fmt.Errorf("session store directory is not configured")
	}
	return filepath.Join(s.dir, sessionID+".json"), nil
}

func (s *Store) Save(snapshot Snapshot) error {
	if s == nil {
		return fmt.Errorf("session store is nil")
	}
	if err := s.ensureDir(); err != nil {
		return err
	}

	snapshot.Version = CurrentVersion
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now()
	}
	snapshot.UpdatedAt = time.Now()

	if err := validateSnapshot(snapshot); err != nil {
		return err
	}

	path, err := s.SessionPath(snapshot.ID)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session %s: %w", snapshot.ID, err)
	}

	tempFile, err := os.CreateTemp(s.dir, "."+snapshot.ID+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp session file: %w", err)
	}

	tempPath := tempFile.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp session file: %w", err)
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp session file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp session file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp session file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename temp session file: %w", err)
	}
	renamed = true

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod session file: %w", err)
	}
	return nil
}

func (s *Store) Load(sessionID string) (Snapshot, error) {
	if s == nil {
		return Snapshot{}, fmt.Errorf("session store is nil")
	}
	path, err := s.SessionPath(sessionID)
	if err != nil {
		return Snapshot{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
		}
		return Snapshot{}, fmt.Errorf("read session file: %w", err)
	}

	snapshot, err := decodeSnapshot(data)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.ID != sessionID {
		return Snapshot{}, fmt.Errorf("%w: session id mismatch", ErrInvalidSessionSchema)
	}
	return snapshot, nil
}

func (s *Store) Delete(sessionID string) error {
	if s == nil {
		return fmt.Errorf("session store is nil")
	}
	path, err := s.SessionPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
		}
		return fmt.Errorf("delete session file: %w", err)
	}
	return nil
}

func (s *Store) List() ([]Info, error) {
	if s == nil {
		return nil, fmt.Errorf("session store is nil")
	}
	if s.dir == "" {
		return nil, fmt.Errorf("session store directory is not configured")
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Info{}, nil
		}
		return nil, fmt.Errorf("read sessions directory: %w", err)
	}

	infos := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read session file %s: %w", entry.Name(), err)
		}

		snapshot, err := decodeSnapshot(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}

		infos = append(infos, Info{
			ID:        snapshot.ID,
			Summary:   summarizeSnapshot(snapshot),
			CreatedAt: snapshot.CreatedAt,
			UpdatedAt: snapshot.UpdatedAt,
			Path:      path,
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		if infos[i].UpdatedAt.Equal(infos[j].UpdatedAt) {
			return infos[i].ID < infos[j].ID
		}
		return infos[i].UpdatedAt.After(infos[j].UpdatedAt)
	})

	return infos, nil
}

func (s *Store) ensureDir() error {
	if s.dir == "" {
		return fmt.Errorf("session store directory is not configured")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create sessions directory: %w", err)
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return fmt.Errorf("chmod sessions directory: %w", err)
	}
	return nil
}

func decodeSnapshot(data []byte) (Snapshot, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrCorruptSessionJSON, err)
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.Version != CurrentVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidSessionSchema, snapshot.Version)
	}
	if err := validateSessionID(snapshot.ID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSessionSchema, err)
	}
	if snapshot.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidSessionSchema)
	}
	if snapshot.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: updated_at is required", ErrInvalidSessionSchema)
	}
	if snapshot.UserMsgCount < 0 {
		return fmt.Errorf("%w: user_msg_count must be non-negative", ErrInvalidSessionSchema)
	}
	if snapshot.SessionOutgoingTokens < 0 || snapshot.SessionIncomingTokens < 0 {
		return fmt.Errorf("%w: session token totals must be non-negative", ErrInvalidSessionSchema)
	}
	for _, msg := range snapshot.ChatMessages {
		if err := validateMessage(msg); err != nil {
			return err
		}
	}
	for _, event := range snapshot.SessionHistory {
		if event.Timestamp.IsZero() {
			return fmt.Errorf("%w: session history timestamp is required", ErrInvalidSessionSchema)
		}
	}
	return nil
}

func validateMessage(msg Message) error {
	switch msg.Role {
	case string(llms.ChatMessageTypeAI), string(llms.ChatMessageTypeHuman), string(llms.ChatMessageTypeSystem):
		return nil
	case string(llms.ChatMessageTypeGeneric):
		if strings.TrimSpace(msg.GenericRole) == "" {
			return fmt.Errorf("%w: generic_role is required for generic messages", ErrInvalidSessionSchema)
		}
		return nil
	case string(llms.ChatMessageTypeFunction):
		if strings.TrimSpace(msg.Name) == "" {
			return fmt.Errorf("%w: name is required for function messages", ErrInvalidSessionSchema)
		}
		return nil
	case string(llms.ChatMessageTypeTool):
		if strings.TrimSpace(msg.ToolCallID) == "" {
			return fmt.Errorf("%w: tool_call_id is required for tool messages", ErrInvalidSessionSchema)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported chat message role %q", ErrInvalidSessionSchema, msg.Role)
	}
}

func validateSessionID(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.Contains(sessionID, "..") {
		return fmt.Errorf("session id must not contain '..'")
	}
	if strings.ContainsRune(sessionID, filepath.Separator) || strings.ContainsRune(sessionID, '/') {
		return fmt.Errorf("session id must not contain path separators")
	}
	for _, r := range sessionID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("session id contains invalid character %q", r)
	}
	return nil
}

func serializeChatMessages(messages []llms.ChatMessage) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		item := Message{
			Role:    string(msg.GetType()),
			Content: msg.GetContent(),
		}

		switch typed := msg.(type) {
		case llms.GenericChatMessage:
			item.GenericRole = typed.Role
			item.Name = typed.Name
		case llms.FunctionChatMessage:
			item.Name = typed.Name
		case llms.ToolChatMessage:
			item.ToolCallID = typed.ID
		}

		out = append(out, item)
	}
	return out
}

func deserializeChatMessages(messages []Message) ([]llms.ChatMessage, error) {
	out := make([]llms.ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if err := validateMessage(msg); err != nil {
			return nil, err
		}

		switch msg.Role {
		case string(llms.ChatMessageTypeAI):
			out = append(out, llms.AIChatMessage{Content: msg.Content})
		case string(llms.ChatMessageTypeHuman):
			out = append(out, llms.HumanChatMessage{Content: msg.Content})
		case string(llms.ChatMessageTypeSystem):
			out = append(out, llms.SystemChatMessage{Content: msg.Content})
		case string(llms.ChatMessageTypeGeneric):
			out = append(out, llms.GenericChatMessage{Content: msg.Content, Role: msg.GenericRole, Name: msg.Name})
		case string(llms.ChatMessageTypeFunction):
			out = append(out, llms.FunctionChatMessage{Name: msg.Name, Content: msg.Content})
		case string(llms.ChatMessageTypeTool):
			out = append(out, llms.ToolChatMessage{ID: msg.ToolCallID, Content: msg.Content})
		default:
			return nil, fmt.Errorf("%w: unsupported chat message role %q", ErrInvalidSessionSchema, msg.Role)
		}
	}
	return out, nil
}

func serializeCommandResults(results []config.CmdRes) []CommandResult {
	out := make([]CommandResult, 0, len(results))
	for _, result := range results {
		item := CommandResult{
			Cmd: result.Cmd,
			Out: result.Out,
		}
		if result.Err != nil {
			item.Err = result.Err.Error()
		}
		out = append(out, item)
	}
	return out
}

func deserializeCommandResults(results []CommandResult) []config.CmdRes {
	out := make([]config.CmdRes, 0, len(results))
	for _, result := range results {
		item := config.CmdRes{
			Cmd: result.Cmd,
			Out: result.Out,
		}
		if strings.TrimSpace(result.Err) != "" {
			item.Err = errors.New(result.Err)
		}
		out = append(out, item)
	}
	return out
}

func summarizeSnapshot(snapshot Snapshot) string {
	for i := 0; i < len(snapshot.SessionHistory); i++ {
		if prompt := summarizePromptCandidate(snapshot.SessionHistory[i].UserPrompt); prompt != "" {
			return trimSummary(prompt)
		}
	}
	for i := 0; i < len(snapshot.ChatMessages); i++ {
		msg := snapshot.ChatMessages[i]
		if msg.Role == string(llms.ChatMessageTypeHuman) {
			if prompt := summarizePromptCandidate(msg.Content); prompt != "" {
				return trimSummary(prompt)
			}
		}
	}
	if prompt := summarizePromptCandidate(snapshot.LastTextPrompt); prompt != "" {
		return trimSummary(prompt)
	}
	return snapshot.ID
}

func summarizePromptCandidate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if extracted := extractAfterMarker(value, "Issue description:"); extracted != "" {
		return extracted
	}
	if extracted := extractSectionValue(value, "## User Task"); extracted != "" {
		return extracted
	}
	if extracted := extractSectionValue(value, "## User Query"); extracted != "" {
		return extracted
	}

	if looksLikeSystemPrompt(value) {
		return ""
	}
	return value
}

func extractAfterMarker(value string, marker string) string {
	lowerValue := strings.ToLower(value)
	lowerMarker := strings.ToLower(marker)
	idx := strings.Index(lowerValue, lowerMarker)
	if idx == -1 {
		return ""
	}
	rest := strings.TrimSpace(value[idx+len(marker):])
	if rest == "" {
		return ""
	}
	line := strings.SplitN(rest, "\n", 2)[0]
	line = strings.TrimSpace(strings.Trim(line, "`\"'"))
	if line == "" || looksLikeSystemPrompt(line) {
		return ""
	}
	return line
}

func extractSectionValue(value string, heading string) string {
	lowerValue := strings.ToLower(value)
	lowerHeading := strings.ToLower(heading)
	idx := strings.Index(lowerValue, lowerHeading)
	if idx == -1 {
		return ""
	}
	rest := strings.TrimSpace(value[idx+len(heading):])
	if rest == "" {
		return ""
	}

	lines := strings.Split(rest, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "- ") {
			continue
		}
		if looksLikeSystemPrompt(trimmed) {
			continue
		}
		return strings.Trim(trimmed, "`\"'")
	}
	return ""
}

func looksLikeSystemPrompt(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "you are ") && strings.Contains(lower, "kubernetes") {
		return true
	}
	if strings.HasPrefix(lower, "# kubernetes diagnostic analysis") {
		return true
	}
	if strings.Contains(lower, "task: analyze the user's issue") {
		return true
	}
	if strings.Contains(lower, "command reference:") && strings.Contains(lower, "kubectl") {
		return true
	}
	if strings.Contains(lower, "## guidelines") && (strings.Contains(lower, "## user task") || strings.Contains(lower, "## user query")) {
		return true
	}
	return false
}

func trimSummary(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	const maxSummaryLen = 72
	if len(value) <= maxSummaryLen {
		return value
	}
	return strings.TrimSpace(value[:maxSummaryLen-3]) + "..."
}
