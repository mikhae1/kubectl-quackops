package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

func TestLoadOnce(t *testing.T) {
	// Reset global state
	loaded = false
	cfgDoc = ConfigFile{}

	// Create a temporary directory and config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "mcp.json")

	testConfig := ConfigFile{
		MCPServers: map[string]*ServerSpec{
			"test-server": {
				Name:    "test-server",
				Command: "echo",
				Args:    []string{"hello"},
				Env:     map[string]string{"TEST": "value"},
			},
		},
	}

	data, err := json.Marshal(testConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Test loading
	loadOnce(configPath)

	if !loaded {
		t.Error("Expected loaded to be true")
	}

	if len(cfgDoc.Servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(cfgDoc.Servers))
	}

	server := cfgDoc.Servers[0]
	if server.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", server.Name)
	}

	if server.Command != "echo" {
		t.Errorf("Expected command 'echo', got '%s'", server.Command)
	}
}

func TestNormalizeServers(t *testing.T) {
	cfgDoc = ConfigFile{
		MCPServers: map[string]*ServerSpec{
			"server1": {
				Command: "cmd1",
				Args:    []string{"arg1"},
			},
			"server2": {
				Name:    "custom-name",
				Command: "cmd2",
				Args:    []string{"arg2"},
			},
		},
	}

	normalizeServers()

	if len(cfgDoc.Servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(cfgDoc.Servers))
	}

	// Check that servers were properly normalized
	server1Found := false
	server2Found := false

	for _, server := range cfgDoc.Servers {
		if server.Name == "server1" && server.Command == "cmd1" {
			server1Found = true
		}
		if server.Name == "custom-name" && server.Command == "cmd2" {
			server2Found = true
		}
	}

	if !server1Found {
		t.Error("server1 not found after normalization")
	}
	if !server2Found {
		t.Error("server2 not found after normalization")
	}

	// Check that MCPServers map was cleared
	if cfgDoc.MCPServers != nil {
		t.Error("Expected MCPServers to be nil after normalization")
	}
}

func TestServerRegistry(t *testing.T) {
	registry := NewServerRegistry()

	if registry == nil {
		t.Fatal("Expected registry to be created")
	}

	if len(registry.servers) != 0 {
		t.Error("Expected empty servers map")
	}

	if len(registry.toolToServer) != 0 {
		t.Error("Expected empty toolToServer map")
	}
}

func TestServerRegistryAddServer(t *testing.T) {
	registry := NewServerRegistry()

	spec := &ServerSpec{
		Name:    "test-server",
		Command: "echo",
	}

	conn := &ServerConnection{
		Spec:  spec,
		Tools: []string{"tool1", "tool2"},
		ToolInfos: []ToolInfo{
			{Name: "tool1", Description: "Description for tool1"},
			{Name: "tool2", Description: "Description for tool2"},
		},
		Connected: true,
	}

	registry.AddServer("test-server", conn)

	// Check server was added
	if len(registry.servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(registry.servers))
	}

	retrievedConn, exists := registry.GetServer("test-server")
	if !exists {
		t.Error("Expected server to exist")
	}

	if retrievedConn.Spec.Name != "test-server" {
		t.Errorf("Expected server name 'test-server', got '%s'", retrievedConn.Spec.Name)
	}

	// Check tools were mapped
	if len(registry.toolToServer) != 2 {
		t.Errorf("Expected 2 tools mapped, got %d", len(registry.toolToServer))
	}

	tool1Conn, exists := registry.GetServerForTool("tool1")
	if !exists {
		t.Error("Expected tool1 to be mapped")
	}
	if tool1Conn != conn {
		t.Error("Expected tool1 to map to the correct connection")
	}
}

func TestServerRegistryGetAllTools(t *testing.T) {
	registry := NewServerRegistry()

	spec1 := &ServerSpec{Name: "server1", Command: "cmd1"}
	conn1 := &ServerConnection{
		Spec:  spec1,
		Tools: []string{"tool1", "tool2"},
		ToolInfos: []ToolInfo{
			{Name: "tool1", Description: "Description for tool1"},
			{Name: "tool2", Description: "Description for tool2"},
		},
		Connected: true,
	}

	spec2 := &ServerSpec{Name: "server2", Command: "cmd2"}
	conn2 := &ServerConnection{
		Spec:  spec2,
		Tools: []string{"tool3"},
		ToolInfos: []ToolInfo{
			{Name: "tool3", Description: "Description for tool3"},
		},
		Connected: true,
	}

	registry.AddServer("server1", conn1)
	registry.AddServer("server2", conn2)

	tools := registry.GetAllTools()

	// Should have tools from both servers only
	expectedTools := map[string]bool{
		"tool1": true,
		"tool2": true,
		"tool3": true,
	}

	if len(tools) != len(expectedTools) {
		t.Errorf("Expected %d tools, got %d: %v", len(expectedTools), len(tools), tools)
	}

	for _, tool := range tools {
		if !expectedTools[tool] {
			t.Errorf("Unexpected tool: %s", tool)
		}
	}
}

func TestServerRegistryGetConnectedServers(t *testing.T) {
	registry := NewServerRegistry()

	// Add connected server
	spec1 := &ServerSpec{Name: "server1", Command: "cmd1"}
	conn1 := &ServerConnection{
		Spec:      spec1,
		Connected: true,
	}

	// Add disconnected server
	spec2 := &ServerSpec{Name: "server2", Command: "cmd2"}
	conn2 := &ServerConnection{
		Spec:      spec2,
		Connected: false,
	}

	registry.AddServer("server1", conn1)
	registry.AddServer("server2", conn2)

	connected := registry.GetConnectedServers()

	if len(connected) != 1 {
		t.Errorf("Expected 1 connected server, got %d", len(connected))
	}

	if connected[0] != "server1" {
		t.Errorf("Expected connected server to be 'server1', got '%s'", connected[0])
	}
}

func TestServerRegistryHealthCheck(t *testing.T) {
	registry := NewServerRegistry()

	spec := &ServerSpec{Name: "test-server", Command: "echo"}
	conn := &ServerConnection{
		Spec:            spec,
		Connected:       true,
		LastHealthCheck: time.Now().Add(-1 * time.Hour), // Old health check
	}

	registry.AddServer("test-server", conn)

	health := registry.GetServerHealth()

	if len(health) != 1 {
		t.Errorf("Expected 1 server in health status, got %d", len(health))
	}

	if !health["test-server"] {
		t.Error("Expected test-server to be healthy")
	}
}

func TestTools(t *testing.T) {
	// Reset global state
	loaded = false
	registry = nil

	cfg := &config.Config{
		MCPConfigPath: "/nonexistent/path",
	}

	tools := Tools(cfg)

	// Should return empty list when no registry
	if len(tools) != 0 {
		t.Errorf("Expected 0 tools when no registry, got %d", len(tools))
	}
}

func TestEnsureToolAllowed(t *testing.T) {
	cfg := &config.Config{
		AllowedTools: []string{"kubectl", "bash"},
		DeniedTools:  []string{"dangerous-tool"},
		SafeMode:     false,
	}

	// Test allowed tool
	err := ensureToolAllowed(cfg, "kubectl")
	if err != nil {
		t.Errorf("Expected kubectl to be allowed, got error: %v", err)
	}

	// Test denied tool
	err = ensureToolAllowed(cfg, "dangerous-tool")
	if err == nil {
		t.Error("Expected dangerous-tool to be denied")
	}

	// Test tool not in allowlist (should be allowed with warning in non-safe mode)
	err = ensureToolAllowed(cfg, "unknown-tool")
	if err != nil {
		t.Errorf("Expected unknown-tool to be allowed with warning, got error: %v", err)
	}
}

func TestWildcardMatch(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"kubectl", "kubectl", true},
		{"kube*", "kubectl", true},
		{"kube*", "kubernetes", true},
		{"*ctl", "kubectl", true},
		{"kubectl", "bash", false},
		{"kube*", "bash", false},
	}

	for _, tt := range tests {
		got := wildcardMatch(tt.pattern, tt.text)
		if got != tt.want {
			t.Errorf("wildcardMatch(%q, %q) = %v, want %v", tt.pattern, tt.text, got, tt.want)
		}
	}
}
