package benchmark

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
	"github.com/mikhae1/kubectl-quackops/tests/fixtures"
)

func TestBenchmarkRunner_BasicFunctionality(t *testing.T) {
	// Create a basic benchmark configuration
	cfg := &BenchmarkConfig{
		Providers: []ProviderConfig{
			{
				Name:   "openai",
				Models: []string{"gpt-4o-mini"},
			},
		},
		Iterations:       2,
		Timeout:          30 * time.Second,
		Parallel:         1,
		WarmupRuns:       0,
		CooldownDelay:    100 * time.Millisecond,
		ComplexityFilter: "simple",
		OutputFormat:     "table",
		Verbose:          true,
	}

	runner := NewBenchmarkRunner(cfg)

	// Add a simple test scenario
	scenario := CreateCustomScenario(
		"test_simple",
		"Simple test scenario",
		"Show me all pods",
		ComplexitySimple,
		"test",
		[]string{"kubectl get pods"},
		[]string{"test", "simple"},
	)
	runner.AddScenario(scenario)

	// Add metrics collectors
	runner.AddMetricsCollector(NewPerformanceCollector())
	runner.AddMetricsCollector(NewTokenCollector())

	// Mock the LLM request function
	originalRequest := llm.Request
	mockResponse := fixtures.MockResponseFixtures{}.KubernetesPodExplanation()[0]
	llm.Request = llm.MockRequestFunc([]llm.MockResponse{mockResponse})
	defer func() {
		llm.Request = originalRequest
	}()

	// Run the benchmark
	ctx := context.Background()
	results, err := runner.Run(ctx)

	if err != nil {
		t.Fatalf("Benchmark run failed: %v", err)
	}

	// Validate results
	if results == nil {
		t.Fatal("Expected results but got nil")
	}

	if len(results.ProviderResults) != 1 {
		t.Errorf("Expected 1 provider result, got %d", len(results.ProviderResults))
	}

	providerResult := results.ProviderResults[0]
	if providerResult.Target.Provider != "openai" {
		t.Errorf("Expected provider 'openai', got '%s'", providerResult.Target.Provider)
	}

	if len(providerResult.ScenarioResults) != 1 {
		t.Errorf("Expected 1 scenario result, got %d", len(providerResult.ScenarioResults))
	}

	scenarioResult := providerResult.ScenarioResults["test_simple"]
	if scenarioResult == nil {
		t.Fatal("Expected scenario result for 'test_simple' but got nil")
	}

	// Validate run results (should have 2 iterations + 1 warmup, but warmup not counted)
	if len(scenarioResult.Runs) != 2 {
		t.Errorf("Expected 2 runs, got %d", len(scenarioResult.Runs))
	}

	// Check that metrics were collected
	if len(scenarioResult.Runs) > 0 {
		firstRun := scenarioResult.Runs[0]
		if len(firstRun.Metrics) == 0 {
			t.Error("Expected metrics to be collected")
		}

		// Check for specific metrics
		if _, exists := firstRun.Metrics["response_time_ms"]; !exists {
			t.Error("Expected response_time_ms metric")
		}
		if _, exists := firstRun.Metrics["input_tokens"]; !exists {
			t.Error("Expected input_tokens metric")
		}
	}
}

func TestBenchmarkRunner_MultipleProviders(t *testing.T) {
	// Test benchmark with multiple providers and models
	cfg := &BenchmarkConfig{
		Providers: []ProviderConfig{
			{
				Name:   "openai",
				Models: []string{"z-ai/glm-4.5-air:free", "moonshotai/kimi-k2:free"},
			},
			{
				Name:   "google",
				Models: []string{"gemini-2.5-flash"},
			},
		},
		Iterations:       1,
		Timeout:          10 * time.Second,
		Parallel:         1,
		WarmupRuns:       0,
		CooldownDelay:    50 * time.Millisecond,
		ComplexityFilter: "simple",
		OutputFormat:     "json",
		Verbose:          false,
	}

	runner := NewBenchmarkRunner(cfg)

	// Add multiple scenarios
	scenarios := []TestScenario{
		CreateCustomScenario("list_pods", "List all pods", "Show me all pods", ComplexitySimple, "basic", []string{"kubectl get pods"}, []string{"pods"}),
		CreateCustomScenario("list_services", "List services", "List all services", ComplexitySimple, "basic", []string{"kubectl get svc"}, []string{"services"}),
	}
	runner.AddScenarios(scenarios)

	// Add all collectors
	runner.AddMetricsCollector(NewPerformanceCollector())
	runner.AddMetricsCollector(NewTokenCollector())

	// Mock responses for each provider/model combination
	originalRequest := llm.Request
	mockResponses := []llm.MockResponse{
		{Content: "OpenAI GLM response for pods", TokensUsed: 50},
		{Content: "OpenAI GLM response for services", TokensUsed: 45},
		{Content: "Moonshot Kimi response for pods", TokensUsed: 55},
		{Content: "Moonshot Kimi response for services", TokensUsed: 48},
		{Content: "Google Gemini response for pods", TokensUsed: 52},
		{Content: "Google Gemini response for services", TokensUsed: 47},
	}
	llm.Request = llm.MockRequestFunc(mockResponses)
	defer func() {
		llm.Request = originalRequest
	}()

	// Run the benchmark
	ctx := context.Background()
	results, err := runner.Run(ctx)

	if err != nil {
		t.Fatalf("Multi-provider benchmark failed: %v", err)
	}

	// Validate we got results for all provider/model combinations
	expectedCombinations := 3 // 2 OpenAI models + 1 Google model
	if len(results.ProviderResults) != expectedCombinations {
		t.Errorf("Expected %d provider results, got %d", expectedCombinations, len(results.ProviderResults))
	}

	// Check that each provider result has results for both scenarios
	for _, providerResult := range results.ProviderResults {
		if len(providerResult.ScenarioResults) != 2 {
			t.Errorf("Provider %s/%s: expected 2 scenario results, got %d",
				providerResult.Target.Provider, providerResult.Target.Model, len(providerResult.ScenarioResults))
		}

		// Verify scenarios exist
		if _, exists := providerResult.ScenarioResults["list_pods"]; !exists {
			t.Errorf("Provider %s/%s: missing 'list_pods' scenario", providerResult.Target.Provider, providerResult.Target.Model)
		}
		if _, exists := providerResult.ScenarioResults["list_services"]; !exists {
			t.Errorf("Provider %s/%s: missing 'list_services' scenario", providerResult.Target.Provider, providerResult.Target.Model)
		}
	}
}

func TestDefaultScenarios(t *testing.T) {
	scenarios := GetDefaultScenarios()

	if len(scenarios) == 0 {
		t.Fatal("Expected default scenarios but got none")
	}

	// Check that we have scenarios of different complexities
	complexityCounts := make(map[ScenarioComplexity]int)
	for _, scenario := range scenarios {
		complexityCounts[scenario.Complexity]++
	}

	if complexityCounts[ComplexitySimple] == 0 {
		t.Error("Expected at least one simple scenario")
	}
	if complexityCounts[ComplexityMedium] == 0 {
		t.Error("Expected at least one medium scenario")
	}
	if complexityCounts[ComplexityComplex] == 0 {
		t.Error("Expected at least one complex scenario")
	}

	// Validate each scenario
	for i, scenario := range scenarios {
		if err := ValidateScenario(scenario); err != nil {
			t.Errorf("Scenario %d (%s) validation failed: %v", i, scenario.Name, err)
		}
	}
}

func TestScenarioFiltering(t *testing.T) {
	// Test filtering by complexity
	simpleScenarios := GetScenariosByComplexity(ComplexitySimple)
	for _, scenario := range simpleScenarios {
		if scenario.Complexity != ComplexitySimple {
			t.Errorf("Expected simple scenario, got %s", scenario.Complexity)
		}
	}

	// Test filtering by category
	basicScenarios := GetScenariosByCategory("basic_operations")
	for _, scenario := range basicScenarios {
		if scenario.Category != "basic_operations" {
			t.Errorf("Expected basic_operations category, got %s", scenario.Category)
		}
	}

	// Test filtering by tag
	podScenarios := GetScenariosByTag("pods")
	for _, scenario := range podScenarios {
		found := false
		for _, tag := range scenario.Tags {
			if strings.ToLower(tag) == "pods" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Scenario %s should have 'pods' tag", scenario.Name)
		}
	}

	// Test getting scenario by name
	scenario, err := GetScenarioByName("list_pods")
	if err != nil {
		t.Errorf("Failed to get scenario by name: %v", err)
	}
	if scenario == nil {
		t.Error("Expected scenario but got nil")
	}
	if scenario.Name != "list_pods" {
		t.Errorf("Expected scenario name 'list_pods', got '%s'", scenario.Name)
	}

	// Test non-existent scenario
	_, err = GetScenarioByName("nonexistent_scenario")
	if err == nil {
		t.Error("Expected error for non-existent scenario")
	}
}

func TestMetricsCollectors(t *testing.T) {
	// Test performance collector
	t.Run("PerformanceCollector", func(t *testing.T) {
		collector := NewPerformanceCollector()

		ctx := &MetricsContext{
			Scenario: TestScenario{
				Name:   "test",
				Prompt: "test prompt",
			},
			Target: BenchmarkTarget{
				Provider: "openai",
				Model:    "gpt-4o-mini",
			},
			Config:    &config.Config{Provider: "openai", Model: "gpt-4o-mini"},
			StartTime: time.Now(),
			EndTime:   time.Now().Add(500 * time.Millisecond),
			Duration:  500 * time.Millisecond,
			Response:  "Test response with some content",
			Error:     nil,
		}

		metrics := collector.Collect(ctx)

		if _, exists := metrics["response_time_ms"]; !exists {
			t.Error("Expected response_time_ms metric")
		}
		if _, exists := metrics["tokens_per_second"]; !exists {
			t.Error("Expected tokens_per_second metric")
		}
		if _, exists := metrics["success"]; !exists {
			t.Error("Expected success metric")
		}
	})

	// Test token collector
	t.Run("TokenCollector", func(t *testing.T) {
		collector := NewTokenCollector()

		ctx := &MetricsContext{
			Scenario: TestScenario{
				Name:   "test",
				Prompt: "Show me all the pods in the cluster",
			},
			Target: BenchmarkTarget{
				Provider: "openai",
				Model:    "gpt-4o-mini",
			},
			Config:   &config.Config{Provider: "openai", Model: "gpt-4o-mini"},
			Response: "Here are the pods in your cluster. Use kubectl get pods to see them.",
		}

		metrics := collector.Collect(ctx)

		if _, exists := metrics["input_tokens"]; !exists {
			t.Error("Expected input_tokens metric")
		}
		if _, exists := metrics["output_tokens"]; !exists {
			t.Error("Expected output_tokens metric")
		}
		if _, exists := metrics["total_tokens"]; !exists {
			t.Error("Expected total_tokens metric")
		}
		if _, exists := metrics["token_efficiency"]; !exists {
			t.Error("Expected token_efficiency metric")
		}
	})

	// Test cost collector
	t.Run("CostCollector", func(t *testing.T) {
		collector := NewCostCollector()

		ctx := &MetricsContext{
			Scenario: TestScenario{
				Name:   "test",
				Prompt: "Show me all pods",
			},
			Target: BenchmarkTarget{
				Provider: "openai",
				Model:    "gpt-4o-mini",
			},
			Config:   &config.Config{Provider: "openai", Model: "gpt-4o-mini"},
			Response: "kubectl get pods",
		}

		metrics := collector.Collect(ctx)

		if _, exists := metrics["total_cost_usd"]; !exists {
			t.Error("Expected total_cost_usd metric")
		}
		if _, exists := metrics["input_cost_usd"]; !exists {
			t.Error("Expected input_cost_usd metric")
		}
		if _, exists := metrics["output_cost_usd"]; !exists {
			t.Error("Expected output_cost_usd metric")
		}
	})

	// Test quality collector
	t.Run("QualityCollector", func(t *testing.T) {
		collector := NewQualityCollector()

		ctx := &MetricsContext{
			Scenario: TestScenario{
				Name:             "test",
				Prompt:           "Show me all pods that are failing",
				ExpectedCommands: []string{"kubectl get pods", "kubectl describe pod"},
			},
			Target: BenchmarkTarget{
				Provider: "openai",
				Model:    "gpt-4o-mini",
			},
			Config: &config.Config{Provider: "openai", Model: "gpt-4o-mini"},
			Response: `# Pod Status Check

To check for failing pods, use these commands:

kubectl get pods --all-namespaces
kubectl describe pod <pod-name>

This will show you the current status and any error messages.`,
		}

		metrics := collector.Collect(ctx)

		if _, exists := metrics["quality_score"]; !exists {
			t.Error("Expected quality_score metric")
		}
		if _, exists := metrics["contains_kubectl_commands"]; !exists {
			t.Error("Expected contains_kubectl_commands metric")
		}
		if _, exists := metrics["has_explanation"]; !exists {
			t.Error("Expected has_explanation metric")
		}
		if _, exists := metrics["is_structured"]; !exists {
			t.Error("Expected is_structured metric")
		}

		// Check quality score is reasonable
		if score, ok := metrics["quality_score"].(float64); ok {
			if score < 0 || score > 1 {
				t.Errorf("Quality score should be between 0 and 1, got %f", score)
			}
			// Should be high quality since it has kubectl commands and explanation
			if score < 0.5 {
				t.Errorf("Expected high quality score for good response, got %f", score)
			}
		}
	})
}

func TestReportGeneration(t *testing.T) {
	// Create mock benchmark results
	results := &BenchmarkResults{
		StartTime:     time.Now().Add(-5 * time.Minute),
		EndTime:       time.Now(),
		TotalDuration: 5 * time.Minute,
		ProviderResults: []*ProviderResult{
			{
				Target: BenchmarkTarget{
					Provider: "openai",
					Model:    "z-ai/glm-4.5-air:free",
				},
				AggregateStats: AggregateStats{
					OverallSuccessRate: 0.95,
					AvgResponseTime:    850.0,
					TotalTokensUsed:    1500,
					TotalCost:          0.0015,
					AvgQualityScore:    0.85,
					ScenariosCompleted: 3,
					TotalErrors:        0,
					ErrorRate:          0.05,
				},
				ScenarioResults: map[string]*ScenarioResult{
					"list_pods": {
						Scenario: TestScenario{Name: "list_pods", Complexity: ComplexitySimple},
						Stats: ScenarioStats{
							TotalRuns:       3,
							SuccessfulRuns:  3,
							SuccessRate:     1.0,
							AvgResponseTime: 800 * time.Millisecond,
							AvgInputTokens:  15,
							AvgOutputTokens: 45,
							QualityScore:    0.9,
							TokensPerSecond: 56.25,
						},
					},
				},
			},
			{
				Target: BenchmarkTarget{
					Provider: "google",
					Model:    "gemini-2.5-flash",
				},
				AggregateStats: AggregateStats{
					OverallSuccessRate: 0.98,
					AvgResponseTime:    650.0,
					TotalTokensUsed:    1200,
					TotalCost:          0.0008,
					AvgQualityScore:    0.88,
					ScenariosCompleted: 3,
					TotalErrors:        0,
					ErrorRate:          0.02,
				},
				ScenarioResults: map[string]*ScenarioResult{
					"list_pods": {
						Scenario: TestScenario{Name: "list_pods", Complexity: ComplexitySimple},
						Stats: ScenarioStats{
							TotalRuns:       3,
							SuccessfulRuns:  3,
							SuccessRate:     1.0,
							AvgResponseTime: 650 * time.Millisecond,
							AvgInputTokens:  15,
							AvgOutputTokens: 40,
							QualityScore:    0.88,
							TokensPerSecond: 61.54,
						},
					},
				},
			},
		},
	}

	cfg := &BenchmarkConfig{
		OutputFormat: "table",
		Verbose:      true,
	}

	generator := NewReportGenerator(results, cfg)

	// Test table output
	t.Run("TableOutput", func(t *testing.T) {
		var buf bytes.Buffer
		err := generator.GenerateTable(&buf)
		if err != nil {
			t.Fatalf("Failed to generate table report: %v", err)
		}

		output := buf.String()
		if len(output) == 0 {
			t.Error("Expected table output but got empty string")
		}

		// Check for expected content
		if !strings.Contains(output, "Provider Benchmark Results") {
			t.Error("Expected table to contain title")
		}
		if !strings.Contains(output, "z-ai/glm-4.5-air:free") {
			t.Error("Expected table to contain OpenAI model")
		}
		if !strings.Contains(output, "gemini-2.5-flash") {
			t.Error("Expected table to contain Google model")
		}
	})

	// Test JSON output
	t.Run("JSONOutput", func(t *testing.T) {
		var buf bytes.Buffer
		err := generator.GenerateJSON(&buf)
		if err != nil {
			t.Fatalf("Failed to generate JSON report: %v", err)
		}

		output := buf.String()
		if len(output) == 0 {
			t.Error("Expected JSON output but got empty string")
		}

		// Should be valid JSON containing our data
		if !strings.Contains(output, `"provider": "openai"`) {
			t.Error("Expected JSON to contain OpenAI provider")
		}
		if !strings.Contains(output, `"model": "gemini-2.5-flash"`) {
			t.Error("Expected JSON to contain Gemini model")
		}
	})

	// Test CSV output
	t.Run("CSVOutput", func(t *testing.T) {
		var buf bytes.Buffer
		err := generator.GenerateCSV(&buf)
		if err != nil {
			t.Fatalf("Failed to generate CSV report: %v", err)
		}

		output := buf.String()
		if len(output) == 0 {
			t.Error("Expected CSV output but got empty string")
		}

		// Should contain CSV header
		if !strings.Contains(output, "Provider,Model,Scenario") {
			t.Error("Expected CSV header")
		}
		if !strings.Contains(output, "openai,z-ai/glm-4.5-air:free") {
			t.Error("Expected CSV to contain OpenAI data")
		}
	})

	// Test Markdown output
	t.Run("MarkdownOutput", func(t *testing.T) {
		var buf bytes.Buffer
		err := generator.GenerateMarkdown(&buf)
		if err != nil {
			t.Fatalf("Failed to generate Markdown report: %v", err)
		}

		output := buf.String()
		if len(output) == 0 {
			t.Error("Expected Markdown output but got empty string")
		}

		// Should contain Markdown headers
		if !strings.Contains(output, "# LLM Provider Benchmark Results") {
			t.Error("Expected Markdown title")
		}
		if !strings.Contains(output, "## Provider Comparison") {
			t.Error("Expected provider comparison section")
		}
	})
}

func TestRealWorldScenarioExecution(t *testing.T) {
	// Test a realistic benchmark scenario with the mentioned models
	cfg := &BenchmarkConfig{
		Providers: []ProviderConfig{
			{
				Name:    "openai",
				Models:  []string{"z-ai/glm-4.5-air:free", "moonshotai/kimi-k2:free"},
				BaseURL: "https://openrouter.ai/api/v1", // OpenRouter endpoint
			},
			{
				Name:   "google",
				Models: []string{"gemini-2.5-flash"},
			},
		},
		Iterations:          2,
		Timeout:             30 * time.Second,
		Parallel:            1,
		WarmupRuns:          1,
		CooldownDelay:       500 * time.Millisecond,
		ComplexityFilter:    "simple",
		EnableQualityChecks: true,
		EnableCostTracking:  true,
		Verbose:             true,
	}

	runner := NewBenchmarkRunner(cfg)

	// Load realistic scenarios
	runner.AddScenarios(GetScenariosByComplexity(ComplexitySimple))

	// Load all collectors
	runner.LoadDefaultCollectors()

	// Mock realistic responses
	originalRequest := llm.Request
	responses := []llm.MockResponse{
		// Multiple responses for different provider/model/scenario combinations
		{Content: "To see all pods, use: `kubectl get pods --all-namespaces`", TokensUsed: 25},
		{Content: "Check pod status with: `kubectl get pods -o wide`", TokensUsed: 22},
		{Content: "List services: `kubectl get services` or `kubectl get svc`", TokensUsed: 20},
		{Content: "Show nodes: `kubectl get nodes -o wide`", TokensUsed: 18},
		{Content: "View deployments: `kubectl get deployments --all-namespaces`", TokensUsed: 24},
		{Content: "Check namespaces: `kubectl get namespaces`", TokensUsed: 16},
		// Repeat for second model
		{Content: "Use kubectl get pods to see all running pods", TokensUsed: 28},
		{Content: "kubectl get pods -o wide shows detailed pod information", TokensUsed: 26},
		{Content: "kubectl get svc lists all services in current namespace", TokensUsed: 24},
		{Content: "kubectl get nodes shows cluster nodes and their status", TokensUsed: 25},
		{Content: "kubectl get deploy shows deployment status", TokensUsed: 22},
		{Content: "kubectl get ns lists all namespaces", TokensUsed: 18},
		// Google responses
		{Content: "# Pod Listing\n\n```bash\nkubectl get pods --all-namespaces\n```\n\nThis shows all pods across namespaces.", TokensUsed: 35},
		{Content: "# Pod Status\n\n```bash\nkubectl get pods -o wide\n```\n\nShows detailed pod information.", TokensUsed: 32},
		{Content: "# Services\n\n```bash\nkubectl get services\n```\n\nLists all services.", TokensUsed: 28},
		{Content: "# Cluster Nodes\n\n```bash\nkubectl get nodes -o wide\n```\n\nShows node details.", TokensUsed: 30},
		{Content: "# Deployments\n\n```bash\nkubectl get deployments\n```\n\nShows deployment status.", TokensUsed: 29},
		{Content: "# Namespaces\n\n```bash\nkubectl get namespaces\n```\n\nLists namespaces.", TokensUsed: 26},
	}

	llm.Request = llm.MockRequestFunc(responses)
	defer func() {
		llm.Request = originalRequest
	}()

	// Run the benchmark (this tests the full integration)
	ctx := context.Background()
	results, err := runner.Run(ctx)

	// For this test, we expect it to work even with mocked data
	if err != nil {
		t.Fatalf("Real-world scenario benchmark failed: %v", err)
	}

	// Validate we got comprehensive results
	expectedProviderModels := 3 // 2 OpenAI + 1 Google
	if len(results.ProviderResults) != expectedProviderModels {
		t.Errorf("Expected %d provider/model results, got %d", expectedProviderModels, len(results.ProviderResults))
	}

	// Check that each provider has quality and cost metrics
	for _, provider := range results.ProviderResults {
		providerName := fmt.Sprintf("%s/%s", provider.Target.Provider, provider.Target.Model)

		if provider.AggregateStats.AvgQualityScore == 0 {
			t.Errorf("Provider %s: expected quality score > 0", providerName)
		}

		if len(provider.ScenarioResults) == 0 {
			t.Errorf("Provider %s: expected scenario results", providerName)
		}

		// Check scenario-level results
		for scenarioName, scenarioResult := range provider.ScenarioResults {
			if len(scenarioResult.Runs) == 0 {
				t.Errorf("Provider %s, scenario %s: expected run results", providerName, scenarioName)
			}

			if scenarioResult.Stats.SuccessRate == 0 {
				t.Errorf("Provider %s, scenario %s: expected success rate > 0", providerName, scenarioName)
			}
		}
	}

	// Test report generation with real results
	reportGen := NewReportGenerator(results, cfg)

	var buf bytes.Buffer
	err = reportGen.GenerateTable(&buf)
	if err != nil {
		t.Fatalf("Failed to generate report from real results: %v", err)
	}

	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected report output but got empty string")
	}

	// Should contain our specific models
	if !strings.Contains(output, "z-ai/glm-4.5-air:free") {
		t.Error("Report should contain z-ai/glm-4.5-air:free model")
	}
	if !strings.Contains(output, "moonshotai/kimi-k2:free") {
		t.Error("Report should contain moonshotai/kimi-k2:free model")
	}
	if !strings.Contains(output, "gemini-2.5-flash") {
		t.Error("Report should contain gemini-2.5-flash model")
	}
}

func TestBenchmarkConfigValidation(t *testing.T) {
	// Test various configuration scenarios

	t.Run("ValidConfig", func(t *testing.T) {
		cfg := &BenchmarkConfig{
			Providers: []ProviderConfig{
				{Name: "openai", Models: []string{"gpt-4o-mini"}},
			},
			Iterations:   3,
			Timeout:      30 * time.Second,
			Parallel:     1,
			OutputFormat: "table",
		}

		runner := NewBenchmarkRunner(cfg)
		runner.AddScenario(CreateCustomScenario("test", "test", "test prompt", ComplexitySimple, "test", nil, nil))

		// Should not panic or error during setup
		if runner.config == nil {
			t.Error("Expected config to be set")
		}
	})

	t.Run("EmptyProviders", func(t *testing.T) {
		cfg := &BenchmarkConfig{
			Providers:    []ProviderConfig{},
			Iterations:   1,
			OutputFormat: "table",
		}

		runner := NewBenchmarkRunner(cfg)
		runner.AddScenario(CreateCustomScenario("test", "test", "test prompt", ComplexitySimple, "test", nil, nil))

		// Should handle empty providers gracefully
		ctx := context.Background()
		results, err := runner.Run(ctx)

		// Should complete but with no results
		if err != nil && !strings.Contains(err.Error(), "no scenarios match") {
			t.Errorf("Expected specific error or success, got: %v", err)
		}

		if results != nil && len(results.ProviderResults) != 0 {
			t.Error("Expected no provider results for empty providers")
		}
	})
}

// Benchmark tests to validate performance of the benchmarking system itself
func BenchmarkScenarioExecution(b *testing.B) {
	scenario := CreateCustomScenario("bench_test", "Benchmark test", "Show pods", ComplexitySimple, "test", nil, nil)

	// Mock fast responses
	originalRequest := llm.Request
	llm.Request = llm.MockRequestFunc([]llm.MockResponse{
		{Content: "kubectl get pods", TokensUsed: 10},
	})
	defer func() {
		llm.Request = originalRequest
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner := NewBenchmarkRunner(&BenchmarkConfig{
			Iterations:   1,
			Timeout:      5 * time.Second,
			OutputFormat: "json",
		})
		runner.AddScenario(scenario)
		runner.AddMetricsCollector(NewPerformanceCollector())

		ctx := context.Background()
		_, err := runner.Run(ctx)
		if err != nil {
			b.Fatalf("Benchmark execution failed: %v", err)
		}
	}
}

func BenchmarkReportGeneration(b *testing.B) {
	// Create sample results
	results := &BenchmarkResults{
		ProviderResults: []*ProviderResult{
			{
				Target: BenchmarkTarget{Provider: "openai", Model: "test"},
				ScenarioResults: map[string]*ScenarioResult{
					"test": {
						Stats: ScenarioStats{
							AvgResponseTime: 500 * time.Millisecond,
							SuccessRate:     1.0,
							QualityScore:    0.8,
						},
					},
				},
			},
		},
	}

	cfg := &BenchmarkConfig{OutputFormat: "table"}
	generator := NewReportGenerator(results, cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		err := generator.GenerateTable(&buf)
		if err != nil {
			b.Fatalf("Report generation failed: %v", err)
		}
	}
}
