package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type ConfigFile struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type Registry interface {
	RegisterServer(name string, server Server)
	RegisterClient(name string, client Client)
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error)
	Close() error
	LoadExternalServers(ctx context.Context, workspaceRoot string) error
}

type mcpRegistry struct {
	clients   map[string]Client
	clientsMu sync.RWMutex
	
	// Tool-to-Client cache
	toolMap   map[string]Client
	toolMapMu sync.RWMutex
}

func NewRegistry() Registry {
	return &mcpRegistry{
		clients: make(map[string]Client),
		toolMap: make(map[string]Client),
	}
}

func (r *mcpRegistry) RegisterServer(name string, server Server) {
	r.clientsMu.Lock()
	defer r.clientsMu.Unlock()
	r.clients[name] = NewInProcessClient(server)
}

func (r *mcpRegistry) RegisterClient(name string, client Client) {
	r.clientsMu.Lock()
	defer r.clientsMu.Unlock()
	r.clients[name] = client
}

func (r *mcpRegistry) ListTools(ctx context.Context) ([]Tool, error) {
	r.clientsMu.RLock()
	clients := make(map[string]Client, len(r.clients))
	for name, client := range r.clients {
		clients[name] = client
	}
	r.clientsMu.RUnlock()

	var allTools []Tool
	tempToolMap := make(map[string]Client)

	for serverName, client := range clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			log.Printf("[MCP Registry] Warning: Failed to list tools from server %s: %v", serverName, err)
			continue
		}

		for _, t := range tools {
			allTools = append(allTools, t)
			tempToolMap[t.Name] = client
		}
	}

	r.toolMapMu.Lock()
	r.toolMap = tempToolMap
	r.toolMapMu.Unlock()

	return allTools, nil
}

func (r *mcpRegistry) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	// Ensure we populate/update the tool map if empty
	r.toolMapMu.RLock()
	client, exists := r.toolMap[name]
	r.toolMapMu.RUnlock()

	if !exists {
		// Try refreshing tools list once
		_, err := r.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("tool '%s' not found, and failed to refresh tool list: %w", name, err)
		}

		r.toolMapMu.RLock()
		client, exists = r.toolMap[name]
		r.toolMapMu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("tool '%s' not found in any registered MCP server", name)
		}
	}

	return client.CallTool(ctx, name, arguments)
}

func (r *mcpRegistry) LoadExternalServers(ctx context.Context, workspaceRoot string) error {
	configPath := filepath.Join(workspaceRoot, "mcp_config.json")
	
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Printf("[MCP Registry] No mcp_config.json found at %s, skipping external servers.", configPath)
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read mcp_config.json: %w", err)
	}

	var config ConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse mcp_config.json: %w", err)
	}

	for name, srvCfg := range config.MCPServers {
		log.Printf("[MCP Registry] Starting external server '%s' using command '%s'...", name, srvCfg.Command)
		
		client, err := NewStdioClient(ctx, srvCfg.Command, srvCfg.Args, srvCfg.Env)
		if err != nil {
			log.Printf("[MCP Registry] Error starting external server '%s': %v", name, err)
			continue
		}

		r.RegisterClient(name, client)
		log.Printf("[MCP Registry] Successfully registered external server '%s'", name)
	}

	// Trigger a listing to warm the cache
	_, _ = r.ListTools(ctx)

	return nil
}

func (r *mcpRegistry) Close() error {
	r.clientsMu.Lock()
	defer r.clientsMu.Unlock()

	var firstErr error
	for name, client := range r.clients {
		if err := client.Close(); err != nil {
			log.Printf("[MCP Registry] Error closing client '%s': %v", name, err)
			if firstErr == nil {
				firstErr = err
			}
		}
		delete(r.clients, name)
	}
	return firstErr
}
