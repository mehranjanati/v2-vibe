package llm

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestEmitChunkFinishReason(t *testing.T) {
	c := NewClient(Config{})
	ch := make(chan StreamChunk, 8)
	fr := ""
	if !c.emitChunk(context.Background(), `{"choices":[{"delta":{"content":"hi"}}]}`, ch, &fr) {
		t.Fatal("emitChunk stopped early")
	}
	// Finish-reason-only chunk (empty delta) must not be dropped.
	if !c.emitChunk(context.Background(), `{"choices":[{"delta":{},"finish_reason":"length"}]}`, ch, &fr) {
		t.Fatal("emitChunk stopped early")
	}
	if fr != "length" {
		t.Fatalf("finish reason = %q, want length", fr)
	}
	close(ch)
}

func TestReadSSEReportsFinishReasonOnDone(t *testing.T) {
	c := NewClient(Config{})
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n" +
		"data: [DONE]\n\n"
	ch := make(chan StreamChunk, 8)
	go c.readSSE(context.Background(), &fakeReadCloser{data: body}, ch)
	var last StreamChunk
	for cc := range ch {
		last = cc
	}
	if !last.Done || last.FinishReason != "length" {
		t.Fatalf("final chunk = %+v, want Done with finish=length", last)
	}
	_ = time.Second // keep time import stable if unused elsewhere
}

// TestReadSSEConnectionLost verifies that a body dying mid-stream
// (unexpected EOF, no [DONE]) surfaces as a ConnectionLostError chunk
// instead of a generic read error.
func TestReadSSEConnectionLost(t *testing.T) {
	c := NewClient(Config{})
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":"
	ch := make(chan StreamChunk, 8)
	go c.readSSE(context.Background(), &fakeReadCloser{data: body, die: io.ErrUnexpectedEOF}, ch)
	var last StreamChunk
	for cc := range ch {
		last = cc
	}
	if last.Err == nil {
		t.Fatal("expected an error chunk")
	}
	if !IsConnectionLost(last.Err) {
		t.Fatalf("expected ConnectionLostError, got %T: %v", last.Err, last.Err)
	}
}

// TestReadSSEBodyEndsWithoutDONE verifies a clean body close without
// [DONE] is also classified as connection lost.
func TestReadSSEBodyEndsWithoutDONE(t *testing.T) {
	c := NewClient(Config{})
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"
	ch := make(chan StreamChunk, 8)
	go c.readSSE(context.Background(), &fakeReadCloser{data: body}, ch)
	var last StreamChunk
	for cc := range ch {
		last = cc
	}
	if !IsConnectionLost(last.Err) {
		t.Fatalf("expected ConnectionLostError for missing [DONE], got %v", last.Err)
	}
}


type fakeReadCloser struct {
	data string
	die  error // returned after data is exhausted (nil => io.EOF)
}

func (f *fakeReadCloser) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		if f.die != nil {
			return 0, f.die
		}
		return 0, io.EOF
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func (f *fakeReadCloser) Close() error { return nil }
