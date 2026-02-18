# Kubectl-QuackOps

<img src=".github/quackops-logo.png" alt="QuackOps Logo" width="200" align="left" hspace="20">

**QuackOps** is a powerful kubectl AI-agent plugin designed to enhance your Kubernetes troubleshooting experience with advanced AI assistance. It acts as an intelligent agent, translating natural language queries into actionable insights by analyzing diagnostics directly from your current Kubernetes context.

QuackOps goes beyond simple question-and-answer functionality, offering natural language interaction to translate problems into diagnostic steps, interactive troubleshooting with follow-up questions, and direct command execution (e.g., $ kubectl describe deployment my-app) within a feature-rich shell that includes history, persistence, and auto-completion. Safety is built in with read-only command generation, approval-based safe mode, and secret sanitization, ensuring confident exploration of your cluster. With support for the Model Context Protocol (MCP), QuackOps can integrate with a growing ecosystem of external tools, while the flexibility to switch between local models for privacy or cloud-based models for advanced reasoning provides the right balance of control and power.

## 🎥 Demo

<img src="https://raw.githubusercontent.com/mikhae1/media/master/quackops/quackops-demo.gif" alt="QuackOps Demo" width="800">

## ⚡ Quickstart (2 minutes)

1) Install the plugin via [krew](https://krew.sigs.k8s.io/docs/user-guide/setup/install/):
   ```sh
   kubectl krew index add mikhae1 https://github.com/mikhae1/kubectl-quackops
   kubectl krew install mikhae1/quackops
   ```

2) Pick a provider:
- OpenAI:
  ```sh
  export OPENAI_API_KEY=...             # required
  kubectl quackops -p openai -m gpt-5-mini 'check pod issues'
  ```
- Ollama (local, no API key):
  ```sh
  ollama serve &
  kubectl quackops -p ollama -m llama3.1 'why is my ingress slow?'
  ```

3) Keep it safe in prod:
```sh
kubectl quackops --safe-mode -- 'review pod restarts'
```

4) Use MCP tools:
```sh
kubectl quackops --mcp-client=true --mcp-strict=true -- 'summarize cluster health'
```

## ✅ Prerequisites

- `kubectl` installed and pointed at the cluster you want to debug.
- API access for your chosen LLM provider (unless using Ollama locally).
- For MCP mode, ensure your MCP server(s) are configured.

## 🚀 Key Features

- **AI-Powered Diagnostics:**
  - **Natural Language Queries:** Ask questions like "Why are my pods crash-looping?" and get immediate insights.
  - **Context-Aware Sessions:** QuackOps remembers the context of your troubleshooting session for relevant follow-up suggestions.

- **Interactive Shell:**
  - **Run Any Command:** Execute `kubectl` or any shell command directly in the session by prefixing it with `$`.
  - **Command History:** Use arrow keys to navigate and reuse previous commands.
  - **Auto-Completion:** Get intelligent suggestions for commands and arguments.

- **Multi-Model Support:**
  - **[Ollama](https://ollama.com/):** Run LLMs locally for maximum privacy.
  - **[Google Gemini](https://gemini.google.com/):** Handle massive outputs with large context windows.
  - **[OpenAI](https://openai.com/):** Leverage the latest models for cutting-edge diagnostics.
  - **[Azure OpenAI](https://learn.microsoft.com/azure/ai-services/openai/):** Enterprise-grade OpenAI on Azure.
  - **[Anthropic](https://anthropic.com/):** Get reliable, technically sound analysis.

- **Model Context Protocol (MCP) for Tooling:**
  - **External Tool Integration:** Use MCP to connect to external diagnostic tools like `kubeview-mcp` for extended capabilities.
  - **Standardized Ecosystem:** As MCP is adopted more widely, QuackOps will be able to connect to an ever-growing set of tools.

- **Security and Safety:**
  - **Secret Protection:** Sensitive data is automatically filtered before being sent to an LLM.
  - **Safe Mode:** Review and approve all commands before they are executed with the `--safe-mode` flag.
  - **Command Whitelisting:** Prevents destructive operations by default.

- **Syntax highlighting:**
  - Markdown-based output formatting with color-coded elements for better readability

- **History**:
- **MCP Client Mode (optional):**
  - Prefer external Model Context Protocol (MCP) tools for diagnostics (kubectl/bash and more)
  - Configurable via flags/env and `~/.config/quackops/mcp.yaml`
  - Strict mode to disable local fallback

  - Interactive shell-like experience with command history navigation (up/down arrows)
  - Persistent history storage in a configurable file (default: `~/.quackops/history`)
  - Easily recall previous prompts and commands across sessions

## 🔍 Use Cases

### For Developers:
- **Quick Debugging:** Identify application issues without extensive Kubernetes knowledge
- **Log Analysis:** Find and understand error patterns across distributed services
- **Resource Optimization:** Get recommendations for kubernetes resources based on actual usage
- **Configuration Validation:** Check for common misconfigurations in your deployments

### For DevOps/SRE:
- **Incident Response:** Rapidly diagnose production issues
- **Cluster Maintenance:** Get guidance on upgrades, migrations, and best practices
- **Security Auditing:** Identify potential security risks in your deployments

## 💻 Example

```sh
$ kubectl quackops -v 'find and resolve issues with pods'

kubectl get pods
-- NAME                                            READY   STATUS             RESTARTS        AGE
-- my-nginx-ingress-hello-world-6d8c5b76db-g5696   1/1     Running            14 (149m ago)   58d
-- test-21081                                      1/1     Running            22 (149m ago)   95d
-- example-hello-world-5cd56d45d5-8nh5x            1/1     Running            2 (149m ago)    17d
-- my-nginx-ingress-hello-world-64f78448bd-v567q   0/1     ImagePullBackOff   0               28d
--

kubectl get events
-- LAST SEEN   TYPE     REASON    OBJECT                                              MESSAGE
-- 4m45s       Normal   BackOff   pod/my-nginx-ingress-hello-world-64f78448bd-v567q   Back-off pulling image "nginx:v1.16.0"
--

Based on the information provided:
- The pod `my-nginx-ingress-hello-world-64f78448bd-v567q` is not working
because it is in the `ImagePullBackOff` status which means it is unable to
pull the specified image `nginx:v1.16.0`.

- The issue is likely related to the incorrect image specified or the image
not being available in the specified repository.

To resolve the issue, you can check the image availability, correct the image
name or tag, ensure the repository access is correct, and troubleshoot any
network issues that may be preventing the pod from pulling the image.
```

## 🔁 Common Workflows

- Quick pod triage:
  ```sh
  kubectl quackops -- 'why are pods in crashloopbackoff?'
  ```
- Log pull with guidance:
  ```sh
  kubectl quackops -- 'grab logs from nginx pods and summarize errors'
  ```
- Safe commands run:
  ```sh
  kubectl quackops --safe-mode -- 'suggest commands to debug ingress 503s'
  ```
- MCP-first troubleshooting:
  ```sh
  kubectl quackops --mcp-client=true --mcp-strict=true -- 'list unhealthy deployments'
  ```

## 🛠️ Advanced Examples

### Complex Troubleshooting

```sh
$ kubectl quackops 'why is my ingress not routing traffic properly to backend services?'
```

### Performance Analysis

```sh
$ kubectl quackops 'identify pods consuming excessive CPU or memory in the production namespace'
```

### Security Auditing

```sh
$ kubectl quackops 'check for overly permissive RBAC settings in my cluster'
```

### Multi-Resource Analysis

```sh
$ kubectl quackops 'analyze the connection between my failing deployments and their dependent configmaps'
```

## 📦 Installation

QuackOps is packaged as a kubectl plugin, which is a standalone executable file whose name begins with `kubectl-`.

### Install via Krew (recommended)

1. Make sure [Krew](https://krew.sigs.k8s.io/docs/user-guide/setup/install/) is installed.
2. Add the custom index and install:
   ```sh
   kubectl krew index add mikahe1 https://github.com/mikhae1/kubectl-quackops
   kubectl krew install mikahe1/quackops
   ```
3. Verify:
   ```sh
   kubectl quackops --help
   ```

### Manual install (tarball)

You can install it by moving the executable file to any directory included in your `$PATH`.

1. **Download the QuackOps binary**
   Head over to the [GitHub releases page](https://github.com/mikhae1/kubectl-quackops/releases) and download the latest release archive suitable for your operating system (e.g., `kubectl-quackops-linux-amd64.tar.gz`)

2. **Extract the binary**
   Use the following command to extract the binary from the downloaded archive:
   ```sh
   tar -xzf ~/Downloads/kubectl-quackops-linux-amd64.tar.gz -C ~/Downloads
   ```

3. **Make the binary executable** (if needed):
   ```sh
   chmod +x ~/Downloads/kubectl-quackops
   ```

4. **Move the binary to your `$PATH`:**
   Move the `kubectl-quackops` executable to a directory included in your system's `$PATH`, such as `/usr/local/bin`:
   ```sh
   sudo mv ~/Downloads/kubectl-quackops /usr/local/bin/kubectl-quackops
   ```

5. **Verify the installation:**
   Confirm that QuackOps is recognized as a kubectl plugin by running:
   ```sh
   kubectl plugin list
   ```

6. **Summon the quAck:**
   ```sh
   kubectl quackops
   ```

## 📜 Prompt History

QuackOps automatically stores your prompt history to enable easy access to previous queries:

- **Persistent History:** Your previous prompts are saved to a history file (default: `~/.quackops/history`) and will be available across sessions.
- **History Navigation:** Use up and down arrow keys to navigate through your command history.
- **Customizable:** Control the history file location with `--history-file` option or disable history completely with `--disable-history`.

This feature helps you:
- Quickly recall complex queries without retyping
- Build on previous troubleshooting sessions
- Maintain a record of your cluster diagnostics

### Examples

```sh
# Specify a custom history file location
kubectl quackops --history-file ~/.my_custom_history

# Disable history storage entirely
kubectl quackops --disable-history
```

## 🔄 Shell Commands

QuackOps provides intelligent tab completion for command execution mode (enter with `$`), leveraging bash-compatible shell completions:

- **Command Completions:** When typing `$ `, type and press Tab to see available commands.
- **Argument Completions:** QuackOps supports completions for cli tools like `kubectl`, `helm`, and other CLI tools that implement completion.
- **File Path Completions:** Automatically complete file paths when navigating the filesystem.

Note that completions rely on bash-compatible shell completion functions being available on your system. The feature works best in environments where bash completion is properly configured.

## 🌟 Supported LLMs

QuackOps offers flexible options to tailor your Kubernetes troubleshooting experience.
Choose the LLM provider that best suits your needs.

### Ollama: Local Models for Privacy and Control

For maximum data security, leverage the power of local LLMs with [Ollama](https://ollama.com/).

**Benefits:**

* **Data Sovereignty:** Keep your cluster information confidential. Data remains within your environment, enhancing privacy.
* **Enhanced Security:** Maintain complete control over access and security protocols for your Kubernetes data.
* **Air-Gapped Operation:** Run in environments with no internet connectivity.
* **No API Costs:** Eliminate dependency on external API services and associated costs.

**Getting Started:**

1. **Install Ollama:** Download and install Ollama from [https://ollama.com/download](https://ollama.com/download).

2. **Start ollama server:**
   ```sh
   ollama serve
   ```

3. **Download local LLM model** (e.g., `llama3.1`, `qwen2.5-coder`):
   ```sh
   ollama run llama3.1:8b
   ```

4. **Start interactive chat:**
   ```sh
   kubectl quackops -p ollama
   ```

### OpenAI: Cutting-Edge AI Models

For users seeking the most advanced AI capabilities.

**Benefits:**
- **Advanced Reasoning:** Solve complex cluster issues with sophisticated models
- **Access the Latest Models:** Leverage the latest advancements in LLMs, constantly updated and refined by OpenAI
- **Superior Context Understanding:** Better comprehension of complex Kubernetes architectures and dependencies
- **Multi-step Troubleshooting:** Handle complex diagnostics requiring multiple steps of reasoning

**Getting Started:**

1. **Obtain an API Key:** Get your OpenAI API key at [https://platform.openai.com/api-keys](https://platform.openai.com/api-keys).

2. **Set the API Key:**
   ```sh
   export OPENAI_API_KEY=<YOUR-OPENAI-API-KEY>
   ```

3. **Start QuackOps:**
   ```sh
   kubectl quackops -p openai -m gpt-5-mini
   ```

### Azure OpenAI: Enterprise-grade OpenAI on Azure

**Benefits:**

- Enterprise compliance and data residency on Azure
- Private networking and RBAC with Azure resource controls
- OpenAI-compatible API surface via Azure deployments

**Getting Started:**

1. Create an Azure OpenAI resource and deploy a model (e.g., a deployment of GPT-4o-mini).
2. Set environment variables:
   ```sh
   export QU_AZ_OPENAI_API_KEY=<YOUR-AZURE-OPENAI-API-KEY>
   export QU_AZ_OPENAI_BASE_URL="https://<your-resource-name>.openai.azure.com"
   export QU_AZ_OPENAI_API_VERSION="2025-05-01"  # Optional, defaults to 2025-05-01
   ```
   - Aliases supported: `OPENAI_API_KEY` and `OPENAI_BASE_URL` can also be used.
3. Run QuackOps with the Azure provider, using your deployment name as the model:
   ```sh
   kubectl quackops -p azopenai -m <your-deployment-name>
   ```

Notes:
- If you use embeddings, set `QU_EMBEDDING_MODEL` to your Azure embedding deployment name.
- When a custom base URL is set, streaming is automatically disabled for compatibility.

### Google: Large Contexts

For users requiring extensive context analysis and handling large command outputs.

**Benefits:**
- **Massive Context Windows:** Process more information at once with Gemini's 1M+ token context window
- **Efficient RAG:** Superior handling of long command outputs and cluster state analysis
- **Cost-Effective:** Competitive pricing for enterprise-grade AI capabilities
- **Comprehensive Analysis:** Analyze outputs from multiple resources simultaneously (pods, services, deployments, etc.)

**Getting Started:**

1. **Obtain an API Key:** Get your Google AI API key at [https://makersuite.google.com/app/apikey](https://makersuite.google.com/app/apikey)

2. **Set the API Key:**
   ```sh
   export GOOGLE_API_KEY=<YOUR-GOOGLE-API-KEY>
   ```

3. **Start QuackOps:**
   ```sh
   kubectl quackops -p google -m gemini-2.5-flash --throttle-rpm 10
   ```

### Anthropic: Reliable Technical Analysis

For users requiring clear explanations and technical reliability.

**Benefits:**
- **Clear Explanations:** Receive clear, technically precise explanations of complex issues
- **Consistent Outputs:** More predictable response quality for mission-critical diagnostics
- **Structured Analysis:** Well-organized recommendations for methodical troubleshooting
- **Low Hallucination Rate:** Higher accuracy when analyzing complex Kubernetes states

**Getting Started:**

1. **Obtain an API Key:** Get your Anthropic API key at [https://console.anthropic.com/](https://console.anthropic.com/)

2. **Set the API Key:**
   ```sh
   export ANTHROPIC_API_KEY=<YOUR-ANTHROPIC-API-KEY>
   ```

3. **Start QuackOps:**
   ```sh
   kubectl quackops -p anthropic -m claude-3-opus-20240229
   ```

## ⚙️ Configuration Options

QuackOps is highly configurable through environment variables, command-line flags, or config files:

### Config Files

QuackOps automatically loads configuration from config files in your home directory. Config files use simple `KEY=VALUE` format and support all `QU_*` environment variables.

**Config file locations (in order of preference):**
- `~/.quackops/config`
- `~/.config/quackops/config`

**Configuration priority (highest to lowest):**
1. Command-line arguments
2. Environment variables
3. Config file values
4. Default values

**Example config file (`~/.quackops/config`):**

```bash
QU_SAFE_MODE=false
QU_MCP_CLIENT=true
```

### Environment Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `OPENAI_API_KEY` | string |  | OpenAI API key (required for `openai` provider) |
| `QU_OPENAI_BASE_URL` | string |  | Custom base URL for OpenAI-compatible APIs (e.g., for DeepSeek, local OpenAI-compatible servers). When set, streaming is automatically disabled for OpenAI to improve compatibility with non-standard SSE implementations. |
| `GOOGLE_API_KEY` | string |  | Google AI API key (required for `google` provider) |
| `ANTHROPIC_API_KEY` | string |  | Anthropic API key (required for `anthropic` provider) |
| `QU_LLM_PROVIDER` | string | `ollama` | LLM model provider (`ollama`, `openai`, `azopenai`, `google`, `anthropic`) |
| `QU_LLM_MODEL` | string | provider-dependent | LLM model to use. Defaults: `lla3.1` (ollama), `gpt-5-mini` (openai), `gpt-4o-mini` (azopenai), `gemini-2.5-flash-preview-04-17` (google), `claude-3-7-sonnet-latest` (anthropic) |
| `QU_OLLAMA_BASE_URL` | string | `http://localhost:11434` | Ollama server base URL (used with `ollama` provider) |
| `QU_SAFE_MODE` | bool | `false` | Require confirmation before executing commands |
| `QU_RETRIES` | int | `3` | Number of retries for kubectl commands |
| `QU_TIMEOUT` | int | `30` | Timeout for kubectl commands (seconds) |
| `QU_MAX_TOKENS` | int | provider-dependent | Max tokens in LLM context window. Defaults: `4096` (ollama), `128000` (openai/google), `200000` (anthropic) |
| `QU_ALLOWED_KUBECTL_CMDS` | []string | see `defaultAllowedKubectlCmds` | Comma-separated allowlist of kubectl command prefixes |
| `QU_BLOCKED_KUBECTL_CMDS` | []string | see `defaultBlockedKubectlCmds` | Comma-separated denylist of kubectl command prefixes |
| `QU_DISABLE_MARKDOWN_FORMAT` | bool | `false` | Disable Markdown formatting and colorization |
| `QU_DISABLE_ANIMATION` | bool | `false` | Disable typewriter animation effect |
| `QU_MAX_COMPLETIONS` | int | `50` | Maximum number of completions to display |
| `QU_HISTORY_FILE` | string | `~/.quackops/history` | Path to the history file |
| `QU_DISABLE_HISTORY` | bool | `false` | Disable storing prompt history in a file |
| `QU_KUBECTL_BINARY` | string | `kubectl` | Path to the kubectl binary |
| `QU_COMMAND_PREFIX` | string | `$` | Single-character prefix to enter command mode and mark shell commands |
| `QU_THEME` | string | `dracula` | UI theme (`dracula`, `cyanide`); env overrides config |
| `QU_DISABLE_BASELINE` | bool | `false` | Disable baseline diagnostic pack before LLM |
| `QU_BASELINE_LEVEL` | string | `minimal` | Baseline diagnostic level: minimal (13 commands), standard (+ workloads), comprehensive (+ metrics/policies) |
| `QU_BASELINE_NAMESPACE_FILTER` | string | `` | Comma-separated namespaces for baseline commands (empty = all namespaces) |
| `QU_EVENTS_WINDOW_MINUTES` | int | `60` | Events time window in minutes for summarization |
| `QU_EVENTS_WARN_ONLY` | bool | `true` | Include only Warning events in summaries |
| `QU_LOGS_TAIL` | int | `200` | Tail lines for log aggregation when triggered by playbooks |
| `QU_LOGS_ALL_CONTAINERS` | bool | `false` | Aggregate logs from all containers when collecting logs |
| `QU_TOOL_OUTPUT_MAX_LINES` | int | `40` | Maximum number of lines to show in MCP tool output blocks |
| `QU_TOOL_OUTPUT_MAX_LINE_LEN` | int | `140` | Maximum line length to show in MCP tool output blocks |
| `QU_DIAGNOSTIC_RESULT_MAX_LINES` | int | `10` | Maximum lines for diagnostic result display |
| `QU_MCP_CLIENT` | bool | `true` | Enable MCP client mode to use external MCP servers for tools |
| `QU_MCP_TOOL_TIMEOUT` | int | `30` | Timeout for MCP tool calls (seconds) |
| `QU_MCP_STRICT` | bool | `false` | Strict MCP mode: do not fall back to local execution when MCP fails |
| `QU_MCP_MAX_TOOL_CALLS` | int | `10` | Maximum iterative MCP tool calls per model response |
| `QU_MCP_MAX_TOOL_CALLS_TOTAL` | int | `30` | Total MCP tool-call budget per user request (`0` = unlimited) |
| `QU_MCP_TOOL_RESULT_BUDGET_BYTES` | int | `200000` | Maximum cumulative MCP tool result bytes per user request (`0` = unlimited) |
| `QU_MCP_STALL_THRESHOLD` | int | `2` | Consecutive identical MCP tool-call rounds before loop is considered stalled (`0` = disabled) |
| `QU_MCP_LOG` | bool | `false` | Enable logging of MCP server stdio to a file |
| `QU_MCP_LOG_FORMAT` | string | `jsonl` | MCP log format: jsonl (default), text, or yaml |
| `QU_EMBEDDING_MODEL` | string | provider-dependent | Embedding model. Defaults: `models/text-embedding-004` (google), `text-embedding-3-small` (openai), `nomic-embed-text` (anthropic) |
| `QU_OLLAMA_EMBEDDING_MODELS` | string | `nomic-embed-text,mxbai-embed-large,all-minilm-l6-v2` | Comma-separated list of Ollama embedding models |
| `QU_ALLOWED_TOOLS` | []string | `*` | Comma-separated allowlist of tool names when invoking via MCP (`*` = allow all) |
| `QU_DENIED_TOOLS` | []string |  | Comma-separated denylist of tool names when invoking via MCP |
| `QU_THROTTLE_REQUESTS_PER_MINUTE` | int | `60` | Maximum number of LLM requests per minute |
| `QU_THROTTLE_DELAY_OVERRIDE_MS` | int | `0` | Override throttle delay in milliseconds |
| `QU_AUTO_COMPACT` | bool | `true` | Enable auto-compaction of long chat history using a summary |
| `QU_AUTO_COMPACT_TRIGGER_PERCENT` | int | `95` | Trigger auto-compaction at this percentage of model context window |
| `QU_AUTO_COMPACT_TARGET_PERCENT` | int | `60` | Target context usage percentage after auto-compaction |
| `QU_AUTO_COMPACT_KEEP_MESSAGES` | int | `8` | Keep this many most recent non-system messages verbatim during compaction |
| `QU_KUBECTL_SYSTEM_PROMPT` | string | see `defaultKubectlStartPrompt` | Start prompt for kubectl command generation |
| `QU_KUBECTL_SHORT_PROMPT` | string | code default | Short prompt for kubectl command generation |
| `QU_KUBECTL_FORMAT_PROMPT` | string | see `defaultKubectlFormatPrompt` | Format prompt for kubectl command generation |
| `QU_DIAGNOSTIC_ANALYSIS_PROMPT` | string | see `defaultDiagnosticAnalysisPrompt` | Prompt for diagnostic analysis |
| `QU_MARKDOWN_FORMAT_PROMPT` | string | "Format your response using Markdown, including headings, lists, and code blocks for improved readability in a terminal environment." | Markdown formatting guidance |
| `QU_PLAIN_FORMAT_PROMPT` | string | "Provide a clear, concise analysis that is easy to read in a terminal environment." | Plain text formatting guidance |

### Command-Line Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-p, --provider` | LLM model provider (e.g., 'ollama', 'openai', 'azopenai', 'google', 'anthropic') | `ollama` |
| `-m, --model` | LLM model to use | Provider-dependent |
| `-u, --api-url` | URL for LLM API (used with 'ollama' provider) | `http://localhost:11434` |
| `-s, --safe-mode` | Enable safe mode to prevent executing commands without confirmation | `false` |
| `-r, --retries` | Number of retries for kubectl commands | `3` |
| `-t, --timeout` | Timeout for kubectl commands in seconds | `30` |
| `-x, --max-tokens` | Maximum number of tokens in LLM context window | `4096` |
| `-v, --verbose` | Enable verbose output | `false` |
| `-c, --disable-secrets-filter` | Disable filtering sensitive data in secrets from being sent to LLMs | `false` |
| `-d, --disable-markdown` | Disable Markdown formatting and colorization of LLM outputs | `false` |
| `-a, --disable-animation` | Disable typewriter animation effect for LLM outputs | `false` |
| `--disable-history` | Disable storing prompt history in a file | `false` |
| `--history-file` | Path to the history file | `~/.quackops/history` |
| `--throttle-rpm` | Maximum number of LLM requests per minute | `60` |
| `--mcp-client` | Enable MCP client mode | `true` |
| `--mcp-config` | Comma-separated MCP client config paths | `~/.config/quackops/mcp.yaml, ~/.quackops/mcp.json` |
| `--mcp-tool-timeout` | Timeout for MCP tools (seconds) | `30` |
| `--mcp-strict` | Strict MCP mode (no fallback) | `false` |
| `--mcp-max-tool-calls` | Maximum MCP tool-call rounds per model response | `10` |
| `--mcp-max-tool-calls-total` | Total MCP tool-call budget per user request (`0` = unlimited) | `30` |
| `--mcp-tool-result-budget-bytes` | Maximum cumulative MCP tool result bytes per request (`0` = unlimited) | `200000` |
| `--mcp-stall-threshold` | Consecutive identical MCP tool-call rounds before stalling (`0` = disabled) | `2` |
| `--auto-compact` | Enable auto-compaction of long chat history | `true` |
| `--auto-compact-trigger-percent` | Trigger auto-compaction at this context percentage | `95` |
| `--auto-compact-target-percent` | Target context percentage after compaction | `60` |
| `--auto-compact-keep-messages` | Keep recent non-system messages uncompressed | `8` |

### MCP Configuration (JSON)

Create `~/.quackops/mcp.json`:

```json
{
  "mcpServers": {
    "kubeview-mcp": {
      "command": "npx",
      "args": [
        "-y",
        "https://github.com/mikhae1/kubeview-mcp"
      ],
      "env": {
        "KUBECONFIG": "/Users/mikhae1/.kube/config"
      }
    }
  }
}
```

Run quackops:
```sh
$ kubectl quackops -p openai -m moonshotai/kimi-k2:free --mcp-client=true --mcp-strict=true
```

In MCP mode, QuackOps prefers MCP tools for diagnostics, with optional strict mode to avoid local fallback. Tools can be restricted using `QU_ALLOWED_TOOLS`/`QU_DENIED_TOOLS`.

## 🛡️ Security Considerations

QuackOps is designed with security in mind, but there are important considerations for using it in production environments:

- **Use Read-Only Kubernetes Users:** For maximum safety, operate with a read-only Kubernetes user when interacting with the cluster through this plugin.

- **Enable Safe Mode:** In production environments, activate the `--safe-mode` option to ensure all commands are manually reviewed before execution.

- **Data Privacy:** By default, QuackOps filters sensitive data from secrets before sending to LLMs. Disable this only if you understand the implications.

- **Command Restrictions:** The tool prevents execution of potentially destructive commands. Configure additional blocked commands with the `QU_KUBECTL_BLOCKED_CMDS_EXTRA` environment variable.

- **Local Models:** For sensitive environments, use Ollama with local models to ensure your cluster data never leaves your infrastructure.

## 🧰 Troubleshooting

- `kubectl quackops: command not found`: ensure the binary is on your `PATH`, then re-run `kubectl plugin list`.
- Provider auth errors: export the right API key (`OPENAI_API_KEY`, `GOOGLE_API_KEY`, etc.) and try `--verbose` for more detail.
- Ollama connection errors: verify `ollama serve` is running and `QU_OLLAMA_BASE_URL` matches the server URL.
- MCP strict failures: drop `--mcp-strict` or update your `~/.quackops/mcp.json` so servers can start.

## 📊 Benchmarking

QuackOps includes a comprehensive benchmarking tool for evaluating and comparing LLM provider performance on Kubernetes troubleshooting tasks.

**📖 [Benchmark Documentation](docs/benchmark.md)** - Complete technical reference for the benchmarking functionality

### Quick Example
```bash
# Build benchmark tool
make build-benchmark

# Compare models across providers
./kubectl-quackops-benchmark --models=openai/gpt-5-mini,google/gemini-2.5-flash --iterations=5

# Generate detailed markdown report
./kubectl-quackops-benchmark --models=openai/moonshotai/kimi-k2:free --complexity=simple --format=markdown --output=results.md
```

## 🤝 Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for details on how to get started.

## 📜 License

This project is licensed under the MIT License.
