package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mikhae1/kubectl-quackops/pkg/llm/metadata"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/mikhae1/kubectl-quackops/pkg/style"
	"github.com/tmc/langchaingo/llms"
)

// CmdRes represents the result of executing a command
type CmdRes struct {
	Cmd string
	Out string
	Err error
}

type KubectlPrompt = struct {
	MatchRe         *regexp.Regexp
	Prompt          string
	AllowedKubectls []string
	BlockedKubectls []string
	UseDefaultCmds  bool
}

// SlashCommand represents a slash command with its variations and description
type SlashCommand struct {
	Commands    []string // All variations of the command
	Primary     string   // Primary command to show
	Description string   // Command description
}

// SessionEvent represents a single interaction in the session history
type SessionEvent struct {
	Timestamp  time.Time
	UserPrompt string
	ToolCalls  []ToolCallData
	AIResponse string
}

// ToolCallData represents a recorded tool call
type ToolCallData struct {
	Name   string
	Args   map[string]any
	Result string
}

type Config struct {
	ChatMessages   []llms.ChatMessage
	SessionHistory []SessionEvent

	AllowedKubectlCmds []string
	BlockedKubectlCmds []string

	DuckASCIIArt       string
	Provider           string
	Model              string
	OllamaApiURL       string
	AzOpenAIAPIVersion string
	Retries            int
	Timeout            int
	// Token window configuration
	DefaultMaxTokens      int // Provider/model default or auto-detected context window
	UserMaxTokens         int // Explicit user override via env/flag; >0 disables auto-detect
	SpinnerTimeout        int
	SafeMode              bool
	Verbose               bool
	DisableSecretFilter   bool
	DisableMarkdownFormat bool
	DisableAnimation      bool
	MaxCompletions        int
	HistoryFile           string
	DisableHistory        bool
	KubectlBinaryPath     string

	// SuppressContentPrint prevents Chat from printing model message bodies
	SuppressContentPrint bool

	// Test-friendly switch to skip sleeps/backoffs/throttle waits
	SkipWaits bool

	// Embedding model configuration
	EmbeddingModel        string
	OllamaEmbeddingModels string

	// Prompt templates for various parts of the application
	KubectlStartPrompt       string
	KubectlShortPrompt       string
	KubectlFormatPrompt      string
	DiagnosticAnalysisPrompt string
	MarkdownFormatPrompt     string
	PlainFormatPrompt        string

	// Kubectl command suggestion controls
	KubectlMaxSuggestions int  // Maximum number of kubectl commands the LLM should suggest
	KubectlReturnJSON     bool // Prefer JSON array output for command suggestions

	KubectlPrompts       []KubectlPrompt
	StoredUserCmdResults []CmdRes
	SlashCommands        []SlashCommand

	// Token accounting for last LLM exchange (shown in prompt)
	LastOutgoingTokens int
	LastIncomingTokens int

	// EditMode indicates the persistent shell edit mode toggled by '!'
	EditMode      bool
	CommandPrefix string

	// SelectedPrompt tracks the currently selected MCP prompt for UI highlighting
	// Format: prompt name without leading slash (e.g., "code-mode")
	SelectedPrompt string

	// MCPPromptServer tracks the server of the currently selected MCP prompt
	// Used to filter tools when a prompt is active
	MCPPromptServer string

	// Diagnostics toggles and knobs
	EnableBaseline          bool
	BaselineLevel           string // "minimal", "standard", "comprehensive"
	BaselineIncludeMetrics  bool   // Include pod/node metrics if available
	BaselineNamespaceFilter string // Comma-separated namespaces (empty = all)
	EnablePriorityScoring   bool   // Add priority field to findings
	MaxFindingsPerCategory  int    // Limit findings per category (0 = unlimited)
	EventsWindowMinutes     int
	EventsWarningsOnly      bool
	LogsTail                int
	LogsAllContainers       bool

	// MCP client mode
	MCPClientEnabled bool
	MCPConfigPath    string
	MCPToolTimeout   int
	MCPStrict        bool
	// Maximum number of iterative MCP tool calls per LLM response
	MCPMaxToolCalls int

	// MCP logging for debugging (raw server stdio)
	MCPLogEnabled bool
	MCPLogFile    string
	MCPLogFormat  string

	// Tool policy
	AllowedTools []string
	DeniedTools  []string

	// Presentation
	ToolOutputMaxLines       int
	ToolOutputMaxLineLen     int
	DiagnosticResultMaxLines int

	// LLM Request Throttling
	ThrottleRequestsPerMinute int
	ThrottleDelayOverride     time.Duration

	// Token Management
	InputTokenReservePercent int // Percentage of max tokens to reserve for input (default 20%)
	MinInputTokenReserve     int // Minimum tokens to reserve for input (default 1024)
	MinOutputTokens          int // Minimum output tokens to ensure (default 512)

	// Auto-detection settings
	AutoDetectMaxTokens   bool          // Enable auto-detection of max tokens from model metadata
	ModelMetadataCacheTTL time.Duration // Cache TTL for model metadata (default 1 hour)
	ModelMetadataTimeout  time.Duration // HTTP timeout for model metadata requests (default 10 seconds)

	// Benchmark configuration
	BenchmarkEnabled        bool          // Enable benchmark mode
	BenchmarkProviders      []string      // List of providers to benchmark (comma-separated string parsed)
	BenchmarkModels         []string      // List of models to benchmark (comma-separated string parsed)
	BenchmarkIterations     int           // Number of iterations per scenario
	BenchmarkTimeout        time.Duration // Timeout for benchmark requests
	BenchmarkParallel       int           // Number of parallel benchmark executions
	BenchmarkWarmupRuns     int           // Number of warmup runs before measurement
	BenchmarkCooldownDelay  time.Duration // Delay between benchmark runs
	BenchmarkScenarioFilter []string      // Filter to run only specific scenarios
	BenchmarkComplexity     string        // Filter by complexity: simple, medium, complex, all
	BenchmarkOutputFormat   string        // Output format: json, csv, markdown, table
	BenchmarkOutputFile     string        // File to write benchmark results
	BenchmarkVerbose        bool          // Verbose benchmark logging
	BenchmarkEnableQuality  bool          // Enable quality evaluation
	BenchmarkEnableCost     bool          // Enable cost tracking
}

// UIColorRoles defines terminal color roles and gradient palette for consistent UI styling.
// These are constants (no env overrides) to keep branding and readability consistent.
type UIColorRoles struct {
	// Role colors
	Primary  lipgloss.Style
	Accent   lipgloss.Style
	Info     lipgloss.Style
	Dim      lipgloss.Style
	Shadow   lipgloss.Style
	Ok       lipgloss.Style
	Warn     lipgloss.Style
	Error    lipgloss.Style
	Provider lipgloss.Style
	Model    lipgloss.Style
	Command  lipgloss.Style
	Light    lipgloss.Style

	// MCP/header specific
	Header lipgloss.Style
	Border lipgloss.Style
	Label  lipgloss.Style
	Output lipgloss.Style

	// Gradient palette used for banners and left-border accents
	Gradient []lipgloss.Style
}

// Colors is the globally shared UI palette.
var Colors = initUIColorRoles()

func initUIColorRoles() *UIColorRoles {
	return &UIColorRoles{
		Primary: lipgloss.NewStyle().Foreground(style.ColorWhite),
		Accent:  style.Title,
		Info:    style.Info,
		Dim:     style.Debug,
		Shadow:  style.Debug.Faint(true),
		Light:   style.Success,

		Ok:    style.Success,
		Warn:  style.Warning,
		Error: style.Error,

		Provider: lipgloss.NewStyle().Foreground(style.ColorPurple),
		Model:    lipgloss.NewStyle().Foreground(style.ColorPink),
		Command:  style.Command,

		Header: style.Title,
		Border: style.Debug,
		Label:  style.SubTitle,
		Output: style.Info,

		Gradient: []lipgloss.Style{
			lipgloss.NewStyle().Foreground(style.ColorCyan),
			lipgloss.NewStyle().Foreground(style.ColorBlue), // Assuming ColorBlue exists or map to Cyan/Purple
		},
	}
}

// envFirst returns the first non-empty value among provided environment variable keys
func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// ProviderDefaults groups default values per provider
type ProviderDefaults struct {
	DefaultMaxTokens      int
	DefaultModel          string
	DefaultEmbeddingModel string
}

var providerDefaults = map[string]ProviderDefaults{
	"google": {
		DefaultMaxTokens:      128000,
		DefaultModel:          "gemini-2.5-flash-preview-04-17",
		DefaultEmbeddingModel: "models/text-embedding-004",
	},
	"ollama": {
		DefaultMaxTokens:      4096,
		DefaultModel:          "llama3.1",
		DefaultEmbeddingModel: "models/text-embedding-large-exp",
	},
	"openai": {
		DefaultMaxTokens:      128000,
		DefaultModel:          "gpt-5-mini",
		DefaultEmbeddingModel: "text-embedding-3-small",
	},
	"azopenai": {
		DefaultMaxTokens:      128000,
		DefaultModel:          "gpt-4o-mini",
		DefaultEmbeddingModel: "text-embedding-3-small",
	},
	"anthropic": {
		DefaultMaxTokens:      200000,
		DefaultModel:          "claude-3-7-sonnet-latest",
		DefaultEmbeddingModel: "nomic-embed-text",
	},
}

var defaultProviderFallback = ProviderDefaults{
	DefaultMaxTokens:      16000,
	DefaultModel:          "llama3.1",
	DefaultEmbeddingModel: "models/text-embedding-large-exp",
}

// GetOpenAIBaseURL returns the OpenAI base URL from environment variables
// Supports both QU_OPENAI_BASE_URL and OPENAI_BASE_URL (as alias)
func GetOpenAIBaseURL() string {
	return envFirst("QU_OPENAI_BASE_URL", "OPENAI_BASE_URL")
}

// GetAzOpenAIBaseURL returns the Azure OpenAI base URL from environment variables
// Supports both QU_AZ_OPENAI_BASE_URL and OPENAI_BASE_URL (as alias)
func GetAzOpenAIBaseURL() string {
	return envFirst("QU_AZ_OPENAI_BASE_URL", "OPENAI_BASE_URL")
}

// GetAzOpenAIAPIKey returns the Azure OpenAI API key from environment variables
// Supports both QU_AZ_OPENAI_API_KEY and OPENAI_API_KEY (as alias)
func GetAzOpenAIAPIKey() string {
	return envFirst("QU_AZ_OPENAI_API_KEY", "OPENAI_API_KEY")
}

// LoadConfig initializes the application configuration
func LoadConfig() *Config {
	// Load configuration from config file first
	configFileValues = loadConfigFile()

	provider := getEnvArg("QU_LLM_PROVIDER", "ollama").(string)

	// Centralized provider defaults
	pd, ok := providerDefaults[provider]
	if !ok {
		pd = defaultProviderFallback
	}

	defaultMaxTokens := pd.DefaultMaxTokens
	defaultModel := pd.DefaultModel
	defaultEmbeddingModel := pd.DefaultEmbeddingModel

	// Get home directory for history file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultHistoryFile := ""
	if homeDir != "" {
		defaultHistoryFile = fmt.Sprintf("%s/.quackops/history", homeDir)
	}

	config := &Config{
		ChatMessages:          []llms.ChatMessage{},
		DuckASCIIArt:          defaultDuckASCIIArt,
		Provider:              provider,
		Model:                 getEnvArg("QU_LLM_MODEL", defaultModel).(string),
		OllamaApiURL:          getEnvArg("QU_OLLAMA_BASE_URL", "http://localhost:11434").(string),
		AzOpenAIAPIVersion:    getEnvArg("QU_AZ_OPENAI_API_VERSION", "2025-05-01").(string),
		SafeMode:              getEnvArg("QU_SAFE_MODE", false).(bool),
		Retries:               getEnvArg("QU_RETRIES", 3).(int),
		Timeout:               getEnvArg("QU_TIMEOUT", 30).(int),
		DefaultMaxTokens:      defaultMaxTokens,
		UserMaxTokens:         getEnvArg("QU_MAX_TOKENS", 0).(int),
		AllowedKubectlCmds:    getEnvArg("QU_ALLOWED_KUBECTL_CMDS", defaultAllowedKubectlCmds).([]string),
		BlockedKubectlCmds:    getEnvArg("QU_BLOCKED_KUBECTL_CMDS", defaultBlockedKubectlCmds).([]string),
		DisableMarkdownFormat: getEnvArg("QU_DISABLE_MARKDOWN_FORMAT", false).(bool),
		DisableAnimation:      getEnvArg("QU_DISABLE_ANIMATION", false).(bool),
		MaxCompletions:        getEnvArg("QU_MAX_COMPLETIONS", 50).(int),
		HistoryFile:           getEnvArg("QU_HISTORY_FILE", defaultHistoryFile).(string),
		DisableHistory:        getEnvArg("QU_DISABLE_HISTORY", false).(bool),
		KubectlBinaryPath:     getEnvArg("QU_KUBECTL_BINARY", "kubectl").(string),
		SuppressContentPrint:  false,
		SkipWaits:             getEnvArg("QU_SKIP_WAITS", false).(bool),
		SpinnerTimeout:        300,
		CommandPrefix:         getEnvArg("QU_COMMAND_PREFIX", "!").(string),
		StoredUserCmdResults:  []CmdRes{},
		// Diagnostics toggles
		EnableBaseline:           getEnvArg("QU_ENABLE_BASELINE", true).(bool),
		BaselineLevel:            getEnvArg("QU_BASELINE_LEVEL", "minimal").(string),
		BaselineIncludeMetrics:   getEnvArg("QU_BASELINE_INCLUDE_METRICS", true).(bool),
		BaselineNamespaceFilter:  getEnvArg("QU_BASELINE_NAMESPACE_FILTER", "").(string),
		EnablePriorityScoring:    getEnvArg("QU_ENABLE_PRIORITY_SCORING", true).(bool),
		MaxFindingsPerCategory:   getEnvArg("QU_MAX_FINDINGS_PER_CATEGORY", 0).(int),
		EventsWindowMinutes:      getEnvArg("QU_EVENTS_WINDOW_MINUTES", 60).(int),
		EventsWarningsOnly:       getEnvArg("QU_EVENTS_WARN_ONLY", true).(bool),
		LogsTail:                 getEnvArg("QU_LOGS_TAIL", 200).(int),
		LogsAllContainers:        getEnvArg("QU_LOGS_ALL_CONTAINERS", false).(bool),
		ToolOutputMaxLines:       getEnvArg("QU_TOOL_OUTPUT_MAX_LINES", 40).(int),
		ToolOutputMaxLineLen:     getEnvArg("QU_TOOL_OUTPUT_MAX_LINE_LEN", 140).(int),
		DiagnosticResultMaxLines: getEnvArg("QU_DIAGNOSTIC_RESULT_MAX_LINES", 10).(int),

		// MCP client mode
		MCPClientEnabled: getEnvArg("QU_MCP_CLIENT", true).(bool),
		MCPConfigPath: func() string {
			if homeDir == "" {
				return ""
			}
			defaultPaths := []string{
				filepath.Join(homeDir, ".config", "quackops", "mcp.yaml"),
				filepath.Join(homeDir, ".quackops", "mcp.json"),
			}
			return strings.Join(defaultPaths, ",")
		}(),
		MCPToolTimeout:  getEnvArg("QU_MCP_TOOL_TIMEOUT", 30).(int),
		MCPStrict:       getEnvArg("QU_MCP_STRICT", false).(bool),
		MCPMaxToolCalls: getEnvArg("QU_MCP_MAX_TOOL_CALLS", 10).(int),
		MCPLogEnabled:   getEnvArg("QU_MCP_LOG", false).(bool),
		MCPLogFile: func() string {
			if homeDir != "" {
				return fmt.Sprintf("%s/.quackops/mcp.log", homeDir)
			}
			return "mcp.log"
		}(),
		MCPLogFormat: getEnvArg("QU_MCP_LOG_FORMAT", "jsonl").(string),

		// Embedding model configuration
		EmbeddingModel:        getEnvArg("QU_EMBEDDING_MODEL", defaultEmbeddingModel).(string),
		OllamaEmbeddingModels: getEnvArg("QU_OLLAMA_EMBEDDING_MODELS", "nomic-embed-text,mxbai-embed-large,all-minilm-l6-v2").(string),

		// Tool policy
		AllowedTools: getEnvArg("QU_ALLOWED_TOOLS", defaultAllowedTools).([]string),
		DeniedTools:  getEnvArg("QU_DENIED_TOOLS", defaultDeniedTools).([]string),

		// LLM Request Throttling
		ThrottleRequestsPerMinute: getEnvArg("QU_THROTTLE_REQUESTS_PER_MINUTE", 60).(int),
		ThrottleDelayOverride:     time.Duration(getEnvArg("QU_THROTTLE_DELAY_OVERRIDE_MS", 0).(int)) * time.Millisecond,

		// Token Management
		InputTokenReservePercent: getEnvArg("QU_INPUT_TOKEN_RESERVE_PERCENT", 20).(int),
		MinInputTokenReserve:     getEnvArg("QU_MIN_INPUT_TOKEN_RESERVE", 1024).(int),
		MinOutputTokens:          getEnvArg("QU_MIN_OUTPUT_TOKENS", 512).(int),

		// Auto-detection settings
		AutoDetectMaxTokens:   getEnvArg("QU_AUTO_DETECT_MAX_TOKENS_ENABLE", true).(bool),
		ModelMetadataCacheTTL: time.Duration(getEnvArg("QU_MODEL_METADATA_CACHE_TTL", 3600).(int)) * time.Second, // 1 hour default
		ModelMetadataTimeout:  time.Duration(getEnvArg("QU_MODEL_METADATA_TIMEOUT", 10).(int)) * time.Second,

		// Benchmark configuration
		BenchmarkEnabled:        getEnvArg("QU_BENCHMARK_ENABLED", false).(bool),
		BenchmarkProviders:      getEnvArg("QU_BENCHMARK_PROVIDERS", []string{"openai", "azopenai", "anthropic", "google", "ollama"}).([]string),
		BenchmarkModels:         getEnvArg("QU_BENCHMARK_MODELS", []string{"gpt-4o-mini", "claude-3-haiku", "gemini-2.5-flash", "llama3.1"}).([]string),
		BenchmarkIterations:     getEnvArg("QU_BENCHMARK_ITERATIONS", 3).(int),
		BenchmarkTimeout:        time.Duration(getEnvArg("QU_BENCHMARK_TIMEOUT", 120).(int)) * time.Second,
		BenchmarkParallel:       getEnvArg("QU_BENCHMARK_PARALLEL", 1).(int),
		BenchmarkWarmupRuns:     getEnvArg("QU_BENCHMARK_WARMUP_RUNS", 1).(int),
		BenchmarkCooldownDelay:  time.Duration(getEnvArg("QU_BENCHMARK_COOLDOWN_DELAY", 1000).(int)) * time.Millisecond,
		BenchmarkScenarioFilter: getEnvArg("QU_BENCHMARK_SCENARIO_FILTER", []string{}).([]string),
		BenchmarkComplexity:     getEnvArg("QU_BENCHMARK_COMPLEXITY", "all").(string),
		BenchmarkOutputFormat:   getEnvArg("QU_BENCHMARK_OUTPUT_FORMAT", "table").(string),
		BenchmarkOutputFile:     getEnvArg("QU_BENCHMARK_OUTPUT_FILE", "").(string),
		BenchmarkVerbose:        getEnvArg("QU_BENCHMARK_VERBOSE", false).(bool),
		BenchmarkEnableQuality:  getEnvArg("QU_BENCHMARK_ENABLE_QUALITY", true).(bool),
		BenchmarkEnableCost:     getEnvArg("QU_BENCHMARK_ENABLE_COST", true).(bool),

		// Prompt templates
		KubectlStartPrompt:       getEnvArg("QU_KUBECTL_SYSTEM_PROMPT", defaultKubectlStartPrompt).(string),
		KubectlShortPrompt:       getEnvArg("QU_KUBECTL_SHORT_PROMPT", "As a Kubernetes expert, based on your previous response, provide only the essential and safe read-only kubectl commands to help diagnose the following issue").(string),
		KubectlFormatPrompt:      getEnvArg("QU_KUBECTL_FORMAT_PROMPT", defaultKubectlFormatPrompt).(string),
		DiagnosticAnalysisPrompt: getEnvArg("QU_DIAGNOSTIC_ANALYSIS_PROMPT", defaultDiagnosticAnalysisPrompt).(string),
		MarkdownFormatPrompt:     getEnvArg("QU_MARKDOWN_FORMAT_PROMPT", "Format your response using Markdown, including headings, lists, and code blocks for improved readability in a terminal environment.").(string),
		PlainFormatPrompt:        getEnvArg("QU_PLAIN_FORMAT_PROMPT", "Provide a clear, concise analysis that is easy to read in a terminal environment.").(string),

		// Kubectl suggestions
		KubectlMaxSuggestions: getEnvArg("QU_KUBECTL_MAX_SUGGESTIONS", 12).(int),
		KubectlReturnJSON:     getEnvArg("QU_KUBECTL_RETURN_JSON", true).(bool),

		SlashCommands: defaultSlashCommands(),

		KubectlPrompts: []KubectlPrompt{
			{
				MatchRe:        regexp.MustCompile(`\b(error|fail|crash|exception|debug|warn|issue|problem|trouble|fault|bug)s?\b`),
				Prompt:         " Focus on diagnostics, particularly for error logs and status checks.",
				UseDefaultCmds: true,
			},
			{
				MatchRe: regexp.MustCompile(`\b(performance|perf|slow|cpu|memory|latency|throughput|bandwidth|speed|load)s?\b`),
				Prompt:  " Include commands to assess resource usage and performance metrics. Use 'kubectl get --raw /apis/metrics.k8s.io/v1beta1/nodes' to get real instance name and type.",
				AllowedKubectls: []string{
					"top pod",
					"top node",
				},
				UseDefaultCmds: false,
			},
			{
				MatchRe: regexp.MustCompile(`\b(log|logging|trace|tracing|audit|auditing|event|history|record)s?\b`),
				Prompt:  " Include commands to view logs and audit events.",
				AllowedKubectls: []string{
					"logs -l",
					"logs --all-containers=true",
					"logs daemonset/",
					"logs job/",
					"logs cronjob/",
					"get pods -o name | while read pod; do echo \"Logs from $pod:\"; kubectl logs $pod --tail=10; done",
				},
				UseDefaultCmds: false,
			},
			{
				MatchRe: regexp.MustCompile(`\b(deployment|replica|scale|scaling|rolling|restart|recreate|rollback)s?\b`),
				Prompt:  " Include commands to analyze deployments and replicas.",
				AllowedKubectls: []string{
					"get deployment",
					"describe deployment",
					"get pods -l",
					"get pods -o wide",
					"get deployments --all-namespaces -o wide",
					"get replicasets -A",
					"get daemonsets -A",
					"get statefulsets -A",
					"rollout status deployment",
				},
				UseDefaultCmds: false,
			},
			// gateways, http routes
			{
				MatchRe: regexp.MustCompile(`\b(gateway|route|httproute)s?\b`),
				Prompt:  "Include commands to analyze Kubernetes gateways and routes.",
				AllowedKubectls: []string{
					"get gateway -A",
					"get gatewayclasses -A",
					"get httproute -A",
					"describe gateway",
				},
				UseDefaultCmds: true,
			},
			// ingress
			{
				MatchRe: regexp.MustCompile(`\b(ingress|ingressclass|ingressroute)s?\b`),
				Prompt:  "Include commands to analyze Ingress resources.",
				AllowedKubectls: []string{
					"get ingress",
					"get ingress -A",
					"get ingressclass -A",
					"describe ingress",
					"get service",
				},
				UseDefaultCmds: false,
			},
			// hpas
			{
				MatchRe: regexp.MustCompile(`\b(hpa|horizontal pod autoscaler)s?\b`),
				Prompt:  "Include commands to analyze Horizontal Pod Autoscalers.",
				AllowedKubectls: []string{
					"get hpa -A",
					"describe hpa",
				},
				UseDefaultCmds: true,
			},
			// services
			{
				MatchRe: regexp.MustCompile(`\b(service|svc)s?\b`),
				Prompt:  "Include commands to analyze services.",
				AllowedKubectls: []string{
					"get service",
					"get service -o wide -A",
					"describe service",
				},
				UseDefaultCmds: true,
			},
			// storage
			{
				MatchRe: regexp.MustCompile(`\b(pc|pvc|storage|volume|persistent|claim|disk|space)s?\b`),
				Prompt:  "Include commands to analyze storage and volumes.",
				AllowedKubectls: []string{
					"get pv",
					"get pvc",
					"get pv -A",
					"get pvc -A",
					"describe pv",
					"describe pvc",
				},
				UseDefaultCmds: false,
			},
			// network
			{
				MatchRe: regexp.MustCompile(`\b(network|subnet|cidr|ip|firewall|security|policy|ingress|egress|route|loadbalancer|lb|service|svc|endpoint|dns|domain|hostname|port|protocol|tcp|udp|icmp|http|https|tls|ssl|certificate|cert|ca|crl|ocsp|revocation|trust|key|encryption|decryption|authentication)s?\b`),
				Prompt:  "Include commands to analyze network resources and connectivity.",
				AllowedKubectls: []string{
					"get networkpolicy",
					"get networkpolicy -A -o wide",
					"describe networkpolicy",
					"get endpoints -A",
					"get endpoints -A -o wide",
					"describe endpoints",
					"describe endpoints -A",
					"get service -A -o wide",
				},
				UseDefaultCmds: false,
			},
			// roles
			{
				MatchRe: regexp.MustCompile(`\b(rbac|role|clusterrole|rolebinding|clusterrolebinding|permission|access|authorization|auth)s?\b`),
				Prompt:  "Include commands to analyze roles and permissions.",
				AllowedKubectls: []string{
					"auth can-i",
					"auth can-i -A",
					"get role",
					"get role -A",
					"get clusterrole",
					"get clusterrole -A",
					"get rolebinding",
					"get rolebinding -A",
					"get clusterrolebinding",
					"get clusterrolebinding -A",
					"describe role",
					"describe clusterrole",
					"describe rolebinding",
					"describe clusterrolebinding",
				},
				UseDefaultCmds: false,
			},
		},
	}

	// Auto-enable SkipWaits under `go test` without requiring env/flags
	if !config.SkipWaits && isRunningUnderGoTest() {
		config.SkipWaits = true
	}

	return config
}

// isRunningUnderGoTest heuristically detects if the program is executing under `go test`.
func isRunningUnderGoTest() bool {
	// Look for typical -test.* flags in args
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	// Check test binary naming convention
	exe := filepath.Base(os.Args[0])
	if strings.HasSuffix(exe, ".test") {
		return true
	}
	return false
}

// configFileValues holds key-value pairs loaded from config files
var configFileValues map[string]string

// loadConfigFile loads environment variables from config files in order of preference:
// 1. ~/.quackops/config
// 2. ~/.config/quackops/config
// Returns a map of config values that can be used as lowest priority fallback.
func loadConfigFile() map[string]string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil // Can't determine home directory
	}

	// Try config files in order of preference
	configPaths := []string{
		filepath.Join(homeDir, ".quackops", "config"),
		filepath.Join(homeDir, ".config", "quackops", "config"),
	}

	for _, configPath := range configPaths {
		if values := loadConfigFromFile(configPath); values != nil {
			return values // Successfully loaded from this file
		}
	}

	return nil
}

// loadConfigFromFile loads environment variables from a specific config file
// Returns map of key-value pairs if successful, nil otherwise
func loadConfigFromFile(configPath string) map[string]string {
	file, err := os.Open(configPath)
	if err != nil {
		return nil // File doesn't exist or can't be opened
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue // Skip malformed lines
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}

		values[key] = value
	}

	if scanner.Err() != nil {
		return nil
	}

	return values
}

// GetEnv returns the value of the environment variable with the given key.
func getEnvArg(key string, fallback interface{}) interface{} {
	getValue := func(value string) interface{} {
		switch fallback.(type) {
		case string:
			return value
		case int:
			intVal, err := strconv.Atoi(value)
			if err != nil {
				fmt.Printf("Error: Value '%s' must be an integer, but got '%s'\n", key, value)
				os.Exit(1)
			}
			return intVal
		case bool:
			// Accept true/false/1/0
			boolVal, err := strconv.ParseBool(value)
			if err != nil {
				fmt.Printf("Error: Value '%s' must be a boolean, but got '%s'\n", key, value)
				os.Exit(1)
			}
			return boolVal
		case float64:
			floatVal, err := strconv.ParseFloat(value, 64)
			if err != nil {
				fmt.Printf("Error: Value '%s' must be a float, but got '%s'\n", key, value)
				os.Exit(1)
			}
			return floatVal
		case []string:
			return strings.Split(value, ",")
		default:
			fmt.Printf("Error: Unsupported type for fallback value of '%s'\n", key)
			os.Exit(1)
		}
		return nil
	}

	// Priority order: 1. CLI args, 2. Environment variables, 3. Config file, 4. Default fallback

	// Check CLI arguments first (highest priority)
	for _, arg := range os.Args[1:] {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := parts[0]
		v := parts[1]

		if k == key {
			return getValue(v)
		}
	}

	// Check environment variables second
	if val, exists := os.LookupEnv(key); exists {
		return getValue(val)
	}

	// Check config file values third
	if configFileValues != nil {
		if val, exists := configFileValues[key]; exists {
			return getValue(val)
		}
	}

	// Use default fallback (lowest priority)
	return fallback
}

var defaultAllowedKubectlCmds = []string{
	"get", "get -A",
	"describe",
	"logs", "logs --tail 10",
	"top",
	"explain",
	"cluster-info",
	"api-resources",
	"events",
	"auth can-i",
	"api-versions",
	"rollout status deployment",
	"--all-namespaces",
}

var defaultBlockedKubectlCmds = []string{
	"delete",
	"apply",
	"edit",
	"patch",
	"create",
	"replace",
	"set",
	"scale",
	"autoscale",
	"expose",
	"annotate",
	"label",
	"convert",
	"exec",
	"port-forward",
	"proxy",
	"run",
	"wait",
	"cordon",
	"uncordon",
	"drain",
	"attach",
	"config",
	"cp",
	"rm",
	"mv",
}

var defaultAllowedTools = []string{"*"}

var defaultDeniedTools = []string{}

var defaultDuckASCIIArt = `4qCA4qCA4qCA4qKA4qOE4qKA4qO04qC/4qCf4qCb4qCb4qCb4qCb4qCb4qC34qK24qOE4qGA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCACuKggOKggOKggOKggOKggOKiv+Khv+Kgg+KggOKggOKggOKggOKggOKggOKggOKggOKhgOKiqeKjt+KhhOKggOKggOKggOKggOKggOKggOKggArioIDioIDioIDiorDiopbiob/ioIHioIDioIDioIDioIDiooLio6bio4TioIDioIBv4qGH4qK44qO34qCA4qCA4qCA4qCA4qCA4qCA4qCACuKggOKggOKggOKggOKjvOKgg+KggOKggOKggOKggOKggOKggG/ioZvioIDioIDioJDioJDioJLiorvioYbioIDioIDioIDioIDioIDioIAK4qCA4qCA4qCA4qCA4qO/4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qKA4qO04qG+4qC/4qC/4qK/4qG/4qK34qOk4qOA4qGA4qCA4qCA4qCACuKggOKggOKggOKggOKjv+KhgOKggOKggOKggOKggOKggOKggOKisOKjv+KgieKggOKguuKgh+KgmOKgg+KggOKgieKgmeKgm+Kit+KjhOKggArioIDioIDioIDioIDioLjioJ/ioIPioIDioIDiorjioYfioIDioIDiorjio4fio6Dio4TioIDioIDioIDioIDioIDioIDioIDioIDioIDioIAK4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qOg4qO/4qO34qG24qCA4qCY4qC74qC/4qCb4qCB4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCACuKggOKggOKggOKggOKggOKggOKggOKggOKgieKggeKgieKgieKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggAo=`

// Default prompt templates
var defaultKubectlStartPrompt = `You are an expert Kubernetes administrator specializing in cluster diagnostics.

Task: Analyze the user's issue and provide appropriate kubectl commands for diagnostics.

## Guidelines:
- Provide only safe, read-only commands that will not modify cluster state
- Commands should be specific and target the exact resources relevant to the issue
- Focus on commands that provide the most useful diagnostic information
- Include namespace flags where appropriate (-n or --all-namespaces/-A)
- Prefer commands that give comprehensive information (e.g., -o wide, --show-labels)
`

var defaultKubectlFormatPrompt = `
## Output format:
- Return a JSON array of strings. Each element must be a full command starting with "kubectl "
- Use only actual resource names in the cluster; do not use placeholders like <namespace>
- Never include destructive commands that modify cluster state
- Prefer the most information-dense variants (e.g., -o wide, --show-labels)
`

var defaultDiagnosticAnalysisPrompt = `# Kubernetes Diagnostic Analysis

%s

## User Task
%s

## Guidelines
- You are an experienced Kubernetes administrator with deep expertise in diagnostics
- Analyze the context above and provide insights on the issue
- Identify potential problems or anomalies in the cluster state
- Support claims with concrete evidence from the provided context
- Be concise and prioritize highest-impact findings first
- Suggest next steps or additional commands if needed
- %s

`

// ConfigDetectMaxTokens attempts to auto-detect and enhance config values using model metadata
func (cfg *Config) ConfigDetectMaxTokens() {
	// Skip if auto-detection is disabled
	if !cfg.AutoDetectMaxTokens {
		return
	}

	// Only attempt auto-detection for supported providers
	if cfg.Provider != "openai" && cfg.Provider != "azopenai" && cfg.Provider != "google" && cfg.Provider != "anthropic" && cfg.Provider != "ollama" {
		return
	}

	// Skip if user provided an explicit override (>0)
	if cfg.UserMaxTokens > 0 {
		return
	}

	// Create metadata service
	metadataService := metadata.NewMetadataService(cfg.ModelMetadataTimeout, cfg.ModelMetadataCacheTTL)

	// Determine base URL for the API call depending on provider
	baseURL := GetProviderBaseURL(cfg)
	if cfg.Provider == "azopenai" && baseURL == "" {
		// Azure OpenAI requires a custom endpoint, cannot use default
		fmt.Fprintf(os.Stderr, "Warning: Azure OpenAI requires QU_AZ_OPENAI_BASE_URL or OPENAI_BASE_URL environment variable\n")
		return
	}

	// Attempt to get context length
	contextLength, err := metadataService.GetModelContextLength(cfg.Provider, cfg.Model, baseURL)
	if err != nil {
		// Log warning but don't fail - keep existing value
		fmt.Fprintf(os.Stderr, "Warning: Failed to auto-detect max tokens for model %s: %v. Using default: %d\n", cfg.Model, err, cfg.DefaultMaxTokens)
		return
	}

	// Update DefaultMaxTokens with auto-detected value
	logger.Log("info", "Auto-detected max tokens for model %s: %d\n", cfg.Model, contextLength)
	cfg.DefaultMaxTokens = contextLength
}

// GetProviderBaseURL returns the base URL for the configured provider, applying env/config defaults
func GetProviderBaseURL(cfg *Config) string {
	switch cfg.Provider {
	case "openai":
		if baseURL := GetOpenAIBaseURL(); baseURL != "" {
			return baseURL
		}
		if strings.Contains(cfg.Model, "/") || strings.Contains(cfg.Model, "openrouter") {
			return "https://openrouter.ai/api/v1"
		}
		return "https://api.openai.com"
	case "azopenai":
		if baseURL := GetAzOpenAIBaseURL(); baseURL != "" {
			return baseURL
		}
		// No sensible default for Azure — must be provided by user
		return ""
	case "google":
		if baseURL := os.Getenv("QU_GOOGLE_BASE_URL"); baseURL != "" {
			return baseURL
		}
		if baseURL := os.Getenv("GOOGLE_GEMINI_BASE_URL"); baseURL != "" {
			return baseURL
		}
		return "https://generativelanguage.googleapis.com"
	case "anthropic":
		if baseURL := os.Getenv("QU_ANTHROPIC_BASE_URL"); baseURL != "" {
			return baseURL
		}
		return "https://api.anthropic.com"
	case "ollama":
		if cfg.OllamaApiURL != "" {
			return cfg.OllamaApiURL
		}
		return "http://localhost:11434"
	default:
		return ""
	}
}

// defaultSlashCommands returns the default slash commands configuration
func defaultSlashCommands() []SlashCommand {
	return []SlashCommand{
		{
			Commands:    []string{"/help", "/h", "/?"},
			Primary:     "/help",
			Description: "Show help information",
		},
		{
			Commands:    []string{"/version"},
			Primary:     "/version",
			Description: "Show version information",
		},
		{
			Commands:    []string{"/model", "/models"},
			Primary:     "/model",
			Description: "Interactive model selector for current provider",
		},
		{
			Commands:    []string{"/reset"},
			Primary:     "/reset",
			Description: "Reset conversation context",
		},
		{
			Commands:    []string{"/clear"},
			Primary:     "/clear",
			Description: "Clear context and screen",
		},
		{
			Commands:    []string{"/mcp"},
			Primary:     "/mcp",
			Description: "Show MCP details",
		},
		{
			Commands:    []string{"/servers"},
			Primary:     "/servers",
			Description: "List MCP servers",
		},
		{
			Commands:    []string{"/tools"},
			Primary:     "/tools",
			Description: "List MCP tools",
		},
		{
			Commands:    []string{"/prompts"},
			Primary:     "/prompts",
			Description: "List MCP prompts",
		},
		{
			Commands:    []string{"/history"},
			Primary:     "/history",
			Description: "Show full session history",
		},
		{
			Commands:    []string{"/bye", "/exit", "/quit", "/q"},
			Primary:     "/quit",
			Description: "Exit the application",
		},
	}
}

// (EnvVarInfo and GetEnvVarsInfo removed; docs moved to README)
