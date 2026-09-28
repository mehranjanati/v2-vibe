package llm

import "testing"

// TestNewConfigFromEnvWorkersAI verifies that when no AI Gateway URL is set,
// the config falls back to Workers AI's OpenAI-compatible endpoint built from
// CLOUDFLARE_ACCOUNT_ID (so the backend needs no AI Gateway at all).
func TestNewConfigFromEnvWorkersAI(t *testing.T) {
	orig := envLookup
	env := map[string]string{
		"AI_GATEWAY_URL":        "",
		"AI_GATEWAY_API_KEY":    "",
		"CLOUDFLARE_ACCOUNT_ID": "acct123",
		"CLOUDFLARE_API_TOKEN":  "cf-token-abc",
		"DEFAULT_MODEL":         "@cf/qwen/qwen2.5-coder-32b-instruct",
	}
	envLookup = func(k string) string { return env[k] }
	defer func() { envLookup = orig }()

	cfg := NewConfigFromEnv()

	wantURL := "https://api.cloudflare.com/client/v4/accounts/acct123/ai/v1"
	if cfg.GatewayURL != wantURL {
		t.Errorf("GatewayURL = %q, want %q", cfg.GatewayURL, wantURL)
	}
	// API key falls back to CLOUDFLARE_API_TOKEN when no gateway key is set.
	if cfg.APIKey != "cf-token-abc" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "cf-token-abc")
	}
	if cfg.Model != "@cf/qwen/qwen2.5-coder-32b-instruct" {
		t.Errorf("Model = %q, want default workers model", cfg.Model)
	}
}

// TestNewConfigFromEnvGateway keeps AI Gateway mode intact when the URL is set.
func TestNewConfigFromEnvGateway(t *testing.T) {
	orig := envLookup
	env := map[string]string{
		"AI_GATEWAY_URL":        "https://gateway.ai.cloudflare.com/v1/acct/gw",
		"AI_GATEWAY_API_KEY":    "gw-key",
		"CLOUDFLARE_ACCOUNT_ID": "acct123",
		"CLOUDFLARE_API_TOKEN":  "cf-token-abc",
		"DEFAULT_MODEL":         "model-x",
	}
	envLookup = func(k string) string { return env[k] }
	defer func() { envLookup = orig }()

	cfg := NewConfigFromEnv()

	if cfg.GatewayURL != "https://gateway.ai.cloudflare.com/v1/acct/gw" {
		t.Errorf("GatewayURL = %q, want the configured gateway URL", cfg.GatewayURL)
	}
	// Uses the gateway key preferentially.
	if cfg.APIKey != "gw-key" {
		t.Errorf("APIKey = %q, want gateway key", cfg.APIKey)
	}
}
