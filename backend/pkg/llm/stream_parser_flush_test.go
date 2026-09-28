package llm

import "testing"

func TestStreamParserFlushAtClosingFenceNoTrailingNewline(t *testing.T) {
	// Regression: a document ending exactly at the closing fence (no
	// trailing newline) used to panic in Flush with
	// "slice bounds out of range [6:4]" when final-fence math overran.
	out := "```public/js/main.js\nApp.init();\n```"
	for split := 1; split <= len(out)+1; split++ {
		p := NewStreamParser()
		var evs []StreamEvent
		for i := 0; i < len(out); i += split {
			e := i + split
			if e > len(out) {
				e = len(out)
			}
			evs = append(evs, p.Feed(out[i:e])...)
		}
		evs = append(evs, p.Flush()...) // must not panic
		files := reconstruct(t, evs)
		if files["public/js/main.js"] != "App.init();\n" && files["public/js/main.js"] != "App.init();" {
			t.Fatalf("split=%d: main.js = %q", split, files["public/js/main.js"])
		}
	}
}

func TestStreamParserFlushJunkAfterClosingFence(t *testing.T) {
	// Closing fence followed by trailing junk with no newline at EOF.
	out := "```public/a.js\nok\n``` trailing words"
	for split := 1; split <= len(out)+1; split++ {
		p := NewStreamParser()
		var evs []StreamEvent
		for i := 0; i < len(out); i += split {
			e := i + split
			if e > len(out) {
				e = len(out)
			}
			evs = append(evs, p.Feed(out[i:e])...)
		}
		evs = append(evs, p.Flush()...)
		files := reconstruct(t, evs)
		if files["public/a.js"] != "ok\n" && files["public/a.js"] != "ok" {
			t.Fatalf("split=%d: a.js = %q", split, files["public/a.js"])
		}
	}
}
