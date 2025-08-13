package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	jsonschema "github.com/modelcontextprotocol/go-sdk/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

// Minimal MCP client facade that supports calling external tools by spawning stdio servers or using HTTP endpoints
// For the first iteration we only support running commands via a local shell using configured tools: kubectl and bash

type ServerSpec struct {
	Name    string            `yaml:"name" json:"name"`
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args" json:"args"`
	URL     string            `yaml:"url" json:"url"`
	Env     map[string]string `yaml:"env" json:"env"`
	Auth    *struct {
		Type  string `yaml:"type" json:"type"`
		Token string `yaml:"token" json:"token"`
	} `yaml:"auth" json:"auth"`
}

type ConfigFile struct {
	Servers    []ServerSpec           `yaml:"servers" json:"servers"`
	MCPServers map[string]*ServerSpec `yaml:"mcpServers" json:"mcpServers"`
}

// ToolInfo represents a discovered MCP tool and its input schema
type ToolInfo struct {
	Name        string
	Title       string
	Description string
	InputSchema *jsonschema.Schema
}

// ServerConnection represents a connection to an MCP server
type ServerConnection struct {
	Spec            *ServerSpec
	Session         *sdkmcp.ClientSession
	Tools           []string
	ToolInfos       []ToolInfo
	Connected       bool
	LastError       error
	LastHealthCheck time.Time
	Process         *exec.Cmd
	ctx             context.Context
	cancel          context.CancelFunc
}

// ServerRegistry manages multiple MCP server connections
type ServerRegistry struct {
	mu              sync.RWMutex
	servers         map[string]*ServerConnection
	toolToServer    map[string]*ServerConnection
	sessionToServer map[*sdkmcp.ClientSession]*ServerConnection
}

var (
	// global state
	loaded   bool
	cfgDoc   ConfigFile
	started  bool
	registry *ServerRegistry
)

func loadOnce(path string) {
	if loaded {
		return
	}
	loaded = true
	// Attempt path provided, then fallback to defaults
	tryPaths := []string{}
	if path != "" {
		tryPaths = append(tryPaths, path)
	}
	if home, err := os.UserHomeDir(); err == nil {
		tryPaths = append(tryPaths, filepath.Join(home, ".config", "quackops", "mcp.yaml"))
		tryPaths = append(tryPaths, filepath.Join(home, ".quackops", "mcp.json"))
	}
	for _, p := range tryPaths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			// Detect by extension, else by first non-space char
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".json" || (len(data) > 0 && strings.HasPrefix(strings.TrimSpace(string(data)), "{")) {
				if err := json.Unmarshal(data, &cfgDoc); err == nil {
					normalizeServers()
					return
				}
			} else {
				if err := yaml.Unmarshal(data, &cfgDoc); err == nil {
					normalizeServers()
					return
				}
			}
		}
	}
}

// normalizeServers converts mcpServers map format to servers array format
func normalizeServers() {
	if len(cfgDoc.MCPServers) > 0 {
		for name, server := range cfgDoc.MCPServers {
			if server != nil {
				// Set the name if not already set
				if server.Name == "" {
					server.Name = name
				}
				cfgDoc.Servers = append(cfgDoc.Servers, *server)
			}
		}
		// Clear the map to avoid confusion
		cfgDoc.MCPServers = nil
	}
}

// NewServerRegistry creates a new server registry
func NewServerRegistry() *ServerRegistry {
	return &ServerRegistry{
		servers:         make(map[string]*ServerConnection),
		toolToServer:    make(map[string]*ServerConnection),
		sessionToServer: make(map[*sdkmcp.ClientSession]*ServerConnection),
	}
}

// AddServer adds a server connection to the registry
func (r *ServerRegistry) AddServer(name string, conn *ServerConnection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[name] = conn
	// Update tool mapping
	for _, tool := range conn.Tools {
		r.toolToServer[tool] = conn
	}
	if conn.Session != nil {
		r.sessionToServer[conn.Session] = conn
	}
}

// GetServer retrieves a server connection by name
func (r *ServerRegistry) GetServer(name string) (*ServerConnection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	conn, exists := r.servers[name]
	return conn, exists
}

// GetServerForTool retrieves the server that provides a specific tool
func (r *ServerRegistry) GetServerForTool(tool string) (*ServerConnection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	conn, exists := r.toolToServer[tool]
	return conn, exists
}

// GetAllTools returns all available tools from all connected servers
func (r *ServerRegistry) GetAllTools() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]string, 0, len(r.toolToServer))
	for tool := range r.toolToServer {
		tools = append(tools, tool)
	}
	return tools
}

// GetAllToolInfos returns all available tools with their descriptions
func (r *ServerRegistry) GetAllToolInfos() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var allToolInfos []ToolInfo
	for _, conn := range r.servers {
		if conn.Connected {
			allToolInfos = append(allToolInfos, conn.ToolInfos...)
		}
	}
	return allToolInfos
}

// GetConnectedServers returns a list of connected server names
func (r *ServerRegistry) GetConnectedServers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	servers := make([]string, 0, len(r.servers))
	for name, conn := range r.servers {
		if conn.Connected {
			servers = append(servers, name)
		}
	}
	return servers
}

// CleanupAll stops all server connections
func (r *ServerRegistry) CleanupAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, conn := range r.servers {
		if conn.cancel != nil {
			conn.cancel()
		}
		if conn.Process != nil {
			conn.Process.Process.Kill()
		}
	}
	r.servers = make(map[string]*ServerConnection)
	r.toolToServer = make(map[string]*ServerConnection)
	r.sessionToServer = make(map[*sdkmcp.ClientSession]*ServerConnection)
}

// HealthCheckAll performs health checks on all connected servers
func (r *ServerRegistry) HealthCheckAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, conn := range r.servers {
		if conn.Connected {
			go r.healthCheckServer(name, conn)
		}
	}
}

// healthCheckServer performs a health check on a single server
func (r *ServerRegistry) healthCheckServer(name string, conn *ServerConnection) {
	// Skip if health check was done recently (within last 30 seconds)
	if time.Since(conn.LastHealthCheck) < 30*time.Second {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use MCP ping for health check
	err := conn.Session.Ping(ctx, &sdkmcp.PingParams{})

	r.mu.Lock()
	defer r.mu.Unlock()

	conn.LastHealthCheck = time.Now()
	if err != nil {
		logger.Log("warn", "[MCP] Health check failed for server %s: %v", name, err)
		conn.Connected = false
		conn.LastError = err
		// Remove tools from mapping
		for tool := range r.toolToServer {
			if r.toolToServer[tool] == conn {
				delete(r.toolToServer, tool)
			}
		}
	} else {
		logger.Log("info", "[MCP] Health check passed for server %s", name)
		conn.LastError = nil
		if !conn.Connected {
			// Server recovered, re-add tools
			conn.Connected = true
			for _, tool := range conn.Tools {
				r.toolToServer[tool] = conn
			}
		}
	}
}

// GetServerHealth returns health status of all servers
func (r *ServerRegistry) GetServerHealth() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := make(map[string]bool)
	for name, conn := range r.servers {
		health[name] = conn.Connected
	}
	return health
}

// ReconnectFailed attempts to reconnect to failed servers
func (r *ServerRegistry) ReconnectFailed(cfg *config.Config) {
	r.mu.Lock()
	failedServers := make([]*ServerConnection, 0)
	for _, conn := range r.servers {
		if !conn.Connected && conn.Spec != nil {
			failedServers = append(failedServers, conn)
		}
	}
	r.mu.Unlock()

	if len(failedServers) == 0 {
		return
	}

	logger.Log("info", "[MCP] Attempting to reconnect %d failed server(s)", len(failedServers))

	for _, conn := range failedServers {
		go r.attemptReconnect(conn, cfg)
	}
}

// attemptReconnect tries to reconnect a single failed server
func (r *ServerRegistry) attemptReconnect(conn *ServerConnection, cfg *config.Config) {
	if conn.Spec == nil {
		return
	}

	name := conn.Spec.Name
	if name == "" {
		name = conn.Spec.Command
	}

	logger.Log("info", "[MCP] Attempting to reconnect server %s", name)

	// Create new connection context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Clean up old resources
	if conn.cancel != nil {
		conn.cancel()
	}
	if conn.Process != nil {
		conn.Process.Process.Kill()
	}

	// Create new command
	cmd := exec.CommandContext(ctx, conn.Spec.Command, conn.Spec.Args...)
	if conn.Spec.Env != nil {
		env := os.Environ()
		for k, v := range conn.Spec.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}

	// Update connection
	conn.Process = cmd
	conn.ctx = ctx
	conn.cancel = cancel

	// Attempt connection
	transport := sdkmcp.NewCommandTransport(cmd)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "quackops-mcp-client",
		Version: "v0.1.0",
	}, &sdkmcp.ClientOptions{
		ToolListChangedHandler: func(ctx context.Context, cs *sdkmcp.ClientSession, p *sdkmcp.ToolListChangedParams) {
			// Refresh tools for this session
			r.refreshToolsForSession(cs)
		},
		KeepAlive: 30 * time.Second,
	})

	sess, err := client.Connect(ctx, transport)
	if err != nil {
		logger.Log("warn", "[MCP] Failed to reconnect server %s: %v", name, err)
		r.mu.Lock()
		conn.LastError = err
		conn.Connected = false
		r.mu.Unlock()
		return
	}

	// Success - update connection
	r.mu.Lock()
	conn.Session = sess
	conn.Connected = true
	conn.LastError = nil
	conn.LastHealthCheck = time.Now()
	r.sessionToServer[sess] = conn

	// Re-discover tools from the server
	toolInfos := discoverToolInfos(sess)
	conn.ToolInfos = toolInfos

	// Extract tool names for backward compatibility
	var tools []string
	for _, tool := range toolInfos {
		tools = append(tools, tool.Name)
	}
	conn.Tools = tools

	// Re-add tools to mapping
	for _, tool := range tools {
		r.toolToServer[tool] = conn
	}
	r.mu.Unlock()

	logger.Log("info", "[MCP] Successfully reconnected server %s with %d tools", name, len(tools))
}

// Tools returns a list of tool names from all connected servers
func Tools(cfg *config.Config) []string {
	loadOnce(cfg.MCPConfigPath)
	if registry == nil {
		// Return empty list if registry not initialized
		return []string{}
	}
	return registry.GetAllTools()
}

// GetToolInfos returns all available tools with descriptions
func GetToolInfos(cfg *config.Config) []ToolInfo {
	loadOnce(cfg.MCPConfigPath)
	if registry == nil {
		return []ToolInfo{}
	}
	return registry.GetAllToolInfos()
}

// Servers returns configured MCP server names for display
func Servers(cfg *config.Config) []string {
	loadOnce(cfg.MCPConfigPath)
	if registry == nil {
		return []string{}
	}

	var out []string
	for _, s := range cfgDoc.Servers {
		label := s.Name
		if label == "" {
			label = s.Command
		}
		if label == "" {
			label = s.URL
		}
		if label == "" {
			continue
		}
		out = append(out, label)
	}
	return out
}

// GetConnectedServerNames returns only the names of connected servers
func GetConnectedServerNames(cfg *config.Config) []string {
	loadOnce(cfg.MCPConfigPath)
	if registry == nil {
		return []string{}
	}
	return registry.GetConnectedServers()
}

// Start initializes MCP client mode with parallel server startup
func Start(cfg *config.Config) error {
	if started {
		return nil
	}
	loadOnce(cfg.MCPConfigPath)

	// Initialize registry
	registry = NewServerRegistry()

	if len(cfgDoc.Servers) == 0 {
		logger.Log("info", "[MCP] No servers configured")
		started = true
		return nil
	}

	logger.Log("info", "[MCP] Starting %d server(s) in parallel", len(cfgDoc.Servers))

	// Start all servers in parallel goroutines
	var wg sync.WaitGroup
	for i, server := range cfgDoc.Servers {
		wg.Add(1)
		go func(idx int, s ServerSpec) {
			defer wg.Done()
			startServer(idx, s, cfg)
		}(i, server)
	}

	// Wait for all servers to finish startup attempts
	wg.Wait()

	connectedCount := len(registry.GetConnectedServers())
	logger.Log("info", "[MCP] Startup complete: %d/%d servers connected", connectedCount, len(cfgDoc.Servers))

	started = true
	return nil
}

// startServer attempts to start a single MCP server according to requirements:
// Start server with exec.Command and mcp.NewCommandTransport.
// mcp.NewClient(...).Connect(ctx, transport) → *ClientSession.
// ListTools and cache: name, description/title, InputSchema.
func startServer(idx int, spec ServerSpec, cfg *config.Config) {
	name := spec.Name
	if name == "" {
		name = fmt.Sprintf("server-%d", idx)
	}

	if spec.Command == "" {
		logger.Log("warn", "[MCP] Server %s has no command specified", name)
		return
	}

	logger.Log("info", "[MCP] Starting server %s: %s %s", name, spec.Command, strings.Join(spec.Args, " "))

	// Create connection context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	conn := &ServerConnection{
		Spec:      &spec,
		Connected: false,
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start server with exec.Command as specified in requirements
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	if spec.Env != nil {
		env := os.Environ()
		for k, v := range spec.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}
	conn.Process = cmd

	// Create transport using mcp.NewCommandTransport as specified
	transport := sdkmcp.NewCommandTransport(cmd)

	// Create client with enhanced implementation info
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "kubectl-quackops-mcp-client",
		Version: "v0.2.0",
	}, &sdkmcp.ClientOptions{
		ToolListChangedHandler: func(ctx context.Context, cs *sdkmcp.ClientSession, p *sdkmcp.ToolListChangedParams) {
			logger.Log("info", "[MCP] Tool list changed for server %s, refreshing tools", name)
			// Refresh tools for this session
			registry.refreshToolsForSession(cs)
		},
		KeepAlive: 30 * time.Second,
	})

	// Connect to create ClientSession as specified in requirements
	sess, err := client.Connect(ctx, transport)
	if err != nil {
		logger.Log("warn", "[MCP] Failed to connect to server %s: %v", name, err)
		conn.LastError = err
		conn.Connected = false
		registry.AddServer(name, conn)
		cancel() // Clean up context since connection failed
		return
	}

	conn.Session = sess
	conn.Connected = true
	conn.LastError = nil
	conn.LastHealthCheck = time.Now()

	// ListTools and cache: name, description/title, InputSchema as specified
	toolInfos := discoverAndCacheToolInfos(sess, name)
	conn.ToolInfos = toolInfos

	// Extract tool names for backward compatibility
	var tools []string
	for _, tool := range toolInfos {
		tools = append(tools, tool.Name)
	}
	conn.Tools = tools

	// Add to registry
	registry.AddServer(name, conn)

	logger.Log("info", "[MCP] Successfully connected to server %s with %d tools", name, len(tools))
	for _, tool := range toolInfos {
		logger.Log("debug", "[MCP] Tool: %s - %s", tool.Name, tool.Description)
	}
}

// discoverTools attempts to discover available tools from an MCP server
func discoverTools(session *sdkmcp.ClientSession) []string {
	tools := discoverToolInfos(session)
	var toolNames []string
	for _, tool := range tools {
		toolNames = append(toolNames, tool.Name)
	}
	return toolNames
}

// discoverAndCacheToolInfos discovers and caches tool information with enhanced error handling
func discoverAndCacheToolInfos(session *sdkmcp.ClientSession, serverName string) []ToolInfo {
	logger.Log("info", "[MCP] Discovering tools for server %s", serverName)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use the SDK's paginated Tools iterator to fetch all tools
	var tools []ToolInfo
	toolCount := 0

	for tool, err := range session.Tools(ctx, &sdkmcp.ListToolsParams{}) {
		if err != nil {
			logger.Log("warn", "[MCP] Error during tool discovery for server %s: %v", serverName, err)
			// Stop discovery on error; return what we have
			break
		}
		if tool == nil {
			continue
		}

		toolCount++

		// Build comprehensive tool information
		desc := tool.Description
		title := tool.Title

		// Enhanced fallback logic for descriptions
		if desc == "" && tool.Annotations != nil {
			desc = tool.Annotations.Title
		}
		if title == "" && tool.Annotations != nil {
			title = tool.Annotations.Title
		}
		if desc == "" {
			desc = title
		}
		if desc == "" {
			desc = fmt.Sprintf("Tool: %s", tool.Name)
		}

		// Validate and cache input schema
		var schema *jsonschema.Schema
		if tool.InputSchema != nil {
			schema = tool.InputSchema
			logger.Log("debug", "[MCP] Tool %s has input schema", tool.Name)
		} else {
			logger.Log("debug", "[MCP] Tool %s has no input schema", tool.Name)
		}

		toolInfo := ToolInfo{
			Name:        tool.Name,
			Title:       title,
			Description: desc,
			InputSchema: schema,
		}

		tools = append(tools, toolInfo)
		logger.Log("debug", "[MCP] Cached tool: %s (%s)", tool.Name, desc)
	}

	logger.Log("info", "[MCP] Successfully discovered %d tools for server %s", len(tools), serverName)
	return tools
}

// discoverToolInfos attempts to discover available tools with descriptions from an MCP server
// Kept for backward compatibility
func discoverToolInfos(session *sdkmcp.ClientSession) []ToolInfo {
	return discoverAndCacheToolInfos(session, "unknown")
}

// refreshToolsForSession re-discovers tools for a given session and updates mappings
func (r *ServerRegistry) refreshToolsForSession(session *sdkmcp.ClientSession) {
	r.mu.Lock()
	conn, ok := r.sessionToServer[session]
	r.mu.Unlock()
	if !ok || conn == nil {
		return
	}

	// Discover tools from server
	toolInfos := discoverToolInfos(session)

	r.mu.Lock()
	// Remove existing mappings for this connection
	for tool, srv := range r.toolToServer {
		if srv == conn {
			delete(r.toolToServer, tool)
		}
	}
	// Update connection tools
	conn.ToolInfos = toolInfos
	conn.Tools = nil
	for _, ti := range toolInfos {
		conn.Tools = append(conn.Tools, ti.Name)
		r.toolToServer[ti.Name] = conn
	}
	r.mu.Unlock()

	logger.Log("info", "[MCP] Refreshed tools for server %s: %d tool(s)", conn.Spec.Name, len(conn.Tools))
}

// Stop shuts down all MCP client resources
func Stop() {
	if !started {
		return
	}
	if registry != nil {
		registry.CleanupAll()
	}
	started = false
	logger.Log("info", "[MCP] All servers stopped")
}

// HealthCheck performs health checks on all MCP servers
func HealthCheck() map[string]bool {
	if !started || registry == nil {
		return make(map[string]bool)
	}
	registry.HealthCheckAll()
	return registry.GetServerHealth()
}

// ReconnectFailedServers attempts to reconnect to any failed servers
func ReconnectFailedServers(cfg *config.Config) {
	if !started || registry == nil {
		return
	}
	registry.ReconnectFailed(cfg)
}

// executeToolOnServer executes a tool on a specific MCP server with enhanced content handling
func executeToolOnServer(conn *ServerConnection, toolName string, args map[string]any, timeout int) (string, error) {
	if !conn.Connected || conn.Session == nil {
		return "", fmt.Errorf("server not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	params := &sdkmcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	}

	logger.Log("info", "[MCP] Executing tool %s on server %s with args: %v", toolName, conn.Spec.Name, args)
	res, err := conn.Session.CallTool(ctx, params)
	if err != nil {
		return "", fmt.Errorf("tool call failed: %w", err)
	}

	if res.IsError {
		// Extract error details if available
		errorMsg := "tool returned error"
		if len(res.Content) > 0 {
			var errorDetails strings.Builder
			for _, c := range res.Content {
				if t, ok := c.(*sdkmcp.TextContent); ok {
					if errorDetails.Len() > 0 {
						errorDetails.WriteString(" ")
					}
					errorDetails.WriteString(t.Text)
				}
			}
			if errorDetails.Len() > 0 {
				errorMsg = fmt.Sprintf("tool error: %s", errorDetails.String())
			}
		}
		return "", fmt.Errorf("%s", errorMsg)
	}

	// Enhanced content handling: prefer StructuredContent, then TextContent
	if res.StructuredContent != nil {
		// Pretty print structured content for better readability
		if data, err := json.MarshalIndent(res.StructuredContent, "", "  "); err == nil {
			logger.Log("info", "[MCP] Tool '%s' returned structured content (%d bytes)", toolName, len(data))
			return string(data), nil
		} else {
			logger.Log("warn", "[MCP] Failed to marshal structured content for tool '%s': %v", toolName, err)
		}
	}

	// Process text content with proper formatting
	var contentBuilder strings.Builder
	textItemCount := 0

	for _, c := range res.Content {
		if t, ok := c.(*sdkmcp.TextContent); ok {
			if contentBuilder.Len() > 0 {
				contentBuilder.WriteString("\n")
			}
			contentBuilder.WriteString(t.Text)
			textItemCount++
		}
	}

	if contentBuilder.Len() == 0 {
		return "", fmt.Errorf("tool returned no usable content")
	}

	result := contentBuilder.String()
	logger.Log("info", "[MCP] Tool '%s' returned %d text content item(s), total length: %d", toolName, textItemCount, len(result))

	return result, nil
}

// CallToolByName locates the MCP server for a given tool and executes it with the provided arguments
func CallToolByName(cfg *config.Config, toolName string, args map[string]any) (string, error) {
	loadOnce(cfg.MCPConfigPath)
	if err := ensureToolAllowed(cfg, toolName); err != nil {
		return "", err
	}
	if registry == nil {
		return "", fmt.Errorf("mcp client not initialized")
	}
	conn, found := registry.GetServerForTool(toolName)
	if !found || !conn.Connected {
		return "", fmt.Errorf("tool '%s' not available via MCP", toolName)
	}
	return executeToolOnServer(conn, toolName, args, cfg.MCPToolTimeout)
}

// ExecShellViaMCP executes a raw shell command via bash -lc
func ExecShellViaMCP(cfg *config.Config, shell string) (string, error) {
	loadOnce(cfg.MCPConfigPath)
	if err := ensureToolAllowed(cfg, "bash"); err != nil {
		return "", err
	}

	// Try to execute via MCP servers
	if registry != nil {
		// Look for a server that provides bash
		if conn, found := registry.GetServerForTool("bash"); found && conn.Connected {
			result, err := executeToolOnServer(conn, "bash", map[string]any{"command": shell}, cfg.MCPToolTimeout)
			if err == nil {
				return result, nil
			}
			logger.Log("warn", "[MCP] Failed to execute bash via MCP: %v", err)
		}
	}

	return execViaShell(cfg, shell)
}

func execViaShell(cfg *config.Config, cmd string) (string, error) {
	if strings.TrimSpace(cmd) == "" {
		return "", errors.New("empty command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.MCPToolTimeout)*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "bash", "-lc", cmd)
	out, err := c.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out) + fmt.Sprintf("\n*** COMMAND TIMED OUT AFTER %d SECONDS ***\n", cfg.MCPToolTimeout), fmt.Errorf("mcp tool timed out after %d seconds", cfg.MCPToolTimeout)
	}
	return string(out), err
}

func ensureToolAllowed(cfg *config.Config, tool string) error {
	tool = strings.TrimSpace(tool)
	// Denylist takes precedence (supports wildcards)
	for _, d := range cfg.DeniedTools {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if wildcardMatch(d, tool) {
			return fmt.Errorf("tool '%s' is denied by policy", tool)
		}
	}
	// Allow if allowlist is empty
	if len(cfg.AllowedTools) == 0 {
		return nil
	}
	// Allow if any allowlist entry matches (supports wildcards)
	for _, a := range cfg.AllowedTools {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if wildcardMatch(a, tool) {
			return nil
		}
	}
	// Not explicitly allowed: prompt for confirmation when in safe mode; otherwise allow with warning
	if cfg.SafeMode {
		fmt.Printf("Allow MCP tool '%s'? (y/N): ", tool)
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			return fmt.Errorf("tool '%s' not allowed by user", tool)
		}
		return nil
	}
	// Non-safe mode: permit but inform via stderr
	fmt.Fprintf(os.Stderr, "[quackops] warning: tool '%s' is not in allowlist; proceeding.\n", tool)
	return nil
}

func wildcardMatch(pattern, s string) bool {
	// Simple glob: '*' wildcard, case-insensitive
	p := strings.ToLower(strings.TrimSpace(pattern))
	t := strings.ToLower(strings.TrimSpace(s))
	// Fast path exact
	if p == t {
		return true
	}
	// Replace '*' with regex equivalent isn't necessary; use filepath.Match semantics
	ok, err := filepath.Match(p, t)
	if err != nil {
		return false
	}
	return ok
}
