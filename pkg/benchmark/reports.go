package benchmark

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/themes"
)

// ReportGenerator handles benchmark result formatting and output
type ReportGenerator struct {
	results *BenchmarkResults
	config  *BenchmarkConfig
}

// NewReportGenerator creates a new report generator
func NewReportGenerator(results *BenchmarkResults, config *BenchmarkConfig) *ReportGenerator {
	return &ReportGenerator{
		results: results,
		config:  config,
	}
}

// Generate creates a report in the specified format
func (rg *ReportGenerator) Generate(format string, writer io.Writer) error {
	switch strings.ToLower(format) {
	case "json":
		return rg.GenerateJSON(writer)
	case "csv":
		return rg.GenerateCSV(writer)
	case "markdown":
		return rg.GenerateMarkdown(writer)
	case "table", "":
		return rg.GenerateTable(writer)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// GenerateAndSave generates and saves report to file or stdout
func (rg *ReportGenerator) GenerateAndSave(format, filename string) error {
	var writer io.Writer = os.Stdout

	if filename != "" {
		file, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer file.Close()
		writer = file
	}

	return rg.Generate(format, writer)
}

// GenerateJSON outputs results as JSON
func (rg *ReportGenerator) GenerateJSON(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rg.results)
}

// GenerateCSV outputs results as CSV
func (rg *ReportGenerator) GenerateCSV(writer io.Writer) error {
	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	// Write header
	header := []string{
		"Provider",
		"Model",
		"Scenario",
		"Complexity",
		"Success_Rate",
		"Avg_Response_Time_MS",
		"Min_Response_Time_MS",
		"Max_Response_Time_MS",
		"Avg_Input_Tokens",
		"Avg_Output_Tokens",
		"Tokens_Per_Second",
		"Quality_Score",
		"Total_Cost_USD",
		"Error_Rate",
	}

	if err := csvWriter.Write(header); err != nil {
		return err
	}

	// Write data rows
	for _, providerResult := range rg.results.ProviderResults {
		for _, scenarioResult := range providerResult.ScenarioResults {
			row := []string{
				providerResult.Target.Provider,
				providerResult.Target.Model,
				scenarioResult.Scenario.Name,
				string(scenarioResult.Scenario.Complexity),
				fmt.Sprintf("%.2f", scenarioResult.Stats.SuccessRate),
				fmt.Sprintf("%.0f", float64(scenarioResult.Stats.AvgResponseTime.Milliseconds())),
				fmt.Sprintf("%d", scenarioResult.Stats.MinResponseTime.Milliseconds()),
				fmt.Sprintf("%d", scenarioResult.Stats.MaxResponseTime.Milliseconds()),
				fmt.Sprintf("%.1f", scenarioResult.Stats.AvgInputTokens),
				fmt.Sprintf("%.1f", scenarioResult.Stats.AvgOutputTokens),
				fmt.Sprintf("%.1f", scenarioResult.Stats.TokensPerSecond),
				fmt.Sprintf("%.3f", scenarioResult.Stats.QualityScore),
				fmt.Sprintf("%.6f", scenarioResult.Stats.AvgCostPerRun),
				fmt.Sprintf("%.2f", 1.0-scenarioResult.Stats.SuccessRate),
			}

			if err := csvWriter.Write(row); err != nil {
				return err
			}
		}
	}

	return nil
}

// GenerateMarkdown outputs results as Markdown
func (rg *ReportGenerator) GenerateMarkdown(writer io.Writer) error {
	fmt.Fprintf(writer, "# LLM Provider Benchmark Results\n\n")
	fmt.Fprintf(writer, "**Generated:** %s\n", rg.results.EndTime.Format(time.RFC3339))
	fmt.Fprintf(writer, "**Duration:** %v\n", rg.results.TotalDuration)
	fmt.Fprintf(writer, "**Providers:** %d\n", len(rg.results.ProviderResults))
	fmt.Fprintf(writer, "**Total Scenarios:** %d\n\n", len(rg.getUniqueScenarios()))

	// Executive summary
	if rg.results.Summary != nil {
		fmt.Fprintf(writer, "## Executive Summary\n\n")
		fmt.Fprintf(writer, "- **Overall Success Rate:** %.1f%%\n", rg.results.Summary.OverallSuccessRate*100)
		if rg.results.Summary.FastestProvider != "" {
			fmt.Fprintf(writer, "- **Fastest Provider:** %s\n", rg.results.Summary.FastestProvider)
		}
		if rg.results.Summary.MostReliable != "" {
			fmt.Fprintf(writer, "- **Most Reliable:** %s\n", rg.results.Summary.MostReliable)
		}
		if rg.results.Summary.MostCostEffective != "" {
			fmt.Fprintf(writer, "- **Most Cost Effective:** %s\n", rg.results.Summary.MostCostEffective)
		}
		if rg.results.Summary.BestQuality != "" {
			fmt.Fprintf(writer, "- **Best Quality:** %s\n", rg.results.Summary.BestQuality)
		}
		fmt.Fprintf(writer, "\n")
	}

	// Provider comparison
	fmt.Fprintf(writer, "## Provider Comparison\n\n")
	rg.writeProviderComparisonMarkdown(writer)

	// Scenario-by-scenario breakdown
	fmt.Fprintf(writer, "\n## Detailed Results by Scenario\n\n")
	scenarios := rg.getUniqueScenarios()
	for _, scenario := range scenarios {
		rg.writeScenarioBreakdownMarkdown(writer, scenario)
	}

	// Performance details
	fmt.Fprintf(writer, "\n## Performance Details\n\n")
	rg.writePerformanceDetailsMarkdown(writer)

	return nil
}

// GenerateTable outputs results as a formatted table
func (rg *ReportGenerator) GenerateTable(writer io.Writer) error {
	fmt.Fprintf(writer, "\n🚀 LLM Provider Benchmark Results\n")
	fmt.Fprintf(writer, "═══════════════════════════════════════════════════════════════\n\n")

	// Summary information
	fmt.Fprintf(writer, "📊 Summary:\n")
	fmt.Fprintf(writer, "  • Duration: %v\n", rg.results.TotalDuration)
	fmt.Fprintf(writer, "  • Providers: %d\n", len(rg.results.ProviderResults))
	fmt.Fprintf(writer, "  • Scenarios: %d\n", len(rg.getUniqueScenarios()))
	fmt.Fprintf(writer, "  • Generated: %s\n\n", rg.results.EndTime.Format("2006-01-02 15:04:05"))

	// Provider overview table
	fmt.Fprintf(writer, "🏆 Provider Rankings:\n")
	rg.writeProviderComparisonTable(writer)

	// Detailed scenario results
	fmt.Fprintf(writer, "\n📈 Detailed Results:\n")
	rg.writeDetailedResultsTable(writer)

	// Render sample LLM outputs using terminal windows
	fmt.Fprintf(writer, "\n🪟 Sample LLM Outputs (truncated):\n")
	rg.writeLLMSampleWindows(writer)

	// Best/worst performers
	fmt.Fprintf(writer, "\n🎯 Key Insights:\n")
	rg.writeKeyInsights(writer)

	return nil
}

// Helper methods for different output sections

func (rg *ReportGenerator) writeProviderComparisonMarkdown(writer io.Writer) {
	fmt.Fprintf(writer, "| Provider/Model | Avg Response Time | Success Rate | Avg Quality Score | Avg Cost/Request |\n")
	fmt.Fprintf(writer, "|---------------|------------------|--------------|-------------------|------------------|\n")

	// Sort providers by overall performance
	providers := make([]*ProviderResult, len(rg.results.ProviderResults))
	copy(providers, rg.results.ProviderResults)
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].AggregateStats.AvgResponseTime < providers[j].AggregateStats.AvgResponseTime
	})

	for _, provider := range providers {
		providerName := fmt.Sprintf("%s/%s", provider.Target.Provider, provider.Target.Model)
		fmt.Fprintf(writer, "| %s | %.0fms | %.1f%% | %.2f | $%.6f |\n",
			providerName,
			provider.AggregateStats.AvgResponseTime,
			provider.AggregateStats.OverallSuccessRate*100,
			provider.AggregateStats.AvgQualityScore,
			provider.AggregateStats.TotalCost/float64(provider.AggregateStats.ScenariosCompleted),
		)
	}
}

func (rg *ReportGenerator) writeScenarioBreakdownMarkdown(writer io.Writer, scenario string) {
	fmt.Fprintf(writer, "### %s\n\n", strings.Title(strings.ReplaceAll(scenario, "_", " ")))

	// Find all results for this scenario
	scenarioResults := []*ScenarioResult{}
	for _, provider := range rg.results.ProviderResults {
		if result, exists := provider.ScenarioResults[scenario]; exists {
			scenarioResults = append(scenarioResults, result)
		}
	}

	if len(scenarioResults) == 0 {
		return
	}

	// Sort by response time
	sort.Slice(scenarioResults, func(i, j int) bool {
		return scenarioResults[i].Stats.AvgResponseTime < scenarioResults[j].Stats.AvgResponseTime
	})

	fmt.Fprintf(writer, "| Provider/Model | Response Time | Success Rate | Quality | Tokens/sec |\n")
	fmt.Fprintf(writer, "|----------------|---------------|--------------|---------|------------|\n")

	for _, result := range scenarioResults {
		providerName := fmt.Sprintf("%s/%s", result.Target.Provider, result.Target.Model)
		fmt.Fprintf(writer, "| %s | %.0fms | %.1f%% | %.2f | %.1f |\n",
			providerName,
			float64(result.Stats.AvgResponseTime.Milliseconds()),
			result.Stats.SuccessRate*100,
			result.Stats.QualityScore,
			result.Stats.TokensPerSecond,
		)
	}

	fmt.Fprintf(writer, "\n")
}

func (rg *ReportGenerator) writePerformanceDetailsMarkdown(writer io.Writer) {
	fmt.Fprintf(writer, "### Response Time Distribution\n\n")

	for _, provider := range rg.results.ProviderResults {
		fmt.Fprintf(writer, "**%s/%s**\n", provider.Target.Provider, provider.Target.Model)

		// Calculate overall stats across all scenarios
		var totalMinTime, totalMaxTime, totalAvgTime time.Duration
		totalScenarios := 0

		for _, scenarioResult := range provider.ScenarioResults {
			totalMinTime += scenarioResult.Stats.MinResponseTime
			totalMaxTime += scenarioResult.Stats.MaxResponseTime
			totalAvgTime += scenarioResult.Stats.AvgResponseTime
			totalScenarios++
		}

		if totalScenarios > 0 {
			avgMinTime := totalMinTime / time.Duration(totalScenarios)
			avgMaxTime := totalMaxTime / time.Duration(totalScenarios)
			avgAvgTime := totalAvgTime / time.Duration(totalScenarios)

			fmt.Fprintf(writer, "- Min: %v, Avg: %v, Max: %v\n", avgMinTime, avgAvgTime, avgMaxTime)
		}
		fmt.Fprintf(writer, "\n")
	}
}

func (rg *ReportGenerator) writeProviderComparisonTable(writer io.Writer) {
	w := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)

	fmt.Fprintf(w, "Rank\tProvider/Model\tResponse Time\tSuccess Rate\tQuality\tCost/Request\n")
	fmt.Fprintf(w, "----\t--------------\t-------------\t------------\t-------\t------------\n")

	// Sort providers by a composite score
	providers := make([]*ProviderResult, len(rg.results.ProviderResults))
	copy(providers, rg.results.ProviderResults)
	sort.Slice(providers, func(i, j int) bool {
		// Simple composite score: higher success rate and quality, lower response time
		scoreI := providers[i].AggregateStats.OverallSuccessRate + providers[i].AggregateStats.AvgQualityScore - (providers[i].AggregateStats.AvgResponseTime / 1000.0)
		scoreJ := providers[j].AggregateStats.OverallSuccessRate + providers[j].AggregateStats.AvgQualityScore - (providers[j].AggregateStats.AvgResponseTime / 1000.0)
		return scoreI > scoreJ
	})

	for i, provider := range providers {
		costPerRequest := 0.0
		if provider.AggregateStats.ScenariosCompleted > 0 {
			costPerRequest = provider.AggregateStats.TotalCost / float64(provider.AggregateStats.ScenariosCompleted)
		}

		fmt.Fprintf(w, "%d\t%s/%s\t%.0fms\t%.1f%%\t%.2f\t$%.6f\n",
			i+1,
			provider.Target.Provider,
			provider.Target.Model,
			provider.AggregateStats.AvgResponseTime,
			provider.AggregateStats.OverallSuccessRate*100,
			provider.AggregateStats.AvgQualityScore,
			costPerRequest,
		)
	}

	w.Flush()
}

func (rg *ReportGenerator) writeDetailedResultsTable(writer io.Writer) {
	scenarios := rg.getUniqueScenarios()

	for _, scenario := range scenarios {
		fmt.Fprintf(writer, "\n📝 %s:\n", strings.Title(strings.ReplaceAll(scenario, "_", " ")))

		w := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "Provider/Model\tResponse Time\tSuccess\tQuality\tTokens/sec\tCost\n")
		fmt.Fprintf(w, "--------------\t-------------\t-------\t-------\t----------\t----\n")

		// Find all results for this scenario
		scenarioResults := []*ScenarioResult{}
		for _, provider := range rg.results.ProviderResults {
			if result, exists := provider.ScenarioResults[scenario]; exists {
				scenarioResults = append(scenarioResults, result)
			}
		}

		// Sort by response time
		sort.Slice(scenarioResults, func(i, j int) bool {
			return scenarioResults[i].Stats.AvgResponseTime < scenarioResults[j].Stats.AvgResponseTime
		})

		for _, result := range scenarioResults {
			fmt.Fprintf(w, "%s/%s\t%.0fms\t%.0f%%\t%.2f\t%.1f\t$%.6f\n",
				result.Target.Provider,
				result.Target.Model,
				float64(result.Stats.AvgResponseTime.Milliseconds()),
				result.Stats.SuccessRate*100,
				result.Stats.QualityScore,
				result.Stats.TokensPerSecond,
				result.Stats.AvgCostPerRun,
			)
		}

		w.Flush()
	}
}

// writeLLMSampleWindows displays representative LLM outputs inside terminal-style windows.
func (rg *ReportGenerator) writeLLMSampleWindows(writer io.Writer) {
	// Pull UI knobs
	uiCfg := config.LoadConfig()
	uiCfg.Theme = themes.Apply(uiCfg.Theme)
	maxLines := uiCfg.ToolOutputMaxLines
	maxLineLen := uiCfg.ToolOutputMaxLineLen

	// Limit to avoid overwhelming the terminal
	maxWindows := 6
	rendered := 0

	scenarios := rg.getUniqueScenarios()
	for _, scenario := range scenarios {
		for _, provider := range rg.results.ProviderResults {
			if rendered >= maxWindows {
				msg := config.Colors.Dim.Sprint("\n… additional outputs omitted …\n")
				fmt.Fprint(writer, msg)
				return
			}
			sr, ok := provider.ScenarioResults[scenario]
			if !ok || sr == nil {
				continue
			}

			// Choose a non-warmup run with a response
			sample := ""
			for i := len(sr.Runs) - 1; i >= 0; i-- {
				run := sr.Runs[i]
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
			if strings.TrimSpace(sample) == "" {
				continue
			}

			title := fmt.Sprintf("%s/%s • %s", provider.Target.Provider, provider.Target.Model, strings.Title(strings.ReplaceAll(scenario, "_", " ")))
			window := lib.RenderTerminalWindow(title, sample, maxLines, maxLineLen)
			fmt.Fprint(writer, window)
			rendered++
		}
	}
}

func (rg *ReportGenerator) writeKeyInsights(writer io.Writer) {
	// Find fastest provider
	var fastest *ProviderResult
	var slowest *ProviderResult
	var mostReliable *ProviderResult
	var cheapest *ProviderResult

	for _, provider := range rg.results.ProviderResults {
		if fastest == nil || provider.AggregateStats.AvgResponseTime < fastest.AggregateStats.AvgResponseTime {
			fastest = provider
		}
		if slowest == nil || provider.AggregateStats.AvgResponseTime > slowest.AggregateStats.AvgResponseTime {
			slowest = provider
		}
		if mostReliable == nil || provider.AggregateStats.OverallSuccessRate > mostReliable.AggregateStats.OverallSuccessRate {
			mostReliable = provider
		}
		if cheapest == nil || provider.AggregateStats.TotalCost < cheapest.AggregateStats.TotalCost {
			cheapest = provider
		}
	}

	if fastest != nil {
		fmt.Fprintf(writer, "  🚀 Fastest: %s/%s (%.0fms avg)\n",
			fastest.Target.Provider, fastest.Target.Model, fastest.AggregateStats.AvgResponseTime)
	}
	if slowest != nil {
		fmt.Fprintf(writer, "  🐌 Slowest: %s/%s (%.0fms avg)\n",
			slowest.Target.Provider, slowest.Target.Model, slowest.AggregateStats.AvgResponseTime)
	}
	if mostReliable != nil {
		fmt.Fprintf(writer, "  🎯 Most Reliable: %s/%s (%.1f%% success)\n",
			mostReliable.Target.Provider, mostReliable.Target.Model, mostReliable.AggregateStats.OverallSuccessRate*100)
	}
	if cheapest != nil {
		fmt.Fprintf(writer, "  💰 Most Cost Effective: %s/%s ($%.6f total)\n",
			cheapest.Target.Provider, cheapest.Target.Model, cheapest.AggregateStats.TotalCost)
	}

	// Speed difference
	if fastest != nil && slowest != nil && len(rg.results.ProviderResults) > 1 {
		speedDiff := slowest.AggregateStats.AvgResponseTime - fastest.AggregateStats.AvgResponseTime
		speedRatio := slowest.AggregateStats.AvgResponseTime / fastest.AggregateStats.AvgResponseTime
		fmt.Fprintf(writer, "  ⚡ Speed difference: %.1fx faster (%.0fms difference)\n", speedRatio, speedDiff)
	}

	// Quality insights
	qualityScores := []float64{}
	for _, provider := range rg.results.ProviderResults {
		if provider.AggregateStats.AvgQualityScore > 0 {
			qualityScores = append(qualityScores, provider.AggregateStats.AvgQualityScore)
		}
	}

	if len(qualityScores) > 0 {
		var totalQuality float64
		maxQuality := 0.0
		minQuality := 1.0

		for _, score := range qualityScores {
			totalQuality += score
			if score > maxQuality {
				maxQuality = score
			}
			if score < minQuality {
				minQuality = score
			}
		}

		avgQuality := totalQuality / float64(len(qualityScores))
		fmt.Fprintf(writer, "  📊 Quality range: %.2f - %.2f (avg: %.2f)\n", minQuality, maxQuality, avgQuality)
	}
}

// Helper methods

func (rg *ReportGenerator) getUniqueScenarios() []string {
	scenarioMap := make(map[string]bool)

	for _, provider := range rg.results.ProviderResults {
		for scenarioName := range provider.ScenarioResults {
			scenarioMap[scenarioName] = true
		}
	}

	scenarios := make([]string, 0, len(scenarioMap))
	for scenario := range scenarioMap {
		scenarios = append(scenarios, scenario)
	}

	sort.Strings(scenarios)
	return scenarios
}

// CalculateSummary calculates benchmark summary statistics
func (rg *ReportGenerator) CalculateSummary() *BenchmarkSummary {
	if len(rg.results.ProviderResults) == 0 {
		return &BenchmarkSummary{}
	}

	summary := &BenchmarkSummary{
		TotalProviders: len(rg.results.ProviderResults),
		TotalScenarios: len(rg.getUniqueScenarios()),
		Rankings:       make(map[string]ProviderRanking),
	}

	// Calculate overall success rate
	var totalSuccessRate float64
	var totalCost float64
	var totalQuality float64
	var fastestTime float64 = -1
	var highestSuccessRate float64
	var lowestCost float64 = -1
	var highestQuality float64

	for _, provider := range rg.results.ProviderResults {
		stats := provider.AggregateStats
		totalSuccessRate += stats.OverallSuccessRate
		totalCost += stats.TotalCost
		totalQuality += stats.AvgQualityScore

		// Track best performers
		if fastestTime < 0 || stats.AvgResponseTime < fastestTime {
			fastestTime = stats.AvgResponseTime
			summary.FastestProvider = provider.Target.String()
		}

		if stats.OverallSuccessRate > highestSuccessRate {
			highestSuccessRate = stats.OverallSuccessRate
			summary.MostReliable = provider.Target.String()
		}

		if lowestCost < 0 || stats.TotalCost < lowestCost {
			lowestCost = stats.TotalCost
			summary.MostCostEffective = provider.Target.String()
		}

		if stats.AvgQualityScore > highestQuality {
			highestQuality = stats.AvgQualityScore
			summary.BestQuality = provider.Target.String()
		}
	}

	numProviders := len(rg.results.ProviderResults)
	summary.OverallSuccessRate = totalSuccessRate / float64(numProviders)

	return summary
}

// ExportConfig exports benchmark configuration for reproducibility
func (rg *ReportGenerator) ExportConfig(writer io.Writer) error {
	fmt.Fprintf(writer, "# Benchmark Configuration\n")
	fmt.Fprintf(writer, "# This configuration can be used to reproduce the benchmark results\n\n")

	fmt.Fprintf(writer, "export QU_BENCHMARK_ENABLED=true\n")
	fmt.Fprintf(writer, "export QU_BENCHMARK_ITERATIONS=%d\n", rg.config.Iterations)
	fmt.Fprintf(writer, "export QU_BENCHMARK_TIMEOUT=%d\n", int(rg.config.Timeout.Seconds()))
	fmt.Fprintf(writer, "export QU_BENCHMARK_PARALLEL=%d\n", rg.config.Parallel)
	fmt.Fprintf(writer, "export QU_BENCHMARK_WARMUP_RUNS=%d\n", rg.config.WarmupRuns)
	fmt.Fprintf(writer, "export QU_BENCHMARK_COOLDOWN_DELAY=%d\n", int(rg.config.CooldownDelay.Milliseconds()))
	fmt.Fprintf(writer, "export QU_BENCHMARK_COMPLEXITY=%s\n", rg.config.ComplexityFilter)
	fmt.Fprintf(writer, "export QU_BENCHMARK_ENABLE_QUALITY=%t\n", rg.config.EnableQualityChecks)
	fmt.Fprintf(writer, "export QU_BENCHMARK_ENABLE_COST=%t\n", rg.config.EnableCostTracking)

	if len(rg.config.Providers) > 0 {
		providers := make([]string, len(rg.config.Providers))
		for i, p := range rg.config.Providers {
			providers[i] = p.Name
		}
		fmt.Fprintf(writer, "export QU_BENCHMARK_PROVIDERS=%s\n", strings.Join(providers, ","))
	}

	return nil
}
