## Changelog

### v2.1.0 — 2025-12-09

- Add local `krew` setup
- Update readline dependency and refactor related code
- Add session history recording, `/history` command, and Ctrl-R for detailed last command/partial interaction view
- Add diagnostic finding priorities
- Add visual context progress bar
- Add MCP tools filtering by prompt server


### v2.0.2 — 2025-12-06

- Improve escape sequence handling in keyboard input


### v2.0.1 — 2025-12-06

- Update Makefile and GitHub Actions to streamline release process and upgrade Go version


### v2.0.0 — 2025-12-06

- Update README.md to include quickstart guide, common workflows, and troubleshooting tips
- Enhance Markdown formatting and improve code block detection
- Refactor LLM request handling to support separate system and user prompts
- Bump: command prefix change from '$' to '!' for improved user interaction
- Enhance MCP prompt handling and user query integration
- Update Go version and dependencies, migrate to Google genai lib
- Update dependencies, enhance MCP configuration, and introduce wave animation demo
- Improve user cancellation handling and ESC key management
- Refactor spinner implementation and remove unused dependency


### v1.8.0 — 2025-08-24

- **Added**
  - Add benchmarking system with dedicated benchmark command (`kubectl-quackops-benchmark`) for evaluating LLM provider performance on Kubernetes troubleshooting tasks.
  - Comprehensive metrics collection and reporting capabilities with support for multiple output formats (table, JSON, CSV, markdown).
  - Cost tracking and quality evaluation features for LLM benchmarking.
  - Support for kubectl command generation testing in benchmarks.
  - New Makefile targets (`build-benchmark`, `test-benchmark`) for building and running benchmarks.

### v1.7.0 — 2025-08-23

- **Added**
  - Support for openrouter.ai LLM models and API settings.
  - Auto-detection of max tokens based on model metadata.
  - Comprehensive test suite with unit, integration, and end-to-end tests.

- **Enhanced**
  - Configuration management with support for new fields (Ollama API URL, token management).
  - Error handling and logging infrastructure.
  - LLM chat functionality and client management.

- **Fixed**
  - Temporary files now excluded from version control via .gitignore.
  - Various improvements to LLM provider handling and metadata service.

### v1.6.0 — 2025-08-19

- **Added**
  - MCP client mode support.
  - MCP logging functionality.
  - Diagnostic analysis and baseline command findings feature.
  - Diagnostic flags.
  - Real-time token tracking for Gemini.
  - Animations in chat prompts.

- **Changed / Enhanced**
  - Configuration management with support for config files; updated README and configuration for LLM models and throttling settings.
  - LLM request handling with improved throttling and error management; implemented request throttling mechanism.
  - Slash command functionality and help display.
  - Diagnostic result rendering.
  - History management in chat sessions; chat session features and prompt handling.
  - Command execution flow, user interaction, and command filtering in `ExecDiagCmds` (including strict MCP mode).
  - Dependencies updated.
  - Welcome banner output.
  - Default history file path in README and configuration files.
  - Environment variable handling (moved documentation to README).

- **Fixed**
  - Baseline diagnostic prompts and command handling.
  - Chat session prompt handling and token calculation.
  - Global signal handling and cleanup.

- **Refactored / Removed**
  - Refactor chat session context clearing; general prompt handling.
  - Remove `EnvVarInfo` struct and `GetEnvVarsInfo` function.
  - Remove redundant return in `startChatSession`.

### v1.5.0 — 2025-04-20

- **Release**
  - Release v1.5.0.

- **Build / CI**
  - Enhance version validation in release workflow; update default version in release workflow.
  - Remove linting.

- **Docs**
  - Update README on interactive sessions, security features, and shell completions.

### Pre v1.5.0 (2024-12-26 → 2025-04-13)

- **Added**
  - Command completion features for `quackops`.
  - TypewriterWriter for animated text output.
  - Markdown formatting support for LLM responses.
  - Spinner support for command execution and LLM responses; spinner timeout configuration for diagnostic commands and LLM responses.

- **Changed / Enhanced**
  - Interactive prompt in `quackops`; readline prompt color.
  - TypewriterWriter delay.
  - LLM request handling consolidated to a common function; enhanced Google AI request handling.
  - MarkdownFormatter color handling.
  - Command processing.

- **Removed**
  - Goldmark dependency; Markdown formatter documentation and example code.

- **Build / Libs**
  - Migrate to `langchaingo` library.

### 2024-12-28

- **Added**
  - Anthropic provider support.

- **Changed / Enhanced**
  - New AI capabilities and improved configuration.
  - README: clarify LLM provider support and improve example command.

### 2024-12-26

- **Changed / Enhanced**
  - Update `lingoose` to v0.3.0 and add Kubernetes context display.

### 2024-08-08 → 2024-08-09

- **Added**
  - Llama 3.1 configuration.

- **Changed / Fixed**
  - Update README examples with recent LLM versions.
  - Remove unnecessary debug statement.

### 2024-06-01 → 2024-06-15

- **Added**
  - Filtering; improved logging and error handling.
  - Project logo.

- **Changed**
  - Improve docs; update logo and timeout in config.

- **Fixed**
  - Typos.

### 2024-05-29

- **Initial release**
  - Initial release of `kubectl-quackops`; README published.
