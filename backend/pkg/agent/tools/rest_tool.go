// Package tools provides dynamic Eino tools for the stateless agent
// engine: OpenAPI-driven REST API execution and Redis-backed Virtual
// File System (VFS) mutation.
package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// RESTEndpoint describes one callable operation exposed to the LLM,
// derived from an OpenAPI schema document.
type RESTEndpoint struct {
	// OperationID is the stable identifier the LLM uses to invoke the
	// operation (OpenAPI `operationId`).
	OperationID string
	// Method is the HTTP method (GET, POST, ...).
	Method string
	// Path is the URL path template, may contain {placeholders}.
	Path string
	// Summary tells the model when to use this operation.
	Summary string
}

// RESTToolConfig configures a dynamic REST tool.
type RESTToolConfig struct {
	// BaseURL is the API root all endpoint paths are resolved against.
	BaseURL string
	// Endpoints lists the operations from the OpenAPI schema the model
	// may call.
	Endpoints []RESTEndpoint
	// DefaultHeaders are sent with every request (e.g. auth, tenant ID).
	DefaultHeaders map[string]string
	// Timeout bounds a single HTTP call. Defaults to 30s.
	Timeout time.Duration
	// MaxResponseBody limits how many bytes of a response body are fed
	// back to the LLM. Defaults to 16 KiB.
	MaxResponseBody int
}

// restToolInput is the JSON schema the model fills when calling the tool.
// It is deliberately generic: any OpenAPI operation can be expressed with
// it, so the tool stays dynamic when the schema changes.
type restToolInput struct {
	// OperationID identifies which configured endpoint to call.
	OperationID string `json:"operation_id"`
	// PathParams fills {placeholders} in the endpoint path template.
	PathParams map[string]string `json:"path_params,omitempty"`
	// Query is appended to the URL as the query string.
	Query map[string]string `json:"query,omitempty"`
	// Headers override/add per-request headers.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is the raw JSON request body (sent as-is for non-GET calls).
	Body string `json:"body,omitempty"`
}

// restToolOutput is returned to the model after execution.
type restToolOutput struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// RESTTool executes configured OpenAPI operations over HTTP. It implements
// tool.InvokableTool, so Eino's ReAct loop can route LLM tool calls to it
// and feed the JSON result back into the next model turn.
type RESTTool struct {
	tool.InvokableTool
	cfg RESTToolConfig
	hc  *http.Client
}

// NewRESTTool builds the dynamic REST tool from cfg. The returned tool's
// name is "rest_api" and its description enumerates the configured
// operations so the model can pick correctly.
func NewRESTTool(cfg RESTToolConfig) (*RESTTool, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("tools: RESTTool: BaseURL is required")
	}
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("tools: RESTTool: at least one endpoint is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxResponseBody <= 0 {
		cfg.MaxResponseBody = 16 * 1024
	}

	var b strings.Builder
	b.WriteString("Execute a configured REST API operation and return the HTTP response.\n")
	b.WriteString("Available operations:\n")
	for _, ep := range cfg.Endpoints {
		fmt.Fprintf(&b, "- %s (%s %s): %s\n", ep.OperationID, ep.Method, ep.Path, ep.Summary)
	}

	t := &RESTTool{cfg: cfg, hc: &http.Client{Timeout: cfg.Timeout}}
	it, err := utils.InferTool("rest_api", b.String(), t.invoke)
	if err != nil {
		return nil, fmt.Errorf("tools: RESTTool: build tool info: %w", err)
	}
	t.InvokableTool = it
	return t, nil
}

// ToolInfo exposes the tool metadata (name/description/schema).
func (t *RESTTool) ToolInfo() *schema.ToolInfo {
	ti, _ := t.InvokableTool.Info(context.Background())
	return ti
}

func (t *RESTTool) invoke(ctx context.Context, in restToolInput) (*restToolOutput, error) {
	var ep *RESTEndpoint
	for i := range t.cfg.Endpoints {
		if t.cfg.Endpoints[i].OperationID == in.OperationID {
			ep = &t.cfg.Endpoints[i]
			break
		}
	}
	if ep == nil {
		return nil, fmt.Errorf("tools: RESTTool: unknown operation_id %q", in.OperationID)
	}

	path := ep.Path
	for k, v := range in.PathParams {
		path = strings.ReplaceAll(path, "{"+k+"}", v)
	}

	q := ""
	if len(in.Query) > 0 {
		parts := make([]string, 0, len(in.Query))
		for k, v := range in.Query {
			parts = append(parts, k+"="+v)
		}
		q = "?" + strings.Join(parts, "&")
	}

	url := strings.TrimRight(t.cfg.BaseURL, "/") + "/" + strings.TrimLeft(path, "/") + q

	method := strings.ToUpper(ep.Method)
	var bodyReader io.Reader
	if method != http.MethodGet && method != http.MethodHead && in.Body != "" {
		bodyReader = strings.NewReader(in.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("tools: RESTTool: build request: %w", err)
	}
	for k, v := range t.cfg.DefaultHeaders {
		req.Header.Set(k, v)
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	if bodyReader != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tools: RESTTool: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(t.cfg.MaxResponseBody)))
	if err != nil {
		return nil, fmt.Errorf("tools: RESTTool: read response: %w", err)
	}

	return &restToolOutput{Status: resp.StatusCode, Body: string(body)}, nil
}
