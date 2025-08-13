package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	genai "github.com/google/generative-ai-go/genai"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/llms"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GoogleNative implements llms.Model using Google's genai client directly,
// with enhanced tool schema conversion that preserves array items and nested properties.
type GoogleNative struct {
	CallbacksHandler callbacks.Handler
	client           *genai.Client
	defaultModel     string
}

var _ llms.Model = &GoogleNative{}

func New(ctx context.Context, apiKey string, defaultModel string) (*GoogleNative, error) {
	var opts []option.ClientOption
	if strings.TrimSpace(apiKey) != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	c, err := genai.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &GoogleNative{client: c, defaultModel: defaultModel}, nil
}

func (g *GoogleNative) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	resp, err := g.GenerateContent(ctx, []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, prompt)}, options...)
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", errors.New("empty response")
	}
	return resp.Choices[0].Content, nil
}

func (g *GoogleNative) GenerateContent(
	ctx context.Context,
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (*llms.ContentResponse, error) {
	if g.CallbacksHandler != nil {
		g.CallbacksHandler.HandleLLMGenerateContentStart(ctx, messages)
	}

	opts := llms.CallOptions{}
	for _, opt := range options {
		opt(&opts)
	}

	modelName := g.defaultModel
	if strings.TrimSpace(opts.Model) != "" {
		modelName = opts.Model
	}

	model := g.client.GenerativeModel(modelName)
	// Gemini requires candidate_count in [1,8]. Clamp to safe defaults when unset or out of range.
	cand := opts.CandidateCount
	if cand < 1 {
		cand = 1
	} else if cand > 8 {
		cand = 8
	}
	model.SetCandidateCount(int32(cand))
	model.SetMaxOutputTokens(int32(opts.MaxTokens))
	model.SetTemperature(float32(opts.Temperature))
	model.SetTopP(float32(opts.TopP))
	model.SetTopK(int32(opts.TopK))
	model.StopSequences = opts.StopWords

	var err error
	if model.Tools, err = convertTools(opts.Tools); err != nil {
		return nil, err
	}

	var response *llms.ContentResponse
	if len(messages) == 1 {
		theMessage := messages[0]
		if theMessage.Role != llms.ChatMessageTypeHuman {
			return nil, fmt.Errorf("got %v message role, want human", theMessage.Role)
		}
		response, err = generateFromSingleMessage(ctx, model, theMessage.Parts, &opts)
	} else {
		response, err = generateFromMessages(ctx, model, messages, &opts)
	}
	if err != nil {
		if g.CallbacksHandler != nil {
			g.CallbacksHandler.HandleLLMError(ctx, err)
		}
		return nil, err
	}

	if g.CallbacksHandler != nil {
		g.CallbacksHandler.HandleLLMGenerateContentEnd(ctx, response)
	}
	return response, nil
}

// convertCandidates mirrors googleai provider behavior
func convertCandidates(candidates []*genai.Candidate, usage *genai.UsageMetadata) (*llms.ContentResponse, error) {
	var contentResponse llms.ContentResponse
	var toolCalls []llms.ToolCall

	for _, candidate := range candidates {
		buf := strings.Builder{}
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				switch v := part.(type) {
				case genai.Text:
					_, err := buf.WriteString(string(v))
					if err != nil {
						return nil, err
					}
				case genai.FunctionCall:
					b, err := json.Marshal(v.Args)
					if err != nil {
						return nil, err
					}
					toolCall := llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: v.Name, Arguments: string(b)}}
					toolCalls = append(toolCalls, toolCall)
				default:
					return nil, fmt.Errorf("unknown part type in response: %T", v)
				}
			}
		}
		contentResponse.Choices = append(contentResponse.Choices, &llms.ContentChoice{Content: buf.String(), ToolCalls: toolCalls})
	}
	return &contentResponse, nil
}

func convertParts(parts []llms.ContentPart) ([]genai.Part, error) {
	convertedParts := make([]genai.Part, 0, len(parts))
	for _, part := range parts {
		var out genai.Part
		switch p := part.(type) {
		case llms.TextContent:
			out = genai.Text(p.Text)
		case llms.ToolCall:
			fc := p.FunctionCall
			var argsMap map[string]any
			if err := json.Unmarshal([]byte(fc.Arguments), &argsMap); err != nil {
				return convertedParts, err
			}
			out = genai.FunctionCall{Name: fc.Name, Args: argsMap}
		case llms.ToolCallResponse:
			out = genai.FunctionResponse{Name: p.Name, Response: map[string]any{"response": p.Content}}
		default:
			// Ignore unsupported types here
			continue
		}
		convertedParts = append(convertedParts, out)
	}
	return convertedParts, nil
}

func convertContent(content llms.MessageContent) (*genai.Content, error) {
	parts, err := convertParts(content.Parts)
	if err != nil {
		return nil, err
	}
	c := &genai.Content{Parts: parts}
	switch content.Role {
	case llms.ChatMessageTypeSystem:
		c.Role = "system"
	case llms.ChatMessageTypeAI:
		c.Role = "model"
	case llms.ChatMessageTypeHuman, llms.ChatMessageTypeGeneric, llms.ChatMessageTypeTool:
		c.Role = "user"
	default:
		return nil, fmt.Errorf("unsupported role: %v", content.Role)
	}
	return c, nil
}

func generateFromSingleMessage(ctx context.Context, model *genai.GenerativeModel, parts []llms.ContentPart, opts *llms.CallOptions) (*llms.ContentResponse, error) {
	convertedParts, err := convertParts(parts)
	if err != nil {
		return nil, err
	}
	if opts.StreamingFunc == nil {
		resp, err := model.GenerateContent(ctx, convertedParts...)
		if err != nil {
			return nil, err
		}
		if len(resp.Candidates) == 0 {
			return nil, errors.New("no content in response")
		}
		return convertCandidates(resp.Candidates, resp.UsageMetadata)
	}
	iter := model.GenerateContentStream(ctx, convertedParts...)
	return convertAndStreamFromIterator(ctx, iter, opts)
}

func generateFromMessages(ctx context.Context, model *genai.GenerativeModel, messages []llms.MessageContent, opts *llms.CallOptions) (*llms.ContentResponse, error) {
	history := make([]*genai.Content, 0, len(messages))
	for _, mc := range messages {
		content, err := convertContent(mc)
		if err != nil {
			return nil, err
		}
		if mc.Role == llms.ChatMessageTypeSystem {
			model.SystemInstruction = content
			continue
		}
		history = append(history, content)
	}
	n := len(history)
	reqContent := history[n-1]
	history = history[:n-1]

	session := model.StartChat()
	session.History = history

	if opts.StreamingFunc == nil {
		resp, err := session.SendMessage(ctx, reqContent.Parts...)
		if err != nil {
			return nil, err
		}
		if len(resp.Candidates) == 0 {
			return nil, errors.New("no content in response")
		}
		return convertCandidates(resp.Candidates, resp.UsageMetadata)
	}
	iter := session.SendMessageStream(ctx, reqContent.Parts...)
	return convertAndStreamFromIterator(ctx, iter, opts)
}

func convertAndStreamFromIterator(ctx context.Context, iter *genai.GenerateContentResponseIterator, opts *llms.CallOptions) (*llms.ContentResponse, error) {
	candidate := &genai.Candidate{Content: &genai.Content{}}
DoStream:
	for {
		resp, err := iter.Next()
		if err != nil {
			// Handle normal end-of-stream conditions gracefully
			if errors.Is(err, iterator.Done) || errors.Is(err, io.EOF) || strings.Contains(strings.ToLower(err.Error()), "no more items in iterator") {
				break DoStream
			}
			return nil, fmt.Errorf("error in stream mode: %w", err)
		}
		if len(resp.Candidates) != 1 {
			return nil, fmt.Errorf("expect single candidate in stream mode; got %v", len(resp.Candidates))
		}
		respCandidate := resp.Candidates[0]
		if respCandidate.Content == nil {
			break DoStream
		}
		candidate.Content.Parts = append(candidate.Content.Parts, respCandidate.Content.Parts...)
		candidate.Content.Role = respCandidate.Content.Role
		candidate.FinishReason = respCandidate.FinishReason
		candidate.SafetyRatings = respCandidate.SafetyRatings
		candidate.CitationMetadata = respCandidate.CitationMetadata
		for _, part := range respCandidate.Content.Parts {
			if text, ok := part.(genai.Text); ok {
				if opts.StreamingFunc(ctx, []byte(text)) != nil {
					break DoStream
				}
			}
		}
	}
	mresp := iter.MergedResponse()
	return convertCandidates([]*genai.Candidate{candidate}, mresp.UsageMetadata)
}

// convertTools converts llms.Tools to genai.Tools, preserving array Items and nested schemas.
func convertTools(tools []llms.Tool) ([]*genai.Tool, error) {
	genaiTools := make([]*genai.Tool, 0, len(tools))
	for i, tool := range tools {
		if tool.Type != "function" {
			return nil, fmt.Errorf("tool [%d]: unsupported type %q, want 'function'", i, tool.Type)
		}
		genaiFuncDecl := &genai.FunctionDeclaration{Name: tool.Function.Name, Description: tool.Function.Description}
		params, ok := tool.Function.Parameters.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool [%d]: unsupported type %T of Parameters", i, tool.Function.Parameters)
		}
		schema, err := buildSchema(params)
		if err != nil {
			return nil, fmt.Errorf("tool [%d]: %w", i, err)
		}
		genaiFuncDecl.Parameters = schema
		genaiTools = append(genaiTools, &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{genaiFuncDecl}})
	}
	return genaiTools, nil
}

func buildSchema(m map[string]any) (*genai.Schema, error) {
	s := &genai.Schema{}
	if ty, ok := m["type"].(string); ok {
		s.Type = convertToolSchemaType(ty)
	}
	if desc, ok := m["description"].(string); ok {
		s.Description = desc
	}
	// Handle array items
	if s.Type == genai.TypeArray {
		if items, ok := m["items"].(map[string]any); ok {
			child, err := buildSchema(items)
			if err != nil {
				return nil, err
			}
			s.Items = child
		} else {
			// Default to string items to satisfy Gemini validation
			s.Items = &genai.Schema{Type: genai.TypeString}
		}
		return s, nil
	}
	// Handle object properties
	if props, ok := m["properties"].(map[string]any); ok {
		s.Properties = make(map[string]*genai.Schema)
		for name, pm := range props {
			if pmMap, ok := pm.(map[string]any); ok {
				child, err := buildSchema(pmMap)
				if err != nil {
					return nil, fmt.Errorf("property %s: %w", name, err)
				}
				s.Properties[name] = child
			}
		}
	}
	// Required fields
	if required, ok := m["required"]; ok {
		if rs, ok := required.([]string); ok {
			s.Required = rs
		} else if ri, ok := required.([]any); ok {
			rs := make([]string, 0, len(ri))
			for _, r := range ri {
				if rStr, ok := r.(string); ok {
					rs = append(rs, rStr)
				}
			}
			s.Required = rs
		}
	}
	return s, nil
}

func convertToolSchemaType(ty string) genai.Type {
	switch ty {
	case "object":
		return genai.TypeObject
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	default:
		return genai.TypeUnspecified
	}
}
