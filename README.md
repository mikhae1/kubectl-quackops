# Kubectl-QuackOps

**QuackOps** is a Kubectl plugin that leverages the power of AI to simplify your Kubernetes troubleshooting and management tasks.

Tired of sifting through endless logs and documentation? QuackOps acts as your personal Kubernetes AI assistant, allowing you to interact with your cluster using natural language. Just describe your issue or request, and QuackOps will provide intelligent insights, suggest relevant commands, etc.

QuackOps is optimized to integrate smoothly with small local models like `llama3` while also providing robust scalability for larger LLMs.

## Features

- **AI Assistance**: Utilizes LLM (Large Language Models) (currently [Ollama](https://ollama.com/) and [OpenAI](https://openai.com/) to provide intelligent suggestions and operations based on user input.
- **Interactive Chat:** Use natural language to ask questions and receive assistance with Kubernetes debugging.
- **Automatic command execution:** The tool can automatically execute whitelisted `kubectl` commands and use results as a context. Plugin consistently adheres to the current Kubernetes config context.
- **Direct command execution:** Use '$' prefix to execute any user command directly and use it's output within the chat.
- **Safe Command Execution:** Review and approve suggested kubectl commands before they are executed with `--safe-mode`.

## Example

```sh
$ kubectl quackops 'why my pod is not working?'

Based on the output of the `kubectl get pods` command, the pod named `my-nginx-ingress-hello-world-64f78448bd-v567q` is not working. The `STATUS` for this pod is `ImagePullBackOff`, indicating that there is an issue pulling the image required for this pod to run.
```

## Installation

A kubectl plugin is a standalone executable file, whose name begins with `kubectl-`.
To install a plugin, move its executable file to anywhere on your `$PATH`.

### Manual installation

1. Download the QuackOps binary
Visit the [GitHub releases page](https://github.com/mikhae1/kubectl-quackops/releases) and download the latest version suitable for your operating system.

1. Extract binary
Use the following command to extract the binary from the downloaded `.tar.gz` file:
    ```sh
    tar -xzf ~/Downloads/kubectl-quackops-linux-amd64.tar.gz -C ~/Downloads
    ```

1. Make the binary executable, if needed:
    ```sh
    chmod +x ~/Downloads/kubectl-quackops
    ```

1. Move the binary to your PATH:
Move the executable to a directory included in your system’s PATH, like `/usr/local/bin`:
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
kubectl quackops
```

## Usage

### Lolcal LLMs

When it comes to the sensitivity of your Kubernetes data, privacy is paramount. That's why we recommend using [Ollama](https://ollama.com/), a self-hosted, open-source LLM platform, with `QuackOps`.

Here's why:
- *Data Stays Yours*: With Ollama, your cluster information never leaves your environment. No need to send sensitive logs or configurations to external servers.
- *Enhanced Security*: Maintain complete control over access and security protocols, ensuring your Kubernetes data remains protected.

To get started with Ollama:
```sh
# pull llama3 model
ollama pull llama3

# start ollama server
$ ollama serve

time=2024-05-23T19:33:25.674+03:00 level=INFO source=routes.go:1143 msg="Listening on 127.0.0.1:11434 (version 0.1.32)"
...

# start interactive chat
$ kubectl quackops -p ollama -m llama3
> ...
```

You can download Ollama here: https://ollama.com/download.


### OpenAI - Tap into Cutting-Edge AI

For users seeking the most advanced AI capabilities, Kubectl-QuackOps seamlessly integrates with OpenAI's powerful APIs.

- **Access the Latest Models**: Leverage the latest advancements in LLMs, constantly updated and refined by OpenAI.
- **Smart Insight**: OpenAI's models enable to debug the most complicated cases.

```sh
export OPENAI_API_KEY=...
$ kubectl quackops
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
