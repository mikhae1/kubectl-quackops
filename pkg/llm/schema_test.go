package llm

import (
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/mcp"
)

func TestFixArraySchemas(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name: "array without items",
			input: map[string]any{
				"type": "array",
			},
			expected: map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
		{
			name: "array with empty items",
			input: map[string]any{
				"type":  "array",
				"items": map[string]any{},
			},
			expected: map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
		{
			name: "nested properties with arrays",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"list": map[string]any{
						"type": "array",
					},
				},
			},
			expected: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"list": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":       "object",
							"properties": map[string]any{},
						},
					},
				},
			},
		},
		{
			name: "complex nested array schema",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jsonPaths": map[string]any{
						"type": "array",
						"items": map[string]any{
							"properties": map[string]any{
								"path": map[string]any{
									"type": "string",
								},
							},
						},
					},
				},
			},
			expected: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jsonPaths": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"path": map[string]any{
									"type": "string",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mcp.FixArraySchemas(tt.input)

			// Check type
			if result["type"] != tt.expected["type"] {
				t.Errorf("Expected type %v, got %v", tt.expected["type"], result["type"])
			}

			// Check items if present
			if expectedItems, exists := tt.expected["items"]; exists {
				if resultItems, hasItems := result["items"]; hasItems {
					if itemsMap, ok := resultItems.(map[string]any); ok {
						if expectedItemsMap, ok := expectedItems.(map[string]any); ok {
							if itemsMap["type"] != expectedItemsMap["type"] {
								t.Errorf("Expected items type %v, got %v", expectedItemsMap["type"], itemsMap["type"])
							}
						}
					}
				} else {
					t.Error("Expected items to be present")
				}
			}
		})
	}
}

func TestDeepScanAndFixArrays(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		verify func(t *testing.T, result any)
	}{
		{
			name: "nested array without proper items",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prometheusQueries": map[string]any{
						"type": "array",
					},
					"jsonPaths": map[string]any{
						"type": "array",
						"items": map[string]any{
							"properties": map[string]any{
								"path": map[string]any{
									"type": "string",
								},
							},
						},
					},
				},
			},
			verify: func(t *testing.T, result any) {
				resultMap, ok := result.(map[string]any)
				if !ok {
					t.Fatal("Expected result to be a map")
				}

				props, ok := resultMap["properties"].(map[string]any)
				if !ok {
					t.Fatal("Expected properties to be a map")
				}

				// Check prometheusQueries
				if pq, exists := props["prometheusQueries"]; exists {
					if pqMap, ok := pq.(map[string]any); ok {
						if items, hasItems := pqMap["items"]; hasItems {
							if itemsMap, ok := items.(map[string]any); ok {
								if itemsMap["type"] != "object" {
									t.Errorf("Expected prometheusQueries items type to be object, got %v", itemsMap["type"])
								}
								if _, hasProps := itemsMap["properties"]; !hasProps {
									t.Error("Expected prometheusQueries items to have properties")
								}
							}
						} else {
							t.Error("Expected prometheusQueries to have items")
						}
					}
				}

				// Check jsonPaths
				if jp, exists := props["jsonPaths"]; exists {
					if jpMap, ok := jp.(map[string]any); ok {
						if items, hasItems := jpMap["items"]; hasItems {
							if itemsMap, ok := items.(map[string]any); ok {
								if itemsMap["type"] != "object" {
									t.Errorf("Expected jsonPaths items type to be object, got %v", itemsMap["type"])
								}
								if _, hasProps := itemsMap["properties"]; !hasProps {
									t.Error("Expected jsonPaths items to have properties")
								}
							}
						}
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mcp.DeepScanAndFixArrays(tt.input)
			tt.verify(t, result)
		})
	}
}

func TestSchemaToAny(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		hasItems bool
	}{
		{
			name:     "nil input",
			input:    nil,
			hasItems: false,
		},
		{
			name: "array schema without items",
			input: map[string]any{
				"type": "array",
			},
			hasItems: true,
		},
		{
			name: "object schema",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
			},
			hasItems: false,
		},
		{
			name: "union type array with missing items",
			input: map[string]any{
				"type": []any{"array", "null"},
			},
			hasItems: true,
		},
		{
			name: "known property defaults applied",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"args": map[string]any{
						"type": "array",
					},
					"dnsNames": map[string]any{
						"type": "array",
					},
					"prometheusQueries": map[string]any{
						"type": "array",
					},
					"jsonPaths": map[string]any{
						"type": "array",
					},
				},
			},
			hasItems: false, // we'll validate items per-property below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mcp.SchemaToAny(tt.input)

			// Result should always be a map
			resultMap, ok := result.(map[string]any)
			if !ok {
				t.Fatal("Expected result to be a map[string]any")
			}

			// Should always have a type
			if _, hasType := resultMap["type"]; !hasType {
				t.Error("Expected result to have a type field")
			}

			// Check items if expected for array at top-level
			if tt.hasItems {
				if _, hasItems := resultMap["items"]; !hasItems {
					t.Error("Expected result to have items field for array type")
				}
			}

			// Additional check for known defaults
			if tt.name == "known property defaults applied" {
				props := resultMap["properties"].(map[string]any)
				if a := props["args"].(map[string]any); a["type"] != "array" || a["items"].(map[string]any)["type"] != "string" {
					t.Errorf("expected args items to be string, got %v", a["items"])
				}
				if d := props["dnsNames"].(map[string]any); d["type"] != "array" || d["items"].(map[string]any)["type"] != "string" {
					t.Errorf("expected dnsNames items to be string, got %v", d["items"])
				}
				if p := props["prometheusQueries"].(map[string]any); p["type"] != "array" || p["items"].(map[string]any)["type"] != "string" {
					t.Errorf("expected prometheusQueries items to be string, got %v", p["items"])
				}
				if j := props["jsonPaths"].(map[string]any); j["type"] != "array" {
					t.Errorf("expected jsonPaths type array")
				} else {
					items := j["items"].(map[string]any)
					if items["type"] != "object" {
						t.Errorf("expected jsonPaths items to be object, got %v", items["type"])
					}
					propsMap := items["properties"].(map[string]any)
					if _, ok := propsMap["path"]; !ok {
						t.Errorf("expected jsonPaths items.properties.path to be present")
					}
				}
			}
		})
	}
}
