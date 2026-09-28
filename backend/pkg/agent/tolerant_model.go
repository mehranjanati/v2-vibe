package agent

import (
	"context"
	"errors"
	"io"
	"log"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// tolerantStreamModel wraps a tool-calling chat model so that a single
// malformed stream frame (e.g. a Workers AI / gateway delta carrying a
// non-string "content") does NOT kill the whole generation.
//
// Why here: with ADK streaming enabled, the compose graph node consumes
// the model stream INTERNALLY before our drainStream ever sees it, and a
// malformed frame surfaces as `[NodeRunError] failed to receive stream
// chunk: ...` on the runner event — aborting the run. By converting the
// malformed frame into a graceful end-of-stream at the model layer, every
// consumer (ADK nodes, drainStream, planexecute) sees a clean stream and
// keeps the partial output; the room's truncation/retry path then
// re-emits whatever was left unfinished.
type tolerantStreamModel struct {
	model.ToolCallingChatModel
}

// newTolerantStreamModel wraps m when it supports tool calling; otherwise
// returns m unchanged.
func newTolerantStreamModel(m model.ChatModel) model.ChatModel {
	if tcm, ok := m.(model.ToolCallingChatModel); ok {
		return &tolerantStreamModel{ToolCallingChatModel: tcm}
	}
	return m
}

// Stream delegates to the inner model and shields the returned stream.
func (t *tolerantStreamModel) Stream(
	ctx context.Context,
	in []*schema.Message,
	opts ...model.Option,
) (*schema.StreamReader[*schema.Message], error) {
	sr, err := t.ToolCallingChatModel.Stream(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return tolerateMalformedChunks(sr), nil
}

// WithTools keeps the tolerance wrapper on derived tool-bound models —
// compose/ADK call this to bind tool schemas, and the derived model is
// what actually streams.
func (t *tolerantStreamModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := t.ToolCallingChatModel.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &tolerantStreamModel{ToolCallingChatModel: inner}, nil
}

// BindTools delegates to the inner model when it supports the deprecated
// binding style (model.ChatModel requires this method).
func (t *tolerantStreamModel) BindTools(tools []*schema.ToolInfo) error {
	type binder interface{ BindTools([]*schema.ToolInfo) error }
	if b, ok := t.ToolCallingChatModel.(binder); ok {
		return b.BindTools(tools)
	}
	return errors.New("agent: inner model does not support BindTools")
}

// tolerateMalformedChunks returns a stream that ends gracefully (EOF)
// when the inner stream fails with a single-frame JSON shape mismatch,
// and forwards every other error untouched.
//
// Ideally a numeric "content" would be coerced to text ("8") and the
// stream would CONTINUE; that coercion happens inside the upstream
// eino-ext acl/openai client at unmarshal time, so by the time the error
// reaches this wrapper the offending frame is already lost. Ending the
// stream gracefully here lets the room's truncation/retry path re-emit
// whatever single token was lost instead of aborting the whole run.
func tolerateMalformedChunks(sr *schema.StreamReader[*schema.Message]) *schema.StreamReader[*schema.Message] {
	out, sw := schema.Pipe[*schema.Message](1)
	go func() {
		defer sw.Close()
		defer sr.Close() //nolint:errcheck // always close the inner stream
		for {
			msg, err := sr.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				if isMalformedChunkError(err) {
					log.Printf("agent: tolerating malformed stream chunk (%v); ending stream with partial output", err)
					return
				}
				_ = sw.Send(nil, err)
				return
			}
			if closed := sw.Send(msg, nil); closed {
				return
			}
		}
	}()
	return out
}
