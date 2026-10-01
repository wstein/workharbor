package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// runMCPPermission is a minimal MCP server over stdio. The agent starts it as a
// child process and calls its "approve" tool whenever a tool needs permission.
// It forwards the request to the supervisor and returns the human's decision.
func runMCPPermission(supervisor, token string) error {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 1<<20), 16<<20)
	out := json.NewEncoder(os.Stdout)
	client := &http.Client{Timeout: approvalTimeout + time.Minute}

	reply := func(id json.RawMessage, result any) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	fail := func(id json.RawMessage, code int, msg string) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
	}

	for in.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &req) != nil {
			continue
		}
		if len(req.ID) == 0 { // a notification, such as notifications/initialized
			continue
		}
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2024-11-05"
			}
			reply(req.ID, map[string]any{
				"protocolVersion": p.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]string{"name": "workharbor", "version": "spike"},
			})
		case "ping":
			reply(req.ID, map[string]any{})
		case "tools/list":
			reply(req.ID, map[string]any{"tools": []map[string]any{{
				"name":        "approve",
				"description": "Ask the human whether the agent may use a tool. Called by the host, not by the model.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tool_name":   map[string]string{"type": "string"},
						"input":       map[string]string{"type": "object"},
						"tool_use_id": map[string]string{"type": "string"},
					},
					"required": []string{"tool_name", "input"},
				},
			}}})
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &p) != nil || p.Name != "approve" {
				fail(req.ID, -32602, "unknown tool")
				continue
			}
			verdict, err := askSupervisor(client, supervisor, token, p.Arguments)
			if err != nil { // fail closed: an unreachable supervisor means deny
				verdict, _ = json.Marshal(map[string]string{"behavior": "deny", "message": "approval service unavailable: " + err.Error()})
			}
			reply(req.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": string(verdict)}}})
		default:
			fail(req.ID, -32601, "method not found")
		}
	}
	return in.Err()
}

// writeMCPConfig writes the MCP server config that makes the agent start this
// binary as its approve tool. The token reaches the helper through its
// environment, so it does not appear in the process list.
func writeMCPConfig(dir, supervisor, token string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cfg := map[string]any{"mcpServers": map[string]any{"workharbor": map[string]any{
		"command": exe,
		"args":    []string{"-mcp-permission", "-supervisor", supervisor},
		"env":     map[string]string{"WH_TOKEN": token},
	}}}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	// Absolute, because the agent runs in its own working directory and would
	// resolve a relative -data path against that.
	path, err := filepath.Abs(filepath.Join(dir, "mcp.json"))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func askSupervisor(c *http.Client, supervisor, token string, args json.RawMessage) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, supervisor+"/internal/permission", bytes.NewReader(args))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("supervisor returned %s", resp.Status)
	}
	return body, nil
}
