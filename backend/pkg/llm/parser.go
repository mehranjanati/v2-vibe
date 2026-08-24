package llm

import (
	"regexp"
	"strings"
)

// FileBlock is a single code block extracted from an LLM response.
type FileBlock struct {
	// Path is the file path from the fenced code block info string,
	// e.g. ```src/index.ts -> "src/index.ts".
	Path string
	// Content is the raw code inside the fence.
	Content string
}

// fenceRe matches a fenced code block: ```lang\n...\n```.
var fenceRe = regexp.MustCompile("(?s)```([^`\\n]*)\\n(.*?)```")

// ParseFileBlocks extracts fenced code blocks from an LLM response.
// Blocks whose info string looks like a file path (contains a dot or a
// slash) are treated as file writes; others are ignored.
func ParseFileBlocks(output string) []FileBlock {
	var blocks []FileBlock
	for _, m := range fenceRe.FindAllStringSubmatch(output, -1) {
		if len(m) < 3 {
			continue
		}
		info := strings.TrimSpace(m[1])
		content := strings.TrimRight(m[2], "\n")

		// Heuristic: a file path contains a slash or a dot.
		if !strings.Contains(info, "/") && !strings.Contains(info, ".") {
			continue
		}
		blocks = append(blocks, FileBlock{
			Path:    strings.TrimSpace(info),
			Content: content,
		})
	}
	return blocks
}
