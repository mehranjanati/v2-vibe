package engine

import (
	"testing"

	"backend/pkg/models"
)

func entry(p, content string) *models.FileEntry {
	return &models.FileEntry{FilePath: p, FileContents: content}
}

const indexHTML = `<!DOCTYPE html>
<html><head>
  <link rel="stylesheet" href="styles.css">
  <link rel="stylesheet" href="https://cdn.example.com/bootstrap.css">
</head>
<body>
  <script src="js/data.js"></script>
  <script src="js/store.js"></script>
  <script src="js/charts.js"></script>
  <script src="js/trade.js"></script>
  <script src="js/main.js"></script>
</body></html>`

func TestMissingReferencedAssetsDetectsMissing(t *testing.T) {
	files := map[string]*models.FileEntry{
		"public/index.html":   entry("public/index.html", indexHTML),
		"public/styles.css":   entry("public/styles.css", "body{}"),
		"public/js/data.js":   entry("public/js/data.js", "// data"),
		"public/js/store.js":  entry("public/js/store.js", "// store"),
		"public/js/charts.js": entry("public/js/charts.js", "// charts"),
		// trade.js and main.js are MISSING — exactly the real-world case.
	}
	got := missingReferencedAssets(files)
	want := map[string]bool{"public/js/trade.js": true, "public/js/main.js": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, m := range got {
		if !want[m] {
			t.Fatalf("unexpected missing entry %q (got %v)", m, got)
		}
	}
}

func TestMissingReferencedAssetsNoneMissing(t *testing.T) {
	files := map[string]*models.FileEntry{
		"public/index.html":   entry("public/index.html", indexHTML),
		"public/styles.css":   entry("public/styles.css", "body{}"),
		"public/js/data.js":   entry("public/js/data.js", ""),
		"public/js/store.js":  entry("public/js/store.js", ""),
		"public/js/charts.js": entry("public/js/charts.js", ""),
		"public/js/trade.js":  entry("public/js/trade.js", ""),
		"public/js/main.js":   entry("public/js/main.js", ""),
	}
	if got := missingReferencedAssets(files); len(got) != 0 {
		t.Fatalf("expected no missing, got %v", got)
	}
}

func TestMissingReferencedAssetsNoEntryHTML(t *testing.T) {
	files := map[string]*models.FileEntry{
		"public/styles.css": entry("public/styles.css", "body{}"),
	}
	if got := missingReferencedAssets(files); got != nil {
		t.Fatalf("no entry html => nil, got %v", got)
	}
}

func TestMissingReferencedAssetsRootEntry(t *testing.T) {
	files := map[string]*models.FileEntry{
		"index.html": entry("index.html", `<html><script src="app.js"></script></html>`),
	}
	got := missingReferencedAssets(files)
	if len(got) != 1 || got[0] != "app.js" {
		t.Fatalf("got %v, want [app.js]", got)
	}
}
