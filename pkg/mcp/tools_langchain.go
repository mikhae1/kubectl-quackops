package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/tmc/langchaingo/llms"
)

// DiscoverLangchainTools maps MCP tools to LangChainGo tools.
func DiscoverLangchainTools(cfg *config.Config) []llms.Tool {
	toolInfos := GetToolInfos(cfg)
	if len(toolInfos) == 0 {
		logger.Log("info", "No MCP tools discovered for LangChain mapping")
		return []llms.Tool{}
	}

	return convertToolInfosToLangchain(toolInfos)
}

// DiscoverLangchainToolsForServer maps MCP tools from a specific server to LangChainGo tools.
// Used when a prompt is active to filter tools to the same server as the prompt.
func DiscoverLangchainToolsForServer(cfg *config.Config, serverName string) []llms.Tool {
	toolInfos := GetToolInfosByServer(cfg, serverName)
	if len(toolInfos) == 0 {
		logger.Log("info", "No MCP tools discovered for server '%s'", serverName)
		return []llms.Tool{}
	}

	logger.Log("info", "Discovering LangChain tools for server '%s' (%d tools)", serverName, len(toolInfos))
	return convertToolInfosToLangchain(toolInfos)
}

// convertToolInfosToLangchain converts ToolInfo slice to LangChainGo tools
func convertToolInfosToLangchain(toolInfos []ToolInfo) []llms.Tool {
	tools := make([]llms.Tool, 0, len(toolInfos))

	for idx, ti := range toolInfos {
		desc := strings.TrimSpace(ti.Description)
		if desc == "" && ti.Title != "" {
			desc = strings.TrimSpace(ti.Title)
		}
		if desc == "" {
			desc = fmt.Sprintf("MCP tool: %s", ti.Name)
		}

		convertedSchema := schemaToAny(ti.InputSchema)
		convertedSchema = sanitizeForGeminiSchema(convertedSchema, ti.Name)

		issues := scanSchemaArrayIssues(convertedSchema)
		if len(issues) > 0 {
			logger.Log("info", "[LLM Tools] Tool[%d] %s has potential array schema issues: %v", idx, ti.Name, issues)
			if schemaBytes, err := json.MarshalIndent(convertedSchema, "", "  "); err == nil {
				logger.Log("info", "[LLM Tools] Tool[%d] %s parameters schema:\n%s", idx, ti.Name, string(schemaBytes))
			}
		} else {
			summary := summarizeArrayProperties(convertedSchema)
			if summary != "" {
				logger.Log("debug", "[LLM Tools] Tool[%d] %s array params: %s", idx, ti.Name, summary)
			}
		}

		lcTool := llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        ti.Name,
				Description: desc,
				Parameters:  convertedSchema,
			},
		}

		tools = append(tools, lcTool)
		logger.Log("debug", "Mapped MCP tool %s to LangChain function with schema", ti.Name)
	}

	logger.Log("info", "Successfully mapped %d MCP tools to LangChain functions", len(tools))

	// Extra Gemini schema debug: print selected fields for known tools
	logSelectedParameterSchemasForGeminiDebug(tools)
	return tools
}

// ExtractToolNames extracts tool names from LangChain tools for logging.
func ExtractToolNames(tools []llms.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Function != nil {
			names = append(names, tool.Function.Name)
		}
	}
	return names
}

// schemaToAny converts a JSON Schema to a generic any for llms.FunctionDefinition
// and applies fixes to satisfy strict providers like Gemini.
func schemaToAny(s any) any {
	if s == nil {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	if m, ok := s.(map[string]any); ok {
		inlined := inlineLocalRefsDeep(m, m)
		if inlinedMap, ok := inlined.(map[string]any); ok {
			fixed := fixArraySchemas(inlinedMap)
			fixed = validateAndFixGoogleAISchema(fixed)
			if _, hasType := fixed["type"]; !hasType {
				fixed["type"] = "object"
			}
			return fixed
		}
		fixed := fixArraySchemas(m)
		fixed = validateAndFixGoogleAISchema(fixed)
		if _, hasType := fixed["type"]; !hasType {
			fixed["type"] = "object"
		}
		return fixed
	}
	b, err := json.Marshal(s)
	if err != nil {
		logger.Log("warn", "Failed to marshal schema: %v", err)
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		logger.Log("warn", "Failed to unmarshal schema: %v", err)
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if m, ok := out.(map[string]any); ok {
		inlined := inlineLocalRefsDeep(m, m)
		if inlinedMap, ok := inlined.(map[string]any); ok {
			fixed := fixArraySchemas(inlinedMap)
			fixed = validateAndFixGoogleAISchema(fixed)
			if _, hasType := fixed["type"]; !hasType {
				fixed["type"] = "object"
			}
			return fixed
		}
		fixed := fixArraySchemas(m)
		fixed = validateAndFixGoogleAISchema(fixed)
		if _, hasType := fixed["type"]; !hasType {
			fixed["type"] = "object"
		}
		return fixed
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// fixArraySchemas recursively fixes array schemas to satisfy Google AI API requirements
func fixArraySchemas(schema map[string]any) map[string]any {
	result := make(map[string]any)
	for key, value := range schema {
		switch key {
		case "properties":
			if props, ok := value.(map[string]any); ok {
				fixedProps := make(map[string]any)
				for propName, propValue := range props {
					if propMap, ok := propValue.(map[string]any); ok {
						fixed := fixArraySchemas(propMap)
						promoteArrayFromCombinators(fixed)
						if isArrayType(fixed["type"]) {
							if _, hasItems := fixed["items"]; !hasItems {
								fixed["items"] = defaultItemsForProperty(propName)
							} else if itemsMap, ok := fixed["items"].(map[string]any); ok {
								if _, hasType := itemsMap["type"]; !hasType {
									fixed["items"] = defaultItemsForProperty(propName)
								}
							}
						}
						fixedProps[propName] = fixed
					} else {
						fixedProps[propName] = propValue
					}
				}
				result[key] = fixedProps
			} else {
				result[key] = value
			}
		case "anyOf", "oneOf", "allOf":
			if list, ok := value.([]any); ok {
				newList := make([]any, 0, len(list))
				for _, elem := range list {
					if elemMap, ok := elem.(map[string]any); ok {
						normalized := fixArraySchemas(elemMap)
						promoteArrayFromCombinators(normalized)
						newList = append(newList, normalized)
					} else {
						newList = append(newList, elem)
					}
				}
				result[key] = newList
			} else {
				result[key] = value
			}
		case "items":
			if itemsValue, ok := value.(map[string]any); ok {
				fixedItems := fixArraySchemas(itemsValue)
				if _, hasType := fixedItems["type"]; !hasType {
					fixedItems["type"] = "object"
					if _, hasProps := fixedItems["properties"]; !hasProps {
						fixedItems["properties"] = map[string]any{}
					}
				}
				result[key] = fixedItems
			} else {
				result[key] = map[string]any{"type": "string"}
			}
		case "type":
			if isArrayType(value) {
				result[key] = value
				if _, hasItems := schema["items"]; !hasItems {
					result["items"] = map[string]any{"type": "object", "properties": map[string]any{}}
				}
			} else {
				result[key] = value
			}
		default:
			if nestedMap, ok := value.(map[string]any); ok {
				result[key] = fixArraySchemas(nestedMap)
			} else {
				result[key] = value
			}
		}
	}
	if isArrayType(result["type"]) {
		if items, hasItems := result["items"]; hasItems {
			if itemsMap, ok := items.(map[string]any); ok {
				if _, hasType := itemsMap["type"]; !hasType {
					itemsMap["type"] = "object"
					itemsMap["properties"] = map[string]any{}
				}
				if itemsMap["type"] == "object" {
					if _, hasProps := itemsMap["properties"]; !hasProps {
						itemsMap["properties"] = map[string]any{}
					}
				}
			}
		} else {
			result["items"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
	}
	return result
}

// validateAndFixGoogleAISchema performs aggressive validation and fixing for Google AI API
func validateAndFixGoogleAISchema(schema map[string]any) map[string]any {
	fixed := deepScanAndFixArrays(schema)
	fixedMap, ok := fixed.(map[string]any)
	if !ok {
		return schema
	}
	applyKnownArrayItemDefaultsRecursive(fixedMap)
	return fixedMap
}

// deepScanAndFixArrays recursively scans and fixes all array types
func deepScanAndFixArrays(obj any) any {
	switch v := obj.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, value := range v {
			result[key] = deepScanAndFixArrays(value)
		}
		if isArrayType(result["type"]) {
			if items, hasItems := result["items"]; !hasItems || items == nil {
				result["items"] = map[string]any{"type": "object", "properties": map[string]any{}}
			} else if itemsMap, ok := items.(map[string]any); ok {
				if _, hasType := itemsMap["type"]; !hasType {
					itemsMap["type"] = "object"
				}
				if itemsMap["type"] == "object" {
					if _, hasProps := itemsMap["properties"]; !hasProps {
						itemsMap["properties"] = map[string]any{}
					}
				}
			} else {
				result["items"] = map[string]any{"type": "object", "properties": map[string]any{}}
			}
		}
		promoteArrayFromCombinators(result)
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = deepScanAndFixArrays(item)
		}
		return result
	default:
		return v
	}
}

// isArrayType returns true if the provided type declaration corresponds to an array.
func isArrayType(t any) bool {
	switch tv := t.(type) {
	case string:
		return tv == "array"
	case []any:
		for _, e := range tv {
			if es, ok := e.(string); ok && es == "array" {
				return true
			}
		}
		return false
	case []string:
		for _, es := range tv {
			if es == "array" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// applyKnownArrayItemDefaults sets item schemas for known array properties when missing or incomplete.
func applyKnownArrayItemDefaultsRecursive(schema map[string]any) {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, prop := range props {
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		if isArrayType(propMap["type"]) {
			desired := defaultItemsForProperty(name)
			if _, hasItems := propMap["items"]; !hasItems {
				propMap["items"] = desired
			} else if itemsMap, ok := propMap["items"].(map[string]any); ok {
				if t, hasType := itemsMap["type"]; !hasType {
					propMap["items"] = desired
				} else if ts, ok := t.(string); ok {
					if strings.EqualFold(name, "jsonPaths") {
						if ts != "object" {
							propMap["items"] = desired
						} else {
							pm, _ := itemsMap["properties"].(map[string]any)
							if pm == nil || pm["path"] == nil {
								if pm == nil {
									pm = map[string]any{}
								}
								pm["path"] = map[string]any{"type": "string"}
								itemsMap["properties"] = pm
								propMap["items"] = itemsMap
							}
						}
					} else if ts == "object" {
						pm, _ := itemsMap["properties"].(map[string]any)
						if len(pm) == 0 {
							propMap["items"] = desired
						}
					}
				}
			}
			props[name] = propMap
		}
		applyKnownArrayItemDefaultsRecursive(propMap)
	}
}

// defaultItemsForProperty provides reasonable defaults for array items based on common property names
func defaultItemsForProperty(propertyName string) map[string]any {
	lower := strings.ToLower(propertyName)
	switch lower {
	case "args", "argv", "prometheusqueries", "dnsnames", "names", "namespaces", "resources", "kinds", "labels":
		return map[string]any{"type": "string"}
	case "jsonpaths":
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}
	default:
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
}

// inlineLocalRefsDeep replaces local JSON Schema $ref references with the referenced schema content.
func inlineLocalRefsDeep(node any, root map[string]any) any {
	switch n := node.(type) {
	case map[string]any:
		if refVal, ok := n["$ref"].(string); ok {
			if target, ok := resolveLocalRef(refVal, root); ok {
				merged := make(map[string]any)
				if tMap, ok := target.(map[string]any); ok {
					for k, v := range tMap {
						merged[k] = inlineLocalRefsDeep(v, root)
					}
				}
				for k, v := range n {
					if k == "$ref" {
						continue
					}
					merged[k] = inlineLocalRefsDeep(v, root)
				}
				return merged
			}
		}
		out := make(map[string]any)
		for k, v := range n {
			out[k] = inlineLocalRefsDeep(v, root)
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = inlineLocalRefsDeep(v, root)
		}
		return out
	default:
		return n
	}
}

// resolveLocalRef looks up a local JSON pointer like "#/$defs/X"
func resolveLocalRef(ref string, root map[string]any) (any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	path := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	cur := any(root)
	for _, p := range path {
		if m, ok := cur.(map[string]any); ok {
			if next, ok := m[p]; ok {
				cur = next
			} else {
				return nil, false
			}
		} else {
			return nil, false
		}
	}
	return cur, true
}

// promoteArrayFromCombinators promotes array definition from anyOf/oneOf/allOf branches
func promoteArrayFromCombinators(obj map[string]any) {
	if isArrayType(obj["type"]) && obj["items"] != nil {
		return
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if list, ok := obj[key].([]any); ok {
			for _, elem := range list {
				if m, ok := elem.(map[string]any); ok {
					if isArrayType(m["type"]) {
						items := m["items"]
						if items == nil {
							items = map[string]any{"type": "object", "properties": map[string]any{}}
						}
						obj["type"] = "array"
						obj["items"] = items
						return
					}
				}
			}
		}
	}
}

// scanSchemaArrayIssues walks a schema and returns issues describing arrays lacking proper items/type.
func scanSchemaArrayIssues(schema any) []string {
	var issues []string
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch n := node.(type) {
		case map[string]any:
			if isArrayType(n["type"]) {
				if _, ok := n["items"]; !ok {
					issues = append(issues, fmt.Sprintf("%s: array missing items", path))
				} else if m, ok := n["items"].(map[string]any); ok {
					if _, hasType := m["type"]; !hasType {
						issues = append(issues, fmt.Sprintf("%s: items missing type", path))
					}
				}
			}
			if props, ok := n["properties"].(map[string]any); ok {
				for k, v := range props {
					walk(v, path+".properties."+k)
				}
			}
			if it, ok := n["items"]; ok {
				walk(it, path+".items")
			}
			for _, key := range []string{"anyOf", "oneOf", "allOf"} {
				if list, ok := n[key].([]any); ok {
					for i, e := range list {
						walk(e, fmt.Sprintf("%s.%s[%d]", path, key, i))
					}
				}
			}
		case []any:
			for i, e := range n {
				walk(e, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	walk(schema, "$")
	return issues
}

// summarizeArrayProperties provides a compact summary of array fields and their item type.
func summarizeArrayProperties(schema any) string {
	summaries := []string{}
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch n := node.(type) {
		case map[string]any:
			if isArrayType(n["type"]) {
				itemType := "<none>"
				if it, ok := n["items"].(map[string]any); ok {
					if t, ok := it["type"].(string); ok {
						itemType = t
					}
				}
				summaries = append(summaries, fmt.Sprintf("%s(items:%s)", path, itemType))
			}
			if props, ok := n["properties"].(map[string]any); ok {
				for k, v := range props {
					walk(v, path+".properties."+k)
				}
			}
			if it, ok := n["items"]; ok {
				walk(it, path+".items")
			}
			for _, key := range []string{"anyOf", "oneOf", "allOf"} {
				if list, ok := n[key].([]any); ok {
					for i, e := range list {
						walk(e, fmt.Sprintf("%s.%s[%d]", path, key, i))
					}
				}
			}
		case []any:
			for i, e := range n {
				walk(e, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	walk(schema, "$")
	return strings.Join(summaries, ", ")
}

// logSelectedParameterSchemasForGeminiDebug prints the exact schemas Gemini complains about.
func logSelectedParameterSchemasForGeminiDebug(tools []llms.Tool) {
	targetFields := map[string][]string{
		"kube_metrics": {"prometheusQueries"},
		"kube_log":     {"jsonPaths"},
		"kube_exec":    {"args"},
		"kube_net":     {"dnsNames"},
	}
	for i, t := range tools {
		if t.Function == nil || t.Function.Parameters == nil {
			continue
		}
		name := t.Function.Name
		wanted, ok := targetFields[name]
		if !ok {
			continue
		}
		params, ok := t.Function.Parameters.(map[string]any)
		if !ok {
			continue
		}
		props, _ := params["properties"].(map[string]any)
		if props == nil {
			logger.Log("info", "[LLM Tools] Tool[%d] %s has no properties in parameters", i, name)
			continue
		}
		dump := map[string]any{}
		for _, field := range wanted {
			if v, ok := props[field]; ok {
				dump[field] = v
			}
		}
		if len(dump) > 0 {
			if b, err := json.MarshalIndent(dump, "", "  "); err == nil {
				logger.Log("info", "[LLM Tools] Tool[%d] %s selected fields schema:\n%s", i, name, string(b))
			}
		}
	}
}

// sanitizeForGeminiSchema removes non-JSON-Schema keywords and applies strict overrides
// for fields known to cause Gemini validation errors.
func sanitizeForGeminiSchema(schema any, toolName string) any {
	cleaned := removeUnknownKeywords(schema)
	if root, ok := cleaned.(map[string]any); ok {
		if root["type"] == nil {
			root["type"] = "object"
		}
		if props, ok := root["properties"].(map[string]any); ok {
			enforce := func(field string, s map[string]any) {
				if _, exists := props[field]; exists {
					props[field] = s
				}
			}
			switch toolName {
			case "kube_metrics":
				enforce("prometheusQueries", map[string]any{"type": "array", "items": map[string]any{"type": "string"}})
			case "kube_log":
				enforce("jsonPaths", map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path":   map[string]any{"type": "string"},
							"equals": map[string]any{"type": "string"},
							"regex":  map[string]any{"type": "string"},
						},
					},
				})
			case "kube_exec":
				enforce("args", map[string]any{"type": "array", "items": map[string]any{"type": "string"}})
			case "kube_net":
				enforce("dnsNames", map[string]any{"type": "array", "items": map[string]any{"type": "string"}})
			}
		}
		return root
	}
	return cleaned
}

// removeUnknownKeywords drops non-standard keywords (like "optional").
func removeUnknownKeywords(node any) any {
	switch n := node.(type) {
	case map[string]any:
		out := make(map[string]any)
		for k, v := range n {
			if k == "optional" || k == "$schema" || k == "$id" || k == "examples" || k == "default" {
				continue
			}
			out[k] = removeUnknownKeywords(v)
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = removeUnknownKeywords(v)
		}
		return out
	default:
		return n
	}
}

// SchemaToAny is an exported wrapper for internal schemaToAny for use by other packages and tests.
func SchemaToAny(s any) any { return schemaToAny(s) }

// FixArraySchemas is an exported wrapper for internal fixArraySchemas for use by other packages and tests.
func FixArraySchemas(schema map[string]any) map[string]any { return fixArraySchemas(schema) }

// DeepScanAndFixArrays is an exported wrapper for internal deepScanAndFixArrays for use by other packages and tests.
func DeepScanAndFixArrays(obj any) any { return deepScanAndFixArrays(obj) }
