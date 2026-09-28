package design

import (
	"math"
	"sort"
	"strings"
)

// tokenize lowercases and splits on non-alphanumeric characters.
func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r | 32) // crude lowercase for ASCII
		} else if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// stopwords are generic instruction verbs and web-filler words that carry no
// product-domain signal. Without this filter a query like "build simple
// coffee landing" matches "Link-in-Bio Page Builder" on the token "build"
// and the coffee shop gets a link-in-bio design instead of a cafe one.
var stopwords = map[string]bool{
	"build": true, "create": true, "make": true, "generate": true,
	"develop": true, "write": true, "add": true, "give": true, "need": true,
	"want": true, "please": true, "design": true,
	"simple": true, "basic": true, "nice": true, "good": true, "great": true,
	"beautiful": true, "new": true, "small": true, "quick": true, "fast": true,
	"very": true, "one": true, "using": true, "use": true, "with": true,
	"for": true, "and": true, "the": true, "that": true, "this": true,
	"from": true, "into": true, "page": true, "pages": true, "site": true,
	"website": true, "webpage": true, "landing": true, "homepage": true,
	"web": true, "html": true, "css": true, "javascript": true,
	"app": true, "application": true, "full": true, "complete": true,
}

// synonyms expands user words to the catalog's product vocabulary (part 1:
// food, drink, health, professional services). The catalog names real
// product rows ("Bakery/Cafe", "Fitness/Gym App", …) but users say
// "coffee", "gym", "lawyer" — without expansion those queries match
// nothing and every generation falls back to the generic blue default.
// Values are tokens that actually appear in products.csv / colors.csv rows.
var synonyms = map[string][]string{
	// food & drink
	"coffee": {"cafe"}, "espresso": {"cafe"}, "latte": {"cafe"},
	"barista": {"cafe"}, "roastery": {"cafe"}, "tea": {"cafe"},
	"boba": {"cafe"}, "cupcake": {"bakery"}, "donut": {"bakery"},
	"croissant": {"bakery"}, "dessert": {"bakery"},
	"pizzeria": {"restaurant"}, "bistro": {"restaurant"},
	"diner": {"restaurant"}, "eatery": {"restaurant"},
	"catering": {"restaurant"}, "sushi": {"restaurant"},
	"burger":  {"restaurant"},
	"brewery": {"brewery"}, "winery": {"brewery"}, "beer": {"brewery"},
	"wine": {"brewery"}, "cocktail": {"brewery"}, "bar": {"brewery"},
	// health & wellbeing
	"gym": {"fitness"}, "yoga": {"fitness"}, "pilates": {"fitness"},
	"workout": {"fitness"}, "crossfit": {"fitness"},
	"dentist": {"dental"}, "dental": {"dental"}, "clinic": {"clinic"},
	"doctor": {"clinic"}, "medical": {"clinic"}, "therapy": {"clinic"},
	"pharmacy": {"pharmacy"}, "meditation": {"meditation"},
	"mindfulness": {"meditation"},

	// professional services
	"lawyer": {"legal"}, "attorney": {"legal"}, "attorneys": {"legal"},
	"realtor": {"real"}, "broker": {"real"}, "estate": {"estate"},
	"hotel": {"hotel"}, "resort": {"hotel"}, "hostel": {"hotel"},
	"motel": {"hotel"}, "airbnb": {"hotel"},
	"travel": {"travel"}, "tour": {"travel"}, "tourism": {"travel"},
	"vacation": {"travel"}, "trip": {"travel"},
	"school": {"learning"}, "course": {"learning"}, "academy": {"learning"},
	"tutor": {"learning"}, "education": {"learning"},
	"teaching": {"learning"}, "bootcamp": {"learning"},
	"insurance": {"insurance"}, "bank": {"finance"}, "banking": {"finance"},
	"fintech": {"fintech"}, "loan": {"finance"}, "mortgage": {"finance"},
	"invest": {"finance"}, "trading": {"fintech"},
	"crypto": {"crypto"}, "bitcoin": {"crypto"}, "nft": {"nft"},
	"web3": {"web3"}, "blockchain": {"crypto"},
	"salon": {"beauty"}, "barber": {"beauty"}, "spa": {"beauty"},
	"beauty": {"beauty"}, "nails": {"beauty"}, "massage": {"beauty"},
	"wellness": {"beauty"},
}

// synonyms2 expands part 2: creative & media, commerce & community,
// industry & local services.
var synonyms2 = map[string][]string{
	"photographer": {"photography"}, "photography": {"photography"},
	"photo": {"photography"}, "portfolio": {"portfolio"},
	"freelance": {"freelancer"}, "freelancer": {"freelancer"},
	"resume": {"resume"}, "cv": {"resume"},
	"podcast": {"podcast"}, "news": {"news"}, "blog": {"blog"},
	"magazine": {"magazine"}, "newsletter": {"newsletter"},
	"music": {"music"}, "band": {"music"}, "concert": {"music"},
	"movie": {"streaming"}, "film": {"streaming"}, "netflix": {"streaming"},
	"museum": {"museum"}, "gallery": {"museum"}, "art": {"museum"},
	"exhibition": {"museum"}, "theater": {"theater"}, "cinema": {"theater"},
	"gaming": {"gaming"}, "game": {"gaming"}, "esports": {"gaming"},
	"dating": {"dating"}, "weather": {"weather"},
	"ecommerce": {"commerce"}, "shop": {"commerce"}, "store": {"commerce"},
	"cart": {"commerce"}, "checkout": {"commerce"}, "retail": {"commerce"},
	"boutique": {"commerce"}, "marketplace": {"marketplace"},
	"charity": {"charity"}, "donation": {"charity"}, "ngo": {"charity"},
	"church": {"church"}, "mosque": {"church"}, "temple": {"church"},
	"community": {"community"}, "forum": {"forum"},
	"membership": {"membership"}, "subscription": {"subscription"},
	"crowdfunding": {"crowdfunding"}, "auction": {"auction"},
	"automotive": {"automotive"}, "car": {"automotive"},
	"cars": {"automotive"}, "dealership": {"automotive"},
	"garage": {"automotive"}, "mechanic": {"automotive"},
	"construction": {"construction"}, "architect": {"architecture"},
	"architecture": {"architecture"}, "interior": {"interior"},
	"furniture": {"interior"}, "decor": {"interior"},
	"logistics": {"logistics"}, "shipping": {"logistics"},
	"pet": {"pet"}, "dog": {"pet"}, "cat": {"pet"}, "vet": {"veterinary"},
	"veterinary": {"veterinary"}, "veterinarian": {"veterinary"},
	"florist": {"florist"}, "flower": {"florist"}, "plant": {"plant"},
	"garden": {"florist"},
	"farm":   {"agriculture"}, "agriculture": {"agriculture"},
	"kids": {"childcare"}, "children": {"childcare"}, "baby": {"childcare"},
	"daycare": {"childcare"}, "nursery": {"childcare"},
	"senior": {"senior"}, "elderly": {"senior"},
	"government": {"government"}, "civic": {"government"},
	"parking": {"parking"}, "transit": {"transit"}, "bus": {"transit"},
	"train": {"transit"}, "metro": {"transit"},
	"wedding": {"wedding"}, "event": {"event"}, "conference": {"event"},
	"meetup": {"event"}, "festival": {"event"},
	"agency": {"agency"}, "studio": {"studio"}, "startup": {"saas"},
	"saas": {"saas"}, "b2b": {"b2b"}, "dashboard": {"dashboard"},
	"analytics": {"analytics"}, "crm": {"crm"},
	"invoice": {"invoice"}, "billing": {"invoice"},
	"booking": {"booking"}, "appointment": {"booking"},
	"reservation": {"booking"},
	"coworking":   {"coworking"}, "nonprofit": {"charity"},
	"security": {"cybersecurity"}, "sports": {"sports"},
	"sport": {"sports"}, "football": {"sports"}, "basketball": {"sports"},
}

// queryTokens normalizes a query into searchable tokens: lowercased, the
// stopwords dropped, then each surviving token's synonym expansions
// appended (coffee → +cafe), de-duplicated, order-preserving.
func queryTokens(q string) []string {
	base := tokenize(q)
	out := make([]string, 0, len(base)+4)
	seen := make(map[string]bool, len(base)*2)
	for _, t := range base {
		if len(t) < 3 || stopwords[t] {
			continue
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
		for _, s := range append(lookupSyn(t, synonyms), lookupSyn(t, synonyms2)...) {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func lookupSyn(t string, m map[string][]string) []string {
	return m[t]
}

// scored is one ranked row: its index into the source slice, its relevance
// score, and the position (within the normalized query tokens) of the
// EARLIEST token that matched. firstHit is the tie-breaker: users state the
// subject first ("dental clinic", "coffee shop", "law firm"), so at equal
// score the row matching the earlier token wins.
type scored struct {
	idx      int
	score    float64
	firstHit int
}

// rankByQuery scores rows by weighted keyword overlap with the normalized
// query tokens (see queryTokens): a token hit adds the field's weight,
// longer tokens count more, and a full phrase substring match adds a bonus.
// Ties break toward the row that matched the earliest query token.
func rankByQuery[T any](rows []T, query string, fields func(T) []string) []int {
	qTokens := queryTokens(query)
	if len(qTokens) == 0 {
		return nil
	}
	scores := make([]scored, 0, len(rows))
	for i, row := range rows {
		s := 0.0
		firstHit := len(qTokens)
		fs := fields(row)
		for fi, f := range fs {
			lf := strings.ToLower(f)
			if lf == "" {
				continue
			}
			// Weight earlier (more significant) fields higher.
			weight := float64(len(fs) - fi)
			for ti, tok := range qTokens {
				if len(tok) < 3 {
					continue
				}
				if strings.Contains(lf, tok) {
					s += weight * (1 + math.Log2(float64(len(tok))))
					if ti < firstHit {
						firstHit = ti
					}
				}
			}
			// Full-phrase match (the query inside the field, or the field
			// inside the query). The reverse direction only counts for
			// substantial field values — otherwise any short generic field
			// that happens to be a substring of the raw query ("build",
			// "page", …) earns an unearned bonus.
			if strings.Contains(lf, strings.ToLower(query)) {
				s += weight * 2
				if firstHit > 0 {
					firstHit = 0
				}
			} else if len(lf) >= 6 && strings.Contains(strings.ToLower(query), lf) {
				s += weight * 2
			}
		}
		if s > 0 {
			scores = append(scores, scored{i, s, firstHit})
		}
	}
	sort.SliceStable(scores, func(a, b int) bool {
		if scores[a].score != scores[b].score {
			return scores[a].score > scores[b].score
		}
		return scores[a].firstHit < scores[b].firstHit
	})
	out := make([]int, 0, len(scores))
	for _, sc := range scores {
		out = append(out, sc.idx)
	}
	return out
}

// bestProduct finds the closest product-type row for the query.
func bestProduct(c *catalog, query string) *productRow {
	idxs := rankByQuery(c.products, query, func(p productRow) []string {
		return []string{p.ProductType, p.Keywords}
	})
	if len(idxs) == 0 {
		return nil
	}
	return &c.products[idxs[0]]
}

// bestColorPalette finds the color row matching the query's product domain.
// Only the ProductType field is searched: the Notes column carries prose and
// stray hex codes ("[Accent adjusted from #F8FAFC]") that would match on
// noise instead of the product domain.
func bestColorPalette(c *catalog, query, productType string) *colorRow {
	q := query
	if productType != "" {
		q = productType + " " + query
	}
	idxs := rankByQuery(c.colors, q, func(r colorRow) []string {
		return []string{r.ProductType}
	})
	if len(idxs) == 0 {
		return nil
	}
	return &c.colors[idxs[0]]
}

// bestLandingPattern finds the landing-page pattern for the query.
func bestLandingPattern(c *catalog, query string) *landingRow {
	idxs := rankByQuery(c.landing, query, func(r landingRow) []string {
		return []string{r.Name, r.Keywords, r.SectionOrder}
	})
	if len(idxs) == 0 {
		return nil
	}
	return &c.landing[idxs[0]]
}

// bestTypography finds a font pairing for the query/mood.
func bestTypography(c *catalog, query, styleName string) *typeRow {
	q := query
	if styleName != "" {
		q = styleName + " " + query
	}
	idxs := rankByQuery(c.typography, q, func(r typeRow) []string {
		return []string{r.Name, r.Mood, r.BestFor, r.Heading, r.Body}
	})
	if len(idxs) == 0 {
		return nil
	}
	return &c.typography[idxs[0]]
}

// bestStyle resolves the recommended style name (from the product row, when
// known) to a style table row, falling back to a query match.
func bestStyle(c *catalog, query, styleHint string) *styleRow {
	if styleHint != "" {
		// The product row names a style (e.g. "Minimalism & Swiss Style").
		hint := strings.ToLower(styleHint)
		for i, s := range c.styles {
			if strings.Contains(strings.ToLower(s.Name), hint) || strings.Contains(hint, strings.ToLower(s.Name)) {
				return &c.styles[i]
			}
			// Token-level partial match.
			for _, t := range tokenize(styleHint) {
				if len(t) >= 4 && strings.Contains(strings.ToLower(s.Name), t) {
					return &c.styles[i]
				}
			}
		}
	}
	idxs := rankByQuery(c.styles, query, func(r styleRow) []string {
		return []string{r.Name, r.Keywords, r.Effects}
	})
	if len(idxs) == 0 {
		return nil
	}
	return &c.styles[idxs[0]]
}
