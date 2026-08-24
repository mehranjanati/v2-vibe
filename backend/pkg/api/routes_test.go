package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"backend/pkg/engine"
)

func newTestApp() *fiber.App {
	app := fiber.New()
	hub := engine.NewEngineHub(nil, nil, nil)
	RegisterRoutes(app, hub)
	return app
}

func performJSON(t *testing.T, app *fiber.App, method, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()
	body := make(map[string]any)
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON in %s %s response: %v", method, path, err)
	}
	return resp.StatusCode, body
}

func decodeSlice(t *testing.T, raw any) []map[string]any {
	t.Helper()
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected an array, got %T", raw)
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected an object in array, got %T", item)
		}
		out = append(out, m)
	}
	return out
}

func TestCapabilitiesResponseShape(t *testing.T) {
	app := newTestApp()
	status, body := performJSON(t, app, http.MethodGet, "/api/capabilities")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if body["success"] != true {
		t.Fatalf("expected success=true, got %v", body["success"])
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", body["data"])
	}
	features := decodeSlice(t, data["features"])
	if len(features) == 0 {
		t.Fatalf("expected at least one feature, got %d", len(features))
	}
	for _, f := range features {
		id, _ := f["id"].(string)
		if id == "" {
			t.Fatalf("feature missing id: %v", f)
		}
		caps, ok := f["capabilities"].(map[string]any)
		if !ok {
			t.Fatalf("feature %q missing capabilities object", id)
		}
		for _, key := range []string{
			"hasPreview", "hasLiveReload", "requiresSandbox", "requiresWebSocket",
			"supportedViews", "defaultView", "supportedExports",
			"hasCustomHeaderActions", "hasCustomSidebar", "hasCustomFileFilter",
			"behaviorType",
		} {
			if _, ok := caps[key]; !ok {
				t.Errorf("feature %q missing capability %q", id, key)
			}
		}
		if _, ok := caps["hasCustomHeaderActions"].(bool); !ok {
			t.Errorf("feature %q hasCustomHeaderActions must be a bool", id)
		}
		if _, ok := caps["supportedViews"].([]any); !ok {
			t.Errorf("feature %q supportedViews must be an array", id)
		}
	}
	for _, key := range []string{"version", "userAccountDeploy"} {
		if _, ok := data[key]; !ok {
			t.Errorf("data missing %q", key)
		}
	}
}

func TestPaginatedAppsResponseShape(t *testing.T) {
	for _, tc := range []struct{ path string }{
		{path: "/api/apps/public"},
		{path: "/api/user/apps"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			app := newTestApp()
			status, body := performJSON(t, app, http.MethodGet, tc.path)
			if status != http.StatusOK {
				t.Fatalf("expected 200, got %d", status)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				t.Fatalf("expected data to be an object, got %T", body["data"])
			}
			if _, ok := data["apps"].([]any); !ok {
				t.Fatalf("expected data.apps to be an array")
			}
			pagination, ok := data["pagination"].(map[string]any)
			if !ok {
				t.Fatalf("expected data.pagination to be an object, got %T", data["pagination"])
			}
			for _, key := range []string{"limit", "offset", "total"} {
				if _, ok := pagination[key]; !ok {
					t.Errorf("pagination missing %q", key)
				}
			}
			if _, ok := pagination["hasMore"].(bool); !ok {
				t.Errorf("pagination hasMore must be a bool")
			}
		})
	}
}

func TestStubResponsesAreValidJSON(t *testing.T) {
	app := newTestApp()
	paths := []string{
		"/api/auth/csrf-token", "/api/auth/profile", "/api/auth/providers",
		"/api/status", "/api/capabilities", "/api/apps/public", "/api/apps",
		"/api/apps/recent", "/api/apps/favorites",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(resp.Body); err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !json.Valid(buf.Bytes()) {
				t.Fatalf("response is not valid JSON: %q", buf.String())
			}
		})
	}
}
