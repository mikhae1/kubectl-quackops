# Kubectl-QuackOps

<img src=".github/quackops-logo.png" alt="QuackOps Logo" width="180" align="left" hspace="20">

**QuackOps** is a powerful `kubectl` plugin that transforms your Kubernetes troubleshooting experience through AI-powered assistance. It serves as your smart companion, translating natural language queries into actionable insights and commands.

Whether you're debugging a failing pod, optimizing resource usage, or seeking best practices, QuackOps streamlines your workflow by combining the precision of `kubectl` with the intuitive understanding of AI. Simply describe what you need, and let QuackOps guide you through your Kubernetes operations.

Built with flexibility in mind, QuackOps works seamlessly with various LLM providers to suit your needs. Whether you prioritize privacy with local models like [llama3](https://ollama.com/library/llama3.1) or seek advanced capabilities with cloud-based LLMs, QuackOps has you covered.

## Features

- **Natural Language Interface:** Interact with your cluster naturally. Ask questions and receive context-aware assistance through interactive chats or single queries.
- **AI-Powered Suggestions:** The tool analyzes your requests, cluster state, and leverages the power of selected LLM to offer intelligent debugging suggestions and solutions.
- **Automated Command Execution:** Streamlines your workflow by automatically executing whitelisted `kubectl` commands. The tool maintains context and uses command outputs to provide accurate assistance.
- **Direct Command Execution:** Execute arbitrary commands directly within the chat interface using the `$` prefix (e.g., `$ kubectl get pods`). The tool integrates the output into its responses for a seamless experience.
- **Safe Command Execution:** By default, sensitive data is not transmitted to language models. Enable `--safe-mode` to manually review and approve any suggested `kubectl` commands before they are executed. This feature ensures that you retain full control over your cluster and helps prevent unintended modifications.
- **Supported LLM Providers:**
  - [Ollama](https://ollama.com/) - For local execution and data privacy
  - [Google](https://gemini.google.com/) - For large context windows
  - [OpenAI](https://openai.com/) - For access to cutting-edge language models
  - [Anthropic](https://anthropic.com/) - For reliable technical analysis and clear explanations

## Example

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

## Installation

QuackOps is packaged as a kubectl plugin, which is a standalone executable file whose name begins with `kubectl-`.
You can install it by moving the executable file to any directory included in your `$PATH`.

1. Download the QuackOps binary
Head over to the [GitHub releases page](https://github.com/mikhae1/kubectl-quackops/releases) and download the latest release archive suitable for your operating system (e.g., `kubectl-quackops-linux-amd64.tar.gz`)

1. Extract the binary
Use the following command to extract the binary from the downloaded archive:
    ```sh
    tar -xzf ~/Downloads/kubectl-quackops-linux-amd64.tar.gz -C ~/Downloads
    ```

1. Make the binary executable (if needed):
    ```sh
    chmod +x ~/Downloads/kubectl-quackops
    ```

1. Move the binary to your `$PATH`:
Move the `kubectl-quackops` executable to a directory included in your system's `$PATH`, such as `/usr/local/bin`:
    ```sh
    mv ~/Downloads/kubectl-quackops /usr/local/bin/kubectl-quackops
    ```

1. Verify the installation:
Confirm that QuackOps is recognized as a kubectl plugin by running:
    ```sh
    kubectl plugin list
    ```

Summon the quAck:

```sh
$ kubectl quackops
```

## Usage

QuackOps offers flexible options to tailor your Kubernetes troubleshooting experience.
Choose the LLM provider that best suits your needs.

### Ollama: Privacy and Control

For maximum data security, leverage the power of local LLMs with [Ollama](https://ollama.com/).

**Benefits:**

* **Data Sovereignty:**  Keep your cluster information confidential. Data remains within your environment, enhancing privacy.
* **Enhanced Security:** Maintain complete control over access and security protocols for your Kubernetes data.

**Getting Started:**

1. **Install Ollama:** Download and install Ollama from [https://ollama.com/download](https://ollama.com/download).

1. Start ollama server:
    ```sh
    ollama serve
    ```

1. Download local LLM model (e.g., llama3.1):
    ```sh
    ollama pull llama3.1
    ```

1. Start interactive chat:
    ```sh
    kubectl quackops -p ollama -m llama3.1
    ```

### OpenAI: Cutting-Edge AI models

For users seeking the most advanced AI capabilities.

- **Access the Latest Models**: Leverage the latest advancements in LLMs, constantly updated and refined by OpenAI.
- **Smart Insight**: OpenAI's models enable you to debug even the most complicated cases.

**Getting Started:**

1. **Obtain an API Key:** Get your OpenAI API key at [https://platform.openai.com/api-keys](https://platform.openai.com/api-keys).

1. **Set the API Key:**
    ```sh
    export OPENAI_API_KEY=<YOUR-OPENAI-API-KEY>
    ```

1. **Start QuackOps:**
    ```sh
    kubectl quackops -p openai -m gpt-4o -x 4096
    ```

### Google: Large Context Window

For users requiring extensive context analysis and handling large command outputs.

**Benefits:**
- **Largest Context Window:** Process more information at once with Gemini's 1M+ token context window
- **Efficient RAG:** Superior handling of long command outputs and cluster state analysis
- **Cost-Effective:** Competitive pricing for enterprise-grade AI capabilities

**Getting Started:**

1. **Obtain an API Key:** Get your Google AI API key at [https://makersuite.google.com/app/apikey](https://makersuite.google.com/app/apikey)

1. **Set the API Key:**
    ```sh
    export GOOGLE_API_KEY=<YOUR-GOOGLE-API-KEY>
    ```

1. **Start QuackOps:**
    ```sh
    kubectl quackops -p google -m gemini-2.0-flash-exp
    ```

## Configuration

The following options can be configured through environment variables or command-line flags:

### Environment Variables

- `OPENAI_API_KEY` - OpenAI API key ([get here](https://platform.openai.com/api-keys))
- `GOOGLE_API_KEY` - Google AI API key ([get here](https://makersuite.google.com/app/apikey))
- `QU_LLM_PROVIDER` - LLM platform to use (`ollama`, `openai`, or `google`)
- `QU_LLM_MODEL` - Name of the LLM model to use
- `QU_OLLAMA_HOST` - Address of the Ollama server
- `QU_KUBECTL_BLOCKED_CMDS_EXTRA` - Additional commands to block
- `DEBUG` - Enable debug logging

### Command-Line Flags

- `-p, --provider` - LLM model provider (e.g., 'ollama', 'openai', 'google')
- `-m, --model` - LLM model to use
- `-u, --api-url` - URL for LLM API (used with 'ollama' provider)
- `-s, --safe-mode` - Enable safe mode to prevent executing commands without confirmation
- `-r, --retries` - Number of retries for kubectl commands
- `-t, --timeout` - Timeout for kubectl commands in seconds
- `-x, --max-tokens` - Maximum number of tokens in LLM context window
- `-v, --verbose` - Enable verbose output
- `-c, --disable-secrets-filter` - Disable filtering sensitive data in secrets from being sent to LLMs

## Safety

For enhanced security, it is advisable to operate with a read-only Kubernetes user when interacting with the cluster through this plugin. In production environments, it is crucial to activate the `--safe-mode` option to ensure operational safety.

## License

This project is licensed under the MIT License.
