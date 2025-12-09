package llm

import (
	"fmt"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/tmc/langchaingo/llms"
)

// MessageBuilder constructs a role-separated message sequence for LLM calls.
// It separates system instructions from user content for safer, structured prompts.
type MessageBuilder struct {
	systemParts []string // core guardrails, tool instructions, type definitions
	contextData string   // RAG context (findings, command outputs)
	userQuery   string   // the actual user question
	mcpPrompt   string   // MCP prompt content (system-level instructions)
}

// NewMessageBuilder creates a new MessageBuilder instance.
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		systemParts: make([]string, 0),
	}
}

// AddSystemInstruction appends a system-level instruction.
func (mb *MessageBuilder) AddSystemInstruction(instruction string) *MessageBuilder {
	if instruction = strings.TrimSpace(instruction); instruction != "" {
		mb.systemParts = append(mb.systemParts, instruction)
	}
	return mb
}

// SetContextData sets the RAG context (diagnostic findings, command outputs).
func (mb *MessageBuilder) SetContextData(data string) *MessageBuilder {
	mb.contextData = strings.TrimSpace(data)
	return mb
}

// SetUserQuery sets the actual user question.
func (mb *MessageBuilder) SetUserQuery(query string) *MessageBuilder {
	mb.userQuery = strings.TrimSpace(query)
	return mb
}

// SetMCPPrompt sets the MCP prompt content (treated as system instruction).
func (mb *MessageBuilder) SetMCPPrompt(prompt string) *MessageBuilder {
	mb.mcpPrompt = strings.TrimSpace(prompt)
	return mb
}

// Build constructs the message sequence with proper role separation.
// Returns system message content and user message content separately.
func (mb *MessageBuilder) Build(cfg *config.Config) (systemContent string, userContent string) {
	var systemBuilder strings.Builder
	var userBuilder strings.Builder

	// System message: core instructions + MCP prompt + tool instructions
	if len(mb.systemParts) > 0 {
		systemBuilder.WriteString(strings.Join(mb.systemParts, "\n\n"))
	}

	if mb.mcpPrompt != "" {
		if systemBuilder.Len() > 0 {
			systemBuilder.WriteString("\n\n")
		}
		systemBuilder.WriteString(mb.mcpPrompt)
	}

	// User message: context data + user query
	if mb.contextData != "" {
		userBuilder.WriteString(mb.contextData)
		if mb.userQuery != "" {
			userBuilder.WriteString("\n\n## User Query\n")
			userBuilder.WriteString(mb.userQuery)
		}
	} else if mb.userQuery != "" {
		userBuilder.WriteString(mb.userQuery)
	}

	return systemBuilder.String(), userBuilder.String()
}

// BuildMessages constructs []llms.MessageContent for the LLM call.
// Prepends a system message if system content is non-empty, then adds user message.
func (mb *MessageBuilder) BuildMessages(cfg *config.Config) []llms.MessageContent {
	systemContent, userContent := mb.Build(cfg)
	var messages []llms.MessageContent

	if systemContent != "" {
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeSystem, systemContent))
	}

	if userContent != "" {
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, userContent))
	}

	return messages
}

// LogRoleSummary logs a summary of message roles for debugging.
func (mb *MessageBuilder) LogRoleSummary(cfg *config.Config) {
	systemContent, userContent := mb.Build(cfg)

	var parts []string
	if systemContent != "" {
		tokens := lib.EstimateTokens(cfg, systemContent)
		parts = append(parts, fmt.Sprintf("system=%d tok", tokens))
	}
	if userContent != "" {
		tokens := lib.EstimateTokens(cfg, userContent)
		parts = append(parts, fmt.Sprintf("user=%d tok", tokens))
	}

	if len(parts) > 0 {
		logger.Log("debug", "[Messages] Role breakdown: %s", strings.Join(parts, ", "))
	}
}

// MessageRoleSummary returns a summary of outbound message roles.
func MessageRoleSummary(messages []llms.MessageContent) string {
	counts := make(map[string]int)
	for _, msg := range messages {
		role := string(msg.Role)
		counts[role]++
	}

	var parts []string
	for role, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", role, count))
	}
	return strings.Join(parts, ", ")
}

// LogOutboundMessages logs role distribution of outbound messages.
func LogOutboundMessages(messages []llms.MessageContent, cfg *config.Config) {
	if len(messages) == 0 {
		return
	}

	// Count by role
	roleCounts := make(map[llms.ChatMessageType]int)
	roleTokens := make(map[llms.ChatMessageType]int)
	totalTokens := 0

	for _, msg := range messages {
		roleCounts[msg.Role]++
		// Estimate tokens from parts
		var content string
		for _, part := range msg.Parts {
			if tp, ok := part.(llms.TextContent); ok {
				content += tp.Text
			}
		}
		tokens := lib.EstimateTokens(cfg, content)
		roleTokens[msg.Role] += tokens
		totalTokens += tokens
	}

	var summaryParts []string
	roleOrder := []llms.ChatMessageType{
		llms.ChatMessageTypeSystem,
		llms.ChatMessageTypeHuman,
		llms.ChatMessageTypeAI,
		llms.ChatMessageTypeTool,
		llms.ChatMessageTypeGeneric,
	}

	for _, role := range roleOrder {
		if count, ok := roleCounts[role]; ok && count > 0 {
			tokens := roleTokens[role]
			summaryParts = append(summaryParts, fmt.Sprintf("%s:%d(%d tok)", role, count, tokens))
		}
	}

	logger.Log("debug", "[Messages] Outbound: %d messages, %d total tokens [%s]",
		len(messages), totalTokens, strings.Join(summaryParts, " "))
}


