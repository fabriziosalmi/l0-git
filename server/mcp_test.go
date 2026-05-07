package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// runRequests pipes a sequence of JSON-RPC requests through runMCP and parses
// the per-line responses. Uses a fresh DB inside t.TempDir().
func runRequests(t *testing.T, requests []map[string]any) []map[string]any {
	t.Helper()
	store, err := openStoreAt(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	var in bytes.Buffer
	for _, r := range requests {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		in.Write(b)
		in.WriteByte('\n')
	}
	var out bytes.Buffer
	if err := runMCP(context.Background(), store, &in, &out); err != nil {
		t.Fatalf("runMCP: %v", err)
	}

	var resps []map[string]any
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal response %q: %v", line, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestMCP_InitializeAndToolsList(t *testing.T) {
	resps := runRequests(t, []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
	})
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	init := resps[0]["result"].(map[string]any)
	if init["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion=%v want %s", init["protocolVersion"], protocolVersion)
	}
	if init["serverInfo"].(map[string]any)["name"] != "l0-git" {
		t.Errorf("serverInfo.name=%v", init["serverInfo"])
	}

	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	got := map[string]bool{}
	for _, tt := range tools {
		got[tt.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"gates_check", "gates_list", "findings_list", "findings_ignore", "findings_delete", "findings_clear"} {
		if !got[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestMCP_GatesCheckAndFindingsList(t *testing.T) {
	root := t.TempDir() // empty dir → many findings
	resps := runRequests(t, []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
			"name":      "gates_check",
			"arguments": map[string]any{"project": root, "gate_id": "readme_present"},
		}},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{
			"name":      "findings_list",
			"arguments": map[string]any{"project": root, "status": "open"},
		}},
	})
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}

	checkText := resps[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var checkRes map[string]any
	if err := json.Unmarshal([]byte(checkText), &checkRes); err != nil {
		t.Fatalf("parse check result: %v (%s)", err, checkText)
	}
	if len(checkRes["findings"].([]any)) != 1 {
		t.Errorf("expected 1 finding from readme_present, got %v", checkRes["findings"])
	}

	listText := resps[1]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var list []map[string]any
	if err := json.Unmarshal([]byte(listText), &list); err != nil {
		t.Fatalf("parse list: %v (%s)", err, listText)
	}
	if len(list) != 1 || list[0]["gate_id"] != "readme_present" {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestMCP_FindingsIgnoreFlow(t *testing.T) {
	// Each runRequests gets its own DB, so chain check → list → ignore →
	// re-list inside a single batch.
	root := t.TempDir()
	resps := runRequests(t, []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
			"name":      "gates_check",
			"arguments": map[string]any{"project": root, "gate_id": "readme_present"},
		}},
	})
	checkText := resps[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var checkRes map[string]any
	if err := json.Unmarshal([]byte(checkText), &checkRes); err != nil {
		t.Fatalf("parse check: %v (%s)", err, checkText)
	}
	findings := checkRes["findings"].([]any)
	if len(findings) == 0 {
		t.Fatalf("expected at least one finding")
	}
	id := int(findings[0].(map[string]any)["id"].(float64))

	full := runRequests(t, []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
			"name":      "gates_check",
			"arguments": map[string]any{"project": root, "gate_id": "readme_present"},
		}},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{
			"name":      "findings_ignore",
			"arguments": map[string]any{"id": id},
		}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{
			"name":      "findings_list",
			"arguments": map[string]any{"project": root, "status": "open"},
		}},
	})
	postText := full[2]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var postOpen []map[string]any
	if err := json.Unmarshal([]byte(postText), &postOpen); err != nil {
		t.Fatalf("parse post-ignore list: %v", err)
	}
	if len(postOpen) != 0 {
		t.Errorf("expected no open findings after ignore, got: %+v", postOpen)
	}
}

func TestMCP_MissingProjectArgError(t *testing.T) {
	resps := runRequests(t, []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
			"name":      "gates_check",
			"arguments": map[string]any{},
		}},
	})
	result := resps[0]["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Errorf("missing project should be a tool error, got %+v", result)
	}
}

func TestMCP_UnknownMethodReturnsRPCError(t *testing.T) {
	resps := runRequests(t, []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "does/not/exist"},
	})
	if _, ok := resps[0]["error"]; !ok {
		t.Fatalf("expected JSON-RPC error, got %+v", resps[0])
	}
}

func TestMCP_NotificationsHaveNoResponse(t *testing.T) {
	resps := runRequests(t, []map[string]any{
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 1, "method": "ping"},
	})
	if len(resps) != 1 {
		t.Fatalf("expected exactly 1 response (for ping), got %d: %+v", len(resps), resps)
	}
	if resps[0]["id"].(float64) != 1 {
		t.Errorf("expected response id=1, got %v", resps[0]["id"])
	}
}
