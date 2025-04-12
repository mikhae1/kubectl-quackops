# Contributing to Kubectl-QuackOps

Thank you for your interest in contributing to Kubectl-QuackOps! This document provides guidelines and instructions for contributing to the project.

## 🔍 Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Development Workflow](#development-workflow)
- [Testing](#testing)
- [Submitting Changes](#submitting-changes)
- [Release Process](#release-process)

## Code of Conduct

We expect all contributors to follow our Code of Conduct. Please be respectful and considerate of others when contributing to the project.

## Development Setup

### Prerequisites

- Go 1.23 or later
- A Kubernetes cluster for testing (minikube, kind, or a remote cluster)
- One of the supported LLM providers:
  - Ollama (for local development)
  - OpenAI API key
  - Google AI API key
  - Anthropic API key

### Setting Up Your Development Environment

1. **Fork and clone the repository**:
   ```bash
   git clone https://github.com/YOUR-USERNAME/kubectl-quackops.git
   cd kubectl-quackops
   ```

2. **Install Go dependencies**:
   ```bash
   go mod download
   ```

3. **Build the project**:
   ```bash
   make build
   ```

4. **Run the binary**:
   ```bash
   make run
   ```

### Environment Variables

For development, you can set these environment variables:

```bash
# For debugging
export DEBUG=1

# For using different LLM providers
export OPENAI_API_KEY=your_api_key
export GOOGLE_API_KEY=your_api_key
export ANTHROPIC_API_KEY=your_api_key
export QU_LLM_PROVIDER=ollama  # or openai, google, anthropic
export QU_LLM_MODEL=llama3.1   # or any other model supported by your provider
```

## Project Structure

The project is organized as follows:

```
kubectl-quackops/
├── .github/           # GitHub-related files (actions, workflows)
├── bin/               # Build binaries
├── cmd/               # Application entry points
│   └── kubectl-quackops.go  # Main entry point
├── pkg/               # Core packages
│   ├── animator/      # Terminal animation utilities
│   ├── cmd/           # Command implementations
│   ├── config/        # Configuration management
│   ├── formatter/     # Output formatting
│   └── logger/        # Logging utilities
├── .krew.yaml         # Krew plugin manifest
├── go.mod             # Go module definition
├── go.sum             # Go module checksums
├── LICENSE            # Project license
├── Makefile           # Build commands
└── README.md          # Project documentation
```

## Development Workflow

### Making Changes

1. **Create a feature branch**:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes**: Implement your feature or fix a bug.

3. **Test your changes**: Ensure your changes work as expected.

4. **Commit your changes**:
   ```bash
   git commit -m "Brief description of your changes"
   ```

### Code Style

- Follow Go best practices and idiomatic Go
- Use meaningful variable and function names
- Add comments for complex logic
- Keep functions concise and focused on a single responsibility

### Common Development Tasks

The `Makefile` provides several useful commands:

- `make build` - Build the binary
- `make run` - Build and run the binary
- `make check-pods` - Test pod-related functionality
- `make check-deployments` - Test deployment-related functionality
- `make check-ingress` - Test ingress-related functionality
- `make check-network` - Test network-related functionality
- `make check-cluster` - Test cluster-related functionality

## Testing

### Local Testing

Before submitting a PR, make sure to test your changes locally:

1. **Build and run the binary**:
   ```bash
   make build
   ./kubectl-quackops
   ```

2. **Test with different LLM providers**: If your changes affect LLM integration, test with different providers to ensure compatibility.

3. **Test with different Kubernetes resources**: Test your changes with various Kubernetes resource types to ensure broad compatibility.

### Testing Specific Features

To test specific functionality, you can use the dedicated make targets:

```bash
make check-pods
make check-deployments
make check-ingress
make check-storage
make check-network
make check-cluster
```

## Submitting Changes

### Pull Requests

1. **Push your changes to your fork**:
   ```bash
   git push origin feature/your-feature-name
   ```

2. **Create a pull request** against the `main` branch of the original repository.

3. **Include in your PR**:
   - A clear description of the changes
   - Any relevant issue numbers
   - Details of how to test your changes

### PR Review Process

1. Maintainers will review your PR as soon as possible
2. Address any comments or requested changes
3. Once approved, your PR will be merged into the main branch

## Release Process

Releases are managed by the project maintainers. The process typically includes:

1. **Version Bump**: Updating version numbers in relevant files
2. **Changelog Update**: Documenting changes in the release notes
3. **Tag Creation**: Creating a Git tag for the new version
4. **Binary Building**: Building and publishing binaries for different platforms
5. **Krew Plugin Update**: Updating the Krew plugin index

## Additional Resources

- [Go Documentation](https://golang.org/doc/)
- [Kubernetes Documentation](https://kubernetes.io/docs/home/)
- [Cobra Command Library](https://github.com/spf13/cobra)
- [LangChain Go Documentation](https://github.com/tmc/langchaingo)

## Questions?

If you have any questions or need help with your contribution, please open an issue on GitHub.

Thank you for contributing to Kubectl-QuackOps!
