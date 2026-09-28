package design

import (
	"context"
	"strings"
	"testing"
)

// TestQueryTokensNormalization pins the query-layer behavior: instruction
// verbs are dropped, domain words survive, synonyms expand into the
// catalog's vocabulary.
func TestQueryTokensNormalization(t *testing.T) {
	got := queryTokens("build simple coffee landing")
	joined := strings.Join(got, ",")
	if strings.Contains(joined, "build") || strings.Contains(joined, "simple") {
		t.Errorf("stopwords leaked into tokens: %v", got)
	}
	if !strings.Contains(joined, "coffee") || !strings.Contains(joined, "cafe") {
		t.Errorf("coffee query lost domain tokens (want coffee+cafe): %v", got)
	}
	if got := queryTokens(""); len(got) != 0 {
		t.Errorf("empty query produced tokens: %v", got)
	}
}

// TestDomainResolutionMatrix is the "always for code generation" guarantee:
// queries across industries must land on their catalog product rows instead
// of generic defaults. Each entry is (query, expected product substring).
func TestDomainResolutionMatrix(t *testing.T) {
	cases := []struct{ query, wantProduct string }{
		{"build simple coffee landing", "bakery/cafe"},
		{"a pizzeria website with menu", "restaurant"},
		{"gym landing page", "fitness"},
		{"law firm site", "legal"},
		{"real estate agency landing", "real estate"},
		{"dental clinic homepage", "dental"},
		{"SaaS analytics dashboard landing page", "dashboard"},
		{"crypto trading platform", "crypto"},
		{"photography portfolio", "photography"},
		{"online course for learning spanish", "learning"},
	}
	for _, tc := range cases {
		b, err := Brief(context.Background(), tc.query)
		if err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		t.Logf("%-46q → %-34q primary=%s", tc.query, b.Product, b.Palette.Primary)
		if !strings.Contains(strings.ToLower(b.Product), tc.wantProduct) {
			t.Errorf("%q → product %q, want something matching %q",
				tc.query, b.Product, tc.wantProduct)
		}
		if b.Palette.Primary == "" {
			t.Errorf("%q: empty palette", tc.query)
		}
	}
}

// TestTokenizeBasic covers the tokenizer edge cases.
func TestTokenizeBasic(t *testing.T) {
	got := tokenize("Hello, World! 42-cool")
	if strings.Join(got, "|") != "hello|world|42|cool" {
		t.Errorf("tokenize = %v", got)
	}
}
