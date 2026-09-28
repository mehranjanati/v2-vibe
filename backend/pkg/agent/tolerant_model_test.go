package agent

import (
	"errors"
	"io"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// drainAll reads a wrapped stream to completion and returns the concatenated
// content plus the terminal error (nil on clean EOF).
func drainAll(t *testing.T, sr *schema.StreamReader[*schema.Message]) (string, error) {
	t.Helper()
	defer sr.Close() //nolint:errcheck
	var content string
	for {
		msg, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			return content, nil
		}
		if err != nil {
			return content, err
		}
		if msg != nil {
			content += msg.Content
		}
	}
}

func TestTolerateMalformedChunksEndsGracefully(t *testing.T) {
	in, sw := schema.Pipe[*schema.Message](2)
	sw.Send(&schema.Message{Content: "hello "}, nil)
	sw.Send(nil, errors.New(`json: cannot unmarshal number into Go struct field .content of type string`))
	sw.Close()

	content, err := drainAll(t, tolerateMalformedChunks(in))
	if err != nil {
		t.Fatalf("malformed chunk must end stream gracefully, got err: %v", err)
	}
	if content != "hello " {
		t.Fatalf("partial content lost: %q", content)
	}
}

func TestTolerateMalformedChunksForwardsRealErrors(t *testing.T) {
	in, sw := schema.Pipe[*schema.Message](2)
	sw.Send(&schema.Message{Content: "partial"}, nil)
	realErr := errors.New("stream interrupted: connection reset")
	sw.Send(nil, realErr)
	sw.Close()

	content, err := drainAll(t, tolerateMalformedChunks(in))
	if err == nil {
		t.Fatal("real transport errors must be forwarded, not swallowed")
	}
	if content != "partial" {
		t.Fatalf("content before error lost: %q", content)
	}
}

func TestTolerateMalformedChunksCleanStream(t *testing.T) {
	in, sw := schema.Pipe[*schema.Message](3)
	sw.Send(&schema.Message{Content: "a"}, nil)
	sw.Send(&schema.Message{Content: "b"}, nil)
	sw.Close()

	content, err := drainAll(t, tolerateMalformedChunks(in))
	if err != nil || content != "ab" {
		t.Fatalf("clean stream damaged: content=%q err=%v", content, err)
	}
}