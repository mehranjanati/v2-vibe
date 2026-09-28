package design

import "strings"

// CoderBrief is the design direction distilled for the coder steps. The
// planner gets the full rendered brief (prose, rationale, conversion notes);
// the coder gets only what it needs to write on-brand, design-system-compliant
// files — the token block verbatim, the font stacks, the section skeleton,
// and one-line motion/a11y rules — so the per-step prompt stays compact.
type CoderBrief struct {
	Style    string   `json:"style,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Sections []string `json:"sections,omitempty"`
	// TokensCSS is the exact :root + .dark block the stylesheet must define.
	// Verbatim, so every file references the same token names.
	TokensCSS string `json:"css_tokens,omitempty"`
	Fonts     struct {
		Heading string `json:"heading,omitempty"`
		Body    string `json:"body,omitempty"`
	} `json:"fonts,omitempty"`
	Motion  []string `json:"motion,omitempty"`
	A11y    []string `json:"a11y,omitempty"`
	Avoid   []string `json:"avoid,omitempty"`
	Product string   `json:"product,omitempty"`
	// Blocks are the page's authoritative section skeleton as compact
	// descriptors (name, section, content slots), in page order. The coder
	// fills these sections with real product content instead of inventing
	// markup. Full fragment bodies are delivered per-file via FileFragments
	// (markup → index.html, rules → styles.css, init → js/main.js), so the
	// shared brief stays compact.
	Blocks []CoderBlock `json:"blocks,omitempty"`
}

// maxCoderMotion / maxCoderA11y cap the distilled lists.
const (
	maxCoderMotion = 4
	maxCoderA11y   = 6
)

// CoderBrief distills the full brief into the compact coder payload.
func (b *DesignBrief) CoderBrief() *CoderBrief {
	if b == nil {
		return nil
	}
	cb := &CoderBrief{
		Style:     b.Style.Name,
		Pattern:   b.Pattern.Name,
		Sections:  b.Pattern.SectionOrder,
		TokensCSS: b.TokensCSS(),
		Product:   b.Product,
	}
	cb.Fonts.Heading = b.Type.HeadingStack
	cb.Fonts.Body = b.Type.BodyStack
	for _, m := range b.Motion {
		if len(cb.Motion) >= maxCoderMotion {
			break
		}
		line := strings.TrimSpace(m.Trigger + ": " + m.Duration + " " + m.Easing)
		if m.Do != "" {
			line += " — " + m.Do
		}
		if line != ":" && line != "" {
			cb.Motion = append(cb.Motion, line)
		}
	}
	for _, a := range b.A11y {
		if len(cb.A11y) >= maxCoderA11y {
			break
		}
		if a.Do != "" {
			cb.A11y = append(cb.A11y, a.Do)
		}
	}
	cb.Avoid = b.AntiPatterns
	// Section fragments: the pattern's structure plus the product domain's
	// necessary sections (e.g. a menu for a cafe).
	cb.Blocks = CoderBlocks(SelectBlocksFor(b))
	return cb
}

// UsageHint is the one-line reminder of how the tokens must be used.
func (c *CoderBrief) UsageHint() string {
	return "Reference tokens only (var(--primary), var(--card), var(--font-heading), …); " +
		"never hardcode hex values. Stylesheet must define the css_tokens block verbatim."
}
