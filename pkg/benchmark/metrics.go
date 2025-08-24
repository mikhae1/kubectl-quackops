package benchmark

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
)

// BenchmarkResults contains all benchmark execution results
type BenchmarkResults struct {
	Config          BenchmarkConfig    `json:"config"`
	StartTime       time.Time          `json:"start_time"`
	EndTime         time.Time          `json:"end_time"`
	TotalDuration   time.Duration      `json:"total_duration"`
	ProviderResults []*ProviderResult  `json:"provider_results"`
	Summary         *BenchmarkSummary  `json:"summary"`
}

// BenchmarkSummary provides high-level benchmark insights
type BenchmarkSummary struct {
	TotalScenarios     int                        `json:"total_scenarios"`
	TotalProviders     int                        `json:"total_providers"`
	OverallSuccessRate float64                    `json:"overall_success_rate"`
	FastestProvider    string                     `json:"fastest_provider"`
	MostReliable       string                     `json:"most_reliable"`
	MostCostEffective  string                     `json:"most_cost_effective"`
	BestQuality        string                     `json:"best_quality"`
	Rankings          map[string]ProviderRanking `json:"rankings"`
}

// ProviderRanking contains ranking information for a provider
type ProviderRanking struct {
	Provider         string  `json:"provider"`
	PerformanceRank  int     `json:"performance_rank"`
	ReliabilityRank  int     `json:"reliability_rank"`
	CostRank         int     `json:"cost_rank"`
	QualityRank      int     `json:"quality_rank"`
	OverallScore     float64 `json:"overall_score"`
}

// ProviderResult contains results for a specific provider/model combination
type ProviderResult struct {
	Target          BenchmarkTarget               `json:"target"`
	StartTime       time.Time                     `json:"start_time"`
	EndTime         time.Time                     `json:"end_time"`
	Duration        time.Duration                 `json:"duration"`
	ScenarioResults map[string]*ScenarioResult    `json:"scenario_results"`
	AggregateStats  AggregateStats                `json:"aggregate_stats"`
}

// BenchmarkTarget identifies a specific provider/model combination
type BenchmarkTarget struct {
	Provider string `json:"provider"` // openai, anthropic, google, ollama
	Model    string `json:"model"`    // specific model name
	BaseURL  string `json:"base_url"` // custom base URL if needed
}

// ScenarioResult contains results for a specific test scenario
type ScenarioResult struct {
	Scenario  TestScenario        `json:"scenario"`
	Target    BenchmarkTarget     `json:"target"`
	StartTime time.Time           `json:"start_time"`
	EndTime   time.Time           `json:"end_time"`
	Duration  time.Duration       `json:"duration"`
	Runs      []RunResult         `json:"runs"`
	Stats     ScenarioStats       `json:"stats"`
}

// RunResult contains results from a single test run
type RunResult struct {
	Iteration int                    `json:"iteration"`
	StartTime time.Time              `json:"start_time"`
	EndTime   time.Time              `json:"end_time"`
	Duration  time.Duration          `json:"duration"`
	Success   bool                   `json:"success"`
	Response  string                 `json:"response,omitempty"`
	Error     error                  `json:"error,omitempty"`
	Metrics   map[string]interface{} `json:"metrics"`
	IsWarmup  bool                   `json:"is_warmup"`
}

// ScenarioStats contains statistical analysis of scenario runs
type ScenarioStats struct {
	TotalRuns          int           `json:"total_runs"`
	SuccessfulRuns     int           `json:"successful_runs"`
	SuccessRate        float64       `json:"success_rate"`
	MinResponseTime    time.Duration `json:"min_response_time"`
	MaxResponseTime    time.Duration `json:"max_response_time"`
	AvgResponseTime    time.Duration `json:"avg_response_time"`
	MedianResponseTime time.Duration `json:"median_response_time"`
	AvgInputTokens     float64       `json:"avg_input_tokens"`
	AvgOutputTokens    float64       `json:"avg_output_tokens"`
	AvgCostPerRun      float64       `json:"avg_cost_per_run"`
	QualityScore       float64       `json:"quality_score"`
	TokensPerSecond    float64       `json:"tokens_per_second"`
	ErrorTypes         map[string]int `json:"error_types"`
}

// AggregateStats contains overall statistics across all scenarios for a provider
type AggregateStats struct {
	OverallSuccessRate    float64 `json:"overall_success_rate"`
	AvgResponseTime       float64 `json:"avg_response_time_ms"`
	TotalTokensUsed       int     `json:"total_tokens_used"`
	TotalCost             float64 `json:"total_cost"`
	AvgQualityScore       float64 `json:"avg_quality_score"`
	ScenariosCompleted    int     `json:"scenarios_completed"`
	TotalErrors           int     `json:"total_errors"`
	ErrorRate             float64 `json:"error_rate"`
	TokensPerSecondAvg    float64 `json:"tokens_per_second_avg"`
}

// MetricsCollector interface for collecting various types of metrics
type MetricsCollector interface {
	// OnStart is called when a test run begins
	OnStart(ctx *MetricsContext)
	
	// Collect gathers metrics from the completed run
	Collect(ctx *MetricsContext) map[string]interface{}
	
	// OnEnd is called when a test run completes
	OnEnd(ctx *MetricsContext)
	
	// Name returns the collector's name
	Name() string
}

// MetricsContext provides context information for metrics collection
type MetricsContext struct {
	Scenario  TestScenario
	Target    BenchmarkTarget
	Config    *config.Config
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
	Response  string
	Error     error
}

// PerformanceCollector collects performance-related metrics
type PerformanceCollector struct{}

func NewPerformanceCollector() *PerformanceCollector {
	return &PerformanceCollector{}
}

func (pc *PerformanceCollector) Name() string {
	return "performance"
}

func (pc *PerformanceCollector) OnStart(ctx *MetricsContext) {
	// Mark start time (already captured in context)
}

func (pc *PerformanceCollector) OnEnd(ctx *MetricsContext) {
	// Cleanup if needed
}

func (pc *PerformanceCollector) Collect(ctx *MetricsContext) map[string]interface{} {
	metrics := make(map[string]interface{})
	
	// Response time metrics
	metrics["response_time_ms"] = ctx.Duration.Milliseconds()
	metrics["response_time_seconds"] = ctx.Duration.Seconds()
	
	// Calculate tokens per second if we have response content
	if ctx.Response != "" && ctx.Duration > 0 {
		outputTokens := lib.EstimateTokens(ctx.Config, ctx.Response)
		tokensPerSecond := float64(outputTokens) / ctx.Duration.Seconds()
		metrics["tokens_per_second"] = tokensPerSecond
		metrics["estimated_output_tokens"] = outputTokens
	}
	
	// Response size metrics
	metrics["response_length_chars"] = len(ctx.Response)
	metrics["response_length_words"] = len(strings.Fields(ctx.Response))
	
	// Success metrics
	metrics["success"] = (ctx.Error == nil)
	if ctx.Error != nil {
		metrics["error_type"] = classifyError(ctx.Error)
	}
	
	return metrics
}

// TokenCollector collects token usage metrics
type TokenCollector struct{}

func NewTokenCollector() *TokenCollector {
	return &TokenCollector{}
}

func (tc *TokenCollector) Name() string {
	return "tokens"
}

func (tc *TokenCollector) OnStart(ctx *MetricsContext) {}

func (tc *TokenCollector) OnEnd(ctx *MetricsContext) {}

func (tc *TokenCollector) Collect(ctx *MetricsContext) map[string]interface{} {
	metrics := make(map[string]interface{})
	
	// Input token estimation
	inputTokens := lib.EstimateTokens(ctx.Config, ctx.Scenario.Prompt)
	metrics["input_tokens"] = inputTokens
	
	// Output token estimation
	outputTokens := 0
	if ctx.Response != "" {
		outputTokens = lib.EstimateTokens(ctx.Config, ctx.Response)
	}
	metrics["output_tokens"] = outputTokens
	
	// Total tokens
	metrics["total_tokens"] = inputTokens + outputTokens
	
	// Token efficiency (output tokens per input token)
	if inputTokens > 0 {
		metrics["token_efficiency"] = float64(outputTokens) / float64(inputTokens)
	}
	
	// Context window utilization
	effectiveMaxTokens := lib.EffectiveMaxTokens(ctx.Config)
	if effectiveMaxTokens > 0 {
		utilization := float64(inputTokens + outputTokens) / float64(effectiveMaxTokens)
		metrics["context_utilization"] = utilization
	}
	
	return metrics
}

// CostCollector collects cost-related metrics
type CostCollector struct{}

func NewCostCollector() *CostCollector {
	return &CostCollector{}
}

func (cc *CostCollector) Name() string {
	return "cost"
}

func (cc *CostCollector) OnStart(ctx *MetricsContext) {}

func (cc *CostCollector) OnEnd(ctx *MetricsContext) {}

func (cc *CostCollector) Collect(ctx *MetricsContext) map[string]interface{} {
	metrics := make(map[string]interface{})
	
	// Get token counts
	inputTokens := lib.EstimateTokens(ctx.Config, ctx.Scenario.Prompt)
	outputTokens := 0
	if ctx.Response != "" {
		outputTokens = lib.EstimateTokens(ctx.Config, ctx.Response)
	}
	
	// Calculate costs based on known pricing
	inputCost, outputCost := calculateTokenCosts(ctx.Target.Provider, ctx.Target.Model, inputTokens, outputTokens)
	totalCost := inputCost + outputCost
	
	metrics["input_cost_usd"] = inputCost
	metrics["output_cost_usd"] = outputCost
	metrics["total_cost_usd"] = totalCost
	
	// Cost efficiency metrics
	if len(ctx.Response) > 0 {
		metrics["cost_per_char"] = totalCost / float64(len(ctx.Response))
		metrics["cost_per_word"] = totalCost / float64(len(strings.Fields(ctx.Response)))
	}
	
	return metrics
}

// QualityCollector evaluates the quality of responses
type QualityCollector struct{}

func NewQualityCollector() *QualityCollector {
	return &QualityCollector{}
}

func (qc *QualityCollector) Name() string {
	return "quality"
}

func (qc *QualityCollector) OnStart(ctx *MetricsContext) {}

func (qc *QualityCollector) OnEnd(ctx *MetricsContext) {}

func (qc *QualityCollector) Collect(ctx *MetricsContext) map[string]interface{} {
	metrics := make(map[string]interface{})
	
	if ctx.Response == "" {
		metrics["quality_score"] = 0.0
		return metrics
	}
	
	// Quality evaluation based on kubectl-specific criteria
	score := evaluateKubectlResponseQuality(ctx.Scenario, ctx.Response)
	metrics["quality_score"] = score
	
	// Specific quality metrics
	metrics["contains_kubectl_commands"] = containsKubectlCommands(ctx.Response)
	metrics["has_explanation"] = hasExplanation(ctx.Response)
	metrics["is_structured"] = isWellStructured(ctx.Response)
	metrics["relevant_to_query"] = isRelevantToQuery(ctx.Scenario.Prompt, ctx.Response)
	
	// Command accuracy if applicable
	if ctx.Scenario.ExpectedCommands != nil {
		commandAccuracy := evaluateCommandAccuracy(ctx.Scenario.ExpectedCommands, ctx.Response)
		metrics["command_accuracy"] = commandAccuracy
	}
	
	return metrics
}

// NewBenchmarkResults creates a new benchmark results container
func NewBenchmarkResults() *BenchmarkResults {
	return &BenchmarkResults{
		ProviderResults: make([]*ProviderResult, 0),
		Summary: &BenchmarkSummary{
			Rankings: make(map[string]ProviderRanking),
		},
	}
}

// Helper functions for quality evaluation

func evaluateKubectlResponseQuality(scenario TestScenario, response string) float64 {
	score := 0.0
	maxScore := 100.0
	
	// Check if response contains kubectl commands (30 points)
	if containsKubectlCommands(response) {
		score += 30.0
	}
	
	// Check if response has good explanation (25 points)
	if hasExplanation(response) {
		score += 25.0
	}
	
	// Check if response is well structured (20 points)
	if isWellStructured(response) {
		score += 20.0
	}
	
	// Check relevance to the query (25 points)
	if isRelevantToQuery(scenario.Prompt, response) {
		score += 25.0
	}
	
	return score / maxScore
}

func containsKubectlCommands(response string) bool {
	kubectlPattern := regexp.MustCompile(`kubectl\s+\w+`)
	return kubectlPattern.MatchString(response)
}

func hasExplanation(response string) bool {
	// Simple heuristic: response should have more than just commands
	lines := strings.Split(response, "\n")
	explanationLines := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 20 && !strings.HasPrefix(line, "kubectl") && !strings.HasPrefix(line, "#") {
			explanationLines++
		}
	}
	return explanationLines > 2
}

func isWellStructured(response string) bool {
	// Check for markdown structure, headings, lists, etc.
	hasHeaders := strings.Contains(response, "#")
	hasLists := strings.Contains(response, "- ") || strings.Contains(response, "* ")
	hasCodeBlocks := strings.Contains(response, "```")
	
	return hasHeaders || hasLists || hasCodeBlocks
}

func isRelevantToQuery(prompt, response string) bool {
	// Simple keyword matching for relevance
	promptWords := extractKeywords(strings.ToLower(prompt))
	responseWords := extractKeywords(strings.ToLower(response))
	
	matches := 0
	for _, word := range promptWords {
		if contains(responseWords, word) {
			matches++
		}
	}
	
	// At least 50% of key prompt words should appear in response
	if len(promptWords) == 0 {
		return true
	}
	return float64(matches)/float64(len(promptWords)) >= 0.5
}

func evaluateCommandAccuracy(expectedCommands []string, response string) float64 {
	if len(expectedCommands) == 0 {
		return 1.0
	}
	
	matches := 0
	for _, expected := range expectedCommands {
		if strings.Contains(response, expected) {
			matches++
		}
	}
	
	return float64(matches) / float64(len(expectedCommands))
}

func extractKeywords(text string) []string {
	// Extract important kubectl-related keywords
	kubernetesKeywords := []string{
		"pod", "pods", "service", "services", "deployment", "deployments",
		"node", "nodes", "namespace", "namespaces", "ingress", "volume",
		"secret", "configmap", "logs", "events", "status", "describe",
		"get", "create", "delete", "apply", "kubectl", "cluster",
		"container", "image", "port", "label", "selector", "endpoint",
	}
	
	words := strings.Fields(text)
	keywords := []string{}
	
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		if len(word) > 2 && contains(kubernetesKeywords, word) {
			keywords = append(keywords, word)
		}
	}
	
	return keywords
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func classifyError(err error) string {
	if err == nil {
		return "none"
	}
	
	errMsg := strings.ToLower(err.Error())
	
	switch {
	case strings.Contains(errMsg, "timeout"):
		return "timeout"
	case strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "429"):
		return "rate_limit"
	case strings.Contains(errMsg, "auth") || strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403"):
		return "authentication"
	case strings.Contains(errMsg, "network") || strings.Contains(errMsg, "connection"):
		return "network"
	case strings.Contains(errMsg, "context") || strings.Contains(errMsg, "token"):
		return "context_limit"
	default:
		return "unknown"
	}
}

// calculateTokenCosts calculates costs based on known pricing models
func calculateTokenCosts(provider, model string, inputTokens, outputTokens int) (float64, float64) {
	// Pricing per 1M tokens (approximate as of 2024)
	type pricing struct {
		inputPrice  float64 // USD per 1M input tokens
		outputPrice float64 // USD per 1M output tokens
	}
	
	prices := map[string]map[string]pricing{
		"openai": {
			"gpt-4":          {30.0, 60.0},
			"gpt-4-turbo":    {10.0, 30.0},
			"gpt-4o":         {5.0, 15.0},
			"gpt-4o-mini":    {0.15, 0.6},
			"gpt-3.5-turbo":  {0.5, 1.5},
		},
		"anthropic": {
			"claude-3-opus":   {15.0, 75.0},
			"claude-3-sonnet": {3.0, 15.0},
			"claude-3-haiku":  {0.25, 1.25},
		},
		"google": {
			"gemini-pro":        {0.5, 1.5},
			"gemini-pro-vision": {0.5, 1.5},
			"gemini-2.5-flash":  {0.075, 0.3},
		},
		"ollama": {
			// Ollama is typically free/self-hosted
			"default": {0.0, 0.0},
		},
	}
	
	// Default to free for unknown models
	defaultPricing := pricing{0.0, 0.0}
	
	providerPrices, exists := prices[provider]
	if !exists {
		return 0.0, 0.0
	}
	
	modelPricing, exists := providerPrices[model]
	if !exists {
		// Try to find a fallback based on model name patterns
		for modelPattern, price := range providerPrices {
			if strings.Contains(model, modelPattern) {
				modelPricing = price
				exists = true
				break
			}
		}
		if !exists {
			modelPricing = defaultPricing
		}
	}
	
	// Calculate costs
	inputCost := (float64(inputTokens) / 1_000_000) * modelPricing.inputPrice
	outputCost := (float64(outputTokens) / 1_000_000) * modelPricing.outputPrice
	
	return inputCost, outputCost
}

// String returns the target as a readable string
func (bt BenchmarkTarget) String() string {
	if bt.BaseURL != "" {
		return fmt.Sprintf("%s/%s@%s", bt.Provider, bt.Model, bt.BaseURL)
	}
	return fmt.Sprintf("%s/%s", bt.Provider, bt.Model)
}