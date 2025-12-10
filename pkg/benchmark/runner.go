package benchmark

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/mikhae1/kubectl-quackops/themes"
)

// BenchmarkRunner orchestrates benchmarking across multiple providers and models
type BenchmarkRunner struct {
	config     *BenchmarkConfig
	scenarios  []TestScenario
	collectors []MetricsCollector
	results    *BenchmarkResults
	mu         sync.RWMutex
}

// BenchmarkConfig defines the benchmark execution parameters
type BenchmarkConfig struct {
	// Target providers and models to benchmark
	Providers []ProviderConfig `json:"providers"`

	// Test execution settings
	Iterations    int           `json:"iterations"`     // Number of runs per scenario
	Timeout       time.Duration `json:"timeout"`        // Max time per request
	Parallel      int           `json:"parallel"`       // Number of parallel executions
	WarmupRuns    int           `json:"warmup_runs"`    // Warmup iterations (not counted)
	CooldownDelay time.Duration `json:"cooldown_delay"` // Delay between test runs

	// Scenario filtering
	ScenarioFilter   []string `json:"scenario_filter"`   // Run only specific scenarios
	ComplexityFilter string   `json:"complexity_filter"` // "simple", "medium", "complex", "all"

	// Output settings
	OutputFormat string `json:"output_format"` // "json", "csv", "markdown", "table"
	OutputFile   string `json:"output_file"`   // File to write results
	Verbose      bool   `json:"verbose"`       // Detailed logging

	// Quality evaluation settings
	EnableQualityChecks     bool `json:"enable_quality_checks"`     // Run kubectl command validation
	EnableCostTracking      bool `json:"enable_cost_tracking"`      // Calculate costs
	EnableKubectlGeneration bool `json:"enable_kubectl_generation"` // Test kubectl command generation
}

// ProviderConfig specifies a provider and models to benchmark
type ProviderConfig struct {
	Name      string   `json:"name"`       // "openai", "anthropic", "google", etc.
	Models    []string `json:"models"`     // List of models to test
	BaseURL   string   `json:"base_url"`   // Custom base URL if needed
	APIKey    string   `json:"api_key"`    // API key (usually from env)
	MaxTokens int      `json:"max_tokens"` // Context window limit
}

// NewBenchmarkRunner creates a new benchmark runner
func NewBenchmarkRunner(config *BenchmarkConfig) *BenchmarkRunner {
	return &BenchmarkRunner{
		config:     config,
		scenarios:  []TestScenario{},
		collectors: []MetricsCollector{},
		results:    NewBenchmarkResults(),
	}
}

// AddScenario adds a test scenario to the benchmark
func (br *BenchmarkRunner) AddScenario(scenario TestScenario) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.scenarios = append(br.scenarios, scenario)
}

// AddScenarios adds multiple test scenarios
func (br *BenchmarkRunner) AddScenarios(scenarios []TestScenario) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.scenarios = append(br.scenarios, scenarios...)
}

// AddMetricsCollector adds a metrics collector
func (br *BenchmarkRunner) AddMetricsCollector(collector MetricsCollector) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.collectors = append(br.collectors, collector)
}

// Run executes the benchmark suite
func (br *BenchmarkRunner) Run(ctx context.Context) (*BenchmarkResults, error) {
	logger.Log("info", "Starting benchmark with %d scenarios across %d providers",
		len(br.scenarios), len(br.config.Providers))

	startTime := time.Now()

	// Filter scenarios if specified
	scenarios := br.filterScenarios()
	if len(scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios match the filter criteria")
	}

	// Initialize results
	br.results = NewBenchmarkResults()
	br.results.Config = *br.config
	br.results.StartTime = startTime

	// Build list of all target models
	var targets []BenchmarkTarget
	for _, providerConfig := range br.config.Providers {
		for _, model := range providerConfig.Models {
			targets = append(targets, BenchmarkTarget{
				Provider: providerConfig.Name,
				Model:    model,
				BaseURL:  providerConfig.BaseURL,
			})
		}
	}

	// Initialize provider results for all targets
	providerResults := make(map[string]*ProviderResult)
	for _, target := range targets {
		key := fmt.Sprintf("%s/%s", target.Provider, target.Model)
		providerResults[key] = &ProviderResult{
			Target:          target,
			ScenarioResults: make(map[string]*ScenarioResult),
			StartTime:       time.Now(),
		}
	}

	// Run benchmarks by scenario (each scenario runs across all models before moving to next scenario)
	for scenarioIndex, scenario := range scenarios {
		logger.Log("info", "Running scenario [%d/%d]: %s across %d models", scenarioIndex+1, len(scenarios), scenario.Name, len(targets))

		for modelIndex, target := range targets {
			logger.Log("info", "Running scenario %s on [%d/%d] %s/%s", scenario.Name, modelIndex+1, len(targets), target.Provider, target.Model)

			if err := br.runSingleScenario(ctx, target, scenario, scenarioIndex, len(scenarios), modelIndex, len(targets), providerResults); err != nil {
				logger.Log("warn", "Failed to run scenario %s on %s/%s: %v", scenario.Name, target.Provider, target.Model, err)
				continue
			}
		}
	}

	// Finalize provider results
	for _, providerResult := range providerResults {
		providerResult.EndTime = time.Now()
		providerResult.Duration = providerResult.EndTime.Sub(providerResult.StartTime)
		br.results.ProviderResults = append(br.results.ProviderResults, providerResult)
	}

	br.results.EndTime = time.Now()
	br.results.TotalDuration = br.results.EndTime.Sub(br.results.StartTime)

	// Calculate aggregate metrics
	br.calculateAggregateMetrics()

	logger.Log("info", "Benchmark completed in %v", br.results.TotalDuration)

	return br.results, nil
}

// runSingleScenario runs a single scenario for a specific provider/model
func (br *BenchmarkRunner) runSingleScenario(ctx context.Context, target BenchmarkTarget, scenario TestScenario, scenarioIndex, totalScenarios, modelIndex, totalModels int, providerResults map[string]*ProviderResult) error {
	// Get or create provider result
	key := fmt.Sprintf("%s/%s", target.Provider, target.Model)
	providerResult := providerResults[key]

	// Create config for this provider/model
	cfg, err := br.createProviderConfig(target)
	if err != nil {
		return fmt.Errorf("failed to create config for %s/%s: %w", target.Provider, target.Model, err)
	}

	// Run warmup iterations if configured (only for the first scenario to avoid excessive warmups)
	if scenarioIndex == 0 && br.config.WarmupRuns > 0 {
		logger.Log("debug", "Running %d warmup iterations for %s/%s", br.config.WarmupRuns, target.Provider, target.Model)
		for i := 0; i < br.config.WarmupRuns; i++ {
			br.runScenarioIteration(ctx, cfg, scenario, target, true)
			time.Sleep(br.config.CooldownDelay)
		}
	}

	// Initialize scenario result
	scenarioResult := &ScenarioResult{
		Scenario:  scenario,
		Target:    target,
		Runs:      make([]RunResult, 0, br.config.Iterations),
		StartTime: time.Now(),
	}

	// Cool formatted output to stdout
	if !br.config.Verbose {
		fmt.Printf("\n🎯 Running scenario [%d/%d]: %s\n", scenarioIndex+1, totalScenarios, scenario.Name)
		fmt.Printf("   └─ Model: %s/%s [%d/%d] • %d iterations\n", target.Provider, target.Model, modelIndex+1, totalModels, br.config.Iterations)
	}

	// Run multiple iterations of this scenario
	for i := 0; i < br.config.Iterations; i++ {
		// Show iteration progress for multiple iterations
		if br.config.Iterations > 1 && !br.config.Verbose {
			fmt.Printf("     ├─ Iteration [%d/%d]\n", i+1, br.config.Iterations)
		}

		run := br.runScenarioIteration(ctx, cfg, scenario, target, false)
		scenarioResult.Runs = append(scenarioResult.Runs, run)

		// Cooldown between iterations
		if i < br.config.Iterations-1 && br.config.CooldownDelay > 0 {
			time.Sleep(br.config.CooldownDelay)
		}
	}

	scenarioResult.EndTime = time.Now()
	scenarioResult.Duration = scenarioResult.EndTime.Sub(scenarioResult.StartTime)

	// Calculate scenario statistics
	br.calculateScenarioStats(scenarioResult)

	// Store scenario result in provider result
	providerResult.ScenarioResults[scenario.Name] = scenarioResult

	// Inline pretty preview window for the scenario output
	if !br.config.Verbose {
		// Pick last non-warmup response or error
		sample := ""
		for i := len(scenarioResult.Runs) - 1; i >= 0; i-- {
			run := scenarioResult.Runs[i]
			if run.IsWarmup {
				continue
			}
			if strings.TrimSpace(run.Response) != "" {
				sample = run.Response
				break
			}
			if sample == "" && run.Error != nil {
				sample = run.Error.Error()
			}
		}
		if strings.TrimSpace(sample) != "" {
			title := fmt.Sprintf("%s/%s • %s", target.Provider, target.Model, scenario.Name)
			window := lib.RenderTerminalWindow(title, sample, cfg.ToolOutputMaxLines, cfg.ToolOutputMaxLineLen)
			fmt.Print(window)
		}
	}

	return nil
}

// runProviderBenchmark runs all scenarios for a specific provider/model (deprecated - kept for compatibility)
func (br *BenchmarkRunner) runProviderBenchmark(ctx context.Context, target BenchmarkTarget, scenarios []TestScenario, currentModel, totalModels int) error {
	// Create config for this provider/model
	cfg, err := br.createProviderConfig(target)
	if err != nil {
		return fmt.Errorf("failed to create config for %s/%s: %w", target.Provider, target.Model, err)
	}

	// Initialize provider results
	providerResult := &ProviderResult{
		Target:          target,
		ScenarioResults: make(map[string]*ScenarioResult),
		StartTime:       time.Now(),
	}

	// Run warmup iterations if configured
	if br.config.WarmupRuns > 0 {
		logger.Log("debug", "Running %d warmup iterations for %s/%s", br.config.WarmupRuns, target.Provider, target.Model)
		for i := 0; i < br.config.WarmupRuns; i++ {
			// Use first scenario for warmup
			if len(scenarios) > 0 {
				br.runScenarioIteration(ctx, cfg, scenarios[0], target, true)
				time.Sleep(br.config.CooldownDelay)
			}
		}
	}

	// Run actual benchmark scenarios
	for scenarioIndex, scenario := range scenarios {
		scenarioResult := &ScenarioResult{
			Scenario:  scenario,
			Target:    target,
			Runs:      make([]RunResult, 0, br.config.Iterations),
			StartTime: time.Now(),
		}

		// Cool formatted output to stdout
		if !br.config.Verbose {
			fmt.Printf("\n🎯 Running scenario [%d/%d]: %s\n", scenarioIndex+1, len(scenarios), scenario.Name)
			fmt.Printf("   └─ Model: %s/%s [%d/%d] • %d iterations\n", target.Provider, target.Model, currentModel, totalModels, br.config.Iterations)
		}

		logger.Log("info", "Running scenario [%d/%d]: %s for %s/%s [model %d/%d] (%d iterations)",
			scenarioIndex+1, len(scenarios), scenario.Name, target.Provider, target.Model, currentModel, totalModels, br.config.Iterations)

		// Run multiple iterations of this scenario
		for i := 0; i < br.config.Iterations; i++ {
			// Show iteration progress for multiple iterations
			if br.config.Iterations > 1 && !br.config.Verbose {
				fmt.Printf("     ├─ Iteration [%d/%d]\n", i+1, br.config.Iterations)
			}

			run := br.runScenarioIteration(ctx, cfg, scenario, target, false)
			scenarioResult.Runs = append(scenarioResult.Runs, run)

			// Cooldown between iterations
			if i < br.config.Iterations-1 && br.config.CooldownDelay > 0 {
				time.Sleep(br.config.CooldownDelay)
			}
		}

		scenarioResult.EndTime = time.Now()
		scenarioResult.Duration = scenarioResult.EndTime.Sub(scenarioResult.StartTime)

		// Calculate scenario statistics
		br.calculateScenarioStats(scenarioResult)

		providerResult.ScenarioResults[scenario.Name] = scenarioResult

		// Inline pretty preview window for the scenario output
		if !br.config.Verbose {
			// Pick last non-warmup response or error
			sample := ""
			for i := len(scenarioResult.Runs) - 1; i >= 0; i-- {
				run := scenarioResult.Runs[i]
				if run.IsWarmup {
					continue
				}
				if strings.TrimSpace(run.Response) != "" {
					sample = run.Response
					break
				}
				if sample == "" && run.Error != nil {
					sample = run.Error.Error()
				}
			}
			if strings.TrimSpace(sample) != "" {
				title := fmt.Sprintf("%s/%s • %s", target.Provider, target.Model, scenario.Name)
				window := lib.RenderTerminalWindow(title, sample, cfg.ToolOutputMaxLines, cfg.ToolOutputMaxLineLen)
				fmt.Print(window)
			}
		}
	}

	providerResult.EndTime = time.Now()
	providerResult.Duration = providerResult.EndTime.Sub(providerResult.StartTime)

	// Store provider results
	br.mu.Lock()
	br.results.ProviderResults = append(br.results.ProviderResults, providerResult)
	br.mu.Unlock()

	return nil
}

// runScenarioIteration runs a single iteration of a scenario
func (br *BenchmarkRunner) runScenarioIteration(ctx context.Context, cfg *config.Config, scenario TestScenario, target BenchmarkTarget, isWarmup bool) RunResult {
	run := RunResult{
		Iteration: len(br.results.ProviderResults), // This will be corrected later
		StartTime: time.Now(),
		IsWarmup:  isWarmup,
	}

	// Create metrics collection context
	metricsCtx := &MetricsContext{
		Scenario:  scenario,
		Target:    target,
		Config:    cfg,
		StartTime: run.StartTime,
	}

	// Notify collectors of start
	for _, collector := range br.collectors {
		collector.OnStart(metricsCtx)
	}

	// Execute the scenario
	response, err := br.executeScenario(ctx, cfg, scenario)
	run.EndTime = time.Now()
	run.Duration = run.EndTime.Sub(run.StartTime)
	run.Response = response
	run.Error = err

	// Determine success
	// Primary rule: if ExpectedContains is provided, require all substrings present
	if err == nil && response != "" {
		if len(scenario.ExpectedContains) > 0 {
			run.Success = containsAllSubstrings(response, scenario.ExpectedContains)
			// Fallback: allow success if the response clearly includes kubectl content or expected commands
			if !run.Success {
				if len(scenario.ExpectedCommands) > 0 {
					for _, cmd := range scenario.ExpectedCommands {
						if stringsContains(response, cmd) {
							run.Success = true
							break
						}
					}
				}
				if !run.Success && containsKubectlCommands(response) {
					run.Success = true
				}
			}
		} else {
			// No strict expectations; any non-empty successful response counts
			run.Success = true
		}
	} else {
		run.Success = false
	}

	// Update metrics context
	metricsCtx.EndTime = run.EndTime
	metricsCtx.Duration = run.Duration
	metricsCtx.Response = response
	metricsCtx.Error = err

	// Collect metrics
	metrics := make(map[string]interface{})
	for _, collector := range br.collectors {
		collectorMetrics := collector.Collect(metricsCtx)
		for k, v := range collectorMetrics {
			metrics[k] = v
		}
		collector.OnEnd(metricsCtx)
	}

	// Attach expected-data evaluation metrics
	if len(scenario.ExpectedContains) > 0 {
		matches := 0
		for _, s := range scenario.ExpectedContains {
			if s != "" && stringsContains(response, s) {
				matches++
			}
		}
		metrics["expected_total"] = len(scenario.ExpectedContains)
		metrics["expected_matches"] = matches
		if len(scenario.ExpectedContains) > 0 {
			metrics["expected_match_ratio"] = float64(matches) / float64(len(scenario.ExpectedContains))
		}
	}

	run.Metrics = metrics

	if br.config.Verbose && !isWarmup {
		status := "SUCCESS"
		if !run.Success {
			status = "FAILED"
		}
		logger.Log("debug", "Run %d [%s]: %s in %v (tokens: %v)",
			run.Iteration, status, scenario.Name, run.Duration, metrics["output_tokens"])
	}

	return run
}

// executeScenario executes a single test scenario
func (br *BenchmarkRunner) executeScenario(ctx context.Context, cfg *config.Config, scenario TestScenario) (string, error) {
	// Respect cancellation if already requested
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// Handle kubectl command generation scenarios specially
	if strings.HasSuffix(scenario.Name, "_kubectl") {
		return br.executeKubectlGenerationScenario(cfg, scenario)
	}

	// If mocked diagnostic data is provided, construct a full diagnostic prompt
	prompt := scenario.Prompt
	if len(scenario.MockedCmdResults) > 0 {
		if aug, err := llm.CreateAugPromptFromCmdResults(cfg, scenario.Prompt, scenario.MockedCmdResults); err == nil && aug != "" {
			prompt = aug
		}
	}

	// Execute the scenario's prompt
	return llm.Request(cfg, prompt, false, false)
}

// executeKubectlGenerationScenario executes kubectl command generation testing
func (br *BenchmarkRunner) executeKubectlGenerationScenario(cfg *config.Config, scenario TestScenario) (string, error) {
	// Generate kubectl commands and validate them
	generatedCommands, err := llm.GenKubectlCmds(cfg, scenario.Prompt, 1)
	if err != nil {
		return "", fmt.Errorf("failed to generate kubectl commands: %w", err)
	}

	if len(generatedCommands) == 0 {
		return "", fmt.Errorf("no kubectl commands were generated")
	}

	// Validate generated commands against expected commands
	accuracy := ValidateGeneratedCommands(scenario.ExpectedCommands, generatedCommands)
	success := accuracy >= 0.5 // Consider successful if at least 50% of expected commands match

	// Generate a report-style response that includes the test results
	response := "Kubectl Command Generation Test Results:\n"
	response += fmt.Sprintf("Scenario: %s\n", scenario.Name)
	response += fmt.Sprintf("Success: %t\n", success)
	response += fmt.Sprintf("Accuracy: %.2f\n", accuracy)
	response += fmt.Sprintf("Expected Commands: %v\n", scenario.ExpectedCommands)
	response += fmt.Sprintf("Generated Commands: %v\n", generatedCommands)

	return response, nil
}

// createProviderConfig creates a config for the specific provider/model
func (br *BenchmarkRunner) createProviderConfig(target BenchmarkTarget) (*config.Config, error) {
	cfg := config.LoadConfig()
	cfg.Theme = themes.Apply(cfg.Theme)

	// Override with benchmark-specific settings
	cfg.Provider = target.Provider
	cfg.Model = target.Model
	cfg.Retries = 1            // Minimize retries for consistent benchmarking
	cfg.SkipWaits = true       // Skip throttling delays for benchmarking
	cfg.EnableBaseline = false // Disable baseline filtering to show all mocked outputs

	// Set provider-specific configurations
	if target.BaseURL != "" {
		switch target.Provider {
		case "ollama":
			cfg.OllamaApiURL = target.BaseURL
		case "openai":
			// For OpenAI provider with custom base URL (like OpenRouter), set the base URL
			// temporarily in environment for ConfigDetectMaxTokens function
			originalBaseURL := os.Getenv("QU_OPENAI_BASE_URL")
			os.Setenv("QU_OPENAI_BASE_URL", target.BaseURL)
			// We'll restore it after ConfigDetectMaxTokens call
			defer func() {
				if originalBaseURL != "" {
					os.Setenv("QU_OPENAI_BASE_URL", originalBaseURL)
				} else {
					os.Unsetenv("QU_OPENAI_BASE_URL")
				}
			}()
		}
	}

	// Auto-detect max tokens for each model to avoid context length errors
	cfg.ConfigDetectMaxTokens()

	return cfg, nil
}

// filterScenarios filters scenarios based on configuration
func (br *BenchmarkRunner) filterScenarios() []TestScenario {
	scenarios := br.scenarios

	// Filter by scenario names if specified
	if len(br.config.ScenarioFilter) > 0 {
		filtered := []TestScenario{}
		for _, scenario := range scenarios {
			for _, filter := range br.config.ScenarioFilter {
				if scenario.Name == filter {
					filtered = append(filtered, scenario)
					break
				}
			}
		}
		scenarios = filtered
	}

	// Filter by complexity if specified
	if br.config.ComplexityFilter != "" && br.config.ComplexityFilter != "all" {
		filtered := []TestScenario{}
		for _, scenario := range scenarios {
			if string(scenario.Complexity) == br.config.ComplexityFilter {
				filtered = append(filtered, scenario)
			}
		}
		scenarios = filtered
	}

	return scenarios
}

// calculateScenarioStats calculates statistics for a scenario result
func (br *BenchmarkRunner) calculateScenarioStats(result *ScenarioResult) {
	if len(result.Runs) == 0 {
		return
	}

	// Collect successful runs for success metrics
	successfulRuns := []RunResult{}
	for _, run := range result.Runs {
		if run.Success {
			successfulRuns = append(successfulRuns, run)
		}
	}

	// Calculate basic stats
	result.Stats.TotalRuns = len(result.Runs)
	result.Stats.SuccessfulRuns = len(successfulRuns)
	result.Stats.SuccessRate = float64(result.Stats.SuccessfulRuns) / float64(result.Stats.TotalRuns)

	// Response time stats
	// Prefer successful runs; if none, use all runs to avoid empty stats
	baseRuns := successfulRuns
	if len(baseRuns) == 0 {
		baseRuns = result.Runs
	}
	durations := make([]time.Duration, len(baseRuns))
	for i, run := range baseRuns {
		durations[i] = run.Duration
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	result.Stats.MinResponseTime = durations[0]
	result.Stats.MaxResponseTime = durations[len(durations)-1]
	result.Stats.MedianResponseTime = durations[len(durations)/2]

	// Calculate average
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	result.Stats.AvgResponseTime = total / time.Duration(len(durations))

	// Token, cost, and quality stats across all runs to reflect output characteristics
	br.calculateTokenStats(result, result.Runs)
}

// calculateTokenStats calculates token usage statistics
func (br *BenchmarkRunner) calculateTokenStats(result *ScenarioResult, runs []RunResult) {
	inputTokens := []int{}
	outputTokens := []int{}
	tokensPerSecond := []float64{}

	for _, run := range runs {
		if it, ok := run.Metrics["input_tokens"].(int); ok {
			inputTokens = append(inputTokens, it)
		}
		if ot, ok := run.Metrics["output_tokens"].(int); ok {
			outputTokens = append(outputTokens, ot)
		}
		if tps, ok := run.Metrics["tokens_per_second"].(float64); ok {
			tokensPerSecond = append(tokensPerSecond, tps)
		}
	}

	if len(inputTokens) > 0 {
		var total int
		for _, tokens := range inputTokens {
			total += tokens
		}
		result.Stats.AvgInputTokens = float64(total) / float64(len(inputTokens))
	}

	if len(outputTokens) > 0 {
		var total int
		for _, tokens := range outputTokens {
			total += tokens
		}
		result.Stats.AvgOutputTokens = float64(total) / float64(len(outputTokens))
	}

	// Calculate average tokens per second
	if len(tokensPerSecond) > 0 {
		var total float64
		for _, tps := range tokensPerSecond {
			total += tps
		}
		result.Stats.TokensPerSecond = total / float64(len(tokensPerSecond))
	}

	// Calculate quality score and cost averages
	qualityScores := []float64{}
	costs := []float64{}

	for _, run := range runs {
		if qs, ok := run.Metrics["quality_score"].(float64); ok {
			qualityScores = append(qualityScores, qs)
		}
		if cost, ok := run.Metrics["total_cost_usd"].(float64); ok {
			costs = append(costs, cost)
		}
	}

	if len(qualityScores) > 0 {
		var total float64
		for _, qs := range qualityScores {
			total += qs
		}
		result.Stats.QualityScore = total / float64(len(qualityScores))
	}

	if len(costs) > 0 {
		var total float64
		for _, cost := range costs {
			total += cost
		}
		result.Stats.AvgCostPerRun = total / float64(len(costs))
	}
}

// calculateAggregateMetrics calculates overall benchmark metrics
func (br *BenchmarkRunner) calculateAggregateMetrics() {
	// Calculate aggregate stats for each provider
	for _, provider := range br.results.ProviderResults {
		var totalSuccessRate float64
		var totalResponseTime float64
		var totalTokensUsed int
		var totalCost float64
		var totalQuality float64
		var scenarioCount int
		var totalErrors int

		for _, scenarioResult := range provider.ScenarioResults {
			totalSuccessRate += scenarioResult.Stats.SuccessRate
			totalResponseTime += float64(scenarioResult.Stats.AvgResponseTime.Milliseconds())
			totalTokensUsed += int(scenarioResult.Stats.AvgInputTokens + scenarioResult.Stats.AvgOutputTokens)
			totalCost += scenarioResult.Stats.AvgCostPerRun
			totalQuality += scenarioResult.Stats.QualityScore
			totalErrors += scenarioResult.Stats.TotalRuns - scenarioResult.Stats.SuccessfulRuns
			scenarioCount++
		}

		if scenarioCount > 0 {
			provider.AggregateStats.OverallSuccessRate = totalSuccessRate / float64(scenarioCount)
			provider.AggregateStats.AvgResponseTime = totalResponseTime / float64(scenarioCount)
			provider.AggregateStats.TotalTokensUsed = totalTokensUsed
			provider.AggregateStats.TotalCost = totalCost
			provider.AggregateStats.AvgQualityScore = totalQuality / float64(scenarioCount)
			provider.AggregateStats.ScenariosCompleted = scenarioCount
			provider.AggregateStats.TotalErrors = totalErrors

			totalRuns := 0
			for _, scenarioResult := range provider.ScenarioResults {
				totalRuns += scenarioResult.Stats.TotalRuns
			}
			if totalRuns > 0 {
				provider.AggregateStats.ErrorRate = float64(totalErrors) / float64(totalRuns)
			}
		}
	}
}

// GetResults returns the current benchmark results
func (br *BenchmarkRunner) GetResults() *BenchmarkResults {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return br.results
}

// LoadDefaultScenarios loads a set of default kubectl-focused test scenarios
func (br *BenchmarkRunner) LoadDefaultScenarios() {
	scenarios := GetScenariosWithOptions(br.config.EnableKubectlGeneration)
	br.AddScenarios(scenarios)
}

// LoadDefaultCollectors loads default metrics collectors
func (br *BenchmarkRunner) LoadDefaultCollectors() {
	br.AddMetricsCollector(NewPerformanceCollector())
	br.AddMetricsCollector(NewTokenCollector())
	if br.config.EnableCostTracking {
		br.AddMetricsCollector(NewCostCollector())
	}
	if br.config.EnableQualityChecks {
		br.AddMetricsCollector(NewQualityCollector())
	}
}

// containsAllSubstrings returns true if all expected substrings are present in the text
func containsAllSubstrings(text string, expected []string) bool {
	if len(expected) == 0 {
		return true
	}
	for _, s := range expected {
		if s == "" {
			continue
		}
		if !stringsContains(text, s) {
			return false
		}
	}
	return true
}

// stringsContains is a lightweight wrapper for substring check; centralized for future enhancements
func stringsContains(haystack, needle string) bool {
	return haystack != "" && needle != "" && strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
