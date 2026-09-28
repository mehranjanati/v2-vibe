//go:build e2e

// Cloudflare E2E: runs against the DEPLOYED vibesdk-v2 Worker
// (https://vibesdk-v2.mehranjannati.workers.dev) and exercises the auth
// plane end-to-end: platform info, CSRF, register, login, profile, and
// bad-credential rejection.
//
// NOTE: the register/login flow writes a throwaway test user into the
// production D1 database (clearly marked e2e-…@vibeos.test).
//
// Run: VIBE_E2E=1 go test -tags e2e ./e2e/ -run Cloudflare -v
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"testing"
	"time"
)

const cfBase = "https://vibesdk-v2.mehranjannati.workers.dev"

func cfDo(t *testing.T, method, url string, body []byte, jar *cookiejar.Jar) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", cfBase)
	client := &http.Client{Timeout: 15 * time.Second, Jar: jar}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func cfGet(t *testing.T, path string, jar *cookiejar.Jar) (int, map[string]any) {
	t.Helper()
	return cfDo(t, http.MethodGet, cfBase+path, nil, jar)
}

func cfPost(t *testing.T, path string, body any, jar *cookiejar.Jar) (int, map[string]any) {
	t.Helper()
	buf, _ := json.Marshal(body)
	return cfDo(t, http.MethodPost, cfBase+path, buf, jar)
}

func okData(t *testing.T, code int, body map[string]any) map[string]any {
	t.Helper()
	if code < 200 || code >= 300 {
		t.Fatalf("unexpected status %d: %v", code, body)
	}
	if success, _ := body["success"].(bool); !success {
		t.Fatalf("expected success:true, got %v", body)
	}
	data, _ := body["data"].(map[string]any)
	return data
}

func TestCloudflarePlatformEndpoints(t *testing.T) {
	jar, _ := cookiejar.New(nil)

	code, body := cfGet(t, "/api/status", jar)
	data := okData(t, code, body)
	if data["status"] != "ok" || data["service"] != "vibesdk-v2" {
		t.Fatalf("status payload: %v", data)
	}

	code, body = cfGet(t, "/api/capabilities", jar)
	okData(t, code, body)

	code, body = cfGet(t, "/api/auth/providers", jar)
	data = okData(t, code, body)
	if email, _ := data["requiresEmailAuth"].(bool); !email {
		t.Fatalf("providers payload: %v", data)
	}
}

func TestCloudflareServesNewSPA(t *testing.T) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(cfBase + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	html := string(raw)
	if resp.StatusCode != 200 || !strings.Contains(html, "<div id=\"root\">") {
		t.Fatalf("SPA shell not served: status=%d len=%d", resp.StatusCode, len(html))
	}
	m := regexpFind(`src="(/assets/index-[^"]+\.js)"`, html)
	if m == "" {
		t.Fatal("no hashed index js asset in shell")
	}
	aresp, err := client.Get(cfBase + m)
	if err != nil || aresp.StatusCode != 200 {
		t.Fatalf("asset %s unreachable: %v", m, err)
	}
	aresp.Body.Close()
}

func TestCloudflareAuthFlow(t *testing.T) {
	jar, _ := cookiejar.New(nil)

	// 1. CSRF token.
	code, body := cfGet(t, "/api/auth/csrf-token", jar)
	data := okData(t, code, body)
	if token, _ := data["token"].(string); token == "" {
		t.Fatalf("csrf token missing: %v", data)
	}

	// 2. Register a throwaway user.
	email := fmt.Sprintf("e2e-%d@vibeos.test", time.Now().UnixNano())
	registerBody := map[string]any{
		"email":    email,
		"password": "e2e-password-123",
		"name":     "E2E Test",
	}
	code, body = cfPost(t, "/api/auth/register", registerBody, jar)
	if code == 409 {
		t.Log("user already registered (rerun); continuing to login")
	} else {
		okData(t, code, body)
	}

	// 3. Login (fresh jar proves a server-side session, not just cookies).
	loginJar, _ := cookiejar.New(nil)
	code, body = cfPost(t, "/api/auth/login", registerBody, loginJar)
	loginData := okData(t, code, body)
	if accessToken, _ := loginData["accessToken"].(string); accessToken == "" {
		t.Fatalf("no accessToken in login response: %v", loginData)
	}

	// 4. Profile with the session cookie.
	code, body = cfGet(t, "/api/auth/profile", loginJar)
	data = okData(t, code, body)
	user, _ := data["user"].(map[string]any)
	if user == nil || user["email"] != email {
		t.Fatalf("profile email mismatch: %v", data)
	}
	t.Logf("auth round-trip OK for %s", email)
}

func TestCloudflareAuthRejectsBadCredentials(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	code, body := cfPost(t, "/api/auth/login", map[string]any{
		"email":    "nobody-e2e@vibeos.test",
		"password": "wrong-password",
	}, jar)
	if code >= 200 && code < 300 {
		t.Fatalf("bad credentials must not succeed: %d %v", code, body)
	}
	if success, _ := body["success"].(bool); success {
		t.Fatalf("bad credentials returned success:true: %v", body)
	}
}

// TestCloudflareControlPlaneSmoke verifies the deployed frontend's control
// plane (the Go backend the Worker config points at) is alive.
func TestCloudflareControlPlaneSmoke(t *testing.T) {
	controlURL := envOr("E2E_CONTROL_PLANE_URL", "http://127.0.0.1:8080")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(controlURL + "/health")
	if err != nil {
		t.Fatalf("control plane unreachable at %s: %v", controlURL, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("control plane unhealthy: %v", body)
	}
}

func regexpFind(pattern, s string) string {
	re := regexp.MustCompile(pattern)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

