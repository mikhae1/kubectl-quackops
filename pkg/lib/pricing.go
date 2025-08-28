package lib

import (
	"fmt"
	"strconv"
	"strings"
)

// DetectPricing parses provider pricing strings and returns a representative numeric price
// and a short display string. The numeric price is the prompt price when available,
// else the completion price, else 0. Short form: "$<prompt>/$<completion>" or "$<value>".
func DetectPricing(promptStr, completionStr string) (float64, string) {
	var (
		promptPrice     float64
		completionPrice float64
		priceShort      string
	)

	var hasPrompt, hasCompletion bool
	if promptStr != "" {
		if p, err := strconv.ParseFloat(promptStr, 64); err == nil {
			promptPrice = p
			hasPrompt = true
		}
	}
	if completionStr != "" {
		if c, err := strconv.ParseFloat(completionStr, 64); err == nil {
			completionPrice = c
			hasCompletion = true
		}
	}

	if hasPrompt && hasCompletion {
		priceShort = fmt.Sprintf("$%g/$%g", promptPrice, completionPrice)
	} else if hasPrompt {
		priceShort = fmt.Sprintf("$%g", promptPrice)
	} else if hasCompletion {
		priceShort = fmt.Sprintf("$%g", completionPrice)
	} else {
		return 0.0, ""
	}

	if hasPrompt {
		return promptPrice, priceShort
	}
	return completionPrice, priceShort
}

// GetDefaultContextLengthForModel returns a reasonable default context length based on model name
func GetDefaultContextLengthForModel(model string) int {
	modelLower := strings.ToLower(strings.TrimSpace(model))
	// OpenAI - modern families
	if strings.Contains(modelLower, "gpt-5") {
		return 128000
	}
	if strings.Contains(modelLower, "gpt-4o") || strings.Contains(modelLower, "gpt-4.1") {
		return 128000
	}
	if strings.Contains(modelLower, "gpt-4") {
		if strings.Contains(modelLower, "32k") {
			return 32768
		}
		if strings.Contains(modelLower, "turbo") || strings.Contains(modelLower, "preview") || strings.Contains(modelLower, "mini") {
			return 128000
		}
		return 8192
	}
	if strings.Contains(modelLower, "gpt-3.5") {
		if strings.Contains(modelLower, "16k") {
			return 16384
		}
		return 4096
	}
	// Google Gemini
	if strings.Contains(modelLower, "gemini") {
		if strings.Contains(modelLower, "2.5") {
			return 2000000
		}
		if strings.Contains(modelLower, "1.5") {
			if strings.Contains(modelLower, "pro") {
				return 2000000
			}
			return 1000000
		}
		return 1000000
	}
	// Anthropic Claude 3.x
	if strings.Contains(modelLower, "claude") {
		return 200000
	}
	// Meta Llama
	if strings.Contains(modelLower, "llama") {
		if strings.Contains(modelLower, "3.2") || strings.Contains(modelLower, "3.1") {
			return 128000
		}
		if strings.Contains(modelLower, "3") {
			return 8192
		}
		return 4096
	}
	// Mistral
	if strings.Contains(modelLower, "mistral") || strings.Contains(modelLower, "mixtral") {
		if strings.Contains(modelLower, "large") {
			return 128000
		}
		return 32768
	}
	// Qwen
	if strings.Contains(modelLower, "qwen") {
		return 131072
	}
	// GLM (Zhipu/ChatGLM)
	if strings.Contains(modelLower, "glm") {
		return 128000
	}
	// DeepSeek
	if strings.Contains(modelLower, "deepseek") {
		return 131072
	}
	// Cohere Command family (via OpenRouter)
	if strings.Contains(modelLower, "command") {
		return 128000
	}
	// Default for unknown models (modern, conservative)
	return 32768
}
