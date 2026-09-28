package llm

import (
	"regexp"
	"strings"
)

// FileBlock is a single code block extracted from an LLM response.
type FileBlock struct {
	Path    string
	Content string
}

// fenceRe matches a fenced code block: ```lang\n...\n```.
var fenceRe = regexp.MustCompile("(?s)```([^`\n]*)\\n(.*?)```")

// headingPathRe matches a Markdown heading that may carry a file path, e.g.
// `### public/index.html` or `## styles.css`. Models frequently drift from
// ` ```path ` fences to `### path` + ` ```lang `, so we accept both.
var headingPathRe = regexp.MustCompile("(?m)^\\s*(#{1,6})\\s*([A-Za-z0-9_./-]+)\\s*:?.*$")

// looksLikePath reports whether s looks like a file path (has a dot or slash)
// rather than a bare code-language tag like "html".
func looksLikePath(s string) bool {
	return strings.Contains(s, "/") || strings.Contains(s, ".")
}

// latestHeadingBefore returns the nearest path-like Markdown heading before
// byte offset pos, or "" when none exists.
func latestHeadingBefore(output string, pos int) string {
	path := ""
	for _, m := range headingPathRe.FindAllStringSubmatchIndex(output, -1) {
		if m[0] >= pos {
			break
		}
		token := headingPathRe.FindStringSubmatch(output[m[0]:m[1]])
		if token != nil && looksLikePath(token[2]) {
			path = strings.TrimSpace(token[2])
		}
	}
	return path
}

// ParseFileBlocks extracts fenced code blocks from an LLM response and maps
// each to a file path. The path comes from the fence info string when it looks
// like a path; otherwise the nearest preceding `### path` heading is used.
// Blocks with no resolvable path are ignored.
func ParseFileBlocks(output string) []FileBlock {
	var blocks []FileBlock
	for _, m := range fenceRe.FindAllStringSubmatchIndex(output, -1) {
		info := strings.TrimSpace(output[m[2]:m[3]])
		content := strings.TrimRight(output[m[4]:m[5]], "\n")

		path := info
		if !looksLikePath(path) {
			path = latestHeadingBefore(output, m[0])
		}
		if !looksLikePath(path) {
			continue
		}
		blocks = append(blocks, FileBlock{Path: path, Content: content})
	}
	return blocks
}
