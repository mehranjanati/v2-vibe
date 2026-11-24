package bundle

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"time"

	"backend/pkg/composition"
)

// The embedded catalog must stay byte-identical to the docs originals: the
// docs gate validates those files, and a drift would mean the runtime
// resolves against assets nobody reviewed.
func TestBundleMatchesDocsExamples(t *testing.T) {
	docs := os.DirFS("../../../../docs/architecture/manifest-schemas/examples")
	entries, err := fs.ReadDir(docs, ".")
	if err != nil {
		t.Fatalf("read docs examples: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the docs example directory is empty; the parity check would prove nothing")
	}
	for _, entry := range entries {
		name := entry.Name()
		want, err := fs.ReadFile(docs, name)
		if err != nil {
			t.Fatalf("read docs %s: %v", name, err)
		}
		got, err := assets.ReadFile("assets/" + name)
		if err != nil {
			t.Fatalf("the embedded catalog is missing %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s drifted from docs/architecture/manifest-schemas/examples; copy the docs file over bundle/assets to resync", name)
		}
	}
	// And nothing extra: the bundled set is exactly the vetted examples.
	embedded, err := fs.ReadDir(assets, "assets")
	if err != nil {
		t.Fatalf("read embedded assets: %v", err)
	}
	if len(embedded) != len(entries) {
		var names []string
		for _, e := range embedded {
			names = append(names, e.Name())
		}
		t.Fatalf("the embedded catalog carries %d files but docs carries %d: %v", len(embedded), len(entries), names)
	}
}

// New must yield a working source, and its honest limits must hold: the
// procedural example System (zero capability refs) resolves, while the
// agentic example System is refused with E_MISSING because its dataset asset
// is not part of the bundled catalog (a registry is ADI-ECO-002, O27).
func TestBundleResolvesAndRefusesHonestly(t *testing.T) {
	source, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fixture := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return data
	}
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	resolve := func(manifest []byte) composition.Result {
		t.Helper()
		return composition.Resolve(context.Background(), composition.Request{
			Manifest:        manifest,
			Mode:            composition.ModeBundled,
			Source:          source,
			CompilerVersion: composition.CompilerVersion,
			Now:             now,
		})
	}

	procedural := fixture("../../../../docs/architecture/system-ir/examples/visual-workflow.system.json")
	result := resolve(procedural)
	if len(result.Findings) != 0 || result.Lock == nil {
		t.Fatalf("the zero-ref procedural System must resolve against the bundle, got findings %v", result.Findings)
	}
	if result.Lock.ResolutionMode != string(composition.ModeBundled) || result.Lock.CompilerVersion != composition.CompilerVersion {
		t.Fatalf("the lock must record its mode and compiler: %+v", result.Lock)
	}

	agentic := fixture("../../../../docs/architecture/manifest-schemas/examples/inbound-sales.system.json")
	result = resolve(agentic)
	if len(result.Findings) == 0 || result.Findings[0].Code != composition.CodeMissing {
		t.Fatalf("an agentic System needs assets the bundle does not carry: expected E_MISSING, got %v", result.Findings)
	}
}
