package engine

import (
	"context"
	"strings"
	"testing"
)

// TestWriteSafeStubForMissing verifies the fallback stub is syntax-safe
// and references the real element ids from index.html.
func TestWriteSafeStubForMissing(t *testing.T) {
	r := NewProjectRoom("stub-test", nil, nil, nil)
	go r.Run()
	defer r.Stop()

	r.UpsertFile("public/index.html",
		`<html><head></head><body>`+
			`<div id="product-grid" class="grid"></div>`+
			`<button id="cart-btn">Cart</button>`+
			`</body></html>`, "")

	r.writeSafeStubForMissing(context.Background(), "public/js/products.js")

	vfs := r.GetVFS()
	entry, ok := vfs["public/js/products.js"]
	if !ok {
		t.Fatalf("stub not written: %v", keysOf(vfs))
	}
	stub := entry.FileContents
	if strings.Contains(stub, "id: ,") || strings.Contains(stub, "price: .") {
		t.Fatalf("stub contains broken patterns: %s", stub)
	}
	if !strings.Contains(stub, `document.getElementById("product-grid")`) {
		t.Fatalf("stub does not use real element id: %s", stub)
	}
	if !strings.Contains(stub, "cart-btn") {
		t.Fatalf("stub missing cart-btn id reference: %s", stub)
	}
	t.Logf("stub (%d bytes):\n%s", len(stub), stub)
}

// TestWriteSafeStubForMissingCSS (A4): a still-missing stylesheet gets a
// minimal, valid CSS stub so the preview renders un-styled instead of
// 404-ing (the previous behavior skipped non-.js files entirely).
func TestWriteSafeStubForMissingCSS(t *testing.T) {
	r := NewProjectRoom("stub-css-test", nil, nil, nil)
	go r.Run()
	defer r.Stop()

	r.UpsertFile("public/index.html",
		`<html><head><link rel="stylesheet" href="styles.css"></head><body>ok</body></html>`, "")

	r.writeSafeStubForMissing(context.Background(), "public/styles.css")

	vfs := r.GetVFS()
	entry, ok := vfs["public/styles.css"]
	if !ok {
		t.Fatalf("css stub not written: %v", keysOf(vfs))
	}
	// CSS stubs contain no executable JS — the broken-JS pattern check is a
	// guard against the stub accidentally being JS content in a .css file.
	if strings.Contains(entry.FileContents, "id: ,") || strings.Contains(entry.FileContents, "price: .") {
		t.Fatalf("css stub contains broken JS patterns: %s", entry.FileContents)
	}
	t.Logf("css stub (%d bytes):\n%s", len(entry.FileContents), entry.FileContents)
}
