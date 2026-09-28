// Package design provides the brand/design intelligence layer for the
// dual-model code-generation pipeline: a searchable catalog of UI styles,
// color palettes, font pairings, landing-page patterns, UX guidelines and
// motion specs, vendored from the ui-ux-pro-max skill (MIT, Next Level
// Builder) into pkg/design/data and loaded once via go:embed.
//
// Before every generation the pipeline builds a DesignBrief from the user's
// prompt: the recommended page pattern (section order, CTA placement), the
// style (keywords + implementation checklist), a complete semantic color
// palette, a font pairing, motion guidance and accessibility rules. That
// brief is injected into the planner prompt (so the plan is structure-aware)
// and into every coder step via plan_context.design (so every file shares
// one design system instead of inventing ad-hoc colors and generic copy).
//
// The whole layer is offline: no network, no API key, no external service.
package design

// Palette is a complete semantic color scheme. The 16 columns of
// colors.csv map onto the shadcn-style token convention (base token +
// -foreground pair) so generated CSS uses a predictable, themeable
// vocabulary instead of ad-hoc names.
type Palette struct {
	Primary               string `json:"primary"`
	PrimaryForeground     string `json:"primary_foreground"`
	Secondary             string `json:"secondary"`
	SecondaryForeground   string `json:"secondary_foreground"`
	Accent                string `json:"accent"`
	AccentForeground      string `json:"accent_foreground"`
	Background            string `json:"background"`
	Foreground            string `json:"foreground"`
	Card                  string `json:"card"`
	CardForeground        string `json:"card_foreground"`
	Muted                 string `json:"muted"`
	MutedForeground       string `json:"muted_foreground"`
	Border                string `json:"border"`
	Destructive           string `json:"destructive"`
	DestructiveForeground string `json:"destructive_foreground"`
	Ring                  string `json:"ring"`
	Notes                 string `json:"notes,omitempty"`
}

// TypePairing is a heading/body font recommendation. CSSStack carries the
// concrete font-family values with system fallbacks so the generated app
// works offline (no Google Fonts CDN dependency).
type TypePairing struct {
	HeadingFont string `json:"heading_font"`
	BodyFont    string `json:"body_font"`
	Mood        string `json:"mood,omitempty"`
	BestFor     string `json:"best_for,omitempty"`
	// HeadingStack and BodyStack are full CSS font-family values including
	// safe fallbacks, e.g. `"Playfair Display", Georgia, serif`.
	HeadingStack string `json:"heading_stack"`
	BodyStack    string `json:"body_stack"`
	Notes        string `json:"notes,omitempty"`
}

// Pattern is a landing-page layout recipe: the ordered sections, where the
// primary CTA goes, and how to optimize for conversion.
type Pattern struct {
	Name          string   `json:"name"`
	SectionOrder  []string `json:"section_order"`
	CTAPlacement  string   `json:"cta_placement,omitempty"`
	ColorStrategy string   `json:"color_strategy,omitempty"`
	Conversion    string   `json:"conversion,omitempty"`
}

// StyleRef is one UI style (e.g. Minimalism & Swiss): its visual identity,
// CSS-level implementation hints, and what NOT to use it for.
type StyleRef struct {
	Name        string   `json:"name"`
	Keywords    string   `json:"keywords,omitempty"`
	Effects     string   `json:"effects,omitempty"`
	CSSKeywords []string `json:"css_keywords,omitempty"`
	DoNotUseFor string   `json:"do_not_use_for,omitempty"`
	Checklist   []string `json:"checklist,omitempty"`
	DesignVars  string   `json:"design_vars,omitempty"`
}

// MotionSpec is one motion/interaction guideline (hover, scroll reveal,
// transition). GSAP snippets from the source data are translated into plain
// CSS guidance — the generated app is static with no JS animation library.
type MotionSpec struct {
	Intensity string `json:"intensity,omitempty"`
	Trigger   string `json:"trigger,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Easing    string `json:"easing,omitempty"`
	Do        string `json:"do,omitempty"`
	Dont      string `json:"dont,omitempty"`
}

// A11yRule is one accessibility/UX guideline with severity.
type A11yRule struct {
	Category string `json:"category"`
	Issue    string `json:"issue"`
	Do       string `json:"do"`
	Dont     string `json:"dont,omitempty"`
	Severity string `json:"severity,omitempty"`
}

// DesignBrief is the assembled design direction for one generation run. It
// is the single source of design truth shared by the planner (section
// structure) and every coder step (colors, fonts, motion, a11y).
type DesignBrief struct {
	Query        string       `json:"query"`
	Product      string       `json:"product"`
	Pattern      Pattern      `json:"pattern"`
	Style        StyleRef     `json:"style"`
	Palette      Palette      `json:"palette"`
	Type         TypePairing  `json:"typography"`
	Motion       []MotionSpec `json:"motion,omitempty"`
	A11y         []A11yRule   `json:"a11y,omitempty"`
	AntiPatterns []string     `json:"anti_patterns,omitempty"`
}
