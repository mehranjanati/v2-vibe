package design

import (
	"strings"
)

// blocks is the full static section registry in canonical order (navigation,
// hero, content sections, closing sections). Built once from the per-file
// halves so the ordering is deterministic.
var blocks = func() []Block {
	all := make([]Block, 0, len(blocksA)+len(blocksB)+len(blocksC))
	all = append(all, blocksA...)
	all = append(all, blocksB...)
	return append(all, blocksC...)
}()

// maxCoderBlocks caps how many block fragments ride one coder step, keeping
// the prompt bounded on long pages.
const maxCoderBlocks = 8

// Blocks returns the whole registry (for inspection/tests).
func Blocks() []Block { return append([]Block(nil), blocks...) }

// BlockByName looks up one registry entry.
func BlockByName(name string) (Block, bool) {
	for _, b := range blocks {
		if b.Name == name {
			return b, true
		}
	}
	return Block{}, false
}

// SelectBlocks chooses the fragments that implement a page pattern, in the
// pattern's own section order.
//
// Matching is intentionally forgiving: a pattern's SectionOrder entries come
// from landing.csv free text ("Hero with headline/image", "Key features
// (3-5)", "CTA section", …), so each entry is matched against a block's
// Section and Keywords by token overlap. Sections with no matching block are
// skipped rather than failing the page, and a page that matches nothing
// still gets the default skeleton.
func SelectBlocks(pattern Pattern) []Block {
	out := make([]Block, 0, maxCoderBlocks)
	used := make(map[string]bool, maxCoderBlocks)

	pick := func(b Block) {
		if used[b.Name] || len(out) >= maxCoderBlocks {
			return
		}
		used[b.Name] = true
		out = append(out, b)
	}

	for _, section := range pattern.SectionOrder {
		if len(out) >= maxCoderBlocks {
			break
		}
		if b, ok := bestBlockFor(section, used); ok {
			pick(b)
		}
	}

	// A landing page always needs a navigation bar and a footer even when
	// the pattern text does not spell them out.
	for _, required := range []string{"navbar-sticky", "footer-4col"} {
		if b, ok := BlockByName(required); ok {
			pick(b)
		}
	}

	if len(out) == 0 {
		return DefaultBlocks()
	}
	return orderBlocksForPage(out)
}

// domainSectionHints adds sections that a product domain needs even when the
// page pattern does not name them. A cafe page without a menu is not a cafe
// page, and a SaaS page without pricing leaves the visitor with no way to
// buy — the landing pattern only describes conversion flow, not domain
// necessities.
var domainSectionHints = []struct {
	// match: any of these tokens (from the product name or the raw query)
	// triggers the extra section.
	match   []string
	section string
}{
	{[]string{"cafe", "bakery", "restaurant", "brewery", "food", "menu", "drink", "coffee"}, "menu"},
	{[]string{"saas", "b2b", "tool", "platform", "developer", "api", "fintech"}, "pricing"},
	{[]string{"portfolio", "photography", "studio", "agency", "freelanc"}, "testimonials"},
	{[]string{"clinic", "dental", "legal", "hotel", "travel", "beauty", "fitness", "insurance"}, "contact"},
	{[]string{"automotive", "car", "cars", "dealership", "marketplace", "commerce", "ecommerce", "shop", "store", "retail", "product", "listing", "catalog", "real", "estate", "property"}, "menu"},
}

// SelectBlocksFor is SelectBlocks plus the product-domain sections the page
// needs regardless of its conversion pattern (see domainSectionHints).
func SelectBlocksFor(b *DesignBrief) []Block {
	if b == nil {
		return DefaultBlocks()
	}
	sel := SelectBlocks(b.Pattern)
	if len(sel) == 0 {
		return sel
	}
	used := make(map[string]bool, len(sel))
	for _, blk := range sel {
		used[blk.Name] = true
	}
	// Search text: the product row plus the raw request, normalized.
	hay := queryTokens(b.Product + " " + b.Query)
	for _, hint := range domainSectionHints {
		if len(sel) >= maxCoderBlocks {
			break
		}
		if !matchesAny(hay, hint.match) {
			continue
		}
		blk, ok := bestBlockFor(hint.section, used)
		if !ok {
			continue
		}
		used[blk.Name] = true
		sel = append(sel, blk)
	}
	return orderBlocksForPage(sel)
}

// matchesAny reports whether any token contains (or is contained by) any of
// the given substrings.
func matchesAny(tokens []string, subs []string) bool {
	for _, t := range tokens {
		for _, s := range subs {
			if strings.Contains(t, s) || strings.Contains(s, t) {
				return true
			}
		}
	}
	return false
}

// DefaultBlocks is the fallback skeleton used when nothing matches.
func DefaultBlocks() []Block {
	ids := []string{"navbar-sticky", "hero-center", "features-grid", "cta-band", "footer-4col"}
	out := make([]Block, 0, len(ids))
	for _, id := range ids {
		if b, ok := BlockByName(id); ok {
			out = append(out, b)
		}
	}
	return out
}

// bestBlockFor finds the highest-scoring unused block for a free-text
// section description.
func bestBlockFor(section string, used map[string]bool) (Block, bool) {
	tokens := queryTokens(section)
	if len(tokens) == 0 {
		return Block{}, false
	}
	best := -1
	bestScore := 0
	for i, b := range blocks {
		if used[b.Name] {
			continue
		}
		if s := blockScore(b, tokens); s > bestScore {
			bestScore = s
			best = i
		}
	}
	if best < 0 {
		return Block{}, false
	}
	return blocks[best], true
}

// blockScore rates one block against the section tokens. A Section hit is
// worth far more than a keyword hit (Section is the block's identity).
func blockScore(b Block, tokens []string) int {
	score := 0
	section := strings.ToLower(b.Section)
	for _, t := range tokens {
		if strings.Contains(section, t) || strings.Contains(t, section) {
			score += 10
		}
		for _, kw := range b.Keywords {
			kw = strings.ToLower(kw)
			if strings.Contains(kw, t) || strings.Contains(t, kw) {
				score += 3
				break
			}
		}
	}
	// Hero variants share Section "hero": prefer the split layout as the
	// richer default so a plain "hero" section gets a two-column page.
	if score > 0 && b.Name == "hero-center" {
		score--
	}
	return score
}

// orderBlocksForPage sorts the selection into page order (nav, hero, content,
// closing) regardless of the order the pattern listed them in — a footer
// above the hero is never what the user wants.
func orderBlocksForPage(in []Block) []Block {
	rank := func(b Block) int {
		switch b.Section {
		case "nav":
			return 0
		case "hero":
			return 1
		case "contact":
			return 7
		case "cta":
			return 8
		case "footer":
			return 9
		default:
			return 5
		}
	}
	out := append([]Block(nil), in...)
	// Stable insertion sort by rank keeps the pattern's relative order inside
	// the same rank bucket.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && rank(out[j-1]) > rank(out[j]); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
