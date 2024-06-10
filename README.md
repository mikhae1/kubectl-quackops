# Kubectl-QuackOps

<img src=".github/quackops-logo.png" alt="QuackOps Logo" width="180" align="left" hspace="20">

**QuackOps** is a Kubectl plugin that leverages the power of AI to simplify your Kubernetes troubleshooting and management tasks.

Tired of sifting through endless logs and documentation? QuackOps acts as your personal Kubernetes AI assistant, allowing you to interact with your cluster using natural language. Just describe your issue or request, and QuackOps will provide intelligent insights, suggest relevant commands, etc.

QuackOps is optimized to integrate smoothly with small local models like [llama3](https://ollama.com/library/llama3) while also providing robust scalability for larger LLMs.

## Features

- **Natural Language Interface:** Interact with your cluster naturally. Ask questions and receive context-aware assistance through interactive chats or single queries.
- **AI-Powered Suggestions:** The tool analyzes your requests, cluster state, and leverages power of selected LLM to offer intelligent debugging suggestions and solutions.
- **Automated Command Execution:** Streamline your workflow with automated execution of whitelisted `kubectl` commands. The tool maintains context and uses command outputs to provide accurate assistance.
- **Direct Command Execution:** Execute arbitrary commands directly within the chat interface using the `$` prefix (e.g., `$ kubectl get pods`). The tool integrates the output into its responses for a seamless experience.
- **Safe Command Execution:**  Review and approve suggested `kubectl` commands before execution with `--safe-mode`. This ensures you maintain control and prevent accidental changes to your cluster.
- **Supported LLM Providers:** Choose your preferred LLM provider, currently [Ollama](https://ollama.com/) and [OpenAI](https://openai.com/).

## Example

```sh
$ kubectl quackops 'my pod is not working'

Based on the output of the `kubectl get pods` command,
the pod named `my-nginx-ingress-hello-world-64f78448bd-v567q`
is not working. The `STATUS` for this pod is `ImagePullBackOff`,
indicating that there is an issue pulling the image required
for this pod to run.
```

## Installation

QuackOps is packaged as a kubectl plugin, which is a standalone executable file whose name begins with `kubectl-`.
You can install it by moving the executable file to any directory included in your `$PATH`.

### Manual installation

1. Download the QuackOps binary
Head over to the [GitHub releases page](https://github.com/mikhae1/kubectl-quackops/releases) and download the latest release archive suitable for your operating system (e.g., `kubectl-quackops-linux-amd64.tar.gz`)

1. Extract the binary
Use the following command to extract the binary from the downloaded archive:
    ```sh
    tar -xzf ~/Downloads/kubectl-quackops-linux-amd64.tar.gz -C ~/Downloads
    ```
This will extract the `kubectl-quackops` binary to your `~/Downloads` directory.

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

Summon the quack:

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

1. Download a Model (e.g., llama3):
    ```sh
    ollama pull llama3
    ```

1. Start ollama server:
    ```sh
    ollama serve
    ```

1. Start interactive chat:

    ```sh
    kubectl quackops -p ollama -m llama3
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
    kubectl quackops -p openai -m gpt-3.5-turbo -x 4096
    > ...
    ```

## Configuration

The following environment variables can be used to configure the tool:

- **OPENAI_API_KEY**: OpenAI API key. Obtain it here: https://platform.openai.com/api-keys
- **QU_LLM_MODEL**: The name of the LLM model to use.
- **QU_LLM_PROVIDER**: The LLM platform to use. Can be either `ollama` or `openai`.
- **QU_OLLAMA_HOST**: The address of the Ollama server.
- **QU_KUBECTL_BLOCKED_CMDS_EXTRA**: Additional commads to block.
- **DEBUG**: Enables debug logging.

## Safety

For enhanced security, it is advisable to operate with a read-only Kubernetes user when interacting with the cluster through this plugin. In production environments, it is crucial to activate the `--safe-mode` option to ensure operational safety.

## License

This project is licensed under the MIT License.
