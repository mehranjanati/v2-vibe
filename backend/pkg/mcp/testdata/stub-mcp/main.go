// Command stub-mcp is a minimal MCP server over stdio, used only by the
// P1-AGT-003 transport tests. It is a real separate process (the transport spawns
// it), so the test proves the protocol path rather than a mocked function.
//
// It implements exactly what the test needs: initialize, tools/list, tools/call.
// Everything else answers a JSON-RPC error.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			write(out, map[string]any{
				"jsonrpc": "2.0",
				"error":   map[string]any{"code": -32700, "message": "parse error"},
			})
			continue
		}

		// Notifications carry no id and expect no reply.
		if len(req.ID) == 0 {
			continue
		}

		switch req.Method {
		case "initialize":
			writeID(out, req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "stub-mcp", "version": "0.0.1"},
			})
		case "tools/list":
			writeID(out, req.ID, map[string]any{
				"tools": []any{
					map[string]any{"name": "lookup", "description": "Look up one record"},
					map[string]any{"name": "explode", "description": "Always reports an error"},
				},
			})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			if params.Name == "explode" {
				writeID(out, req.ID, map[string]any{
					"content": []any{map[string]any{"type": "text", "text": "boom"}},
					"isError": true,
				})
				continue
			}
			raw, _ := json.Marshal(params.Arguments)
			writeID(out, req.ID, map[string]any{
				"content": []any{map[string]any{
					"type": "text",
					"text": fmt.Sprintf("called %s with %s", params.Name, string(raw)),
				}},
			})
		default:
			write(out, map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(req.ID),
				"error":   map[string]any{"code": -32601, "message": "method not found: " + req.Method},
			})
		}
	}
}

func writeID(out *bufio.Writer, id json.RawMessage, result any) {
	write(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func write(out *bufio.Writer, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	out.Write(raw)
	out.WriteByte(10)
	out.Flush()
}
