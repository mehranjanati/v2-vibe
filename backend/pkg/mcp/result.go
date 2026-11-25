package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// P1-AGT-004 (D8.2) — JSON-RPC result parsing shared by every transport.
//
// A transport only moves bytes; what a `tools/list` or `tools/call` result may
// contain is one contract, so it is parsed in exactly one place. `prefix` names
// the adapter in the error ("mcp stdio", "mcp sse") and nothing else differs —
// a server on the wire cannot behave differently because of the binding.

// parseToolList turns a tools/list result into discovery metadata. Side effects
// and approvals are *declared* by the server and deliberately not trusted here:
// execution uses the reviewed Tool Manifest values (see execution.go).
func parseToolList(prefix string, raw json.RawMessage) ([]DiscoveredTool, error) {
	var listing struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		return nil, fmt.Errorf("%s: tools/list result is not a tool list: %w", prefix, err)
	}
	out := make([]DiscoveredTool, 0, len(listing.Tools))
	for _, tool := range listing.Tools {
		out = append(out, DiscoveredTool{Name: tool.Name, Description: tool.Description})
	}
	return out, nil
}

// parseToolCallResult turns a tools/call result into the server's text content.
// `isError: true` is an error, never an empty success: the platform must not
// report a failed tool as a successful one.
func parseToolCallResult(prefix, toolName string, raw json.RawMessage) (string, error) {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("%s: tools/call result is malformed: %w", prefix, err)
	}
	var text strings.Builder
	for _, part := range result.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if result.IsError {
		return "", fmt.Errorf("%s: tool %q reported an error: %s", prefix, toolName, text.String())
	}
	return text.String(), nil
}

// rpcRequestIDEquals compares a JSON-RPC response id with the request id we are
// waiting for. Servers may echo the id as a number or a string.
func rpcRequestIDEquals(raw json.RawMessage, want int64) bool {
	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number == want
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		var parsed int64
		if _, err := fmt.Sscanf(text, "%d", &parsed); err == nil {
			return parsed == want
		}
	}
	return false
}
