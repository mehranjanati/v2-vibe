package design

import (
	"fmt"
	"strings"
)

// token pairs follow the shadcn semantic convention: a base token for the
// surface, and a -foreground token for the content that sits on it.
// Defining the pair together keeps contrast under control.

// TokensCSS renders the palette + typography + radius as the :root token
// block every generated stylesheet must define, plus the .dark override.
// Token names are the shadcn semantic vocabulary (background, foreground,
// primary, primary-foreground, card, card-foreground, muted, muted-foreground,
// secondary, secondary-foreground, accent, accent-foreground, destructive,
// destructive-foreground, border, input, ring) so the whole app shares one
// themeable design system instead of ad-hoc --color-* names.
func (b *DesignBrief) TokensCSS() string {
	var sb strings.Builder
	sb.WriteString(":root {\n")
	writeVar(&sb, "--radius", b.radius())
	sb.WriteString("\n")
	writeVar(&sb, "--background", b.Palette.Background)
	writeVar(&sb, "--foreground", b.Palette.Foreground)
	writeVar(&sb, "--card", b.Palette.Card)
	writeVar(&sb, "--card-foreground", b.Palette.CardForeground)
	writeVar(&sb, "--primary", b.Palette.Primary)
	writeVar(&sb, "--primary-foreground", b.Palette.PrimaryForeground)
	writeVar(&sb, "--secondary", b.Palette.Secondary)
	writeVar(&sb, "--secondary-foreground", b.Palette.SecondaryForeground)
	writeVar(&sb, "--muted", b.Palette.Muted)
	writeVar(&sb, "--muted-foreground", b.Palette.MutedForeground)
	writeVar(&sb, "--accent", b.Palette.Accent)
	writeVar(&sb, "--accent-foreground", b.Palette.AccentForeground)
	writeVar(&sb, "--destructive", b.Palette.Destructive)
	writeVar(&sb, "--destructive-foreground", b.Palette.DestructiveForeground)
	writeVar(&sb, "--border", b.Palette.Border)
	writeVar(&sb, "--input", b.Palette.Border)
	writeVar(&sb, "--ring", b.Palette.Ring)
	sb.WriteString("\n")
	writeVar(&sb, "--font-heading", b.Type.HeadingStack)
	writeVar(&sb, "--font-body", b.Type.BodyStack)
	writeVar(&sb, "--space-sm", "0.5rem")
	writeVar(&sb, "--space-md", "1rem")
	writeVar(&sb, "--space-lg", "2rem")
	writeVar(&sb, "--space-xl", "3rem")
	writeVar(&sb, "--radius-md", "calc(var(--radius) + 2px)")
	writeVar(&sb, "--radius-lg", "calc(var(--radius) + 6px)")
	writeVar(&sb, "--shadow-md", "0 4px 12px rgba(0, 0, 0, 0.08)")
	sb.WriteString("}\n")

	// Dark mode: override the surface tokens, keep the brand primaries. The
	// catalog palette is light-mode; a generated dark variant inverts the
	// neutral surfaces and text while preserving the accent identity.
	dark := b.darkPalette()
	sb.WriteString("\n.dark {\n")
	writeVar(&sb, "--background", dark.Background)
	writeVar(&sb, "--foreground", dark.Foreground)
	writeVar(&sb, "--card", dark.Card)
	writeVar(&sb, "--card-foreground", dark.CardForeground)
	writeVar(&sb, "--muted", dark.Muted)
	writeVar(&sb, "--muted-foreground", dark.MutedForeground)
	writeVar(&sb, "--border", dark.Border)
	writeVar(&sb, "--input", dark.Border)
	sb.WriteString("}\n")
	return sb.String()
}

// radius derives a base border-radius from the style's design variables, or
// a sensible default.
func (b *DesignBrief) radius() string {
	if b.Style.DesignVars != "" {
		for _, part := range strings.Split(b.Style.DesignVars, ",") {
			part = strings.TrimSpace(part)
			if strings.Contains(part, "radius") {
				if i := strings.Index(part, ":"); i >= 0 {
					if v := strings.TrimSpace(part[i+1:]); v != "" {
						return v
					}
				}
			}
		}
	}
	return "0.5rem"
}

// darkPalette produces a dark-mode surface scheme by inverting the neutral
// surfaces/text while keeping the brand primaries readable on dark.
func (b *DesignBrief) darkPalette() Palette {
	p := b.Palette
	// The light-mode foreground is typically dark; use it as the dark
	// background. The light-mode background is light; use it as the dark
	// foreground (text on dark).
	darkBg := p.Foreground
	darkFg := p.Background
	p.Background = darkBg
	p.Foreground = darkFg
	// Card/muted/border should be elevated dark tones, not the light-mode
	// values swapped in. Shade the dark background slightly toward white
	// to get a layered surface hierarchy.
	p.Card = shadeHex(darkBg, 0.08)
	p.CardForeground = darkFg
	p.Muted = shadeHex(darkBg, 0.12)
	p.MutedForeground = shadeHex(darkFg, -0.25)
	p.Border = shadeHex(darkBg, 0.18)
	return p
}

// shadeHex shifts a hex color toward white (amt>0) or black (amt<0) by the
// given fraction (0..1). Returns the original if parsing fails so callers
// can use it without error handling.
func shadeHex(hex string, amt float64) string {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return hex
	}
	r, g, b := 0, 0, 0
	for i := 0; i < 6; i++ {
		v := int(h[i])
		switch {
		case v >= '0' && v <= '9':
			v -= '0'
		case v >= 'a' && v <= 'f':
			v -= 'a' - 10
		case v >= 'A' && v <= 'F':
			v -= 'A' - 10
		default:
			return hex
		}
		switch i {
		case 0:
			r = v << 4
		case 1:
			r |= v
		case 2:
			g = v << 4
		case 3:
			g |= v
		case 4:
			b = v << 4
		case 5:
			b |= v
		}
	}
	shift := func(c int) int {
		if amt >= 0 {
			c += int(float64(255-c) * amt)
		} else {
			c += int(float64(c) * amt)
		}
		if c < 0 {
			return 0
		}
		if c > 255 {
			return 255
		}
		return c
	}
	return fmt.Sprintf("#%02x%02x%02x", shift(r), shift(g), shift(b))
}

func writeVar(sb *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(sb, "  %s: %s;\n", name, value)
}

// UsageSummary lists the token names the coder must USE (as var(--…)) with
// one-line guidance. This is what guarantees every file references the same
// design system.
func (b *DesignBrief) UsageSummary() string {
	return "Use only the defined tokens: surfaces bg/background, text/foreground, " +
		"cards bg/card, primary actions bg/primary + text/primary-foreground, " +
		"secondary bg/secondary, accents bg/accent, borders border, focus ring ring. " +
		"Typography: headings var(--font-heading), body var(--font-body). " +
		"Radius var(--radius/-md/-lg), spacing var(--space-*), shadow var(--shadow-md)."
}
