package design

import (
	"embed"
	"encoding/csv"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/*.csv
var dataFS embed.FS

// catalog holds the parsed tables, loaded once.
type catalog struct {
	styles     []styleRow
	colors     []colorRow
	typography []typeRow
	landing    []landingRow
	ux         []uxRow
	motion     []motionRow
	products   []productRow
	reasoning  []reasoningRow
}

var (
	loadOnce sync.Once
	cat      *catalog
	loadErr  error
)

// getCatalog parses all CSVs exactly once, caching the result. A parse
// failure is stored and returned on every call so a corrupt embedded file
// surfaces as an error, not a silent nil table.
func getCatalog() (*catalog, error) {
	loadOnce.Do(func() {
		c := &catalog{}
		var err error
		if c.styles, err = loadStyles(); err != nil {
			loadErr = fmt.Errorf("styles.csv: %w", err)
			return
		}
		if c.colors, err = loadColors(); err != nil {
			loadErr = fmt.Errorf("colors.csv: %w", err)
			return
		}
		if c.typography, err = loadTypography(); err != nil {
			loadErr = fmt.Errorf("typography.csv: %w", err)
			return
		}
		if c.landing, err = loadLanding(); err != nil {
			loadErr = fmt.Errorf("landing.csv: %w", err)
			return
		}
		if c.ux, err = loadUX(); err != nil {
			loadErr = fmt.Errorf("ux-guidelines.csv: %w", err)
			return
		}
		if c.motion, err = loadMotion(); err != nil {
			loadErr = fmt.Errorf("motion.csv: %w", err)
			return
		}
		if c.products, err = loadProducts(); err != nil {
			loadErr = fmt.Errorf("products.csv: %w", err)
			return
		}
		if c.reasoning, err = loadReasoning(); err != nil {
			loadErr = fmt.Errorf("ui-reasoning.csv: %w", err)
			return
		}
		cat = c
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return cat, nil
}

// readCSV parses one embedded CSV into header-keyed rows.
func readCSV(name string) ([]map[string]string, error) {
	f, err := dataFS.Open("data/" + name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate ragged rows
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("no records")
	}
	header := make([]string, len(recs[0]))
	for i, h := range recs[0] {
		header[i] = strings.TrimSpace(h)
	}
	rows := make([]map[string]string, 0, len(recs)-1)
	for _, rec := range recs[1:] {
		row := make(map[string]string, len(header))
		for i, col := range header {
			if i < len(rec) {
				row[col] = strings.TrimSpace(rec[i])
			}
		}
		// Skip fully-empty rows.
		empty := true
		for _, v := range row {
			if v != "" {
				empty = false
				break
			}
		}
		if !empty {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// splitList splits a comma/semicolon/pipe-delimited cell into trimmed,
// non-empty items.
func splitList(s string) []string {
	var out []string
	for _, sep := range []string{",", ";", "|"} {
		if strings.Contains(s, sep) {
			for _, p := range strings.Split(s, sep) {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			return out
		}
	}
	if t := strings.TrimSpace(s); t != "" {
		out = append(out, t)
	}
	return out
}

// --- row types mirroring the CSV columns ---

type styleRow struct {
	Name        string
	Keywords    string
	Effects     string
	DoNotUseFor string
	CSSKeywords []string
	Checklist   []string
	DesignVars  string
}

type colorRow struct {
	ProductType   string
	Primary       string
	OnPrimary     string
	Secondary     string
	OnSecondary   string
	Accent        string
	OnAccent      string
	Background    string
	Foreground    string
	Card          string
	CardFg        string
	Muted         string
	MutedFg       string
	Border        string
	Destructive   string
	OnDestructive string
	Ring          string
	Notes         string
}

type typeRow struct {
	Name    string
	Heading string
	Body    string
	Mood    string
	BestFor string
	Notes   string
}

type landingRow struct {
	Name          string
	Keywords      string
	SectionOrder  string
	CTAPlacement  string
	ColorStrategy string
	Conversion    string
}

type uxRow struct {
	Category string
	Issue    string
	Do       string
	Dont     string
	Severity string
}

type motionRow struct {
	Intensity string
	Keywords  string
	Trigger   string
	Duration  string
	Easing    string
	Do        string
	Dont      string
}

type productRow struct {
	ProductType    string
	Keywords       string
	PrimaryStyle   string
	LandingPattern string
}

type reasoningRow struct {
	Category      string
	Pattern       string
	StylePriority string
	ColorMood     string
	TypeMood      string
	KeyEffects    string
	AntiPatterns  string
}

// --- loaders: map each CSV's header columns onto the row types ---

func loadStyles() ([]styleRow, error) {
	rows, err := readCSV("styles.csv")
	if err != nil {
		return nil, err
	}
	out := make([]styleRow, 0, len(rows))
	for _, r := range rows {
		if r["Style Category"] == "" {
			continue
		}
		out = append(out, styleRow{
			Name:        r["Style Category"],
			Keywords:    r["Keywords"],
			Effects:     r["Effects & Animation"],
			DoNotUseFor: r["Do Not Use For"],
			CSSKeywords: splitList(r["CSS/Technical Keywords"]),
			Checklist:   splitList(r["Implementation Checklist"]),
			DesignVars:  r["Design System Variables"],
		})
	}
	return out, nil
}

func loadColors() ([]colorRow, error) {
	rows, err := readCSV("colors.csv")
	if err != nil {
		return nil, err
	}
	out := make([]colorRow, 0, len(rows))
	for _, r := range rows {
		if r["Product Type"] == "" {
			continue
		}
		out = append(out, colorRow{
			ProductType:   r["Product Type"],
			Primary:       r["Primary"],
			OnPrimary:     r["On Primary"],
			Secondary:     r["Secondary"],
			OnSecondary:   r["On Secondary"],
			Accent:        r["Accent"],
			OnAccent:      r["On Accent"],
			Background:    r["Background"],
			Foreground:    r["Foreground"],
			Card:          r["Card"],
			CardFg:        r["Card Foreground"],
			Muted:         r["Muted"],
			MutedFg:       r["Muted Foreground"],
			Border:        r["Border"],
			Destructive:   r["Destructive"],
			OnDestructive: r["On Destructive"],
			Ring:          r["Ring"],
			Notes:         r["Notes"],
		})
	}
	return out, nil
}

func loadTypography() ([]typeRow, error) {
	rows, err := readCSV("typography.csv")
	if err != nil {
		return nil, err
	}
	out := make([]typeRow, 0, len(rows))
	for _, r := range rows {
		if r["Font Pairing Name"] == "" {
			continue
		}
		out = append(out, typeRow{
			Name:    r["Font Pairing Name"],
			Heading: r["Heading Font"],
			Body:    r["Body Font"],
			Mood:    r["Mood/Style Keywords"],
			BestFor: r["Best For"],
			Notes:   r["Notes"],
		})
	}
	return out, nil
}

func loadLanding() ([]landingRow, error) {
	rows, err := readCSV("landing.csv")
	if err != nil {
		return nil, err
	}
	out := make([]landingRow, 0, len(rows))
	for _, r := range rows {
		if r["Pattern Name"] == "" {
			continue
		}
		out = append(out, landingRow{
			Name:          r["Pattern Name"],
			Keywords:      r["Keywords"],
			SectionOrder:  r["Section Order"],
			CTAPlacement:  r["Primary CTA Placement"],
			ColorStrategy: r["Color Strategy"],
			Conversion:    r["Conversion Optimization"],
		})
	}
	return out, nil
}

func loadUX() ([]uxRow, error) {
	rows, err := readCSV("ux-guidelines.csv")
	if err != nil {
		return nil, err
	}
	out := make([]uxRow, 0, len(rows))
	for _, r := range rows {
		if r["Issue"] == "" {
			continue
		}
		out = append(out, uxRow{
			Category: r["Category"],
			Issue:    r["Issue"],
			Do:       r["Do"],
			Dont:     r["Don't"],
			Severity: r["Severity"],
		})
	}
	return out, nil
}

func loadMotion() ([]motionRow, error) {
	rows, err := readCSV("motion.csv")
	if err != nil {
		return nil, err
	}
	out := make([]motionRow, 0, len(rows))
	for _, r := range rows {
		if r["Category"] == "" {
			continue
		}
		out = append(out, motionRow{
			Intensity: r["Intensity Tier"],
			Keywords:  r["Keywords"],
			Trigger:   r["Trigger"],
			Duration:  r["Duration"],
			Easing:    r["Easing"],
			Do:        r["Do"],
			Dont:      r["Don't"],
		})
	}
	return out, nil
}

func loadProducts() ([]productRow, error) {
	rows, err := readCSV("products.csv")
	if err != nil {
		return nil, err
	}
	out := make([]productRow, 0, len(rows))
	for _, r := range rows {
		if r["Product Type"] == "" {
			continue
		}
		out = append(out, productRow{
			ProductType:    r["Product Type"],
			Keywords:       r["Keywords"],
			PrimaryStyle:   r["Primary Style Recommendation"],
			LandingPattern: r["Landing Page Pattern"],
		})
	}
	return out, nil
}

func loadReasoning() ([]reasoningRow, error) {
	rows, err := readCSV("ui-reasoning.csv")
	if err != nil {
		return nil, err
	}
	out := make([]reasoningRow, 0, len(rows))
	for _, r := range rows {
		if r["UI_Category"] == "" {
			continue
		}
		out = append(out, reasoningRow{
			Category:      r["UI_Category"],
			Pattern:       r["Recommended_Pattern"],
			StylePriority: r["Style_Priority"],
			ColorMood:     r["Color_Mood"],
			TypeMood:      r["Typography_Mood"],
			KeyEffects:    r["Key_Effects"],
			AntiPatterns:  r["Anti_Patterns"],
		})
	}
	return out, nil
}
