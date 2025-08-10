package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

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

type Config struct {
	ChatMessages []llms.ChatMessage

	AllowedKubectlCmds []string
	BlockedKubectlCmds []string

	DuckASCIIArt          string
	Provider              string
	Model                 string
	ApiURL                string
	Retries               int
	Timeout               int
	MaxTokens             int
	Temperature           float64
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

	KubectlPrompts       []KubectlPrompt
	StoredUserCmdResults []CmdRes

	// Token accounting for last LLM exchange (shown in prompt)
	LastOutgoingTokens int
	LastIncomingTokens int

	// EditMode indicates the persistent shell edit mode toggled by '$'
	EditMode bool

	// Diagnostics toggles and knobs
	EnableBaseline      bool
	EventsWindowMinutes int
	EventsWarningsOnly  bool
	LogsTail            int
	LogsAllContainers   bool
}

// LoadConfig initializes the application configuration
func LoadConfig() *Config {
	provider := getEnvArg("QU_LLM_PROVIDER", "ollama").(string)

	defaultMaxTokens := 16000
	defaultModel := "llama3.1"
	defaultEmbeddingModel := "models/text-embedding-large-exp"
	if provider == "google" {
		// https://ai.google.dev/gemini-api/docs/models/gemini
		defaultMaxTokens = 128000 // best for Gemini exp tier
		defaultModel = "gemini-2.5-flash-preview-04-17"
		defaultEmbeddingModel = "models/text-embedding-004"
	} else if provider == "ollama" {
		// https://ai.meta.com/blog/meta-llama-3-1/
		defaultMaxTokens = 4096
		defaultModel = "llama3.1"
	} else if provider == "openai" {
		// https://platform.openai.com/docs/models/gpt-4o-mini
		defaultMaxTokens = 128000
		defaultModel = "gpt-4o-mini"
		defaultEmbeddingModel = "text-embedding-3-small"
	} else if provider == "anthropic" {
		defaultMaxTokens = 200000
		defaultModel = "claude-3-5-sonnet-latest"
		defaultEmbeddingModel = "nomic-embed-text"
	}

	// Get home directory for history file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	defaultHistoryFile := ""
	if homeDir != "" {
		defaultHistoryFile = fmt.Sprintf("%s/.quackops/history", homeDir)
	}

	return &Config{
		ChatMessages:          []llms.ChatMessage{},
		DuckASCIIArt:          defaultDuckASCIIArt,
		Provider:              provider,
		Model:                 getEnvArg("QU_LLM_MODEL", defaultModel).(string),
		ApiURL:                getEnvArg("QU_API_URL", "http://localhost:11434").(string),
		SafeMode:              getEnvArg("QU_SAFE_MODE", false).(bool),
		Retries:               getEnvArg("QU_RETRIES", 3).(int),
		Timeout:               getEnvArg("QU_TIMEOUT", 30).(int),
		MaxTokens:             getEnvArg("QU_MAX_TOKENS", defaultMaxTokens).(int),
		Temperature:           getEnvArg("QU_TEMPERATURE", 0.0).(float64),
		AllowedKubectlCmds:    getEnvArg("QU_ALLOWED_KUBECTL_CMDS", defaultAllowedKubectlCmds).([]string),
		BlockedKubectlCmds:    getEnvArg("QU_BLOCKED_KUBECTL_CMDS", defaultBlockedKubectlCmds).([]string),
		DisableMarkdownFormat: getEnvArg("QU_DISABLE_MARKDOWN_FORMAT", false).(bool),
		DisableAnimation:      getEnvArg("QU_DISABLE_ANIMATION", false).(bool),
		MaxCompletions:        getEnvArg("QU_MAX_COMPLETIONS", 50).(int),
		HistoryFile:           getEnvArg("QU_HISTORY_FILE", defaultHistoryFile).(string),
		DisableHistory:        getEnvArg("QU_DISABLE_HISTORY", false).(bool),
		KubectlBinaryPath:     getEnvArg("QU_KUBECTL_BINARY", "kubectl").(string),
		SpinnerTimeout:        300,
		StoredUserCmdResults:  []CmdRes{},
		// Diagnostics toggles
		EnableBaseline:      getEnvArg("QU_ENABLE_BASELINE", true).(bool),
		EventsWindowMinutes: getEnvArg("QU_EVENTS_WINDOW_MINUTES", 60).(int),
		EventsWarningsOnly:  getEnvArg("QU_EVENTS_WARN_ONLY", true).(bool),
		LogsTail:            getEnvArg("QU_LOGS_TAIL", 200).(int),
		LogsAllContainers:   getEnvArg("QU_LOGS_ALL_CONTAINERS", false).(bool),

		// Embedding model configuration
		EmbeddingModel:        getEnvArg("QU_EMBEDDING_MODEL", defaultEmbeddingModel).(string),
		OllamaEmbeddingModels: getEnvArg("QU_OLLAMA_EMBEDDING_MODELS", "nomic-embed-text,mxbai-embed-large,all-minilm-l6-v2").(string),

		// Prompt templates
		KubectlStartPrompt:       getEnvArg("QU_KUBECTL_SYSTEM_PROMPT", defaultKubectlStartPrompt).(string),
		KubectlShortPrompt:       getEnvArg("QU_KUBECTL_SHORT_PROMPT", "As a Kubernetes expert, based on your previous response, provide only the essential and safe read-only kubectl commands to help diagnose the following issue").(string),
		KubectlFormatPrompt:      getEnvArg("QU_KUBECTL_FORMAT_PROMPT", defaultKubectlFormatPrompt).(string),
		DiagnosticAnalysisPrompt: getEnvArg("QU_DIAGNOSTIC_ANALYSIS_PROMPT", defaultDiagnosticAnalysisPrompt).(string),
		MarkdownFormatPrompt:     getEnvArg("QU_MARKDOWN_FORMAT_PROMPT", "Format your response using Markdown, including headings, lists, and code blocks for improved readability in a terminal environment.").(string),
		PlainFormatPrompt:        getEnvArg("QU_PLAIN_FORMAT_PROMPT", "Provide a clear, concise analysis that is easy to read in a terminal environment.").(string),

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

	if val, exists := os.LookupEnv(key); exists {
		return getValue(val)
	}

	// Search in os.Args
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
	"cp",
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

var defaultDuckASCIIArt = `4qCA4qCA4qCA4qKA4qOE4qKA4qO04qC/4qCf4qCb4qCb4qCb4qCb4qCb4qC34qK24qOE4qGA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCACuKggOKggOKggOKggOKggOKiv+Khv+Kgg+KggOKggOKggOKggOKggOKggOKggOKggOKhgOKiqeKjt+KhhOKggOKggOKggOKggOKggOKggOKggArioIDioIDioIDiorDiopbiob/ioIHioIDioIDioIDioIDiooLio6bio4TioIDioIBv4qGH4qK44qO34qCA4qCA4qCA4qCA4qCA4qCA4qCACuKggOKggOKggOKggOKjvOKgg+KggOKggOKggOKggOKggOKggG/ioZvioIDioIDioJDioJDioJLiorvioYbioIDioIDioIDioIDioIDioIAK4qCA4qCA4qCA4qCA4qO/4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qKA4qO04qG+4qC/4qC/4qK/4qG/4qK34qOk4qOA4qGA4qCA4qCA4qCACuKggOKggOKggOKggOKjv+KhgOKggOKggOKggOKggOKggOKggOKisOKjv+KgieKggOKguuKgh+KgmOKgg+KggOKgieKgmeKgm+Kit+KjhOKggArioIDioIDioIDioIDioLjioJ/ioIPioIDioIDiorjioYfioIDioIDiorjio4fio6Dio4TioIDioIDioIDioIDioIDioIDioIDioIDioIDioIAK4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qOg4qO/4qO34qG24qCA4qCY4qC74qC/4qCb4qCB4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCA4qCACuKggOKggOKggOKggOKggOKggOKggOKggOKgieKggeKgieKgieKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggOKggAo=`

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
- Provide only the exact commands to run (no markdown formatting)
- One command per line
- Do not include explanations or descriptions
- Use only actual resource names, not placeholders
- Never include destructive commands that modify cluster state
`

var defaultDiagnosticAnalysisPrompt = `# Kubernetes Diagnostic Analysis

%s

## Task
%s

## Guidelines
- You are an experienced Kubernetes administrator with deep expertise in diagnostics
- Analyze the context above and provide insights on the issue
- Identify potential problems or anomalies in the cluster state
- Suggest next steps or additional commands if needed
- %s

`

// EnvVarInfo contains information about an environment variable
type EnvVarInfo struct {
	DefaultValue interface{}
	Type         string
	Description  string
}

// GetEnvVarsInfo returns information about all environment variables used by the application
func GetEnvVarsInfo() map[string]EnvVarInfo {
	envVars := map[string]EnvVarInfo{
		"QU_LLM_PROVIDER": {
			DefaultValue: "ollama",
			Type:         "string",
			Description:  "LLM model provider (e.g., 'ollama', 'openai', 'google', 'anthropic')",
		},
		"QU_LLM_MODEL": {
			DefaultValue: "provider-dependent",
			Type:         "string",
			Description:  "LLM model to use",
		},
		"QU_API_URL": {
			DefaultValue: "http://localhost:11434",
			Type:         "string",
			Description:  "URL for LLM API, used with 'ollama' provider",
		},
		"QU_SAFE_MODE": {
			DefaultValue: false,
			Type:         "bool",
			Description:  "Enable safe mode to prevent executing commands without confirmation",
		},
		"QU_KUBECTL_BINARY": {
			DefaultValue: "kubectl",
			Type:         "string",
			Description:  "Path to kubectl binary to use for executing commands",
		},
		"QU_RETRIES": {
			DefaultValue: 3,
			Type:         "int",
			Description:  "Number of retries for kubectl commands",
		},
		"QU_TIMEOUT": {
			DefaultValue: 30,
			Type:         "int",
			Description:  "Timeout for kubectl commands in seconds",
		},
		"QU_MAX_TOKENS": {
			DefaultValue: "provider-dependent",
			Type:         "int",
			Description:  "Maximum number of tokens in LLM context window",
		},
		"QU_TEMPERATURE": {
			DefaultValue: 0.7,
			Type:         "float",
			Description:  "Temperature parameter for LLM generation",
		},
		"QU_ALLOWED_KUBECTL_CMDS": {
			DefaultValue: "see defaultAllowedKubectlCmds",
			Type:         "[]string",
			Description:  "Comma-separated list of allowed kubectl commands",
		},
		"QU_BLOCKED_KUBECTL_CMDS": {
			DefaultValue: "see defaultBlockedKubectlCmds",
			Type:         "[]string",
			Description:  "Comma-separated list of blocked kubectl commands",
		},
		"QU_KUBECTL_BLOCKED_CMDS_EXTRA": {
			DefaultValue: "",
			Type:         "string",
			Description:  "Comma-separated list of additional blocked kubectl commands",
		},
		"QU_DISABLE_MARKDOWN_FORMAT": {
			DefaultValue: false,
			Type:         "bool",
			Description:  "Disable Markdown formatting and colorization of LLM outputs",
		},
		"QU_DISABLE_ANIMATION": {
			DefaultValue: true,
			Type:         "bool",
			Description:  "Disable typewriter animation effect for LLM outputs",
		},
		"QU_MAX_COMPLETIONS": {
			DefaultValue: 50,
			Type:         "int",
			Description:  "Maximum number of completions to display",
		},
		"QU_HISTORY_FILE": {
			DefaultValue: "~/.quackops/history",
			Type:         "string",
			Description:  "Path to the history file",
		},
		"QU_DISABLE_HISTORY": {
			DefaultValue: false,
			Type:         "bool",
			Description:  "Disable storing prompt history in a file",
		},
		"QU_EMBEDDING_MODEL": {
			DefaultValue: "provider-dependent",
			Type:         "string",
			Description:  "Embedding model to use",
		},
		"QU_OLLAMA_EMBEDDING_MODELS": {
			DefaultValue: "nomic-embed-text,mxbai-embed-large,all-minilm-l6-v2",
			Type:         "string",
			Description:  "Comma-separated list of Ollama embedding models",
		},
		"QU_KUBECTL_SYSTEM_PROMPT": {
			DefaultValue: "see defaultKubectlStartPrompt",
			Type:         "string",
			Description:  "Start prompt for kubectl command generation",
		},
		"QU_KUBECTL_SHORT_PROMPT": {
			DefaultValue: "As a Kubernetes expert, provide safe, read-only kubectl commands to diagnose the following issue.",
			Type:         "string",
			Description:  "Short prompt for kubectl command generation",
		},
		"QU_KUBECTL_SHORT_PROMPT_2": {
			DefaultValue: "As a Kubernetes expert, based on your previous answer, provide only relevant safe read-only kubectl commands to diagnose the following issue.",
			Type:         "string",
			Description:  "Secondary short prompt for kubectl command generation",
		},
		"QU_KUBECTL_FORMAT_PROMPT": {
			DefaultValue: "see defaultKubectlFormatPrompt",
			Type:         "string",
			Description:  "Format prompt for kubectl command generation",
		},
		"QU_DIAGNOSTIC_ANALYSIS_PROMPT": {
			DefaultValue: "see defaultDiagnosticAnalysisPrompt",
			Type:         "string",
			Description:  "Prompt for diagnostic analysis",
		},
		"QU_MARKDOWN_FORMAT_PROMPT": {
			DefaultValue: "Format your response using Markdown, including headings, lists, and code blocks for improved readability in a terminal environment.",
			Type:         "string",
			Description:  "Prompt for Markdown formatting",
		},
		"QU_PLAIN_FORMAT_PROMPT": {
			DefaultValue: "Provide a clear, concise analysis that is easy to read in a terminal environment.",
			Type:         "string",
			Description:  "Prompt for plain text formatting",
		},
		"OPENAI_API_KEY": {
			DefaultValue: "",
			Type:         "string",
			Description:  "OpenAI API key, required when using OpenAI provider",
		},
		"GOOGLE_API_KEY": {
			DefaultValue: "",
			Type:         "string",
			Description:  "Google AI API key, required when using Google provider",
		},
		"ANTHROPIC_API_KEY": {
			DefaultValue: "",
			Type:         "string",
			Description:  "Anthropic API key, required when using Anthropic provider",
		},
	}

	return envVars
}
