package design

// blocksC is the final part of the static section registry: conversion and
// closing sections (pricing, CTA band, contact form, footer).
var blocksC = []Block{
	{
		Name:     "pricing-3-tier",
		Title:    "Three-Tier Pricing",
		Section:  "pricing",
		Keywords: []string{"pricing", "plans", "tiers", "packages", "subscription", "plans and pricing"},
		Slots:    []string{"section heading", "3 tiers: name + price + period + 3-4 included items + button label", "which tier is featured"},
		HTML: `<section class="pricing" id="pricing">
  <h2 class="section-title">Plans</h2>
  <div class="pricing__grid">
    <article class="plan">
      <h3>Starter</h3>
      <p class="plan__price">$0<span>/mo</span></p>
      <ul><li>Included item</li><li>Included item</li></ul>
      <a class="btn btn--primary" href="#cta">Choose</a>
    </article>
    <article class="plan plan--featured">
      <h3>Pro</h3>
      <p class="plan__price">$0<span>/mo</span></p>
      <ul><li>Included item</li><li>Included item</li></ul>
      <a class="btn btn--primary" href="#cta">Choose</a>
    </article>
    <article class="plan">
      <h3>Team</h3>
      <p class="plan__price">$0<span>/mo</span></p>
      <ul><li>Included item</li><li>Included item</li></ul>
      <a class="btn btn--primary" href="#cta">Choose</a>
    </article>
  </div>
</section>`,
		CSS: `.pricing { padding: var(--space-xl) var(--space-lg); background: var(--muted); }
.pricing__grid { display: grid; gap: var(--space-lg); grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); align-items: start; }
.plan { display: grid; gap: var(--space-md); padding: var(--space-lg); border-radius: var(--radius-lg); background: var(--card); color: var(--card-foreground); border: 1px solid var(--border); }
.plan--featured { border-color: var(--primary); box-shadow: var(--shadow-md); }
.plan h3 { font-family: var(--font-heading); margin: 0; }
.plan__price { font-size: 2rem; font-weight: 700; color: var(--primary); margin: 0; }
.plan__price span { font-size: 0.9rem; font-weight: 400; color: var(--muted-foreground); }
.plan ul { margin: 0; padding-left: 1.1rem; color: var(--muted-foreground); }`,
	},
	{
		Name:     "cta-band",
		Title:    "Call To Action Band",
		Section:  "cta",
		Keywords: []string{"cta", "call to action", "conversion", "signup", "sign up", "get started", "final"},
		Slots:    []string{"headline", "one-line next step", "button label"},
		HTML: `<section class="cta" id="cta">
  <h2>Ready to get started?</h2>
  <p>One line on the next step.</p>
  <a class="btn btn--primary" href="#">Primary action</a>
</section>`,
		CSS: `.cta { text-align: center; padding: var(--space-xl) var(--space-lg); background: var(--primary); color: var(--primary-foreground); }
.cta h2 { font-family: var(--font-heading); margin: 0 0 var(--space-sm); }
.cta p { margin: 0 0 var(--space-lg); opacity: 0.9; }
.cta .btn--primary { background: var(--accent); color: var(--accent-foreground); }`,
	},
	{
		Name:     "contact-form",
		Title:    "Contact Form",
		Section:  "contact",
		Keywords: []string{"contact", "form", "enquiry", "inquiry", "get in touch", "message"},
		Slots:    []string{"heading", "form fields needed", "submit label"},
		HTML: `<section class="contact" id="contact">
  <h2 class="section-title">Get in touch</h2>
  <form class="contact__form" id="contact-form">
    <label for="contact-name">Name</label>
    <input id="contact-name" name="name" type="text" required>
    <label for="contact-email">Email</label>
    <input id="contact-email" name="email" type="email" required>
    <label for="contact-message">Message</label>
    <textarea id="contact-message" name="message" rows="4"></textarea>
    <button class="btn btn--primary" type="submit">Send</button>
  </form>
</section>`,
		CSS: `.contact { padding: var(--space-xl) var(--space-lg); background: var(--background); }
.contact__form { display: grid; gap: var(--space-sm); max-width: 32rem; margin: 0 auto; }
.contact__form label { color: var(--muted-foreground); font-size: 0.9rem; }
.contact__form input,
.contact__form textarea {
  padding: 0.65rem 0.8rem;
  border: 1px solid var(--input);
  border-radius: var(--radius-md);
  background: var(--card);
  color: var(--card-foreground);
  font: inherit;
}
.contact__form input:focus-visible,
.contact__form textarea:focus-visible { outline: 2px solid var(--ring); outline-offset: 1px; }`,
		JS: `document.getElementById('contact-form')?.addEventListener('submit', function (e) {
    e.preventDefault();
    this.reset();
    alert('Thanks — we will be in touch.');
  });`,
	},
	{
		Name:     "footer-4col",
		Title:    "Footer",
		Section:  "footer",
		Keywords: []string{"footer", "links", "sitemap", "legal", "bottom"},
		Slots:    []string{"brand name", "footer links matching page ids", "copyright line"},
		HTML: `<footer class="site-footer">
  <p class="site-footer__brand">Brand</p>
  <nav class="site-footer__links" aria-label="Footer">
    <a href="#features">Features</a>
    <a href="#menu">Menu</a>
    <a href="#about">About</a>
    <a href="#contact">Contact</a>
  </nav>
  <p class="site-footer__legal">&copy; 2025 Brand. All rights reserved.</p>
</footer>`,
		CSS: `.site-footer { display: grid; gap: var(--space-md); padding: var(--space-xl) var(--space-lg); background: var(--foreground); color: var(--background); text-align: center; }
.site-footer__brand { font-family: var(--font-heading); font-weight: 700; margin: 0; }
.site-footer__links { display: flex; flex-wrap: wrap; gap: var(--space-md); justify-content: center; }
.site-footer__links a { color: inherit; opacity: 0.8; text-decoration: none; }
.site-footer__links a:hover { opacity: 1; }
.site-footer__legal { margin: 0; font-size: 0.85rem; opacity: 0.7; }`,
	},
}
