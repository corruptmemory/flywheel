package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/corruptmemory/flywheel/broker"
)

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  any `json:"params"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func unixClient(sockPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
	}
}

func jsonrpcCallRaw(t *testing.T, sockPath, method string, params any) jsonrpcResponse {
	t.Helper()
	req := jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	client := unixClient(sockPath)
	resp, err := client.Post("http://localhost/mcp", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST to unix socket: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		t.Fatalf("unmarshal response: %v (body: %s)", err, body)
	}
	return rpcResp
}

func jsonrpcCall(t *testing.T, sockPath, method string, params any) any {
	t.Helper()
	resp := jsonrpcCallRaw(t, sockPath, method, params)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
	var result any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return result
}

func setupTest(t *testing.T, role string) (*Server, broker.Broker) {
	t.Helper()
	dir := t.TempDir()

	b, err := broker.New(broker.Config{
		RequestsDir: filepath.Join(dir, "requests"),
		HandoffDir:  filepath.Join(dir, "handoff"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Stop(); b.Wait() })

	sockPath := filepath.Join(dir, "mcp.sock")
	srv, err := NewServer(Config{
		Role:       role,
		SocketPath: sockPath,
		Broker:     b,
	})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })

	// Wait for socket to be ready.
	for range 50 {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	return srv, b
}

func TestMCPToolsList(t *testing.T) {
	srv, _ := setupTest(t, "builder")

	result := jsonrpcCall(t, srv.config.SocketPath, "tools/list", map[string]any{})
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	tools, ok := m["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got %T", m["tools"])
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		tm := tool.(map[string]any)
		toolNames[tm["name"].(string)] = true
	}

	// Pipeline agent should have these 5 tools.
	for _, name := range []string{"send_request", "check_response", "wait_for_response", "list_my_requests", "write_done"} {
		if !toolNames[name] {
			t.Errorf("expected tool %q in builder tools list", name)
		}
	}
	if len(tools) != 5 {
		t.Errorf("expected 5 tools for builder, got %d", len(tools))
	}

	// PO tools should be absent.
	for _, name := range []string{"list_pending_requests", "read_request", "send_response", "create_amendment", "list_amendments"} {
		if toolNames[name] {
			t.Errorf("unexpected PO tool %q in builder tools list", name)
		}
	}
}

func TestMCPSendRequest(t *testing.T) {
	srv, _ := setupTest(t, "builder")

	result := jsonrpcCall(t, srv.config.SocketPath, "tools/call", map[string]any{
		"name": "send_request",
		"arguments": map[string]any{
			"type":    "PLEASE CLARIFY",
			"content": "What color should the button be?",
		},
	})

	// Result should be MCP content block format.
	m := result.(map[string]any)
	content := m["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("expected type text, got %v", block["type"])
	}

	// Parse the inner JSON.
	var inner map[string]any
	if err := json.Unmarshal([]byte(block["text"].(string)), &inner); err != nil {
		t.Fatalf("unmarshal inner text: %v", err)
	}
	if inner["request_id"] != "000001" {
		t.Errorf("expected request_id 000001, got %v", inner["request_id"])
	}
}

func TestMCPWriteDoneEnforcement(t *testing.T) {
	srv, _ := setupTest(t, "builder")

	// Create a request first.
	jsonrpcCall(t, srv.config.SocketPath, "tools/call", map[string]any{
		"name": "send_request",
		"arguments": map[string]any{
			"type":    "question",
			"content": "What color?",
		},
	})

	// Attempt write_done with outstanding request — should get error.
	resp := jsonrpcCallRaw(t, srv.config.SocketPath, "tools/call", map[string]any{
		"name": "write_done",
		"arguments": map[string]any{
			"status":  "complete",
			"details": "All done.",
		},
	})

	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %s", resp.Error.Message)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Should be an isError MCP response.
	isErr, _ := result["isError"].(bool)
	if !isErr {
		t.Fatalf("expected isError=true for write_done with outstanding request, got result: %s", resp.Result)
	}
}

func TestMCPPOTools(t *testing.T) {
	srv, _ := setupTest(t, "product-owner")

	result := jsonrpcCall(t, srv.config.SocketPath, "tools/list", map[string]any{})
	m := result.(map[string]any)
	tools := m["tools"].([]any)

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		tm := tool.(map[string]any)
		toolNames[tm["name"].(string)] = true
	}

	// PO should have these 6 tools.
	for _, name := range []string{"list_pending_requests", "read_request", "send_response", "create_amendment", "list_amendments", "write_done"} {
		if !toolNames[name] {
			t.Errorf("expected tool %q in PO tools list", name)
		}
	}
	if len(tools) != 6 {
		t.Errorf("expected 6 tools for product-owner, got %d", len(tools))
	}

	// Pipeline tools should be absent.
	for _, name := range []string{"send_request", "check_response", "wait_for_response", "list_my_requests"} {
		if toolNames[name] {
			t.Errorf("unexpected pipeline tool %q in PO tools list", name)
		}
	}

	// Call list_pending_requests — should return empty list.
	callResult := jsonrpcCall(t, srv.config.SocketPath, "tools/call", map[string]any{
		"name":      "list_pending_requests",
		"arguments": map[string]any{},
	})
	cm := callResult.(map[string]any)
	contentBlocks := cm["content"].([]any)
	block := contentBlocks[0].(map[string]any)

	var inner map[string]any
	if err := json.Unmarshal([]byte(block["text"].(string)), &inner); err != nil {
		t.Fatalf("unmarshal inner: %v", err)
	}
	reqs, ok := inner["requests"].([]any)
	if !ok {
		// nil is fine — means empty.
		if inner["requests"] != nil {
			t.Fatalf("expected nil or empty requests list, got %v", inner["requests"])
		}
	} else if len(reqs) != 0 {
		t.Errorf("expected 0 pending requests, got %d", len(reqs))
	}
}

func TestMCPCallUnknownTool(t *testing.T) {
	srv, _ := setupTest(t, "builder")

	resp := jsonrpcCallRaw(t, srv.config.SocketPath, "tools/call", map[string]any{
		"name":      "list_pending_requests",
		"arguments": map[string]any{},
	})

	if resp.Error == nil {
		t.Fatal("expected error for PO-only tool called by builder")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}
