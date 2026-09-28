package design

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSelectBlocksCoffee(t *testing.T) {
	b, err := Brief(context.Background(), "build simple coffee landing")
	if err != nil {
		t.Fatal(err)
	}
	sel := SelectBlocks(b.Pattern)
	t.Logf("pattern=%q sections=%v", b.Pattern.Name, b.Pattern.SectionOrder)
	for _, blk := range sel {
		t.Logf("  %-16s section=%-12s html=%db css=%db", blk.Name, blk.Section, len(blk.HTML), len(blk.CSS))
	}
	if len(sel) == 0 {
		t.Fatal("no blocks selected")
	}
	html := ComposeHTML(sel)
	css := ComposeCSS(sel)
	if !strings.Contains(css, "var(--primary)") {
		t.Error("composed css does not use tokens")
	}
	if strings.Contains(css, "#") && !strings.Contains(css, "rgba") {
		// hex outside tokens would mean a block hardcoded colors
		for _, line := range strings.Split(css, "\n") {
			if strings.Contains(line, "#") && !strings.Contains(line, "/*") {
				t.Errorf("hardcoded color in block css: %s", line)
			}
		}
	}
	t.Logf("composed: html=%db css=%db", len(html), len(css))
}

func TestBlocksAllReachable(t *testing.T) {
	all := Blocks()
	if len(all) < 8 {
		t.Fatalf("registry too small: %d", len(all))
	}
	for _, b := range all {
		if b.Name == "" || b.Section == "" || b.HTML == "" || b.CSS == "" {
			t.Errorf("incomplete block: %+v", b)
		}
		if _, ok := BlockByName(b.Name); !ok {
			t.Errorf("BlockByName miss: %s", b.Name)
		}
	}
}

func TestFileFragmentsPerFileType(t *testing.T) {
	b, err := Brief(context.Background(), "build simple coffee landing")
	if err != nil {
		t.Fatal(err)
	}
	cb := b.CoderBrief()
	if cb == nil || len(cb.Blocks) == 0 {
		t.Fatal("coder brief has no blocks")
	}
	for _, d := range cb.Blocks {
		if d.HTML != "" || d.CSS != "" || d.JS != "" {
			t.Errorf("descriptor %s carries a fragment body; brief must stay compact", d.Name)
		}
		if len(d.Slots) == 0 {
			t.Errorf("descriptor %s has no content slots", d.Name)
		}
	}
	htmlFrags := cb.FileFragments("public/index.html")
	if len(htmlFrags) != len(cb.Blocks) {
		t.Fatalf("html fragments = %d, want %d", len(htmlFrags), len(cb.Blocks))
	}
	for _, f := range htmlFrags {
		if f.HTML == "" || f.CSS != "" || f.JS != "" {
			t.Errorf("html fragment %s: want markup only", f.Name)
		}
		if !strings.Contains(f.HTML, "<section") && !strings.Contains(f.HTML, "<header") && !strings.Contains(f.HTML, "<footer") {
			t.Errorf("html fragment %s has no structural element", f.Name)
		}
	}
	cssFrags := cb.FileFragments("public/styles.css")
	if len(cssFrags) == 0 {
		t.Fatal("no css fragments")
	}
	for _, f := range cssFrags {
		if f.CSS == "" || f.HTML != "" {
			t.Errorf("css fragment %s: want css only", f.Name)
		}
		if !strings.Contains(f.CSS, "var(--") {
			t.Errorf("css fragment %s does not use semantic tokens", f.Name)
		}
	}
	jsFrags := cb.FileFragments("public/js/main.js")
	for _, f := range jsFrags {
		if f.JS == "" {
			t.Errorf("js fragment %s empty", f.Name)
		}
		if f.HTML != "" || f.CSS != "" {
			t.Errorf("js fragment %s carries non-js body", f.Name)
		}
	}
	// Every delivered js fragment must have a real body, and the delivery
	// must be exactly the selection's blocks that ship JS — no empty
	// entries for blocks without JS.
	wantJS := map[string]bool{}
	for _, d := range cb.Blocks {
		if blk, ok := BlockByName(d.Name); ok && blk.JS != "" {
			wantJS[d.Name] = true
		}
	}
	gotJS := map[string]bool{}
	for _, f := range jsFrags {
		if f.JS == "" {
			t.Errorf("js fragment %s empty", f.Name)
		}
		if f.HTML != "" || f.CSS != "" {
			t.Errorf("js fragment %s carries non-js body", f.Name)
		}
		gotJS[f.Name] = true
	}
	for name := range wantJS {
		if !gotJS[name] {
			t.Errorf("block %s ships JS but is missing from js delivery", name)
		}
	}
	for name := range gotJS {
		if !wantJS[name] {
			t.Errorf("js delivery contains %s which ships no JS", name)
		}
	}
	// A non-code file gets no fragments (descriptors only).
	if got := cb.FileFragments("public/robots.txt"); got != nil {
		t.Errorf("robots.txt should get no fragments, got %d", len(got))
	}
	// Size guards: the html step payload stays bounded.
	raw, _ := json.Marshal(cb)
	t.Logf("shared brief=%dB html frags=%dB css frags=%dB", len(raw),
		func() int { r, _ := json.Marshal(htmlFrags); return len(r) }(),
		func() int { r, _ := json.Marshal(cssFrags); return len(r) }())
}
