package design

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

func TestParseSmoke(t *testing.T) {
	c, err := getCatalog()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Logf("styles=%d colors=%d type=%d landing=%d ux=%d motion=%d products=%d reasoning=%d",
		len(c.styles), len(c.colors), len(c.typography), len(c.landing),
		len(c.ux), len(c.motion), len(c.products), len(c.reasoning))
	if len(c.styles) == 0 || len(c.colors) == 0 || len(c.landing) == 0 {
		t.Fatal("core tables failed to parse")
	}
}

// TestBriefCoffee: a coffee landing request must NOT get a generic blue
// palette or a generic structure — it should get an on-brand palette,
// a real section order, a font pairing and motion/a11y guidance.
func TestBriefCoffee(t *testing.T) {
	b, err := Brief(context.Background(), "build simple coffee landing")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	t.Logf("product=%q pattern=%q style=%q", b.Product, b.Pattern.Name, b.Style.Name)
	t.Logf("primary=%s accent=%s bg=%s", b.Palette.Primary, b.Palette.Accent, b.Palette.Background)

	// Domain detection: "coffee" must resolve to the catalog's Bakery/Cafe
	// domain, not the default product (a regression here means the query
	// layer's stopword/synonym handling broke).
	if b.Product != "Bakery/Cafe" {
		t.Errorf("product = %q, want Bakery/Cafe (coffee → cafe synonym failed)", b.Product)
	}
	// Not generic: the palette must not be the hard default blue.
	if b.Palette.Primary == "#2563EB" {
		t.Error("coffee landing got the generic default blue palette — domain detection failed")
	}
	// On-brand: the catalog's cafe palette is warm brown + cream — the
	// primary must be a warm color (more red than blue).
	r, _, bl := hexRGB(b.Palette.Primary)
	if r <= bl {
		t.Errorf("primary %s is not a warm brand color", b.Palette.Primary)
	}
	if len(b.Pattern.SectionOrder) == 0 {
		t.Error("no section order")
	}
	if b.Palette.Background == "" || b.Palette.Foreground == "" {
		t.Error("palette incomplete")
	}
	if b.Type.HeadingStack == "" || !strings.Contains(b.Type.HeadingStack, ",") {
		t.Errorf("heading stack lacks a fallback: %q", b.Type.HeadingStack)
	}
	if len(b.A11y) == 0 {
		t.Error("no a11y rules")
	}
}

// hexRGB parses #RRGGBB into components; unknown formats return 0,0,0.
func hexRGB(s string) (r, g, b int) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)
}

// TestBriefDeterministic: the same query must always produce the same brief.
func TestBriefDeterministic(t *testing.T) {
	a, err1 := Brief(context.Background(), "a SaaS analytics dashboard landing page")
	b, err2 := Brief(context.Background(), "a SaaS analytics dashboard landing page")
	if err1 != nil || err2 != nil {
		t.Fatalf("brief errs: %v %v", err1, err2)
	}
	if a.Palette.Primary != b.Palette.Primary || a.Pattern.Name != b.Pattern.Name ||
		a.Type.HeadingFont != b.Type.HeadingFont {
		t.Errorf("non-deterministic brief: %+v vs %+v", a, b)
	}
}

// TestBriefFallbackNoMatch: a nonsensical query must still yield a valid,
// complete brief (defaults), never nil fields or a panic.
func TestBriefFallbackNoMatch(t *testing.T) {
	b, err := Brief(context.Background(), "zz qxww blorp 123")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	if b.Palette.Primary == "" || b.Palette.Background == "" {
		t.Error("fallback palette incomplete")
	}
	if b.Type.BodyStack == "" {
		t.Error("fallback typography incomplete")
	}
	if len(b.Pattern.SectionOrder) == 0 {
		t.Error("fallback pattern has no sections")
	}
}

// TestTokensCSSContract: the generated token block must define the full
// shadcn-style semantic vocabulary plus a .dark override.
func TestTokensCSSContract(t *testing.T) {
	b, err := Brief(context.Background(), "build simple coffee landing")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	css := b.TokensCSS()
	required := []string{
		"--background", "--foreground", "--card", "--card-foreground",
		"--primary", "--primary-foreground", "--secondary", "--secondary-foreground",
		"--muted", "--muted-foreground", "--accent", "--accent-foreground",
		"--destructive", "--destructive-foreground", "--border", "--input", "--ring",
		"--radius", "--font-heading", "--font-body",
	}
	for _, tok := range required {
		if !strings.Contains(css, tok) {
			t.Errorf("TokensCSS missing %s", tok)
		}
	}
	if !strings.Contains(css, ".dark {") {
		t.Error("no dark-mode override block")
	}
	darkIdx := strings.Index(css, ".dark {")
	dark := css[darkIdx:]
	if !strings.Contains(dark, "--background") {
		t.Error("dark block lacks --background")
	}
}

// TestRenderPlannerBounded: the planner render must stay compact enough to
// fit alongside the VFS context inside the model's window.
func TestRenderPlannerBounded(t *testing.T) {
	b, _ := Brief(context.Background(), "build simple coffee landing")
	out := b.RenderPlanner()
	if len(out) > 4000 {
		t.Errorf("planner render too large: %d chars", len(out))
	}
	if !strings.Contains(out, "Design direction") {
		t.Error("render lacks the header")
	}
	if !strings.Contains(out, "--primary") {
		t.Error("render lacks palette tokens")
	}
}

// TestFontStackFallbacks: font stacks must always carry a system fallback.
func TestFontStackFallbacks(t *testing.T) {
	cases := map[string]string{
		"Playfair Display": "serif",
		"Fira Code":        "monospace",
		"Inter":            "sans-serif",
	}
	for in, want := range cases {
		got := fontStack(in, false)
		if !strings.Contains(got, want) {
			t.Errorf("fontStack(%q) = %q, want fallback containing %q", in, got, want)
		}
		if !strings.Contains(got, ",") {
			t.Errorf("fontStack(%q) = %q, want a comma-separated stack", in, got)
		}
	}
}

// TestNormHex: color cells must normalize to #RRGGBB.
func TestNormHex(t *testing.T) {
	if got := normHex("6f4e37"); got != "#6f4e37" {
		t.Errorf("normHex = %q", got)
	}
	if got := normHex("#6f4e37"); got != "#6f4e37" {
		t.Errorf("normHex already-hex = %q", got)
	}
	if got := normHex(""); got != "" {
		t.Errorf("normHex empty = %q", got)
	}
}
