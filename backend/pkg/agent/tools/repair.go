package tools

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// trailingCommaRe removes ", }" / ",]" style trailing commas. Group 1 keeps
// whitespace/newlines; group 2 is the closing delimiter.
var trailingCommaRe = regexp.MustCompile(`,(\s*)([}\]])`)

// NewJSONRepairTool wraps an invokable tool so that slightly malformed
// model-generated JSON arguments are repaired before execution (the
// components/tool middleware pattern from eino-examples). Models
// frequently emit trailing commas, unclosed braces/brackets or unescaped
// control characters; instead of failing the whole tool call (and the
// model turn), we make one repair attempt first.
func NewJSONRepairTool(inner tool.InvokableTool) tool.InvokableTool {
	return &jsonRepairTool{inner: inner}
}

type jsonRepairTool struct {
	inner tool.InvokableTool
}

func (t *jsonRepairTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *jsonRepairTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	out, err := t.inner.InvokableRun(ctx, args, opts...)
	if err == nil {
		return out, nil
	}
	repaired := RepairJSON(args)
	if repaired == args {
		return out, err // nothing to repair; surface the original failure
	}
	log.Printf("[tool-repair] retrying %s with repaired args (orig error: %v)", toolName(ctx, t.inner), err)
	return t.inner.InvokableRun(ctx, repaired, opts...)
}

// toolName best-effort tool name for logs.
func toolName(ctx context.Context, t tool.BaseTool) string {
	if info, err := t.Info(ctx); err == nil && info != nil {
		return info.Name
	}
	return "?"
}

// RepairJSON attempts to fix common LLM JSON mistakes:
//   - raw control characters (newlines/tabs) inside string literals
//   - trailing commas before } or ]
//   - unbalanced trailing braces/brackets or an unterminated final string
//     (truncated output)
//
// Returns the input unchanged when no repair is possible or the input is
// already valid JSON.
func RepairJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if json.Valid([]byte(s)) {
		return s
	}

	// Single pass: build output, escaping raw control chars inside
	// strings and tracking the open-delimiter stack.
	var b strings.Builder
	var stack []byte
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			case c == '\n':
				b.WriteString(`\n`)
				continue
			case c == '\r':
				b.WriteString(`\r`)
				continue
			case c == '\t':
				b.WriteString(`\t`)
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if n := len(stack); n > 0 && stack[n-1] == c {
				stack = stack[:n-1]
			}
		}
		b.WriteByte(c)
	}
	out := b.String()

	// Close an unterminated string, then any unbalanced delimiters.
	if inStr {
		out += `"`
	}
	for i := len(stack) - 1; i >= 0; i-- {
		out += string(stack[i])
	}

	// Strip trailing commas to a fixpoint (closing delimiters appended
	// above can expose new ones).
	for i := 0; i < 4; i++ {
		next := trailingCommaRe.ReplaceAllString(out, "$1$2")
		if next == out {
			break
		}
		out = next
	}
	if json.Valid([]byte(out)) {
		return out
	}

	// Last resort: cut back to the last "}" that yields valid JSON
	// (drop the partially-written tail).
	for i := len(out) - 1; i > 0; i-- {
		if out[i] == '}' {
			candidate := out[:i+1]
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	return s
}
