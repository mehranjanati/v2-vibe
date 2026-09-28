package design

import (
	"context"
	"strings"
)

// maxMotionSpecs / maxA11yRules bound how many guidelines ride the brief so
// the planner/coder prompts stay compact.
const (
	maxMotionSpecs = 4
	maxA11yRules   = 6
)

// Brief assembles the design direction for one user request: the page
// pattern, the visual style, a complete color palette, a font pairing,
// motion guidance and accessibility rules. It never fails — with no
// catalog match it returns a sensible neutral default so every generation
// still gets a coherent design system instead of ad-hoc guesses.
func Brief(ctx context.Context, query string) (*DesignBrief, error) {
	c, err := getCatalog()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b := &DesignBrief{Query: query}

	prod := bestProduct(c, query)
	if prod != nil {
		b.Product = prod.ProductType
	}

	// Pattern: product's declared landing pattern wins; else a query match;
	// else the general-purpose hero pattern.
	if lp := bestLandingPattern(c, query); lp != nil {
		b.Pattern = toPattern(lp)
	}
	if prod != nil && prod.LandingPattern != "" {
		if lp := matchLandingByName(c, prod.LandingPattern); lp != nil {
			b.Pattern = toPattern(lp)
		}
	}
	if b.Pattern.Name == "" {
		b.Pattern = defaultPattern()
	}

	// Style: the product row names a style; resolve it, else query match.
	styleHint := ""
	if prod != nil {
		styleHint = prod.PrimaryStyle
	}
	if sr := bestStyle(c, query, styleHint); sr != nil {
		b.Style = toStyleRef(sr)
	}
	if b.Style.Name == "" {
		b.Style = defaultStyle()
	}

	// Palette: closest color row to the product domain; else neutral default.
	if cr := bestColorPalette(c, query, b.Product); cr != nil {
		b.Palette = toPalette(cr)
	}
	if b.Palette.Primary == "" {
		b.Palette = defaultPalette()
	}

	// Typography: match on style+query for a mood-appropriate pairing.
	if tr := bestTypography(c, query, b.Style.Name); tr != nil {
		b.Type = toTypePairing(tr)
	}
	if b.Type.HeadingFont == "" {
		b.Type = defaultTypography()
	}

	b.Motion = pickMotion(c, b.Style.Effects)
	b.A11y = pickA11y(c)
	b.AntiPatterns = pickAntiPatterns(c, b.Product, query)

	return b, nil
}

// matchLandingByName finds a landing pattern by (fuzzy) name.
func matchLandingByName(c *catalog, name string) *landingRow {
	ln := strings.ToLower(name)
	for i := range c.landing {
		if strings.Contains(strings.ToLower(c.landing[i].Name), ln) ||
			strings.Contains(ln, strings.ToLower(c.landing[i].Name)) {
			return &c.landing[i]
		}
	}
	idxs := rankByQuery(c.landing, name, func(r landingRow) []string { return []string{r.Name} })
	if len(idxs) == 0 {
		return nil
	}
	return &c.landing[idxs[0]]
}

func toPattern(r *landingRow) Pattern {
	var sections []string
	for _, s := range strings.Split(r.SectionOrder, ">") {
		if s = strings.TrimSpace(s); s != "" {
			sections = append(sections, s)
		}
	}
	return Pattern{
		Name:          r.Name,
		SectionOrder:  sections,
		CTAPlacement:  r.CTAPlacement,
		ColorStrategy: r.ColorStrategy,
		Conversion:    r.Conversion,
	}
}

func toStyleRef(r *styleRow) StyleRef {
	return StyleRef{
		Name:        r.Name,
		Keywords:    r.Keywords,
		Effects:     r.Effects,
		CSSKeywords: r.CSSKeywords,
		DoNotUseFor: r.DoNotUseFor,
		Checklist:   r.Checklist,
		DesignVars:  r.DesignVars,
	}
}

func toPalette(r *colorRow) Palette {
	return Palette{
		Primary:               normHex(r.Primary),
		PrimaryForeground:     normHex(r.OnPrimary),
		Secondary:             normHex(r.Secondary),
		SecondaryForeground:   normHex(r.OnSecondary),
		Accent:                normHex(r.Accent),
		AccentForeground:      normHex(r.OnAccent),
		Background:            normHex(r.Background),
		Foreground:            normHex(r.Foreground),
		Card:                  normHex(r.Card),
		CardForeground:        normHex(r.CardFg),
		Muted:                 normHex(r.Muted),
		MutedForeground:       normHex(r.MutedFg),
		Border:                normHex(r.Border),
		Destructive:           normHex(r.Destructive),
		DestructiveForeground: normHex(r.OnDestructive),
		Ring:                  normHex(r.Ring),
		Notes:                 r.Notes,
	}
}

func toTypePairing(r *typeRow) TypePairing {
	return TypePairing{
		HeadingFont:  r.Heading,
		BodyFont:     r.Body,
		Mood:         r.Mood,
		BestFor:      r.BestFor,
		HeadingStack: fontStack(r.Heading, true),
		BodyStack:    fontStack(r.Body, false),
		Notes:        r.Notes,
	}
}

// normHex normalizes a color cell to "#RRGGBB" when it looks like hex.
func normHex(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "#") && isHexColor(s) {
		return "#" + s
	}
	return s
}

func isHexColor(s string) bool {
	if len(s) != 3 && len(s) != 6 && len(s) != 8 {
		return false
	}
	for _, r := range s {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

// fontStack builds a CSS font-family value with sensible system fallbacks,
// so the generated app never depends on a web-font CDN to look right.
func fontStack(font string, heading bool) string {
	font = strings.TrimSpace(font)
	if font == "" {
		font = "sans-serif"
	}
	lower := strings.ToLower(font)
	var fallback string
	switch {
	case strings.Contains(lower, "serif") && !strings.Contains(lower, "sans"):
		fallback = "Georgia, 'Times New Roman', serif"
	case strings.Contains(lower, "mono") || strings.Contains(lower, "code"):
		fallback = "'Courier New', monospace"
	case strings.Contains(lower, "display") || strings.Contains(lower, "playfair"):
		fallback = "Georgia, serif"
	case heading:
		fallback = "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif"
	default:
		fallback = "system-ui, -apple-system, 'Segoe UI', Roboto, Arial, sans-serif"
	}
	name := font
	if strings.Contains(font, " ") && !strings.HasPrefix(font, "'") {
		name = "'" + font + "'"
	}
	return name + ", " + fallback
}

// pickMotion selects a few motion specs relevant to the style's effects.
func pickMotion(c *catalog, effects string) []MotionSpec {
	idxs := rankByQuery(c.motion, effects, func(r motionRow) []string {
		return []string{r.Keywords, r.Trigger, r.Intensity}
	})
	out := make([]MotionSpec, 0, maxMotionSpecs)
	for _, i := range idxs {
		if len(out) >= maxMotionSpecs {
			break
		}
		r := c.motion[i]
		out = append(out, MotionSpec{
			Intensity: r.Intensity,
			Trigger:   r.Trigger,
			Duration:  r.Duration,
			Easing:    r.Easing,
			Do:        r.Do,
			Dont:      r.Dont,
		})
	}
	return out
}

// pickA11y returns the highest-severity accessibility rules (focus, contrast,
// reduced motion, targets) that every generated page should satisfy.
func pickA11y(c *catalog) []A11yRule {
	want := []string{"high", "medium", "low"}
	out := make([]A11yRule, 0, maxA11yRules)
	seen := map[string]bool{}
	for _, sev := range want {
		for _, r := range c.ux {
			if len(out) >= maxA11yRules {
				return out
			}
			if !strings.EqualFold(r.Severity, sev) || seen[r.Issue] {
				continue
			}
			seen[r.Issue] = true
			out = append(out, A11yRule{
				Category: r.Category,
				Issue:    r.Issue,
				Do:       r.Do,
				Dont:     r.Dont,
				Severity: r.Severity,
			})
		}
	}
	return out
}

// pickAntiPatterns finds anti-patterns for the product domain.
func pickAntiPatterns(c *catalog, productType, query string) []string {
	idxs := rankByQuery(c.reasoning, productType+" "+query, func(r reasoningRow) []string {
		return []string{r.Category}
	})
	if len(idxs) == 0 {
		return nil
	}
	return splitList(c.reasoning[idxs[0]].AntiPatterns)
}

// --- defaults used when no catalog row matches the query ---

func defaultPattern() Pattern {
	return Pattern{
		Name:         "Hero + Features + CTA",
		SectionOrder: []string{"Hero with headline", "Value proposition", "Key features", "CTA section", "Footer"},
		CTAPlacement: "Hero (sticky) + Bottom",
	}
}

func defaultStyle() StyleRef {
	return StyleRef{
		Name:     "Minimal & Clean",
		Keywords: "clean, simple, spacious, high contrast",
		Effects:  "Subtle hover (200-250ms), smooth transitions, clear type hierarchy",
	}
}

func defaultPalette() Palette {
	return Palette{
		Primary:               "#2563EB",
		PrimaryForeground:     "#FFFFFF",
		Secondary:             "#3B82F6",
		SecondaryForeground:   "#000000",
		Accent:                "#EA580C",
		AccentForeground:      "#000000",
		Background:            "#F8FAFC",
		Foreground:            "#1E293B",
		Card:                  "#FFFFFF",
		CardForeground:        "#1E293B",
		Muted:                 "#E9EFF8",
		MutedForeground:       "#475569",
		Border:                "#E2E8F0",
		Destructive:           "#DC2626",
		DestructiveForeground: "#FFFFFF",
		Ring:                  "#2563EB",
	}
}

func defaultTypography() TypePairing {
	return TypePairing{
		HeadingFont:  "Inter",
		BodyFont:     "Inter",
		HeadingStack: "'Inter', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
		BodyStack:    "'Inter', system-ui, -apple-system, 'Segoe UI', Roboto, Arial, sans-serif",
	}
}
