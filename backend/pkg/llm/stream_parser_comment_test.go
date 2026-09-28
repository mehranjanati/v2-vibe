package llm

import (
	"strings"
	"testing"
)

// TestParserAdoptsPathFromHTMLComment reproduces the gap-fill failure mode:
// models tag the file path in a first-line comment instead of the fence
// info string (```html + <!-- public/index.html -->). The parser must
// adopt the path instead of skipping the block as a prose snippet.
func TestParserAdoptsPathFromHTMLComment(t *testing.T) {
	p := NewStreamParser()
	chunks := []string{
		"Sure, here are the missing files:\n\n",
		"```html\n",
		"<!-- public/index.html -->\n",
		"<!DOCTYPE html>\n<html><body>hi</body></html>\n",
		"```\n\n",
		"```css\n/* public/styles.css */\nbody { color: red; }\n```\n",
	}
	var events []StreamEvent
	for _, c := range chunks {
		events = append(events, p.Feed(c)...)
	}
	events = append(events, p.Flush()...)

	files := map[string]string{}
	for _, ev := range events {
		switch ev.Kind {
		case EventEnd:
			files[ev.Path] = ev.Content
		}
	}
	html, ok := files["public/index.html"]
	if !ok {
		t.Fatalf("index.html not parsed; got files %v", files)
	}
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Fatalf("index.html content wrong: %q", html)
	}
	css, ok := files["public/styles.css"]
	if !ok {
		t.Fatalf("styles.css not parsed; got files %v", files)
	}
	if !strings.Contains(css, "color: red") {
		t.Fatalf("styles.css content wrong: %q", css)
	}
}

// TestParserAdoptsPathFromJSLineComment covers the // comment variant.
func TestParserAdoptsPathFromJSLineComment(t *testing.T) {
	p := NewStreamParser()
	out := "```javascript\n// public/js/main.js\nApp.boot();\n```\n"
	var files []string
	for _, ev := range p.Feed(out) {
		if ev.Kind == EventStart {
			files = append(files, ev.Path)
		}
	}
	for _, ev := range p.Flush() {
		if ev.Kind == EventEnd {
			files = append(files, ev.Path)
		}
	}
	if len(files) == 0 || files[0] != "public/js/main.js" {
		t.Fatalf("path = %v, want public/js/main.js", files)
	}
}

// TestParserStillSkipsProseBlocks ensures the comment sniff does not
// swallow genuine prose snippets.
func TestParserStillSkipsProseBlocks(t *testing.T) {
	p := NewStreamParser()
	out := "```html\nThis is a usage example, not a file.\n```\n"
	for _, ev := range p.Feed(out) {
		if ev.Kind == EventStart || ev.Kind == EventEnd {
			t.Fatalf("unexpected file event: %+v", ev)
		}
	}
	for _, ev := range p.Flush() {
		if ev.Kind == EventStart || ev.Kind == EventEnd {
			t.Fatalf("unexpected file event on flush: %+v", ev)
		}
	}
}

// TestPathFromComment covers the comment extraction edge cases.
func TestPathFromComment(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"<!-- public/index.html -->", "public/index.html"},
		{"<!--public/index.html-->", "public/index.html"},
		{"// public/js/main.js", "public/js/main.js"},
		{"/* public/styles.css */", "public/styles.css"},
		{"<!DOCTYPE html>", ""},
		{"// TODO: see docs/README", ""},
		{"plain text", ""},
		{"", ""},
		{"<!-- note without path -->", ""},
	}
	for _, tc := range cases {
		if got := pathFromComment(tc.line); got != tc.want {
			t.Errorf("pathFromComment(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}
