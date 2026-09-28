package design

import (
	"strings"
)

// RenderPlanner renders the brief as compact Markdown for the planner's
// system context (the same pattern BuildRAGContext uses). It gives the
// planner the section structure, the chosen style and the palette so the
// plan it emits is design-aware instead of generic. Capped to stay well
// within the planner's context budget.
func (b *DesignBrief) RenderPlanner() string {
	var sb strings.Builder
	sb.WriteString("## Design direction (follow this exactly)\n\n")
	sb.WriteString("Product: " + b.Product + "\n\n")

	sb.WriteString("### Page pattern: " + b.Pattern.Name + "\n")
	if len(b.Pattern.SectionOrder) > 0 {
		sb.WriteString("Section order (in this order):\n")
		for i, s := range b.Pattern.SectionOrder {
			sb.WriteString("- " + itoaN(i+1) + ". " + s + "\n")
		}
	}
	if b.Pattern.CTAPlacement != "" {
		sb.WriteString("CTA placement: " + b.Pattern.CTAPlacement + "\n")
	}
	if b.Pattern.Conversion != "" {
		sb.WriteString("Conversion: " + b.Pattern.Conversion + "\n")
	}

	sb.WriteString("\n### Visual style: " + b.Style.Name + "\n")
	if b.Style.Keywords != "" {
		sb.WriteString("Keywords: " + b.Style.Keywords + "\n")
	}
	if b.Style.Effects != "" {
		sb.WriteString("Effects: " + b.Style.Effects + "\n")
	}
	if len(b.Style.CSSKeywords) > 0 {
		sb.WriteString("CSS: " + strings.Join(b.Style.CSSKeywords, "; ") + "\n")
	}
	if b.Style.DoNotUseFor != "" {
		sb.WriteString("Avoid: " + b.Style.DoNotUseFor + "\n")
	}
	if len(b.AntiPatterns) > 0 {
		sb.WriteString("Anti-patterns: " + strings.Join(b.AntiPatterns, "; ") + "\n")
	}

	sb.WriteString("\n### Palette (use these exact colors as CSS tokens)\n")
	sb.WriteString(b.TokensCSS())

	sb.WriteString("\n### Typography\n")
	sb.WriteString("Headings: " + b.Type.HeadingFont + " (" + b.Type.HeadingStack + ")\n")
	sb.WriteString("Body: " + b.Type.BodyFont + " (" + b.Type.BodyStack + ")\n")

	if len(b.Motion) > 0 {
		sb.WriteString("\n### Motion\n")
		for _, m := range b.Motion {
			sb.WriteString("- " + m.Trigger + ": " + m.Duration + " " + m.Easing + " — " + m.Do + "\n")
		}
	}
	if len(b.A11y) > 0 {
		sb.WriteString("\n### Accessibility (must satisfy)\n")
		for _, a := range b.A11y {
			sb.WriteString("- " + a.Do + "\n")
		}
	}
	return sb.String()
}

// itoaN formats small ints without strconv import cost in this file.
func itoaN(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
