package tools

import (
	"encoding/json"
	"testing"
)

func TestRepairJSONValidUnchanged(t *testing.T) {
	in := `{"path":"a.html","content":"<p>x</p>"}`
	if got := RepairJSON(in); got != in {
		t.Fatalf("got %q", got)
	}
}

func TestRepairJSONTrailingComma(t *testing.T) {
	in := `{"path":"a.html","content":"x",}`
	got := RepairJSON(in)
	if got != `{"path":"a.html","content":"x"}` {
		t.Fatalf("got %q", got)
	}
}

func TestRepairJSONTruncated(t *testing.T) {
	in := `{"path":"a.html","content":"<p>x</p>",`
	got := RepairJSON(in)
	// Must become valid JSON containing the path (content dropped or closed).
	var m map[string]any
	if err := unmarshalForTest(got, &m); err != nil {
		t.Fatalf("repaired not valid JSON: %q (%v)", got, err)
	}
	if m["path"] != "a.html" {
		t.Fatalf("path lost: %q", got)
	}
}

func TestRepairJSONUnterminatedString(t *testing.T) {
	in := "{\"path\":\"a.html\",\"content\":\"line1\nline2\"}"
	got := RepairJSON(in)
	var m map[string]any
	if err := unmarshalForTest(got, &m); err != nil {
		t.Fatalf("repaired not valid JSON: %q (%v)", got, err)
	}
	if m["path"] != "a.html" {
		t.Fatalf("path lost: %q", got)
	}
}

func TestRepairJSONUnbalancedBraces(t *testing.T) {
	in := `{"nodes":[{"id":"n1"}]`
	got := RepairJSON(in)
	if !jsonValidForTest(got) {
		t.Fatalf("got %q", got)
	}
}

func unmarshalForTest(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func jsonValidForTest(s string) bool { return json.Valid([]byte(s)) }
