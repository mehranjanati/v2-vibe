package llm

import "strings"

// StreamEventKind identifies a streaming parser event.
type StreamEventKind int

const (
	// EventStart is emitted when a file block opener is resolved to a path.
	EventStart StreamEventKind = iota
	// EventChunk carries an incremental piece of the current file content.
	EventChunk
	// EventEnd closes the current file; Content holds the FULL file content.
	EventEnd
)

// StreamEvent is one incremental parser output for the current file.
type StreamEvent struct {
	Kind    StreamEventKind
	Path    string
	Content string
}

// StreamParser incrementally splits an LLM token stream into per-file
// events (the vibesdk pattern): while the model is still generating,
// consumers receive file_generating / file_chunk_generated /
// file_generated-style events with the REAL file path, instead of the raw
// transcript being dumped into a placeholder file and parsed after the fact.
//
// A file's path comes from the fence info string (```public/index.html) or,
// failing that, from the nearest preceding `### path` Markdown heading.
// Prose and language-only fenced blocks (no resolvable path) are skipped.
//
// Flush salvages a truncated stream: if the output was cut off (e.g. by a
// max-token limit) while inside a block, the partial content is still
// delivered as a file so the app is not silently missing entire files.
type StreamParser struct {
	pending   strings.Builder // text not yet consumed by the state machine
	curPath   string          // path of the open block ("" when outside one)
	curBuf    strings.Builder // full content of the open block
	lastPath  string          // nearest path-like heading seen in prose
	truncated []string        // paths salvaged from a cut-off stream
}

// NewStreamParser creates an empty parser.
func NewStreamParser() *StreamParser { return &StreamParser{} }

// TruncatedPaths returns the file paths that were salvaged from a
// truncated stream (Flush closed them without a closing fence), i.e.
// files the model did not finish writing. Empty when the stream ended
// cleanly.
func (p *StreamParser) TruncatedPaths() []string {
	return p.truncated
}

// Feed consumes one streamed chunk and returns any events it completes.
func (p *StreamParser) Feed(chunk string) []StreamEvent {
	p.pending.WriteString(chunk)
	return p.drain(false)
}

// Flush finalizes the stream. A truncated open block is salvaged: its
// partial content is emitted as a final chunk plus an End event so the
// file still lands in the VFS.
func (p *StreamParser) Flush() []StreamEvent {
	events := p.drain(true)
	if p.curPath != "" {
		rest := p.pending.String()
		p.pending.Reset()
		if rest != "" {
			p.curBuf.WriteString(rest)
			events = append(events, StreamEvent{Kind: EventChunk, Path: p.curPath, Content: rest})
		}
		events = append(events, StreamEvent{Kind: EventEnd, Path: p.curPath, Content: p.curBuf.String()})
		p.truncated = append(p.truncated, p.curPath)
		p.curPath = ""
		p.curBuf.Reset()
	}
	p.pending.Reset()
	return events
}

// consumePending drops the first n bytes of pending. Defensive clamp: a
// fence-scan arithmetic slip must never panic the process.
func (p *StreamParser) consumePending(n int) {
	s := p.pending.String()
	if n > len(s) {
		n = len(s)
	}
	if n < 0 {
		n = 0
	}
	p.pending.Reset()
	p.pending.WriteString(s[n:])
}

// scanHeadings updates lastPath from any path-like Markdown headings in s.
func (p *StreamParser) scanHeadings(s string) {
	if path := latestHeadingBefore(s, len(s)); path != "" {
		p.lastPath = path
	}
}

// drain advances the state machine over as much of `pending` as it can.
// When final is false it stops at ambiguous boundaries that need more data.
func (p *StreamParser) drain(final bool) []StreamEvent {
	var events []StreamEvent
	for {
		s := p.pending.String()
		if p.curPath != "" {
			idx, closeLen := scanClosingFence(s, final)
			if idx < 0 {
				// Emit the longest prefix that can never be part of a
				// closing fence so consumers see content live.
				safe := safeEmitLen(s)
				// A trailing "\n```..." candidate whose line has not
				// completed yet (junk may still follow on the same line)
				// must be held whole — safeEmitLen alone would flush it
				// as content once non-backtick junk arrives.
				if cand := strings.LastIndex(s, "\n```"); cand >= 0 &&
					!strings.Contains(s[cand+1:], "\n") {
					safe = cand
				}
				if safe > 0 {
					p.curBuf.WriteString(s[:safe])
					events = append(events, StreamEvent{Kind: EventChunk, Path: p.curPath, Content: s[:safe]})
					p.consumePending(safe)
				}
				return events
			}
			content := s[:idx]
			if idx > 0 && content != "" {
				p.curBuf.WriteString(content)
				events = append(events, StreamEvent{Kind: EventChunk, Path: p.curPath, Content: content})
			}
			events = append(events, StreamEvent{Kind: EventEnd, Path: p.curPath, Content: p.curBuf.String()})
			p.curPath = ""
			p.curBuf.Reset()
			p.consumePending(idx + closeLen)
			continue
		}

		// Outside a block: consume prose (tracking headings) until the
		// next fence opener.
		idx := scanOpeningFence(s)
		if idx < 0 {
			keep := trailingPartialFence(s)
			// Only scan COMPLETE lines: a heading split across chunks
			// must not clobber lastPath with a partial path.
			prose := s[:len(s)-keep]
			if nl := strings.LastIndexByte(prose, '\n'); nl >= 0 {
				p.scanHeadings(prose[:nl+1])
				p.consumePending(nl + 1)
			}
			return events
		}
		p.scanHeadings(s[:idx])
		p.consumePending(idx)
		s = p.pending.String()
		nl := strings.IndexByte(s, '\n')
		if nl < 0 && !final {
			// Info string incomplete; wait for more data.
			return events
		}
		if nl < 0 {
			nl = len(s)
		}
		info := strings.TrimSpace(s[len("```"):nl])
		path := ""
		if looksLikePath(info) {
			path = info
		} else {
			path = p.lastPath
		}
		if path == "" {
			// Language-only info string: models frequently tag the real
			// file path in a first-line comment (```html +
			// <!-- public/index.html -->). Peek at the first content
			// line once it has fully arrived; adopt the path when found,
			// otherwise skip the block entirely (prose snippet).
			rest := s[nl+1:]
			var firstLine string
			var consumed int
			if fnl := strings.IndexByte(rest, '\n'); fnl >= 0 {
				firstLine = rest[:fnl]
				consumed = nl + 1 + fnl + 1
			} else if final {
				firstLine = rest
				consumed = len(s)
			} else {
				// First content line still growing; wait for it.
				return events
			}
			if cmd := pathFromComment(firstLine); cmd != "" {
				p.consumePending(consumed)
				p.curPath = cmd
				p.curBuf.Reset()
				p.lastPath = ""
				events = append(events, StreamEvent{Kind: EventStart, Path: cmd})
				continue
			}
			// Language-only block (prose snippet): skip it entirely. The
			// opener line stays in `pending` until the whole block can be
			// consumed in one go, so no state is lost between chunks.
			if !p.skipForeignBlock(final) {
				return events
			}
			continue
		}
		p.consumePending(nl + 1)
		p.curPath = path
		p.curBuf.Reset()
		p.lastPath = ""
		events = append(events, StreamEvent{Kind: EventStart, Path: path})
	}
}

// scanOpeningFence finds the next line-start ``` fence opener in s.
// Returns its byte index or -1.
func scanOpeningFence(s string) int {
	if strings.HasPrefix(s, "```") {
		return 0
	}
	i := strings.Index(s, "\n```")
	if i < 0 {
		return -1
	}
	return i + 1
}

// scanClosingFence looks for a closing fence (line-start ```) inside the
// open block. Returns the byte offset where block content ends (i.e. the
// index of the newline preceding the fence) and how many bytes of the
// fence (including its trailing newline) to consume, or -1 when not
// found yet.
func scanClosingFence(s string, final bool) (int, int) {
	fenceStart := 0
	if !strings.HasPrefix(s, "```") {
		i := strings.Index(s, "\n```")
		if i < 0 {
			return -1, 0
		}
		fenceStart = i + 1
	}
	after := s[fenceStart+3:]
	if len(after) == 0 {
		if final {
			// Stream ended exactly at the closing fence (no trailing
			// newline): consume the "\n```" tail. len(s) == fenceStart+3,
			// so consume total = idx + closeLen must equal len(s).
			return fenceStart - 1, len(s) - fenceStart + 1
		}
		// Could still grow into a nested "```lang" opener.
		return -1, 0
	}
	if after[0] == '\n' {
		return fenceStart - 1, 4
	}
	// Junk after the closing fence on the same line: consume the line.
	if nl := strings.IndexByte(after, '\n'); nl >= 0 {
		return fenceStart - 1, 3 + nl + 1
	}
	if final {
		return fenceStart - 1, len(s) - fenceStart + 1
	}
	return -1, 0
}

// safeEmitLen returns how many leading bytes of s can be emitted as file
// content without splitting a closing fence that has only partly arrived:
// hold back trailing backticks (up to 3) and a trailing newline that could
// be the start of "\n```".
func safeEmitLen(s string) int {
	n := 0
	for n < 3 && n < len(s) && s[len(s)-1-n] == '`' {
		n++
	}
	hold := n
	if hold < 3 && hold < len(s) && s[len(s)-1-hold] == '\n' {
		hold++
	}
	// A full "\n```" tail may be the start of a closing fence: hold the
	// newline too until we see what follows.
	if n == 3 && len(s) > 3 && s[len(s)-4] == '\n' {
		hold = 4
	}
	return len(s) - hold
}

// trailingPartialFence returns the number of trailing bytes of s that
// could be the beginning of a fence opener ("```") and must be kept.
func trailingPartialFence(s string) int {
	for keep := 3; keep >= 1; keep-- {
		if len(s) >= keep && strings.HasPrefix("```", s[len(s)-keep:]) {
			return keep
		}
	}
	return 0
}

// codeExts lists file extensions accepted for comment-derived paths.
var codeExts = []string{".html", ".htm", ".css", ".js", ".mjs", ".json", ".svg", ".ts", ".jsx", ".tsx"}

// hasCodeExt reports whether p ends with a known code asset extension.
func hasCodeExt(p string) bool {
	lower := strings.ToLower(p)
	for _, ext := range codeExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// pathFromComment extracts a file path from a first-line path comment:
//
//	<!-- public/index.html -->   (HTML)
//	// public/js/main.js         (JS)
//	/* public/styles.css */      (CSS)
//
// Returns "" when the line carries no recognizable path comment. Guards
// against false positives (prose comments like "// see docs/README") by
// requiring a code asset extension and no stray words.
func pathFromComment(line string) string {
	line = strings.TrimSpace(line)
	var inner string
	switch {
	case strings.HasPrefix(line, "<!--"):
		inner = strings.TrimSpace(strings.TrimPrefix(line, "<!--"))
		inner = strings.TrimSpace(strings.TrimSuffix(inner, "-->"))
	case strings.HasPrefix(line, "//"):
		inner = strings.TrimSpace(strings.TrimPrefix(line, "//"))
	case strings.HasPrefix(line, "/*"):
		inner = strings.TrimSpace(strings.TrimPrefix(line, "/*"))
		inner = strings.TrimSpace(strings.TrimSuffix(inner, "*/"))
	default:
		return ""
	}
	if inner == "" || !looksLikePath(inner) || strings.ContainsAny(inner, " \t<>\"'") {
		return ""
	}
	if !hasCodeExt(inner) {
		return ""
	}
	return inner
}

// skipForeignBlock consumes a language-only fenced block (no file path),
// starting from its "```..." opener line which must be at the start of
// pending, up to and including its closing fence. Returns false when the
// block is not yet closed and more data is needed (nothing is consumed).
func (p *StreamParser) skipForeignBlock(final bool) bool {
	s := p.pending.String()
	if !strings.HasPrefix(s, "```") {
		return final // malformed; drop everything
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		if final {
			p.consumePending(len(s))
			return true
		}
		return false // opener info line incomplete
	}
	rest := s[nl+1:]
	if strings.HasPrefix(rest, "```") {
		// Empty body: the next fence is this block's closer only when the
		// opener was language-only, which it is by contract here.
		i := 0
		if j := strings.IndexByte(rest[i:], '\n'); j >= 0 {
			p.consumePending(nl + 1 + i + j + 1)
			return true
		}
		if final {
			p.consumePending(len(s))
			return true
		}
		return false
	}
	i := strings.Index(rest, "\n```")
	if i < 0 {
		return false
	}
	seg := rest[i+1:]
	consume := nl + 1 + i + 1 + 3
	if k := strings.IndexByte(seg[3:], '\n'); k >= 0 {
		consume += 1 + k
	} else if final {
		consume = len(s)
	} else {
		// Wait until the closer's end-of-line is visible.
		return false
	}
	p.consumePending(consume)
	return true
}

