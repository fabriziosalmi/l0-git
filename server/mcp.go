package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

const protocolVersion = "2025-06-18"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpServer struct {
	store *Store
	out   io.Writer
	mu    sync.Mutex
	ctx   context.Context
}

func runMCP(ctx context.Context, store *Store, in io.Reader, out io.Writer) error {
	s := &mcpServer{store: store, out: out, ctx: ctx}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.writeError(nil, -32700, "parse error: "+err.Error())
			continue
		}
		s.handle(&req)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (s *mcpServer) write(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = s.out.Write(b)
}

func (s *mcpServer) writeError(id json.RawMessage, code int, msg string) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *mcpServer) writeResult(id json.RawMessage, result any) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *mcpServer) handle(req *rpcRequest) {
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "l0-git",
				"version": Version,
			},
		})
	case "initialized", "notifications/initialized":
		// no-op
	case "tools/list":
		s.writeResult(req.ID, map[string]any{"tools": toolDefs()})
	case "tools/call":
		s.handleToolCall(req)
	case "ping":
		s.writeResult(req.ID, map[string]any{})
	default:
		if !isNotification {
			s.writeError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func toolDefs() []map[string]any {
	return []map[string]any{
		{
			"name":        "gates_check",
			"description": "Run quality gates against a project root and persist findings. Pass an optional gate_id to run a single gate.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Absolute path to the project root."},
					"gate_id": map[string]any{"type": "string", "description": "Optional gate ID; if omitted, runs all gates."},
				},
				"required": []string{"project"},
			},
		},
		{
			"name":        "gates_list",
			"description": "List the registered gates.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "findings_list",
			"description": "List findings, optionally filtered by project and status (open|resolved|ignored|all).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project": map[string]any{"type": "string", "description": "Optional project root filter."},
					"status":  map[string]any{"type": "string", "description": "Status filter: open|resolved|ignored|all (default: open)."},
					"limit":   map[string]any{"type": "integer", "description": "Max results (default 200)."},
				},
			},
		},
		{
			"name":        "findings_ignore",
			"description": "Mark a finding as ignored so future gate runs do not resurface it.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "integer"}},
				"required":   []string{"id"},
			},
		},
		{
			"name":        "findings_delete",
			"description": "Delete a finding by id.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "integer"}},
				"required":   []string{"id"},
			},
		},
		{
			"name":        "findings_clear",
			"description": "Delete all findings for a project.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"project": map[string]any{"type": "string"}},
				"required":   []string{"project"},
			},
		},
	}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *mcpServer) handleToolCall(req *rpcRequest) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.writeError(req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	result, err := s.dispatchTool(p.Name, p.Arguments)
	if err != nil {
		s.writeResult(req.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
		})
		return
	}
	text, _ := json.MarshalIndent(result, "", "  ")
	s.writeResult(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
	})
}

type checkArgs struct {
	Project string `json:"project"`
	GateID  string `json:"gate_id"`
}

type listArgs struct {
	Project string `json:"project"`
	Status  string `json:"status"`
	Limit   int    `json:"limit"`
}

type idArg struct {
	ID int64 `json:"id"`
}

type projectArg struct {
	Project string `json:"project"`
}

func (s *mcpServer) dispatchTool(name string, args json.RawMessage) (any, error) {
	ctx := s.ctx
	switch name {
	case "gates_check":
		var a checkArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(a.Project) == "" {
			return nil, errors.New("'project' is required")
		}
		return RunChecks(ctx, s.store, a.Project, a.GateID)
	case "gates_list":
		return gateRegistry(), nil
	case "findings_list":
		var a listArgs
		_ = json.Unmarshal(args, &a)
		status := a.Status
		if status == "" {
			status = "open"
		}
		if status == "all" {
			status = ""
		}
		return s.store.List(ctx, a.Project, status, a.Limit)
	case "findings_ignore":
		var a idArg
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		ok, err := s.store.Ignore(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ignored": ok}, nil
	case "findings_delete":
		var a idArg
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		ok, err := s.store.Delete(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": ok}, nil
	case "findings_clear":
		var a projectArg
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(a.Project) == "" {
			return nil, errors.New("'project' is required")
		}
		n, err := s.store.ClearProject(ctx, a.Project)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": n}, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
