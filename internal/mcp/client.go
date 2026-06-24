package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type ContextKey string

const (
	UserIDKey         ContextKey = "mcp-user-id"
	ConversationIDKey ContextKey = "mcp-conversation-id"
)

// Client defines the interface for interacting with an MCP server
type Client interface {
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error)
	Close() error
}

// Server defines the interface for built-in, in-process MCP servers
type Server interface {
	Name() string
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error)
}

// InProcessClient is a fast, in-memory implementation of Client
type InProcessClient struct {
	server Server
}

func NewInProcessClient(server Server) *InProcessClient {
	return &InProcessClient{server: server}
}

func (c *InProcessClient) ListTools(ctx context.Context) ([]Tool, error) {
	return c.server.ListTools(ctx)
}

func (c *InProcessClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	return c.server.CallTool(ctx, name, arguments)
}

func (c *InProcessClient) Close() error {
	return nil
}

// StdioClient implements the MCP client over stdio with a subprocess
type StdioClient struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	pending   map[interface{}]chan *Response
	pendingMu sync.Mutex
	nextID    int64
	closeChan chan struct{}
	closeOnce sync.Once
}

func NewStdioClient(ctx context.Context, command string, args []string, env map[string]string) (*StdioClient, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	
	// Inject env
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// We redirect stderr to our log so we can debug external servers
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	client := &StdioClient{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		pending:   make(map[interface{}]chan *Response),
		closeChan: make(chan struct{}),
	}

	// Start reader loop
	go client.readLoop()

	// Initialize the server
	if err := client.initialize(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to initialize server: %w", err)
	}

	return client, nil
}

func (c *StdioClient) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			log.Printf("[MCP Client] Error unmarshaling line: %v", err)
			continue
		}

		c.pendingMu.Lock()
		ch, exists := c.pending[resp.ID]
		if exists {
			delete(c.pending, resp.ID)
			ch <- &resp
		}
		c.pendingMu.Unlock()
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[MCP Client] Read loop error: %v", err)
	}

	c.Close()
}

func (c *StdioClient) sendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
	id := atomic.AddInt64(&c.nextID, 1)
	
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return err
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
		ID:      id,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}

	ch := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	// Append newline as per stdio transport spec
	_, err = c.stdin.Write(append(reqBytes, '\n'))
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return ctx.Err()
	case <-c.closeChan:
		return fmt.Errorf("client closed")
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("server error: %s (code: %d)", resp.Error.Message, resp.Error.Code)
		}
		if result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}
		return nil
	}
}

func (c *StdioClient) sendNotification(method string, params interface{}) error {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return err
	}

	notif := Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	}

	notifBytes, err := json.Marshal(notif)
	if err != nil {
		return err
	}

	_, err = c.stdin.Write(append(notifBytes, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}
	return nil
}

func (c *StdioClient) initialize(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    make(map[string]interface{}),
		ClientInfo: Implementation{
			Name:    "ConverseAI-Client",
			Version: "1.0.0",
		},
	}

	var result InitializeResult
	if err := c.sendRequest(initCtx, "initialize", params, &result); err != nil {
		return err
	}

	// Send initialized notification
	return c.sendNotification("notifications/initialized", map[string]interface{}{})
}

func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	var result ListToolsResult
	if err := c.sendRequest(ctx, "tools/list", map[string]interface{}{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	params := CallToolParams{
		Name:      name,
		Arguments: arguments,
	}

	var result CallToolResult
	if err := c.sendRequest(ctx, "tools/call", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *StdioClient) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closeChan)
		c.stdin.Close()
		c.stdout.Close()
		
		// Clean up pending requests
		c.pendingMu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()

		// Wait or kill subprocess
		if c.cmd.Process != nil {
			// Try to kill it
			_ = c.cmd.Process.Kill()
			_ = c.cmd.Wait()
		}
	})
	return err
}
