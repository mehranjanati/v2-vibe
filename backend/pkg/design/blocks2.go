package design

// blocksB is the second part of the static section registry (content
// sections: features, menu/products, about, testimonials). Every fragment
// uses only the semantic design tokens, so composing them keeps one
// themeable system.
var blocksB = []Block{
	{
		Name:     "features-grid",
		Title:    "Feature Grid",
		Section:  "features",
		Keywords: []string{"features", "feature", "benefits", "grid", "value", "cards", "key features"},
		Slots:    []string{"section heading", "3-4 cards: short title + one-line benefit each"},
		HTML: `<section class="features" id="features">
  <h2 class="section-title">Why choose us</h2>
  <div class="features__grid">
    <article class="card">
      <h3>Feature one</h3>
      <p>What it does and why it matters.</p>
    </article>
    <article class="card">
      <h3>Feature two</h3>
      <p>What it does and why it matters.</p>
    </article>
    <article class="card">
      <h3>Feature three</h3>
      <p>What it does and why it matters.</p>
    </article>
  </div>
</section>`,
		CSS: `.section-title { font-family: var(--font-heading); font-size: clamp(1.5rem, 3vw, 2rem); text-align: center; color: var(--foreground); margin: 0 0 var(--space-lg); }
.features { padding: var(--space-xl) var(--space-lg); background: var(--background); }
.features__grid { display: grid; gap: var(--space-lg); grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); }
.card { padding: var(--space-lg); border-radius: var(--radius-lg); background: var(--card); color: var(--card-foreground); border: 1px solid var(--border); box-shadow: var(--shadow-md); }
.card h3 { font-family: var(--font-heading); margin: 0 0 var(--space-sm); }
.card p { color: var(--muted-foreground); margin: 0; }`,
	},
	{
		Name:     "menu-grid",
		Title:    "Menu / Product Grid",
		Section:  "menu",
		Keywords: []string{"menu", "products", "items", "price", "price list", "catalog", "food", "drinks", "services", "offerings"},
		Slots:    []string{"section heading", "6-9 items: name + short description + price", "optional category label"},
		HTML: `<section class="menu" id="menu">
  <h2 class="section-title">Our menu</h2>
  <div class="menu__grid">
    <article class="menu__item">
      <h3>Item name</h3>
      <p class="menu__desc">Short description of the item.</p>
      <span class="menu__price">$0.00</span>
    </article>
    <article class="menu__item">
      <h3>Item name</h3>
      <p class="menu__desc">Short description of the item.</p>
      <span class="menu__price">$0.00</span>
    </article>
    <article class="menu__item">
      <h3>Item name</h3>
      <p class="menu__desc">Short description of the item.</p>
      <span class="menu__price">$0.00</span>
    </article>
    <article class="menu__item">
      <h3>Item name</h3>
      <p class="menu__desc">Short description of the item.</p>
      <span class="menu__price">$0.00</span>
    </article>
  </div>
</section>`,
		CSS: `.menu { padding: var(--space-xl) var(--space-lg); background: var(--muted); }
.menu__grid { display: grid; gap: var(--space-lg); grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); }
.menu__item { padding: var(--space-lg); border-radius: var(--radius-lg); background: var(--card); color: var(--card-foreground); text-align: center; border: 1px solid var(--border); }
.menu__item h3 { font-family: var(--font-heading); margin: 0 0 var(--space-sm); }
.menu__desc { color: var(--muted-foreground); margin: 0 0 var(--space-sm); }
.menu__price { display: inline-block; font-weight: 700; color: var(--primary); }`,
	},
	{
		Name:     "about-section",
		Title:    "About / Story",
		Section:  "about",
		Keywords: []string{"about", "story", "why us", "mission", "values", "company"},
		Slots:    []string{"heading", "2-3 sentence brand story"},
		HTML: `<section class="about" id="about">
  <h2 class="section-title">About us</h2>
  <p class="about__body">Two or three sentences on who you are, what you make and what makes it different.</p>
</section>`,
		CSS: `.about { padding: var(--space-xl) var(--space-lg); background: var(--primary); color: var(--primary-foreground); }
.about .section-title { color: inherit; }
.about__body { max-width: 65ch; margin: 0 auto; text-align: center; opacity: 0.9; }`,
	},
	{
		Name:     "testimonials",
		Title:    "Testimonials",
		Section:  "testimonials",
		Keywords: []string{"testimonials", "reviews", "social proof", "customers", "quotes", "trust"},
		Slots:    []string{"section heading", "2-3 quotes: specific result + customer name/role"},
		HTML: `<section class="testimonials" id="testimonials">
  <h2 class="section-title">What customers say</h2>
  <div class="testimonials__grid">
    <figure class="quote">
      <blockquote>Short, specific quote about the result they got.</blockquote>
      <figcaption>Name, role</figcaption>
    </figure>
    <figure class="quote">
      <blockquote>Short, specific quote about the result they got.</blockquote>
      <figcaption>Name, role</figcaption>
    </figure>
  </div>
</section>`,
		CSS: `.testimonials { padding: var(--space-xl) var(--space-lg); background: var(--background); }
.testimonials__grid { display: grid; gap: var(--space-lg); grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); }
.quote { margin: 0; padding: var(--space-lg); border-radius: var(--radius-lg); background: var(--card); color: var(--card-foreground); border: 1px solid var(--border); }
.quote blockquote { margin: 0 0 var(--space-md); font-size: 1.05rem; }
.quote figcaption { color: var(--muted-foreground); font-size: 0.9rem; }`,
	},
}
