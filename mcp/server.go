package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/corruptmemory/flywheel/broker"
)

// Config holds configuration for the MCP server.
type Config struct {
	Role       string        // agent role (builder, code-reviewer, security-reviewer, qa, product-owner)
	SocketPath string        // Unix socket path to listen on (JSON-RPC for agent)
	Broker     broker.Broker // direct broker interface
}

// ToolDef defines a tool that can be called via the MCP protocol.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(args map[string]any) (any, error)
}

// Server is a JSON-RPC 2.0 server that serves MCP tools over a Unix socket.
type Server struct {
	config   Config
	listener net.Listener
	tools    map[string]ToolDef
	mux      *http.ServeMux
	events   <-chan broker.BrokerEvent
}

type jsonrpcReq struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      json.RawMessage        `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]any `json:"params"`
}

type jsonrpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewServer creates a new MCP server, subscribes to broker events,
// registers role-appropriate tools, and begins listening on the
// configured Unix socket.
func NewServer(cfg Config) (*Server, error) {
	// Remove existing socket file if present.
	os.Remove(cfg.SocketPath)

	ln, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", cfg.SocketPath, err)
	}

	// Subscribe to broker events for this role.
	events, err := cfg.Broker.Subscribe(cfg.Role)
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("subscribe broker: %w", err)
	}

	s := &Server{
		config:   cfg,
		listener: ln,
		tools:    make(map[string]ToolDef),
		mux:      http.NewServeMux(),
		events:   events,
	}

	s.registerTools()
	s.mux.HandleFunc("/", s.handleRPC)

	return s, nil
}

// Serve starts serving HTTP on the Unix socket. This blocks until the
// listener is closed.
func (s *Server) Serve() error {
	return http.Serve(s.listener, s.mux)
}

// Close unsubscribes from broker events and closes the listener,
// causing Serve to return.
func (s *Server) Close() error {
	s.config.Broker.Unsubscribe(s.config.Role)
	return s.listener.Close()
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	var req jsonrpcReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	switch req.Method {
	case "initialize":
		writeRPCResult(w, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "lc-mcp",
				"version": "0.1.0",
			},
		})
	case "tools/list":
		s.handleToolsList(w, req)
	case "tools/call":
		s.handleToolsCall(w, req)
	default:
		writeRPCError(w, req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) handleToolsList(w http.ResponseWriter, req jsonrpcReq) {
	var toolList []map[string]any
	for _, td := range s.tools {
		toolList = append(toolList, map[string]any{
			"name":        td.Name,
			"description": td.Description,
			"inputSchema": td.InputSchema,
		})
	}
	if toolList == nil {
		toolList = []map[string]any{}
	}
	writeRPCResult(w, req.ID, map[string]any{"tools": toolList})
}

func (s *Server) handleToolsCall(w http.ResponseWriter, req jsonrpcReq) {
	name, _ := req.Params["name"].(string)
	args, _ := req.Params["arguments"].(map[string]any)
	if args == nil {
		args = map[string]any{}
	}

	td, ok := s.tools[name]
	if !ok {
		writeRPCError(w, req.ID, -32601, fmt.Sprintf("tool not found: %s", name))
		return
	}

	result, err := td.Handler(args)
	if err != nil {
		// Return error as MCP content block with isError flag
		writeRPCResult(w, req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": err.Error()},
			},
			"isError": true,
		})
		return
	}

	// Marshal result to JSON string, wrap in MCP content block
	resultJSON, err := json.Marshal(result)
	if err != nil {
		writeRPCError(w, req.ID, -32603, fmt.Sprintf("marshal result: %v", err))
		return
	}

	writeRPCResult(w, req.ID, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(resultJSON)},
		},
	})
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	resp := jsonrpcResp{JSONRPC: "2.0", ID: id, Result: result}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := jsonrpcResp{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) registerTools() {
	if s.config.Role == "product-owner" {
		s.registerPOTools()
	} else {
		s.registerPipelineTools()
	}
}

func (s *Server) registerPipelineTools() {
	s.tools["send_request"] = ToolDef{
		Name:        "send_request",
		Description: "Send a request to the product owner",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":    map[string]any{"type": "string", "description": "Request type"},
				"content": map[string]any{"type": "string", "description": "Request content"},
			},
			"required": []string{"type", "content"},
		},
		Handler: func(args map[string]any) (any, error) {
			reqType, _ := args["type"].(string)
			content, _ := args["content"].(string)
			id, err := s.config.Broker.CreateRequest(s.config.Role, reqType, content)
			if err != nil {
				return nil, err
			}
			return map[string]any{"request_id": id}, nil
		},
	}

	s.tools["check_response"] = ToolDef{
		Name:        "check_response",
		Description: "Check if a response exists for a request",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]any{"type": "string", "description": "Request ID to check"},
			},
			"required": []string{"request_id"},
		},
		Handler: func(args map[string]any) (any, error) {
			reqID, _ := args["request_id"].(string)
			content, found, err := s.config.Broker.CheckResponse(s.config.Role, reqID)
			if err != nil {
				return nil, err
			}
			if !found {
				return map[string]any{"status": "pending"}, nil
			}
			return map[string]any{"status": "answered", "content": content}, nil
		},
	}

	s.tools["wait_for_response"] = ToolDef{
		Name:        "wait_for_response",
		Description: "Wait for a response to a request, polling until timeout",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id":      map[string]any{"type": "string", "description": "Request ID to wait for"},
				"timeout_seconds": map[string]any{"type": "number", "description": "Timeout in seconds (default 30)"},
			},
			"required": []string{"request_id"},
		},
		Handler: func(args map[string]any) (any, error) {
			reqID, _ := args["request_id"].(string)
			timeout := 30.0
			if t, ok := args["timeout_seconds"].(float64); ok {
				timeout = t
			}

			// Check if already answered.
			content, found, err := s.config.Broker.CheckResponse(s.config.Role, reqID)
			if err != nil {
				return nil, err
			}
			if found {
				return map[string]any{"status": "answered", "content": content}, nil
			}

			// Wait for push event.
			timer := time.NewTimer(time.Duration(timeout * float64(time.Second)))
			defer timer.Stop()
			for {
				select {
				case evt, ok := <-s.events:
					if !ok {
						return nil, fmt.Errorf("broker closed")
					}
					if evt.Event == "response_ready" && evt.RequestID == reqID {
						return map[string]any{"status": "answered", "content": evt.Content}, nil
					}
				case <-timer.C:
					return nil, fmt.Errorf("timeout waiting for response to request %s", reqID)
				}
			}
		},
	}

	s.tools["list_my_requests"] = ToolDef{
		Name:        "list_my_requests",
		Description: "List all requests made by this agent",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(args map[string]any) (any, error) {
			reqs, err := s.config.Broker.ListMyRequests(s.config.Role)
			if err != nil {
				return nil, err
			}
			return map[string]any{"requests": reqs}, nil
		},
	}

	s.tools["write_done"] = ToolDef{
		Name:        "write_done",
		Description: "Write a done file signaling this agent has finished",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":  map[string]any{"type": "string", "description": "Completion status"},
				"details": map[string]any{"type": "string", "description": "Additional details"},
			},
			"required": []string{"status", "details"},
		},
		Handler: func(args map[string]any) (any, error) {
			status, _ := args["status"].(string)
			details, _ := args["details"].(string)
			if err := s.config.Broker.WriteDone(s.config.Role, status, details); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		},
	}
}

func (s *Server) registerPOTools() {
	s.tools["list_pending_requests"] = ToolDef{
		Name:        "list_pending_requests",
		Description: "List all pending requests awaiting response",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(args map[string]any) (any, error) {
			reqs, err := s.config.Broker.ListPendingRequests()
			if err != nil {
				return nil, err
			}
			return map[string]any{"requests": reqs}, nil
		},
	}

	s.tools["read_request"] = ToolDef{
		Name:        "read_request",
		Description: "Read the content of a specific request",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]any{"type": "string", "description": "Request ID to read"},
			},
			"required": []string{"request_id"},
		},
		Handler: func(args map[string]any) (any, error) {
			reqID, _ := args["request_id"].(string)
			content, err := s.config.Broker.ReadRequest(reqID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"content": content}, nil
		},
	}

	s.tools["send_response"] = ToolDef{
		Name:        "send_response",
		Description: "Send a response to a pending request",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"request_id": map[string]any{"type": "string", "description": "Request ID to respond to"},
				"content":    map[string]any{"type": "string", "description": "Response content"},
			},
			"required": []string{"request_id", "content"},
		},
		Handler: func(args map[string]any) (any, error) {
			reqID, _ := args["request_id"].(string)
			content, _ := args["content"].(string)
			if err := s.config.Broker.SendResponse(reqID, content); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		},
	}

	s.tools["create_amendment"] = ToolDef{
		Name:        "create_amendment",
		Description: "Create a spec amendment",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":    map[string]any{"type": "string", "description": "Spec name to amend"},
				"content": map[string]any{"type": "string", "description": "Amendment content"},
			},
			"required": []string{"spec", "content"},
		},
		Handler: func(args map[string]any) (any, error) {
			spec, _ := args["spec"].(string)
			content, _ := args["content"].(string)
			id, err := s.config.Broker.CreateAmendment(spec, content)
			if err != nil {
				return nil, err
			}
			return map[string]any{"amendment_id": id}, nil
		},
	}

	s.tools["list_amendments"] = ToolDef{
		Name:        "list_amendments",
		Description: "List all spec amendments",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(args map[string]any) (any, error) {
			amendments, err := s.config.Broker.ListAmendments()
			if err != nil {
				return nil, err
			}
			return map[string]any{"amendments": amendments}, nil
		},
	}

	s.tools["write_done"] = ToolDef{
		Name:        "write_done",
		Description: "Write a done file signaling the product owner has finished",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":  map[string]any{"type": "string", "description": "Completion status"},
				"details": map[string]any{"type": "string", "description": "Additional details"},
			},
			"required": []string{"status", "details"},
		},
		Handler: func(args map[string]any) (any, error) {
			status, _ := args["status"].(string)
			details, _ := args["details"].(string)
			if err := s.config.Broker.WriteDone(s.config.Role, status, details); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		},
	}
}
