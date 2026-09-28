package design

// Block is one composable static page section — the shadcn "registry:block"
// idea applied to plain HTML/CSS instead of React components.
//
// The planner chooses the page's section order (see Pattern.SectionOrder,
// sourced from landing.csv); the registry maps each named section onto a
// ready-made fragment. The coder then receives those fragments as
// authoritative structure and fills in the product-specific content
// (headings, menu items, prices) instead of inventing markup from scratch —
// which is what produced disconnected, unstyled sections before.
//
// Every fragment uses ONLY the semantic tokens from DesignBrief.TokensCSS()
// (var(--primary), var(--card), var(--font-heading), …) so a composed page is
// themeable and cannot drift from the design system.
type Block struct {
	// Name is the registry id, e.g. "hero-split".
	Name string
	// Title is a human-readable label.
	Title string
	// Section is the canonical section this fragment fills. It is matched
	// against the pattern's SectionOrder entries (hero, features, menu,
	// pricing, testimonials, cta, footer, nav, about, contact).
	Section string
	// Keywords help match a block to a free-text section description.
	Keywords []string
	// Slots names the content the coder must fill into this fragment
	// (headlines, menu items with prices, …). They ride the compact block
	// descriptors so the coder knows what to replace without the full
	// markup riding every step.
	Slots []string
	// HTML is the semantic markup fragment.
	HTML string
	// CSS holds the rules for this fragment, token-only colors.
	CSS string
	// JS is optional initialisation for this fragment ("" when none).
	JS string
}

// blocksA is the first half of the static section registry (navigation and
// hero variants), in canonical order. See blocks.go's package doc for why
// these are Go literals rather than external files.
var blocksA = []Block{
	{
		Name:     "navbar-sticky",
		Title:    "Sticky Navigation",
		Section:  "nav",
		Keywords: []string{"nav", "navigation", "navbar", "header", "menu bar", "sticky"},
		Slots:    []string{"brand name", "3-4 section links matching the page ids", "nav CTA label"},
		HTML: `<header class="site-nav">
  <a class="site-nav__brand" href="#">Brand</a>
  <nav class="site-nav__links" aria-label="Main">
    <a href="#features">Features</a>
    <a href="#menu">Menu</a>
    <a href="#about">About</a>
    <a class="site-nav__cta" href="#cta">Get started</a>
  </nav>
</header>`,
		CSS: `.site-nav {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-md);
  padding: var(--space-md) var(--space-lg);
  background: var(--background);
  border-bottom: 1px solid var(--border);
}
.site-nav__brand { font-family: var(--font-heading); font-weight: 700; color: var(--foreground); text-decoration: none; }
.site-nav__links { display: flex; align-items: center; gap: var(--space-md); }
.site-nav__links a { color: var(--muted-foreground); text-decoration: none; transition: color 200ms ease; }
.site-nav__links a:hover { color: var(--foreground); }
.site-nav__cta {
  padding: 0.5rem 1rem;
  border-radius: var(--radius-md);
  background: var(--primary);
  color: var(--primary-foreground);
}`,
	},
	{
		Name:     "hero-split",
		Title:    "Split Hero",
		Section:  "hero",
		Keywords: []string{"hero", "split", "headline", "cta", "above the fold"},
		Slots:    []string{"H1 headline naming the brand", "one-line lede on the outcome", "primary CTA label"},
		HTML: `<section class="hero-split" id="hero">
  <div class="hero-split__copy">
    <h1>Headline that names the product</h1>
    <p class="hero-split__lede">One sentence on the outcome the visitor gets.</p>
    <a class="btn btn--primary" href="#cta">Primary action</a>
  </div>
  <div class="hero-split__media" role="img" aria-label="Product visual"></div>
</section>`,
		CSS: `.hero-split {
  display: grid;
  gap: var(--space-xl);
  align-items: center;
  padding: var(--space-xl) var(--space-lg);
  background: var(--background);
}
@media (min-width: 768px) { .hero-split { grid-template-columns: 1.1fr 1fr; } }
.hero-split h1 { font-family: var(--font-heading); font-size: clamp(2rem, 5vw, 3rem); line-height: 1.1; color: var(--foreground); margin: 0 0 var(--space-md); }
.hero-split__lede { color: var(--muted-foreground); margin-bottom: var(--space-lg); }
.hero-split__media { min-height: 240px; border-radius: var(--radius-lg); background: var(--muted); }
.btn { display: inline-block; padding: 0.75rem 1.5rem; border-radius: var(--radius-md); text-decoration: none; transition: transform 200ms ease; }
.btn--primary { background: var(--primary); color: var(--primary-foreground); }
.btn:hover { transform: translateY(-2px); }
@media (prefers-reduced-motion: reduce) { .btn, .btn:hover { transition: none; transform: none; } }`,
	},
	{
		Name:     "hero-center",
		Title:    "Centered Hero",
		Section:  "hero",
		Keywords: []string{"hero", "centered", "center", "full bleed", "hero-centric"},
		Slots:    []string{"H1 headline naming the brand", "one-line lede", "primary CTA label"},
		HTML: `<section class="hero-center" id="hero">
  <h1>Headline that names the product</h1>
  <p class="hero-center__lede">One sentence on the outcome the visitor gets.</p>
  <a class="btn btn--primary" href="#cta">Primary action</a>
</section>`,
		CSS: `.hero-center {
  text-align: center;
  padding: var(--space-xl) var(--space-lg);
  background: var(--primary);
  color: var(--primary-foreground);
}
.hero-center h1 { font-family: var(--font-heading); font-size: clamp(2rem, 5vw, 3.25rem); line-height: 1.1; margin: 0 0 var(--space-md); }
.hero-center__lede { opacity: 0.85; margin: 0 auto var(--space-lg); max-width: 44ch; }
.hero-center .btn--primary { background: var(--accent); color: var(--accent-foreground); }`,
	},
}
