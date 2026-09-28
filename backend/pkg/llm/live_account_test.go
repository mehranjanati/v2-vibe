package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveNewAccountStream is a live, opt-in check that the configured
// provider credentials (AI_GATEWAY_URL / AI_GATEWAY_API_KEY / DEFAULT_MODEL)
// authenticate against a real account and that streaming works end to end.
// Skipped unless VIBESDK_LIVE_LLM=1 so CI stays offline.
func TestLiveNewAccountStream(t *testing.T) {
	if os.Getenv("VIBESDK_LIVE_LLM") != "1" {
		t.Skip("set VIBESDK_LIVE_LLM=1 to run the live provider test")
	}
	cfg := NewConfigFromEnv()
	t.Logf("gateway=%s model=%s", cfg.GatewayURL, cfg.Model)
	if cfg.GatewayURL == "" || cfg.APIKey == "" {
		t.Fatal("AI_GATEWAY_URL / AI_GATEWAY_API_KEY must be set")
	}

	client := NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	req := ChatRequest{
		Model:     cfg.Model,
		Messages:  []ChatMessage{{Role: "user", Content: "Reply with exactly: ACCOUNT_OK"}},
		MaxTokens: 12,
	}
	ch, err := client.StreamChat(ctx, req)
	if err != nil {
		t.Fatalf("stream failed (auth/429?): %v", err)
	}
	var out strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
		out.WriteString(chunk.Content)
	}
	got := strings.TrimSpace(out.String())
	t.Logf("streamed: %q", got)
	if !strings.Contains(strings.ToUpper(got), "ACCOUNT_OK") {
		t.Errorf("unexpected completion: %q", got)
	}
}
