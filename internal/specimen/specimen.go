// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package specimen renders a project's tokens as SVG sheets: colour
// swatches, type samples and spacing bars. The sheets are byte-stable: the
// geometry is fixed, every string is escaped, and nothing carries a
// timestamp. No font is embedded; text uses system font lists.
package specimen

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// Layout, in px. Rows are fixed; columns are sized from what they hold
// (see colWidth), between a floor and, for names, a cap.
const (
	margin     = 24
	titleH     = 56
	headH      = 28 // a column header row
	groupH     = 40
	nameMin    = 264 // the name column's floor
	nameCap    = 480 // and its cap: a longer name is cut with "…"
	swatchMin  = 152 // a swatch column's floor
	swatchW    = 128
	swatchH    = 40
	colorRowH  = 92
	spacingRow = 40
	barMax     = 480 // the longest a spacing bar is drawn
	barH       = 16
	valueMin   = 200 // the spacing value column's floor
	typeSampW  = 640 // the type sample column's floor
	typeMinRow = 56
	typeMaxPx  = 72 // a sample is never set larger than this
	sample     = "The quick brown fox jumps over the lazy dog"
	rootSize   = 16 // px in a rem
	colPad     = 24 // the gap a column keeps after its widest text
	nameSize   = 13 // a token name's font size
)

// Text is measured, with no font to hand, by an average advance per
// character: a monospace face's is 0.6em; sansEm is a generous average for
// the system sans faces, so a sample is never wider than measured.
const (
	monoEm = 0.6
	sansEm = 0.55
)

// textW is the estimated width of s set at size px with em per character.
func textW(s string, size, em float64) float64 {
	return em * size * float64(utf8.RuneCountInString(s))
}

// colWidth is the column width that fits each text (its width plus
// colPad), no less than floor.
func colWidth(floor int, widths ...float64) int {
	w := float64(floor)
	for _, x := range widths {
		w = math.Max(w, x+colPad)
	}
	return int(math.Ceil(w))
}

// nameLabel is a token's name as the sheets print it.
func nameLabel(t *tokens.Token) string {
	if t.Retired {
		return t.Name + " (retired)"
	}
	return t.Name
}

// nameCol is the width of a name column holding toks' names and any
// further mono labels at size 11, capped at nameCap.
func nameCol(toks []*tokens.Token, labels ...string) int {
	var ws []float64
	for _, t := range toks {
		ws = append(ws, textW(nameLabel(t), nameSize, monoEm))
	}
	for _, l := range labels {
		ws = append(ws, textW(l, 11, monoEm))
	}
	return min(nameCap, colWidth(nameMin, ws...))
}

const (
	bg      = "#ffffff"
	ink     = "#1a1a1a"
	muted   = "#6b6b6b"
	rule    = "#d9d9d9"
	sansFam = "system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif"
	monoFam = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
)

// context is one sheet set: the fan-out axis value it renders (Axis "" for
// the single set of a project with no fan-out) and the plan that resolves
// values for it.
type context struct {
	axis, value string
	plan        emit.NativePlan
}

func (c context) suffix() string {
	if c.value == "" {
		return ""
	}
	return "-" + c.value
}

// Render returns every sheet, keyed by file name (colors[-v].svg,
// type[-v].svg, spacing[-v].svg).
func Render(p *project.Project) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, c := range contexts(p) {
		title := p.Config.Content
		if c.value != "" {
			title += " · " + c.axis + "=" + c.value
		}
		out["colors"+c.suffix()+".svg"] = colors(p, c, title)
		out["type"+c.suffix()+".svg"] = typeSheet(p, c, title)
		out["spacing"+c.suffix()+".svg"] = spacing(p, c, title)
	}
	return out, nil
}

// contexts lists the sheet sets: one per fan-out value of every axis a
// fanning-out platform uses, or one when no platform fans out. A value two
// axes share is named axis-value so the files do not collide.
func contexts(p *project.Project) []context {
	var cs []context
	for _, pl := range p.Config.Platforms {
		if pl.FanOut == "" {
			continue
		}
		np := emit.PlanNative(p.Config, pl)
		if np.FanOut == nil {
			continue
		}
		for _, o := range np.Outputs() {
			if !slices.ContainsFunc(cs, func(c context) bool { return c.axis == np.FanOut.Name && c.value == o.FanValue }) {
				cs = append(cs, context{axis: np.FanOut.Name, value: o.FanValue, plan: np})
			}
		}
	}
	if len(cs) == 0 {
		return []context{{plan: emit.PlanNative(p.Config, config.Platform{})}}
	}
	seen := map[string]int{}
	for _, c := range cs {
		seen[c.value]++
	}
	for i, c := range cs {
		if seen[c.value] > 1 {
			cs[i].value = c.axis + "-" + c.value
		}
	}
	return cs
}

// ctxFor is the resolution context for one appearance value ("" for none).
func (c context) ctxFor(appearance string) config.Context {
	ctx := config.Context{}
	if c.plan.FanOut != nil {
		ctx[c.plan.FanOut.Name] = strings.TrimPrefix(c.value, c.axis+"-")
	}
	if c.plan.Appearance != nil && appearance != "" {
		ctx[c.plan.Appearance.Name] = appearance
	}
	return ctx
}

// appearances is the appearance values in axis order, or [""] when the
// project has no appearance axis.
func (c context) appearances() []string {
	if c.plan.Appearance == nil || len(c.plan.Appearance.Values) == 0 {
		return []string{""}
	}
	return c.plan.Appearance.Values
}

// tiers are the colour groups in sheet order; "" collects untiered tokens.
var tiers = []string{"semantic", "component", "primitive", ""}

func tierTitle(t string) string {
	if t == "" {
		return "untiered"
	}
	return t
}

// doc is an SVG under construction.
type doc struct{ b strings.Builder }

func (d *doc) f(format string, a ...any) { fmt.Fprintf(&d.b, format, a...) }

func (d *doc) open(w, h int, title string) {
	d.f("<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%d\" height=\"%d\" viewBox=\"0 0 %d %d\">\n", w, h, w, h)
	d.f("<title>%s</title>\n", esc(title))
	d.f("<rect width=\"%d\" height=\"%d\" fill=\"%s\"/>\n", w, h, bg)
}

func (d *doc) text(x, y int, size int, fill, fam, weight, s string) {
	d.f("<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"%d\" font-weight=\"%s\" fill=\"%s\">%s</text>\n",
		x, y, esc(fam), size, weight, fill, esc(s))
}

// name writes a token's name in a column colW wide, with its description
// as a tooltip. A name too long for the column is cut with "…", and the
// tooltip then carries the whole name.
func (d *doc) name(x, y, colW int, t *tokens.Token) {
	label, tip := nameLabel(t), t.Description
	if textW(label, nameSize, monoEm) > float64(colW-colPad) {
		full := label
		r := []rune(label)
		n := int(float64(colW-colPad)/(monoEm*nameSize)) - 1
		label = string(r[:max(1, n)]) + "…"
		tip = full
		if t.Description != "" {
			tip += ": " + t.Description
		}
	}
	d.f("<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"%d\" font-weight=\"400\" fill=\"%s\">", x, y, esc(monoFam), nameSize, ink)
	if tip != "" {
		d.f("<title>%s</title>", esc(tip))
	}
	d.f("%s</text>\n", esc(label))
}

func (d *doc) close() []byte {
	d.b.WriteString("</svg>\n")
	return []byte(d.b.String())
}

// esc escapes s for XML text and attribute values, dropping characters XML
// cannot carry.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '&':
			b.WriteString("&amp;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\'':
			b.WriteString("&apos;")
		case r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r != 0xFFFE && r != 0xFFFF && !(r >= 0xD800 && r <= 0xDFFF):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// num formats v to at most three decimals, no exponent, no "-0".
func num(v float64) string {
	v = math.Round(v*1000) / 1000
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// hex is c's sRGB value, gamut-mapped by CSS Color 4, as #rrggbb.
func hex(c color.OKLCH) string {
	r, g, b := c.ToSRGB(color.CSSColor4)
	ch := func(v float64) int { return int(math.Round(math.Min(1, math.Max(0, v)) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", ch(r), ch(g), ch(b))
}

func oklchText(c color.OKLCH) string {
	s := fmt.Sprintf("oklch(%s %s %s", num(c.L), num(c.C), num(c.H))
	if c.Alpha != 1 {
		s += " / " + num(c.Alpha)
	}
	return s + ")"
}

// ordered lists the tokens of the given types in Set order.
func ordered(p *project.Project, keep func(*tokens.Token) bool) []*tokens.Token {
	var out []*tokens.Token
	for _, n := range p.Tokens.Order {
		if t := p.Tokens.Tokens[n]; t != nil && keep(t) {
			out = append(out, t)
		}
	}
	return out
}

func colors(p *project.Project, c context, title string) []byte {
	apps := c.appearances()
	all := ordered(p, func(t *tokens.Token) bool { return t.Type == "color" })
	// Resolve every swatch first: its labels size the columns.
	type swatch struct {
		lit color.OKLCH
		ok  bool
	}
	cells := map[*tokens.Token][]swatch{}
	var ws []float64
	for _, a := range apps {
		ws = append(ws, swatchW, textW(a, 12, sansEm))
	}
	for _, t := range all {
		for _, a := range apps {
			v, err := c.plan.Value(p.Tokens, t.Name, c.ctxFor(a))
			lit, ok := valueColor(v, err)
			cells[t] = append(cells[t], swatch{lit, ok})
			if ok {
				ws = append(ws, textW(hex(lit), 12, monoEm), textW(oklchText(lit), 11, monoEm))
			}
		}
	}
	nameW, colW := nameCol(all), colWidth(swatchMin, ws...)
	w := margin*2 + nameW + colW*len(apps)
	h := margin + titleH + headH
	groups := map[string][]*tokens.Token{}
	for _, t := range all {
		tier := t.Tier
		if !slices.Contains(tiers, tier) {
			tier = ""
		}
		groups[tier] = append(groups[tier], t)
	}
	for _, tier := range tiers {
		if len(groups[tier]) > 0 {
			h += groupH + colorRowH*len(groups[tier])
		}
	}
	h += margin
	var d doc
	d.open(w, h, title+" · colors")
	y := margin + 28
	d.text(margin, y, 20, ink, sansFam, "700", title+" · colors")
	y = margin + titleH + 16
	for i, a := range apps {
		if a != "" {
			d.text(margin+nameW+i*colW, y, 12, muted, sansFam, "600", a)
		}
	}
	y = margin + titleH + headH
	for _, tier := range tiers {
		g := groups[tier]
		if len(g) == 0 {
			continue
		}
		d.text(margin, y+26, 14, ink, sansFam, "700", tierTitle(tier))
		d.f("<line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"%s\"/>\n", margin, y+32, w-margin, y+32, rule)
		y += groupH
		for _, t := range g {
			d.name(margin, y+24, nameW, t)
			for i, sw := range cells[t] {
				x := margin + nameW + i*colW
				if !sw.ok {
					d.text(x, y+24, 12, muted, sansFam, "400", "unresolved")
					continue
				}
				d.f("<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"4\" fill=\"%s\" fill-opacity=\"%s\" stroke=\"%s\"/>\n",
					x, y+8, swatchW, swatchH, hex(sw.lit), num(sw.lit.Alpha), rule)
				d.text(x, y+swatchH+26, 12, ink, monoFam, "400", hex(sw.lit))
				d.text(x, y+swatchH+42, 11, muted, monoFam, "400", oklchText(sw.lit))
			}
			y += colorRowH
		}
	}
	return d.close()
}

func valueColor(v *tokens.Value, err error) (color.OKLCH, bool) {
	if err != nil || v == nil {
		return color.OKLCH{}, false
	}
	c, ok := v.Lit.(color.OKLCH)
	return c, ok
}

// px is a dimension in px (rem is 16px).
func px(d tokens.Dim) float64 {
	if d.Unit == "rem" {
		return d.V * rootSize
	}
	return d.V
}

func dimText(d tokens.Dim) string {
	if d.Unit == "rem" {
		return num(d.V) + "rem = " + num(px(d)) + "px"
	}
	return num(d.V) + "px"
}

func isTypeDim(t *tokens.Token) bool {
	return t.Type == "dimension" && (strings.Contains(t.Name, "font-size") || strings.Contains(t.Name, "line-height"))
}

func typeSheet(p *project.Project, c context, title string) []byte {
	toks := ordered(p, func(t *tokens.Token) bool {
		return t.Type == "fontFamily" || t.Type == "fontWeight" || isTypeDim(t)
	})
	type row struct {
		tok    *tokens.Token
		label  string
		family string
		weight string
		size   float64
		lineH  float64
		h      int
	}
	rows := make([]row, 0, len(toks))
	h := margin + titleH
	for _, t := range toks {
		r := row{tok: t, family: sansFam, weight: "400", size: rootSize, h: typeMinRow}
		v, err := c.plan.Value(p.Tokens, t.Name, c.ctxFor(""))
		if err != nil || v == nil {
			r.label = "unresolved"
			rows = append(rows, r)
			h += r.h
			continue
		}
		switch lit := v.Lit.(type) {
		case []string:
			r.label = strings.Join(lit, ", ")
			r.family = fontList(lit)
		case float64:
			r.label = num(lit)
			r.weight = strconv.Itoa(int(math.Round(math.Min(1000, math.Max(1, lit)))))
		case tokens.Dim:
			r.label = dimText(lit)
			if strings.Contains(t.Name, "font-size") {
				r.size = math.Min(typeMaxPx, math.Max(1, px(lit)))
			} else {
				r.lineH = math.Max(0, px(lit))
				r.size = rootSize
			}
		default:
			r.label = "unresolved"
		}
		lh := r.lineH
		if lh == 0 {
			lh = math.Round(r.size * 1.25)
		}
		r.h = max(typeMinRow, int(math.Ceil(lh))+24)
		rows = append(rows, r)
		h += r.h
	}
	h += margin
	var labels []string
	largest := 0.0
	for _, r := range rows {
		labels = append(labels, r.label)
		largest = math.Max(largest, r.size)
	}
	nameW := nameCol(toks, labels...)
	// The sample column fits the largest sample, which typeMaxPx caps.
	w := margin*2 + nameW + colWidth(typeSampW, textW(sample, largest, sansEm))
	var d doc
	d.open(w, h, title+" · type")
	d.text(margin, margin+28, 20, ink, sansFam, "700", title+" · type")
	y := margin + titleH
	for _, r := range rows {
		d.f("<line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"%s\"/>\n", margin, y, w-margin, y, rule)
		d.name(margin, y+22, nameW, r.tok)
		d.text(margin, y+40, 11, muted, monoFam, "400", r.label)
		lh := r.lineH
		if lh == 0 {
			lh = math.Round(r.size * 1.25)
		}
		base := y + 12 + int(math.Round(lh*0.8))
		d.f("<text x=\"%d\" y=\"%d\" font-family=\"%s\" font-size=\"%s\" font-weight=\"%s\" fill=\"%s\">%s</text>\n",
			margin+nameW, base, esc(r.family), num(r.size), r.weight, ink, esc(sample))
		y += r.h
	}
	return d.close()
}

// generic are the CSS generic family keywords, which must stay unquoted.
var generic = map[string]bool{
	"serif": true, "sans-serif": true, "monospace": true, "cursive": true, "fantasy": true,
	"system-ui": true, "ui-serif": true, "ui-sans-serif": true, "ui-monospace": true, "ui-rounded": true,
	"emoji": true, "math": true, "fangsong": true, "-apple-system": true,
}

// fontList is a CSS font-family list: the token's families, quoted, then
// the generic sans-serif, so a family the viewer lacks falls back.
func fontList(fams []string) string {
	parts := make([]string, 0, len(fams)+1)
	for _, f := range fams {
		if generic[f] {
			parts = append(parts, f)
			continue
		}
		parts = append(parts, "'"+strings.NewReplacer("'", "", "\\", "").Replace(f)+"'")
	}
	parts = append(parts, "sans-serif")
	return strings.Join(parts, ", ")
}

func spacing(p *project.Project, c context, title string) []byte {
	toks := ordered(p, func(t *tokens.Token) bool { return t.Type == "dimension" && !isTypeDim(t) })
	dims := make([]*tokens.Dim, len(toks))
	var vw []float64
	for i, t := range toks {
		v, err := c.plan.Value(p.Tokens, t.Name, c.ctxFor(""))
		if err == nil && v != nil {
			if dim, ok := v.Lit.(tokens.Dim); ok {
				dims[i] = &dim
				vw = append(vw, textW(dimText(dim), 12, monoEm))
			}
		}
	}
	nameW := nameCol(toks)
	h := margin + titleH + spacingRow*len(toks) + margin
	w := margin*2 + nameW + barMax + 16 + colWidth(valueMin, vw...)
	var d doc
	d.open(w, h, title+" · spacing")
	d.text(margin, margin+28, 20, ink, sansFam, "700", title+" · spacing")
	y := margin + titleH
	for i, t := range toks {
		d.name(margin, y+25, nameW, t)
		dim := dims[i]
		if dim == nil {
			d.text(margin+nameW, y+25, 12, muted, sansFam, "400", "unresolved")
			y += spacingRow
			continue
		}
		bar := math.Min(barMax, math.Max(0, px(*dim)))
		d.f("<rect x=\"%d\" y=\"%d\" width=\"%s\" height=\"%d\" fill=\"%s\"/>\n", margin+nameW, y+(spacingRow-barH)/2, num(bar), barH, "#3b6fd4")
		d.text(margin+nameW+barMax+16, y+25, 12, muted, monoFam, "400", dimText(*dim))
		y += spacingRow
	}
	return d.close()
}
