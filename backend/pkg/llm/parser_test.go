package llm

import "testing"

func TestParseFileBlocksInfoString(t *testing.T) {
	out := "x\n```public/index.html\n<h1>hi</h1>\n```\ny"
	blocks := ParseFileBlocks(out)
	if len(blocks) != 1 || blocks[0].Path != "public/index.html" {
		t.Fatalf("got %+v", blocks)
	}
}

func TestParseFileBlocksMarkdownHeading(t *testing.T) {
	out := "### public/index.html\n```html\n<b>ok</b>\n```\n## public/styles.css\n```css\nbody{}\n```"
	blocks := ParseFileBlocks(out)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Path != "public/index.html" || blocks[1].Path != "public/styles.css" {
		t.Fatalf("paths wrong: %+v", blocks)
	}
}

func TestParseFileBlocksIgnoresLanguageOnly(t *testing.T) {
	out := "Here is a snippet:\n```python\nprint(1)\n```"
	if blocks := ParseFileBlocks(out); len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %+v", blocks)
	}
}
