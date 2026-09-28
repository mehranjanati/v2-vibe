//go:build e2e

// Full-stack E2E: drives the REAL Go backend (docker compose on :8080)
// over WebSocket exactly like the frontend does, against the REAL LLM
// (Workers AI / AI Gateway), then inspects the generated output for
// structure and preview-readiness.
//
// Prereqs: docker compose up (redis + backend), LLM env configured.
// Run: go test -tags e2e ./e2e/ -run E2EFullStack -v
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var (
	baseURL   = envOr("E2E_BASE_URL", "http://127.0.0.1:8080")
	prompt    = envOr("E2E_PROMPT", "یک فروشگاه آنلاین ساده با لیست محصولات، سبد خرید و جستجو بساز")
	e2ePrompt = regexp.MustCompile(`(?i)<script[^>]*\bsrc=["']([^"']+)["']`)
	e2eHref   = regexp.MustCompile(`(?i)<link[^>]*\bhref=["']([^"']+)["']`)
)

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

type wsEvent struct {
	Type           string `json:"type"`
	ConversationID string `json:"conversationId,omitempty"`
	Plan           string `json:"plan,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Message        string `json:"message,omitempty"`
	FilePath       string `json:"filePath,omitempty"`
	Error          string `json:"error,omitempty"`
	File           *struct {
		FilePath     string `json:"filePath"`
		FileContents string `json:"fileContents"`
	} `json:"file,omitempty"`
}

func TestE2EFullStackGeneration(t *testing.T) {
	if os.Getenv("VIBE_E2E") != "1" {
		t.Skip("set VIBE_E2E=1 to run the full-stack E2E")
	}

	// ---- 1. Create a session ----
	resp, err := http.Post(baseURL+"/api/agent/session", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	var sess struct {
		AgentID      string `json:"agentId"`
		WebsocketURL string `json:"websocketUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil || sess.AgentID == "" {
		t.Fatalf("bad session response: %v", err)
	}
	wsURL := strings.Replace(sess.WebsocketURL, "http://", "ws://", 1)
	t.Logf("session=%s ws=%s", sess.AgentID, wsURL)

	// ---- 2. Connect WebSocket ----
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	send := func(v any) {
		b, _ := json.Marshal(v)
		if err := conn.WriteJSON(v); err != nil {
			t.Errorf("ws write %s: %v", b, err)
		}
	}

	// ---- 3. Kick off generation ----
	send(map[string]any{"type": "generate_all", "message": prompt})

	// ---- 4. Consume events until terminal ----
	var (
		plan       string
		files      = map[string]string{}
		terminated string
		deadline   = time.Now().Add(8 * time.Minute)
	)
	conn.SetReadDeadline(deadline)
	for terminated == "" {
		var raw json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var ev wsEvent
		_ = json.Unmarshal(raw, &ev)
		switch ev.Type {
		case "plan_proposed":
			plan = ev.Plan
			t.Logf("[plan proposed, %d bytes] sending plan_approved", len(plan))
			send(map[string]any{"type": "plan_approved"})
		case "file_generated":
			if ev.File != nil {
				files[ev.File.FilePath] = ev.File.FileContents
				t.Logf("[file] %s (%d bytes)", ev.File.FilePath, len(ev.File.FileContents))
			}
		case "generation_complete":
			terminated = "complete"
		case "generation_interrupted", "generation_cancelled":
			terminated = ev.Type + ": " + ev.Reason
		case "error":
			t.Errorf("[error event] %s", ev.Error)
		}
	}
	t.Logf("terminal: %s | files=%d | planBytes=%d", terminated, len(files), len(plan))

	// ---- 5. Inspect the OUTPUT (structure + preview readiness) ----
	if plan == "" {
		t.Error("plan is empty")
	} else {
		t.Logf("PLAN:\n%.400s", plan)
	}

	var entry string
	for _, cand := range []string{"public/index.html", "index.html"} {
		if _, ok := files[cand]; ok {
			entry = cand
			break
		}
	}
	if entry == "" {
		t.Fatalf("no entry index.html generated; files=%v", keys(files))
	}

	// Referenced-but-missing scan (mirrors the backend gap-fill detector).
	html := files[entry]
	dir := strings.TrimSuffix(entry, "/index.html")
	refs := map[string]bool{}
	for _, re := range []*regexp.Regexp{e2ePrompt, e2eHref} {
		for _, m := range re.FindAllStringSubmatch(html, -1) {
			refs[m[1]] = true
		}
	}
	var missing []string
	for ref := range refs {
		ref = strings.TrimSpace(ref)
		low := strings.ToLower(ref)
		if !strings.Contains(ref, ".") ||
			strings.HasPrefix(low, "http") || strings.HasPrefix(ref, "//") ||
			strings.HasSuffix(low, ".json") ||
			(!strings.HasSuffix(low, ".js") && !strings.HasSuffix(low, ".css")) {
			continue
		}
		found := false
		for p := range files {
			if strings.TrimPrefix(p, "/") == ref || strings.TrimPrefix(p, "/") == dir+"/"+ref {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, ref)
		}
	}

	t.Logf("=== OUTPUT REPORT ===")
	t.Logf("entry=%s", entry)
	for p, c := range files {
		t.Logf("  %-28s %6d bytes", p, len(c))
	}
	t.Logf("missing referenced assets: %v", missing)

	if len(files) < 3 {
		t.Errorf("too few files generated (%d) — structure incomplete", len(files))
	}
	if len(missing) > 0 {
		t.Errorf("entry HTML references files that were never generated: %v — preview WILL be broken", missing)
	}
	if !strings.Contains(files[entry], "</html>") {
		t.Errorf("entry HTML not terminated (truncated)")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

var _ = fmt.Sprintf
