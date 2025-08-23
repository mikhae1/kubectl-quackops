## Changelog

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

