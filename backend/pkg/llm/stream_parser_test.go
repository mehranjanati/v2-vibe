package llm

import (
	"strings"
	"testing"
)

// feed feeds s through the parser in small chunks (to exercise the
// incremental state machine) and returns all events plus the Flush events.
func feed(t *testing.T, s string, split int) []StreamEvent {
	t.Helper()
	p := NewStreamParser()
	var evs []StreamEvent
	for i := 0; i < len(s); i += split {
		end := i + split
		if end > len(s) {
			end = len(s)
		}
		evs = append(evs, p.Feed(s[i:end])...)
	}
	evs = append(evs, p.Flush()...)
	return evs
}

// reconstruct reassembles per-path file contents from parser events.
func reconstruct(t *testing.T, evs []StreamEvent) map[string]string {
	t.Helper()
	files := map[string]string{}
	open := ""
	for _, ev := range evs {
		switch ev.Kind {
		case EventStart:
			if open != "" {
				t.Fatalf("Start while %q open", open)
			}
			open = ev.Path
			files[ev.Path] = ""
		case EventChunk:
			if ev.Path != open {
				t.Fatalf("Chunk for %q but %q open", ev.Path, open)
			}
			files[ev.Path] += ev.Content
		case EventEnd:
			if ev.Path != open {
				t.Fatalf("End for %q but %q open", ev.Path, open)
			}
			if files[ev.Path] != ev.Content {
				t.Fatalf("End content mismatch for %q:\nstreamed=%q\nfull=%q", ev.Path, files[ev.Path], ev.Content)
			}
			open = ""
		}
	}
	if open != "" {
		t.Fatalf("file %q never closed", open)
	}
	return files
}

func TestStreamParserBasic(t *testing.T) {
	out := "Intro prose\n```public/index.html\n<h1>hi</h1>\n```\n```public/styles.css\nbody{}\n```\n"
	for _, split := range []int{1, 3, 7, 64} {
		files := reconstruct(t, feed(t, out, split))
		if len(files) != 2 {
			t.Fatalf("split %d: want 2 files, got %v", split, files)
		}
		if strings.TrimRight(files["public/index.html"], "\n") != "<h1>hi</h1>" {
			t.Fatalf("split %d: html=%q", split, files["public/index.html"])
		}
		if strings.TrimRight(files["public/styles.css"], "\n") != "body{}" {
			t.Fatalf("split %d: css=%q", split, files["public/styles.css"])
		}
	}
}

func TestStreamParserHeadingPath(t *testing.T) {
	out := "### public/app.js\n```js\nconsole.log(1);\n```\n"
	files := reconstruct(t, feed(t, out, 5))
	if strings.TrimRight(files["public/app.js"], "\n") != "console.log(1);" {
		t.Fatalf("got %v", files)
	}
}

func TestStreamParserSkipsLanguageOnlyBlocks(t *testing.T) {
	out := "Note:\n```python\nprint(1)\n```\n```public/index.html\n<p>x</p>\n```\n"
	files := reconstruct(t, feed(t, out, 4))
	if len(files) != 1 || strings.TrimRight(files["public/index.html"], "\n") != "<p>x</p>" {
		t.Fatalf("got %v", files)
	}
}

func TestStreamParserSalvagesTruncatedBlock(t *testing.T) {
	// Simulates a max-token cutoff: the last block never closes.
	out := "```public/styles.css\nbody { color: red; }\n\nhtml {"
	p := NewStreamParser()
	var evs []StreamEvent
	evs = append(evs, p.Feed(out)...)
	evs = append(evs, p.Flush()...)
	files := reconstruct(t, evs)
	if files["public/styles.css"] != "body { color: red; }\n\nhtml {" {
		t.Fatalf("got %q", files["public/styles.css"])
	}
}

func TestTruncatedPathsFlagged(t *testing.T) {
	// Clean stream: nothing truncated.
	p := NewStreamParser()
	p.Feed("```public/a.html\nhi\n```\n")
	p.Flush()
	if got := p.TruncatedPaths(); len(got) != 0 {
		t.Fatalf("clean stream flagged truncated: %v", got)
	}

	// Truncated stream: the open block is flagged.
	p2 := NewStreamParser()
	p2.Feed("```public/styles.css\nbody {")
	p2.Flush()
	got := p2.TruncatedPaths()
	if len(got) != 1 || got[0] != "public/styles.css" {
		t.Fatalf("want [public/styles.css], got %v", got)
	}
}

func TestStreamParserNoTranscriptFile(t *testing.T) {
	// The raw transcript itself must never surface as a file.
	out := "Here you go:\n```public/index.html\n<p>x</p>\n```\nDone."
	for _, ev := range feed(t, out, 2) {
		if ev.Path == "generated-output.txt" {
			t.Fatal("transcript leaked as a file")
		}
	}
}

func TestStreamParserMatchesBatchParser(t *testing.T) {
	out := "Plan\n## public/index.html\n```html\n<b>ok</b>\n```\n```workflow.json\n{\"nodes\":[]}\n```\n"
	batch := ParseFileBlocks(out)
	stream := reconstruct(t, feed(t, out, 3))
	if len(batch) != len(stream) {
		t.Fatalf("batch=%d stream=%v", len(batch), stream)
	}
	for _, b := range batch {
		got, ok := stream[b.Path]
		if !ok {
			t.Fatalf("missing %s in stream", b.Path)
		}
		if strings.TrimRight(got, "\n") != strings.TrimRight(b.Content, "\n") {
			t.Fatalf("content mismatch for %s: %q vs %q", b.Path, got, b.Content)
		}
	}
}
