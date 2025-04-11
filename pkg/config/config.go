package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

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
	SafeMode              bool
	Verbose               bool
	DisableSecretFilter   bool
	DisableMarkdownFormat bool
	DisableAnimation      bool

	KubectlPrompts []KubectlPrompt
}

// LoadConfig initializes the application configuration
func LoadConfig() *Config {
	provider := getEnvArg("QU_LLM_PROVIDER", "ollama").(string)

	defaultMaxTokens := 16000
	defaultModel := "llama3.1"
	if provider == "google" {
		defaultMaxTokens = 1048576 // https://ai.google.dev/gemini-api/docs/models/gemini
		defaultModel = "gemini-2.0-flash"
	} else if provider == "ollama" {
		defaultMaxTokens = 4096 // https://ai.meta.com/blog/meta-llama-3-1/
		defaultModel = "llama3.1"
	} else if provider == "openai" {
		defaultMaxTokens = 128000 // https://openai.com/index/gpt-4o-mini-advancing-cost-efficient-intelligence/
		defaultModel = "gpt-4o-mini"
	} else if provider == "anthropic" {
		defaultMaxTokens = 200000 // Claude has a 200k context window
		defaultModel = "claude-3-5-sonnet-latest"
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
		AllowedKubectlCmds:    getEnvArg("QU_ALLOWED_KUBECTL_CMDS", defaultAllowedKubectlCmds).([]string),
		BlockedKubectlCmds:    getEnvArg("QU_BLOCKED_KUBECTL_CMDS", defaultBlockedKubectlCmds).([]string),
		DisableMarkdownFormat: getEnvArg("QU_DISABLE_MARKDOWN_FORMAT", false).(bool),
		DisableAnimation:      getEnvArg("QU_DISABLE_ANIMATION", false).(bool),
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
