package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplayTranscripts replays every fixture in
// backend/testdata/llm_transcripts through the StreamParser (in several
// chunk sizes) and asserts the invariants that guard against the
// generated-output regression and truncation data loss.
func TestReplayTranscripts(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "testdata", "llm_transcripts"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no fixtures found in testdata/llm_transcripts")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "llm_transcripts", entry.Name()))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			out := string(raw)

			for _, split := range []int{1, 3, 13, 4096} {
				files := reconstruct(t, feed(t, out, split))
				for path := range files {
					if strings.Contains(path, "generated-output") {
						t.Fatalf("split=%d: transcript leaked as file %q", split, path)
					}
					if strings.TrimSpace(files[path]) == "" {
						t.Fatalf("split=%d: file %q is empty", split, path)
					}
				}
				if len(files) == 0 {
					t.Fatalf("split=%d: no files parsed from fixture", split)
				}
				// Chunk-size independence: every split yields the same
				// file set with identical contents.
				if split == 1 {
					baseline := files
					for _, s2 := range []int{3, 13, 4096} {
						other := reconstruct(t, feed(t, out, s2))
						if len(other) != len(baseline) {
							t.Fatalf("split=%d: file count %d != baseline %d", s2, len(other), len(baseline))
						}
						for p, c := range baseline {
							if strings.TrimRight(other[p], "\n") != strings.TrimRight(c, "\n") {
								t.Fatalf("split=%d: content mismatch for %s", s2, p)
							}
						}
					}
				}
			}
		})
	}
}

func TestReplayTruncatedFixtureSalvagesCSS(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "llm_transcripts", "truncated.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	files := reconstruct(t, feed(t, string(raw), 7))
	css, ok := files["public/styles.css"]
	if !ok {
		t.Fatalf("truncated styles.css dropped; got %v", files)
	}
	if !strings.Contains(css, "body { color: red; }") {
		t.Fatalf("salvaged css incomplete: %q", css)
	}
}
