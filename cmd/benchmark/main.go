package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/benchmark"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

const (
	version = "1.0.0"
	banner  = `
🦆 kubectl-quackops LLM Benchmark Tool v%s
═══════════════════════════════════════════════════════════════
Benchmark and compare LLM providers for kubectl operations
`
)

func main() {
	var (
		// Basic options
		showVersion = flag.Bool("version", false, "Show version and exit")
		showHelp    = flag.Bool("help", false, "Show help and exit")
		verbose     = flag.Bool("verbose", false, "Enable verbose logging")
		configFile  = flag.String("config", "", "Path to benchmark configuration file")

		// Model selection (provider/model format)
		models    = flag.String("models", "", "Comma-separated list of models with provider prefix (e.g., 'openai/gpt-4o-mini,google/gemini-2.5-flash,openai/z-ai/glm-4.5-air:free')")
		providers = flag.String("providers", "", "Optional: Override providers (auto-detected from models if not specified)")

		// Execution control
		iterations = flag.Int("iterations", 0, "Number of iterations per scenario (0 = use config default)")
		timeout    = flag.Int("timeout", 0, "Timeout in seconds per request (0 = use config default)")
		parallel   = flag.Int("parallel", 0, "Number of parallel executions (0 = use config default)")
		warmup     = flag.Int("warmup", -1, "Number of warmup runs (-1 = use config default, 0 = disable warmups)")
		cooldown   = flag.Int("cooldown", 0, "Cooldown delay in milliseconds between runs (0 = use config default)")

		// Scenario filtering
		scenarios  = flag.String("scenarios", "", "Comma-separated list of specific scenarios to run")
		complexity = flag.String("complexity", "", "Filter by complexity: simple, medium, complex, all")
		category   = flag.String("category", "", "Filter by category (e.g., 'basic_operations', 'troubleshooting')")
		tags       = flag.String("tags", "", "Filter by tags (e.g., 'pods,services')")

		// Output control
		format = flag.String("format", "", "Output format: table, json, csv, markdown (default: table)")
		output = flag.String("output", "", "Output file (default: stdout)")
		quiet  = flag.Bool("quiet", false, "Suppress progress output")

		// Feature toggles
		noQuality       = flag.Bool("no-quality", false, "Disable quality evaluation")
		noCost          = flag.Bool("no-cost", false, "Disable cost tracking")
		kubectlGeneration = flag.Bool("kubectl-generation", false, "Enable kubectl command generation testing")
		dryRun    = flag.Bool("dry-run", false, "Show what would be benchmarked without running")

		// Advanced options
		listScenarios = flag.Bool("list-scenarios", false, "List available scenarios and exit")
		exportConfig  = flag.Bool("export-config", false, "Export benchmark configuration and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, banner, version)
		fmt.Fprintf(os.Stderr, "\nUsage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Benchmark OpenAI models with simple scenarios\n")
		fmt.Fprintf(os.Stderr, "  %s --models=openai/z-ai/glm-4.5-air:free,openai/moonshotai/kimi-k2:free --complexity=simple\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Compare specific models across providers\n")
		fmt.Fprintf(os.Stderr, "  %s --models=openai/gpt-4o-mini,google/gemini-2.5-flash,anthropic/claude-3-haiku --iterations=5 --format=markdown\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Run comprehensive benchmark and save results\n")
		fmt.Fprintf(os.Stderr, "  %s --models=openai/gpt-4o-mini,google/gemini-2.5-flash --iterations=3 --output=benchmark-results.json --format=json\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Environment Variables:\n")
		fmt.Fprintf(os.Stderr, "  QU_BENCHMARK_* - All benchmark configuration can be set via environment variables\n")
		fmt.Fprintf(os.Stderr, "  API keys: OPENAI_API_KEY, ANTHROPIC_API_KEY, GOOGLE_API_KEY\n")
		fmt.Fprintf(os.Stderr, "\nFor more information, visit: https://github.com/mikhae1/kubectl-quackops\n")
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("kubectl-quackops benchmark tool v%s\n", version)
		os.Exit(0)
	}

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	// Setup logging level based on flags
	// The logger package uses DEBUG environment variable, but we can control output with our flags

	// Handle list scenarios
	if *listScenarios {
		listAvailableScenarios()
		os.Exit(0)
	}

	// Load base configuration
	cfg := config.LoadConfig()

	// Build benchmark configuration
	benchConfig, err := buildBenchmarkConfig(cfg, &cliOptions{
		configFile:        *configFile,
		providers:         *providers,
		models:            *models,
		iterations:        *iterations,
		timeout:           *timeout,
		parallel:          *parallel,
		warmup:            *warmup,
		cooldown:          *cooldown,
		scenarios:         *scenarios,
		complexity:        *complexity,
		category:          *category,
		tags:              *tags,
		format:            *format,
		output:            *output,
		noQuality:         *noQuality,
		noCost:            *noCost,
		verbose:           *verbose,
		kubectlGeneration: *kubectlGeneration,
	})
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Handle export config
	if *exportConfig {
		exportBenchmarkConfig(benchConfig)
		os.Exit(0)
	}

	// Validate configuration
	if len(benchConfig.Providers) == 0 {
		log.Fatal("No models specified. Use --models with provider/model format (e.g., --models=openai/gpt-4o-mini,google/gemini-2.5-flash)")
	}

	// Setup benchmark runner
	runner := benchmark.NewBenchmarkRunner(benchConfig)

	// Load scenarios
	if err := loadScenarios(runner, benchConfig); err != nil {
		log.Fatalf("Failed to load scenarios: %v", err)
	}

	// Load metrics collectors
	runner.LoadDefaultCollectors()

	// Show what will be benchmarked
	if !*quiet {
		showBenchmarkPlan(benchConfig)
	}

	if *dryRun {
		fmt.Println("\n✅ Dry run completed. Use without --dry-run to execute benchmark.")
		os.Exit(0)
	}

	// Confirmation prompt removed for non-interactive runs

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintf(os.Stderr, "\n🛑 Benchmark interrupted. Cleaning up...\n")
		cancel()
	}()

	// Run the benchmark
	if !*quiet {
		fmt.Printf(banner, version)
		fmt.Println("🚀 Starting benchmark execution...")
	}

	startTime := time.Now()
	results, err := runner.Run(ctx)
	duration := time.Since(startTime)

	if err != nil {
		if ctx.Err() == context.Canceled {
			log.Fatal("Benchmark was cancelled")
		}
		log.Fatalf("Benchmark failed: %v", err)
	}

	if !*quiet {
		fmt.Printf("✅ Benchmark completed in %v\n\n", duration)
	}

	// Generate and output results
	if err := outputResults(results, benchConfig); err != nil {
		log.Fatalf("Failed to output results: %v", err)
	}

	if !*quiet {
		fmt.Println("🎉 Benchmark completed successfully!")
	}
}

type cliOptions struct {
	configFile, providers, models                         string
	iterations, timeout, parallel, warmup, cooldown       int
	scenarios, complexity, category, tags, format, output string
	noQuality, noCost, verbose, kubectlGeneration          bool
}

func buildBenchmarkConfig(baseCfg *config.Config, opts *cliOptions) (*benchmark.BenchmarkConfig, error) {
	// Start with configuration from environment/config files
	cfg := &benchmark.BenchmarkConfig{
		Providers:           []benchmark.ProviderConfig{},
		Iterations:          baseCfg.BenchmarkIterations,
		Timeout:             time.Duration(baseCfg.BenchmarkTimeout.Seconds()) * time.Second,
		Parallel:            baseCfg.BenchmarkParallel,
		WarmupRuns:          baseCfg.BenchmarkWarmupRuns,
		CooldownDelay:       baseCfg.BenchmarkCooldownDelay,
		ScenarioFilter:      baseCfg.BenchmarkScenarioFilter,
		ComplexityFilter:    baseCfg.BenchmarkComplexity,
		OutputFormat:        baseCfg.BenchmarkOutputFormat,
		OutputFile:          baseCfg.BenchmarkOutputFile,
		Verbose:               baseCfg.BenchmarkVerbose,
		EnableQualityChecks:   baseCfg.BenchmarkEnableQuality,
		EnableCostTracking:    baseCfg.BenchmarkEnableCost,
		EnableKubectlGeneration: false, // Default to false, can be overridden by CLI
	}

	// Override with CLI options
	if opts.iterations > 0 {
		cfg.Iterations = opts.iterations
	}
	if opts.timeout > 0 {
		cfg.Timeout = time.Duration(opts.timeout) * time.Second
	}
	if opts.parallel > 0 {
		cfg.Parallel = opts.parallel
	}
	if opts.warmup >= 0 {
		cfg.WarmupRuns = opts.warmup
	}
	if opts.cooldown > 0 {
		cfg.CooldownDelay = time.Duration(opts.cooldown) * time.Millisecond
	}
	if opts.complexity != "" {
		cfg.ComplexityFilter = opts.complexity
	}
	if opts.format != "" {
		cfg.OutputFormat = opts.format
	}
	if opts.output != "" {
		cfg.OutputFile = opts.output
	}
	if opts.verbose {
		cfg.Verbose = true
	}
	if opts.noQuality {
		cfg.EnableQualityChecks = false
	}
	if opts.noCost {
		cfg.EnableCostTracking = false
	}
	if opts.kubectlGeneration {
		cfg.EnableKubectlGeneration = true
	}

	// Parse scenario filters
	if opts.scenarios != "" {
		cfg.ScenarioFilter = strings.Split(opts.scenarios, ",")
	}

	// Parse models in provider/model format
	modelList := baseCfg.BenchmarkModels
	if opts.models != "" {
		modelList = strings.Split(opts.models, ",")
	}

	// Auto-detect providers from model names and group models by provider
	providerModelMap := make(map[string][]string)

	for _, modelStr := range modelList {
		modelStr = strings.TrimSpace(modelStr)
		if modelStr == "" {
			continue
		}

		// Parse provider/model format
		if strings.Contains(modelStr, "/") {
			parts := strings.SplitN(modelStr, "/", 2)
			provider := strings.TrimSpace(parts[0])
			model := strings.TrimSpace(parts[1])

			if provider != "" && model != "" {
				providerModelMap[provider] = append(providerModelMap[provider], model)
			}
		} else {
			// Legacy format: model without provider, try to auto-detect or use default
			provider := detectProviderFromModel(modelStr)
			providerModelMap[provider] = append(providerModelMap[provider], modelStr)
		}
	}

	// Override with explicit providers if specified
	if opts.providers != "" {
		explicitProviders := strings.Split(opts.providers, ",")
		// If providers are explicitly specified, use all models for all providers
		newProviderModelMap := make(map[string][]string)
		allModels := []string{}
		for _, models := range providerModelMap {
			allModels = append(allModels, models...)
		}
		for _, provider := range explicitProviders {
			provider = strings.TrimSpace(provider)
			if provider != "" {
				newProviderModelMap[provider] = allModels
			}
		}
		providerModelMap = newProviderModelMap
	}

	// Create provider configs from the map
	for provider, models := range providerModelMap {
		if len(models) == 0 {
			continue
		}

		providerConfig := benchmark.ProviderConfig{
			Name:   provider,
			Models: models,
		}

		// Set provider-specific base URLs for OpenRouter models
		if provider == "openai" && hasOpenRouterModels(models) {
			// Use OpenRouter's OpenAI-compatible endpoint
			providerConfig.BaseURL = "https://openrouter.ai/api/v1"
		}

		cfg.Providers = append(cfg.Providers, providerConfig)
	}

	// Validate configuration
	if cfg.Iterations <= 0 {
		cfg.Iterations = 3 // Default
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second // Default
	}
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = "table"
	}

	return cfg, nil
}

func hasOpenRouterModels(models []string) bool {
	for _, model := range models {
		if strings.Contains(model, "/") ||
			strings.Contains(model, "z-ai/") ||
			strings.Contains(model, "moonshotai/") ||
			strings.Contains(model, "deepseek/") ||
			strings.Contains(model, "alibaba/") {
			return true // Likely an OpenRouter model
		}
	}
	return false
}

func detectProviderFromModel(model string) string {
	modelLower := strings.ToLower(model)

	// Detect provider from model name patterns
	switch {
	case strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "o1"):
		return "openai"
	case strings.Contains(modelLower, "claude"):
		return "anthropic"
	case strings.Contains(modelLower, "gemini"):
		return "google"
	case strings.Contains(modelLower, "llama") || strings.Contains(modelLower, "mistral"):
		return "ollama"
	default:
		// Default to openai for unknown models (many are available via OpenAI-compatible APIs)
		return "openai"
	}
}

func loadScenarios(runner *benchmark.BenchmarkRunner, cfg *benchmark.BenchmarkConfig) error {
	var scenarios []benchmark.TestScenario

	// Get all available scenarios (including kubectl generation if enabled)
	allScenarios := benchmark.GetScenariosWithOptions(cfg.EnableKubectlGeneration)

	// Load scenarios based on filters
	if len(cfg.ScenarioFilter) > 0 {
		// Load specific scenarios from all available scenarios
		for _, scenarioName := range cfg.ScenarioFilter {
			var found *benchmark.TestScenario
			for _, scenario := range allScenarios {
				if scenario.Name == scenarioName {
					found = &scenario
					break
				}
			}
			if found == nil {
				return fmt.Errorf("scenario '%s' not found", scenarioName)
			}
			scenarios = append(scenarios, *found)
		}
	} else {
		// Load all available scenarios
		scenarios = allScenarios
	}

	// Apply complexity filter
	if cfg.ComplexityFilter != "" && cfg.ComplexityFilter != "all" {
		filtered := []benchmark.TestScenario{}
		for _, scenario := range scenarios {
			if string(scenario.Complexity) == cfg.ComplexityFilter {
				filtered = append(filtered, scenario)
			}
		}
		scenarios = filtered
	}

	if len(scenarios) == 0 {
		return fmt.Errorf("no scenarios match the specified filters")
	}

	runner.AddScenarios(scenarios)
	return nil
}

func showBenchmarkPlan(cfg *benchmark.BenchmarkConfig) {
	fmt.Printf("📋 Benchmark Plan:\n")
	fmt.Printf("  • Providers: %d\n", len(cfg.Providers))
	for _, provider := range cfg.Providers {
		fmt.Printf("    - %s: %s\n", provider.Name, strings.Join(provider.Models, ", "))
	}
	fmt.Printf("  • Iterations: %d per scenario\n", cfg.Iterations)
	fmt.Printf("  • Timeout: %v per request\n", cfg.Timeout)
	fmt.Printf("  • Complexity Filter: %s\n", cfg.ComplexityFilter)
	if cfg.EnableQualityChecks {
		fmt.Printf("  • Quality Evaluation: Enabled\n")
	}
	if cfg.EnableCostTracking {
		fmt.Printf("  • Cost Tracking: Enabled\n")
	}
	if cfg.EnableKubectlGeneration {
		fmt.Printf("  • Kubectl Command Generation: Enabled\n")
	}
	fmt.Printf("  • Output Format: %s\n", cfg.OutputFormat)
	if cfg.OutputFile != "" {
		fmt.Printf("  • Output File: %s\n", cfg.OutputFile)
	}
	fmt.Printf("\n")
}

func shouldConfirmExecution(cfg *benchmark.BenchmarkConfig) bool {
	// Require confirmation for long-running benchmarks
	totalRequests := len(cfg.Providers) * cfg.Iterations
	if totalRequests > 50 {
		return true
	}
	if cfg.Timeout > 60*time.Second {
		return true
	}
	return false
}

func confirmExecution() bool {
	fmt.Print("This benchmark may take a while. Continue? (y/N): ")
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(response) == "y" || strings.ToLower(response) == "yes"
}

func outputResults(results *benchmark.BenchmarkResults, cfg *benchmark.BenchmarkConfig) error {
	generator := benchmark.NewReportGenerator(results, cfg)

	// Calculate summary
	results.Summary = generator.CalculateSummary()

	// Generate report
	return generator.GenerateAndSave(cfg.OutputFormat, cfg.OutputFile)
}

func listAvailableScenarios() {
	// Check if kubectl generation flag is set
	includeKubectlGeneration := false
	for _, arg := range os.Args {
		if arg == "--kubectl-generation" {
			includeKubectlGeneration = true
			break
		}
	}
	
	scenarios := benchmark.GetScenariosWithOptions(includeKubectlGeneration)
	stats := benchmark.GetScenarioStats()

	fmt.Printf("📝 Available Test Scenarios (%d total):\n\n", len(scenarios))

	// Group by complexity
	complexities := []benchmark.ScenarioComplexity{
		benchmark.ComplexitySimple,
		benchmark.ComplexityMedium,
		benchmark.ComplexityComplex,
	}

	for _, complexity := range complexities {
		fmt.Printf("🔹 %s Scenarios:\n", strings.Title(string(complexity)))

		// Filter scenarios by complexity from our loaded scenarios
		for _, scenario := range scenarios {
			if scenario.Complexity == complexity {
				fmt.Printf("  • %-20s %s\n", scenario.Name, scenario.Description)
			}
		}
		fmt.Printf("\n")
	}

	fmt.Printf("📊 Statistics:\n")
	if complexityCounts, ok := stats["complexity_counts"].(map[benchmark.ScenarioComplexity]int); ok {
		for complexity, count := range complexityCounts {
			fmt.Printf("  • %s: %d scenarios\n", strings.Title(string(complexity)), count)
		}
	}
	if categoryCounts, ok := stats["category_counts"].(map[string]int); ok {
		fmt.Printf("  • Categories: %d\n", len(categoryCounts))
		for category, count := range categoryCounts {
			fmt.Printf("    - %s: %d\n", category, count)
		}
	}

	fmt.Printf("\nExample usage:\n")
	fmt.Printf("  --scenarios=list_pods,check_pod_status\n")
	fmt.Printf("  --complexity=simple\n")
	fmt.Printf("  --category=basic_operations\n")
}

func exportBenchmarkConfig(cfg *benchmark.BenchmarkConfig) {
	generator := benchmark.NewReportGenerator(&benchmark.BenchmarkResults{}, cfg)

	fmt.Printf("# Benchmark Configuration Export\n")
	fmt.Printf("# Generated: %s\n\n", time.Now().Format(time.RFC3339))

	if err := generator.ExportConfig(os.Stdout); err != nil {
		log.Fatalf("Failed to export configuration: %v", err)
	}

	fmt.Printf("\n# To use this configuration:\n")
	fmt.Printf("# 1. Save to a file (e.g., benchmark.env)\n")
	fmt.Printf("# 2. Source the file: source benchmark.env\n")
	fmt.Printf("# 3. Run: kubectl-quackops-benchmark\n")
}
