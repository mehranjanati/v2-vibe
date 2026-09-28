package design

import "strings"

// maxFragmentChars caps one fragment's markup when handed to the coder, so a
// verbose block never crowds out the rest of the prompt.
const maxFragmentChars = 1400

// CoderBlock is one registry entry as seen by the coder.
//
// It comes in two shapes:
//
//   - Descriptor (Name/Section/Slots only): compact, rides EVERY coder step
//     inside the shared CoderBrief so the coder always knows the page's
//     authoritative section list and what content each section needs.
//   - Fragment (plus the relevant HTML/CSS/JS body): emitted only for the
//     step that owns that file type — markup for index.html, rules for
//     styles.css, init code for js/main.js — via FileFragments. This keeps
//     the per-step payload bounded while still giving the owning step the
//     authoritative skeleton.
type CoderBlock struct {
	Name    string   `json:"name"`
	Section string   `json:"section,omitempty"`
	Slots   []string `json:"slots,omitempty"`
	HTML    string   `json:"html,omitempty"`
	CSS     string   `json:"css,omitempty"`
	JS      string   `json:"js,omitempty"`
}

// CoderBlocks distills a selection into compact descriptors (no fragment
// bodies). These ride every coder step.
func CoderBlocks(sel []Block) []CoderBlock {
	if len(sel) == 0 {
		return nil
	}
	out := make([]CoderBlock, 0, len(sel))
	for _, b := range sel {
		out = append(out, CoderBlock{
			Name:    b.Name,
			Section: b.Section,
			Slots:   b.Slots,
		})
	}
	return out
}

// fragmentKind reports which fragment body a file path needs: html, css or
// js. An empty result means the file takes no fragments.
func fragmentKind(path string) string {
	p := strings.ToLower(path)
	switch {
	case strings.HasSuffix(p, ".css"):
		return "css"
	case strings.HasSuffix(p, ".js"):
		return "js"
	case strings.HasSuffix(p, ".html"), strings.HasSuffix(p, ".htm"):
		return "html"
	}
	return ""
}

// FileFragments returns the selected blocks as full fragments for the file
// kind that path belongs to — markup for .html, style rules for .css,
// init code for .js. Each fragment carries its descriptor plus the single
// relevant body, truncated to maxFragmentChars. A path of another kind (or
// a nil receiver) gets nil: those steps ride on the compact descriptors
// already present in CoderBrief.Blocks.
func (c *CoderBrief) FileFragments(path string) []CoderBlock {
	kind := fragmentKind(path)
	if c == nil || kind == "" {
		return nil
	}
	out := make([]CoderBlock, 0, len(c.Blocks))
	for _, desc := range c.Blocks {
		blk, ok := BlockByName(desc.Name)
		if !ok {
			continue
		}
		frag := CoderBlock{Name: blk.Name, Section: blk.Section, Slots: blk.Slots}
		switch kind {
		case "html":
			frag.HTML = truncateFragment(blk.HTML, maxFragmentChars)
		case "css":
			frag.CSS = truncateFragment(blk.CSS, maxFragmentChars)
		case "js":
			frag.JS = truncateFragment(blk.JS, maxFragmentChars)
		}
		// Skip blocks with no body of the requested kind (e.g. most blocks
		// ship no JS) so the payload carries only actionable fragments.
		if frag.HTML == "" && frag.CSS == "" && frag.JS == "" {
			continue
		}
		out = append(out, frag)
	}
	return out
}

func truncateFragment(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n/* …truncated … */"
}

// SectionList returns the section names of a selection, for prompts.
func SectionList(sel []Block) []string {
	out := make([]string, 0, len(sel))
	for _, b := range sel {
		out = append(out, b.Section)
	}
	return out
}

// ComposeHTML returns the selected fragments' markup in page order.
func ComposeHTML(sel []Block) string {
	var sb strings.Builder
	for _, b := range sel {
		sb.WriteString(strings.TrimSpace(b.HTML))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ComposeCSS returns the selected fragments' CSS in page order, for use as
// the stylesheet body after the token block. Duplicate fragments are only
// emitted once.
func ComposeCSS(sel []Block) string {
	var sb strings.Builder
	seen := map[string]bool{}
	for _, b := range sel {
		body := strings.TrimSpace(b.CSS)
		if body == "" || seen[body] {
			continue
		}
		seen[body] = true
		sb.WriteString("/* " + b.Name + " */\n")
		sb.WriteString(body)
		sb.WriteString("\n\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ComposeJS returns the concatenated initialisation snippets of a selection.
func ComposeJS(sel []Block) string {
	var sb strings.Builder
	for _, b := range sel {
		body := strings.TrimSpace(b.JS)
		if body == "" {
			continue
		}
		sb.WriteString("// " + b.Name + "\n")
		sb.WriteString(body)
		sb.WriteString("\n\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
