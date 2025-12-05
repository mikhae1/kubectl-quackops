package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"iter"

	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/llms"
	"google.golang.org/genai"
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
	cfg := &genai.ClientConfig{Backend: genai.BackendGeminiAPI}
	if strings.TrimSpace(apiKey) != "" {
		cfg.APIKey = apiKey
	}
	c, err := genai.NewClient(ctx, cfg)
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

	config, err := buildGenerateConfig(opts)
	if err != nil {
		return nil, err
	}

	var response *llms.ContentResponse
	if len(messages) == 1 {
		theMessage := messages[0]
		if theMessage.Role != llms.ChatMessageTypeHuman {
			return nil, fmt.Errorf("got %v message role, want human", theMessage.Role)
		}
		response, err = g.generateFromSingleMessage(ctx, modelName, theMessage, config, &opts)
	} else {
		response, err = g.generateFromMessages(ctx, modelName, messages, config, &opts)
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
func convertCandidates(candidates []*genai.Candidate, usage *genai.GenerateContentResponseUsageMetadata) (*llms.ContentResponse, error) {
	var contentResponse llms.ContentResponse
	var toolCalls []llms.ToolCall

	for _, candidate := range candidates {
		buf := strings.Builder{}
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				switch {
				case part.Text != "":
					if _, err := buf.WriteString(part.Text); err != nil {
						return nil, err
					}
				case part.FunctionCall != nil:
					b, err := json.Marshal(part.FunctionCall.Args)
					if err != nil {
						return nil, err
					}
					toolCall := llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: part.FunctionCall.Name, Arguments: string(b)}}
					toolCalls = append(toolCalls, toolCall)
				case part.FunctionResponse != nil:
					b, err := json.Marshal(part.FunctionResponse.Response)
					if err != nil {
						return nil, err
					}
					toolCall := llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: part.FunctionResponse.Name, Arguments: string(b)}}
					toolCalls = append(toolCalls, toolCall)
				default:
					return nil, fmt.Errorf("unknown part type in response: %+v", part)
				}
			}
		}
		contentResponse.Choices = append(contentResponse.Choices, &llms.ContentChoice{Content: buf.String(), ToolCalls: toolCalls})
	}
	return &contentResponse, nil
}

func buildGenerateConfig(opts llms.CallOptions) (*genai.GenerateContentConfig, error) {
	cfg := &genai.GenerateContentConfig{}

	// Gemini requires candidate_count in [1,8]. Clamp to safe defaults when unset or out of range.
	cand := opts.CandidateCount
	if cand < 1 {
		cand = 1
	} else if cand > 8 {
		cand = 8
	}
	cfg.CandidateCount = int32(cand)
	if opts.MaxTokens > 0 {
		cfg.MaxOutputTokens = int32(opts.MaxTokens)
	}
	cfg.Temperature = genai.Ptr(float32(opts.Temperature))
	cfg.TopP = genai.Ptr(float32(opts.TopP))
	cfg.TopK = genai.Ptr(float32(opts.TopK))
	cfg.StopSequences = opts.StopWords

	tools, err := convertTools(opts.Tools)
	if err != nil {
		return nil, err
	}
	cfg.Tools = tools

	return cfg, nil
}

func convertParts(parts []llms.ContentPart) ([]*genai.Part, error) {
	convertedParts := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		var out *genai.Part
		switch p := part.(type) {
		case llms.TextContent:
			out = &genai.Part{Text: p.Text}
		case llms.ToolCall:
			fc := p.FunctionCall
			var argsMap map[string]any
			if err := json.Unmarshal([]byte(fc.Arguments), &argsMap); err != nil {
				return convertedParts, err
			}
			out = &genai.Part{FunctionCall: &genai.FunctionCall{Name: fc.Name, Args: argsMap}}
		case llms.ToolCallResponse:
			var respMap map[string]any
			if err := json.Unmarshal([]byte(p.Content), &respMap); err != nil {
				respMap = map[string]any{"response": p.Content}
			}
			out = &genai.Part{FunctionResponse: &genai.FunctionResponse{Name: p.Name, Response: respMap}}
		default:
			// Ignore unsupported types here
			continue
		}
		convertedParts = append(convertedParts, out)
	}
	return convertedParts, nil
}

func convertContent(content llms.MessageContent, cfg *genai.GenerateContentConfig) (*genai.Content, error) {
	parts, err := convertParts(content.Parts)
	if err != nil {
		return nil, err
	}
	c := &genai.Content{Parts: parts}
	switch content.Role {
	case llms.ChatMessageTypeSystem:
		c.Role = "system"
		cfg.SystemInstruction = c
		return nil, nil
	case llms.ChatMessageTypeAI:
		c.Role = "model"
	case llms.ChatMessageTypeHuman, llms.ChatMessageTypeGeneric, llms.ChatMessageTypeTool:
		c.Role = "user"
	default:
		return nil, fmt.Errorf("unsupported role: %v", content.Role)
	}
	return c, nil
}

func (g *GoogleNative) generateFromSingleMessage(ctx context.Context, modelName string, message llms.MessageContent, cfg *genai.GenerateContentConfig, opts *llms.CallOptions) (*llms.ContentResponse, error) {
	convertedParts, err := convertParts(message.Parts)
	if err != nil {
		return nil, err
	}
	contents := []*genai.Content{{Role: "user", Parts: convertedParts}}
	if opts.StreamingFunc == nil {
		resp, err := g.client.Models.GenerateContent(ctx, modelName, contents, cfg)
		if err != nil {
			return nil, err
		}
		if len(resp.Candidates) == 0 {
			return nil, errors.New("no content in response")
		}
		return convertCandidates(resp.Candidates, resp.UsageMetadata)
	}
	stream := g.client.Models.GenerateContentStream(ctx, modelName, contents, cfg)
	return convertAndStream(ctx, stream, opts)
}

func (g *GoogleNative) generateFromMessages(ctx context.Context, modelName string, messages []llms.MessageContent, cfg *genai.GenerateContentConfig, opts *llms.CallOptions) (*llms.ContentResponse, error) {
	contents := make([]*genai.Content, 0, len(messages))
	for _, mc := range messages {
		content, err := convertContent(mc, cfg)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}
		contents = append(contents, content)
	}
	if len(contents) == 0 {
		return nil, errors.New("no user/model messages provided")
	}
	if opts.StreamingFunc == nil {
		resp, err := g.client.Models.GenerateContent(ctx, modelName, contents, cfg)
		if err != nil {
			return nil, err
		}
		if len(resp.Candidates) == 0 {
			return nil, errors.New("no content in response")
		}
		return convertCandidates(resp.Candidates, resp.UsageMetadata)
	}
	stream := g.client.Models.GenerateContentStream(ctx, modelName, contents, cfg)
	return convertAndStream(ctx, stream, opts)
}

func convertAndStream(ctx context.Context, stream iter.Seq2[*genai.GenerateContentResponse, error], opts *llms.CallOptions) (*llms.ContentResponse, error) {
	candidate := &genai.Candidate{Content: &genai.Content{}}
	var usage *genai.GenerateContentResponseUsageMetadata
	for resp, err := range stream {
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("error in stream mode: %w", err)
		}
		if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
			continue
		}
		respCandidate := resp.Candidates[0]
		candidate.Content.Parts = append(candidate.Content.Parts, respCandidate.Content.Parts...)
		if candidate.Content.Role == "" {
			candidate.Content.Role = respCandidate.Content.Role
		}
		usage = resp.UsageMetadata
		for _, part := range respCandidate.Content.Parts {
			if part.Text != "" && opts.StreamingFunc != nil {
				if opts.StreamingFunc(ctx, []byte(part.Text)) != nil {
					return convertCandidates([]*genai.Candidate{candidate}, usage)
				}
			}
		}
	}
	return convertCandidates([]*genai.Candidate{candidate}, usage)
}

// convertTools converts llms.Tools to a single genai.Tool with all function declarations.
// Gemini expects tools as an array, but best-practice is to include all function_declarations
// in one Tool object. This ensures all MCP functions are exposed.
func convertTools(tools []llms.Tool) ([]*genai.Tool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	funcDecls := make([]*genai.FunctionDeclaration, 0, len(tools))
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
		funcDecls = append(funcDecls, genaiFuncDecl)
	}
	return []*genai.Tool{{FunctionDeclarations: funcDecls}}, nil
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
