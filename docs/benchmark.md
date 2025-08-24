# QuackOps Benchmark Tool

Technical documentation for the kubectl-quackops benchmarking functionality.

## Overview

The QuackOps benchmark tool provides comprehensive performance testing and comparison capabilities for LLM providers when performing Kubernetes troubleshooting tasks. It evaluates response quality, performance metrics, and cost efficiency across different AI models.

## Architecture

The benchmark system consists of several key components:

- **BenchmarkRunner**: Orchestrates test execution across providers and scenarios
- **TestScenario**: Defines individual test cases with complexity levels and validation criteria
- **MetricsCollector**: Gathers performance, quality, and cost metrics
- **ReportGenerator**: Produces formatted results in multiple output formats

## Quick Start

### Building
```bash
make build-benchmark
```

### Basic Usage
```bash
# Compare models across providers
./kubectl-quackops-benchmark --models=openai/gpt-4o-mini,google/gemini-2.5-flash --iterations=5

# Run specific scenarios only
./kubectl-quackops-benchmark --models=openai/z-ai/glm-4.5-air:free --scenarios=list_pods,check_pod_status --iterations=3

# Generate detailed report
./kubectl-quackops-benchmark --models=openai/moonshotai/kimi-k2:free --complexity=simple --format=markdown --output=results.md
```

## Test Scenarios

### Complexity Levels
- **Simple**: Basic operations (list pods, get services)
- **Medium**: Intermediate diagnostics (analyze failing pods, check resource usage)
- **Complex**: Advanced troubleshooting (multi-resource analysis, security audits)

### Categories
- `basic_operations`: Fundamental kubectl commands
- `troubleshooting`: Diagnostic and problem-solving scenarios
- `security`: Security analysis and RBAC checks
- `performance`: Resource utilization and optimization

## Configuration

### CLI Options
```bash
# Model selection
--models=provider/model,provider/model2    # Comma-separated model list
--providers=openai,google,anthropic        # Override providers

# Execution control
--iterations=5                             # Runs per scenario
--timeout=60                               # Request timeout (seconds)
--parallel=3                              # Parallel executions
--warmup=2                                # Warmup runs (not counted)

# Filtering
--scenarios=scenario1,scenario2           # Specific scenarios
--complexity=simple|medium|complex|all    # Complexity filter
--category=basic_operations               # Category filter

# Output
--format=table|json|csv|markdown          # Output format
--output=results.json                     # Output file
--quiet                                   # Suppress progress
```

### Environment Variables
All benchmark settings can be configured via environment variables with `QU_BENCHMARK_*` prefix:

```bash
export QU_BENCHMARK_ITERATIONS=5
export QU_BENCHMARK_TIMEOUT=60s
export QU_BENCHMARK_OUTPUT_FORMAT=json
```

## Metrics Collection

### Performance Metrics
- **Response Time**: Total request duration
- **Tokens Per Second**: Processing speed
- **Success Rate**: Completion percentage

### Quality Metrics
- **Command Accuracy**: Validates generated kubectl commands
- **Content Relevance**: Checks for expected keywords/phrases
- **Error Rate**: Failed or invalid responses

### Cost Tracking
- **Token Usage**: Input and output token consumption
- **Estimated Cost**: Provider-specific pricing calculations
- **Cost Per Query**: Average cost per successful request

## Output Formats

### Table (Default)
Human-readable tabular output for terminal viewing

### JSON
Structured data suitable for programmatic analysis:
```json
{
  "summary": {
    "total_scenarios": 10,
    "total_requests": 50,
    "avg_response_time": 2.34,
    "overall_success_rate": 0.96
  },
  "provider_results": [...]
}
```

### CSV
Comma-separated format for spreadsheet analysis

### Markdown
Formatted reports with tables and summaries for documentation

## Advanced Features

### Model Context Protocol Testing
Enable kubectl command generation testing with MCP integration:
```bash
./kubectl-quackops-benchmark --kubectl-generation --models=openai/gpt-4o --scenarios=kubectl_generation
```

### Quality Evaluation
Automated validation of generated kubectl commands:
```bash
./kubectl-quackops-benchmark --models=openai/gpt-4o-mini --no-quality=false
```

### Cost Analysis
Track token usage and estimated costs:
```bash
./kubectl-quackops-benchmark --models=openai/gpt-4o-mini --no-cost=false
```

## Integration

### CI/CD Pipeline Example
```yaml
- name: Run LLM Benchmark
  run: |
    make build-benchmark
    ./kubectl-quackops-benchmark \
      --models=openai/gpt-4o-mini,google/gemini-2.5-flash \
      --iterations=3 \
      --format=json \
      --output=benchmark-results.json
```

### Custom Scenarios
Scenarios can be extended by modifying `pkg/benchmark/scenarios.go`:

```go
TestScenario{
    Name:        "custom_scenario",
    Description: "Custom diagnostic scenario",
    Complexity:  ComplexityMedium,
    Category:    "troubleshooting",
    Prompt:      "Analyze pod restart patterns",
    ExpectedCommands: []string{"kubectl get pods", "kubectl describe"},
    Tags:        []string{"pods", "diagnostics"},
}
```

## Performance Considerations

- **Parallel Execution**: Use `--parallel` to speed up large benchmarks
- **Warmup Runs**: Enable `--warmup` to account for cold start effects  
- **Rate Limiting**: Provider throttling automatically handled via `pkg/llm/throttle.go`
- **Token Limits**: Scenarios designed to fit within model context windows

## Troubleshooting

### Common Issues
- **API Key Missing**: Ensure provider API keys are set in environment
- **Rate Limiting**: Reduce `--parallel` or increase `--cooldown`
- **Timeout Errors**: Increase `--timeout` for complex scenarios

### Debug Mode
```bash
DEBUG=1 ./kubectl-quackops-benchmark --models=openai/gpt-4o-mini --scenarios=list_pods --iterations=1
```

## See Also

- [Main Documentation](../README.md)
- [Configuration Reference](../README.md#️-configuration-options)
- [LLM Provider Setup](../README.md#-supported-llms)