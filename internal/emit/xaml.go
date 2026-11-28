// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// XAML writes WinUI 3 ResourceDictionary files, one per fan-out value when
// the xaml out path contains {<fanOut>}. Colours, and each shadow layer's
// colour, are SolidColorBrush resources in ThemeDictionaries: Light and
// Dark from the appearance values light and dark (the same values with no
// appearance axis), then HighContrast mapping every brush to the WinUI
// system colour options.highContrast gives its token, or, with no
// highContrast, Default repeating the base appearance. Every other token
// is a typed resource after them, sorted by key: x:Double, KeySpline or
// FontFamily. A token varying on a runtime axis other than appearance
// gets one resource per combination of those axes, its key suffixed with
// Pascal(value) per axis, plus the plain key at the base combination.
// Colours are sRGB #AARRGGBB, mapped into gamut by CSS Color 4 or, with
// gamut: clip-srgb, clipped.
func XAML(p *project.Project, pl config.Platform) (map[string][]byte, error) {
	x := &xamler{p: p, np: PlanNative(p.Config, pl), template: p.Config.NamingFor("xaml"), gamut: color.CSSColor4}
	if v, ok := pl.Option("gamut"); ok && v == "clip-srgb" {
		x.gamut = color.Clip
	}
	if err := x.themes(); err != nil {
		return nil, err
	}
	if strings.Contains(header(p), "--") {
		return nil, fmt.Errorf("the header %q holds \"--\", which an XML comment cannot", header(p))
	}

	tmpl := outPath(p, pl, "xaml")
	fan := ""
	if x.np.FanOut != nil {
		fan = "{" + x.np.FanOut.Name + "}"
	}
	perFile := fan != "" && strings.Contains(tmpl, fan)
	rest := tmpl
	if perFile {
		rest = strings.ReplaceAll(rest, fan, "")
	}
	if strings.ContainsAny(rest, "{}") {
		return nil, fmt.Errorf("xaml out path %s has a placeholder that is not the fan-out axis", tmpl)
	}

	items, err := x.plan(perFile)
	if err != nil {
		return nil, err
	}
	if err := x.checkHighContrast(); err != nil {
		return nil, err
	}

	outputs := x.np.Outputs()
	if !perFile {
		outputs = outputs[:1]
	}
	out := map[string][]byte{}
	for _, o := range outputs {
		f := tmpl
		if perFile {
			f = strings.ReplaceAll(f, fan, o.FanValue)
		}
		b, err := x.file(items, o)
		if err != nil {
			return nil, err
		}
		out[f] = b
	}
	return out, nil
}

type xamler struct {
	p        *project.Project
	np       NativePlan
	template string
	gamut    color.Gamut
	light    string // the appearance value of the Light theme, "" for the base
	dark     string // the appearance value of the Dark theme, "" for the base
}

// xamlTheme is one theme dictionary: its key and the appearance value it
// resolves colours at ("" for the base).
type xamlTheme struct{ key, appearance string }

// themes checks the appearance axis: WinUI's themes are Light and Dark,
// so its values must be light or dark. A theme the axis has no value for
// takes the base.
func (x *xamler) themes() error {
	a := x.np.Appearance
	if a == nil {
		return nil
	}
	for _, v := range a.Values {
		switch v {
		case "light":
			x.light = v
		case "dark":
			x.dark = v
		default:
			return fmt.Errorf("appearance value %q has no WinUI theme dictionary; XAML themes are Light and Dark", v)
		}
	}
	return nil
}

// themeList is the theme dictionaries in file order.
func (x *xamler) themeList() []xamlTheme {
	last := xamlTheme{key: "Default"}
	if len(x.np.Platform.HighContrast()) > 0 {
		last = xamlTheme{key: "HighContrast"}
	}
	return []xamlTheme{{"Light", x.light}, {"Dark", x.dark}, last}
}

// xamlItem is one resource: Part of token T in runtime context Ctx (the
// plain key's is empty, so every runtime axis takes its base). Layer is a
// shadow's layer (from 0), -1 otherwise; Part is "" for the token's value
// or a layer's brush, or OffsetX, OffsetY, Blur or Spread.
type xamlItem struct {
	Key   string
	T     *tokens.Token
	Ctx   config.Context
	Layer int
	Part  string
}

func (it xamlItem) brush() bool {
	return it.T.Type == "color" || (it.T.Type == "shadow" && it.Part == "")
}

// xamlKeyRE is a key WinUI resolves from markup: XAML has no escape for
// any other.
var xamlKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// xamlTypes are the token types XAML has a resource for.
var xamlTypes = []string{"color", "cubicBezier", "dimension", "duration", "fontFamily", "fontWeight", "number", "shadow"}

// shadowParts are a shadow layer's geometry resources.
var shadowParts = []string{"OffsetX", "OffsetY", "Blur", "Spread"}

// key is t's XAML resource key: its names.xaml or the naming template's.
func (x *xamler) key(t *tokens.Token) (string, error) {
	k := NameFor(x.p.Config, t, "xaml")
	if !xamlKeyRE.MatchString(k) {
		if _, own := t.Names["xaml"]; own {
			return "", fmt.Errorf("token %q: its xaml name %q is not a XAML key", t.Name, k)
		}
		return "", fmt.Errorf("token %q: xaml naming template %q gives %q, which is not a XAML key", t.Name, x.template, k)
	}
	return k, nil
}

// plan lists every resource of the dictionaries, the same in each output.
// It checks what XAML cannot express: a token varying per fan-out value
// in a file shared by every value, a non-colour varying on appearance, an
// inset shadow layer, a shadow whose layer count changes between outputs,
// and two resources of one key.
func (x *xamler) plan(perFile bool) ([]xamlItem, error) {
	var items []xamlItem
	owner := map[string]string{} // key → token
	add := func(it xamlItem) error {
		if prev, dup := owner[it.Key]; dup {
			if prev == it.T.Name {
				return fmt.Errorf("token %q makes XAML key %s twice", prev, it.Key)
			}
			a, b := prev, it.T.Name
			if b < a {
				a, b = b, a
			}
			return fmt.Errorf("tokens %q and %q both make XAML key %s", a, b, it.Key)
		}
		owner[it.Key] = it.T.Name
		items = append(items, it)
		return nil
	}
	np := x.np
	for _, t := range emitted(x.p, "xaml") {
		if !slices.Contains(xamlTypes, t.Type) {
			return nil, fmt.Errorf("token %q: a %s token has no XAML resource", t.Name, t.Type)
		}
		key, err := x.key(t)
		if err != nil {
			return nil, err
		}
		varies := np.Varies(x.p.Tokens, t)
		on := func(a *config.Axis) bool { return a != nil && slices.Contains(varies, a.Name) }
		if !perFile && on(np.FanOut) {
			return nil, fmt.Errorf("token %q varies per %s; put {%[2]s} in the xaml out path", t.Name, np.FanOut.Name)
		}
		// The axes the token's non-colour part switches on: for a shadow,
		// those its geometry changes with.
		geomOn := func(a config.Axis) (bool, error) {
			switch {
			case !on(&a) || t.Type == "color":
				return false, nil
			case t.Type == "shadow":
				return shadowGeometryVaries(x.p, np, t, varies, a)
			}
			return true, nil
		}
		if np.Appearance != nil {
			if v, err := geomOn(*np.Appearance); err != nil {
				return nil, err
			} else if v {
				return nil, fmt.Errorf("token %q varies on appearance; XAML theme dictionaries hold only brushes here", t.Name)
			}
		}
		var colourAxes, geomAxes []config.Axis
		for _, a := range np.Runtime {
			if on(&a) {
				colourAxes = append(colourAxes, a)
			}
			if v, err := geomOn(a); err != nil {
				return nil, err
			} else if v {
				geomAxes = append(geomAxes, a)
			}
		}
		// ctxs are the plain key's context then one per combination.
		ctxs := func(axes []config.Axis) []config.Context {
			out := []config.Context{{}}
			if len(axes) > 0 {
				out = append(out, combos(axes)...)
			}
			return out
		}
		suffix := func(axes []config.Axis, ctx config.Context) string {
			s := ""
			for _, a := range axes {
				if v, ok := ctx[a.Name]; ok {
					s += Pascal(v)
				}
			}
			return s
		}
		switch t.Type {
		case "color":
			for _, c := range ctxs(colourAxes) {
				if err := add(xamlItem{Key: key + suffix(colourAxes, c), T: t, Ctx: c, Layer: -1}); err != nil {
					return nil, err
				}
			}
		case "shadow":
			for _, c := range ctxs(colourAxes) {
				n, err := x.layers(t, c)
				if err != nil {
					return nil, err
				}
				for i := range n {
					if err := add(xamlItem{Key: fmt.Sprintf("%sLayer%d%s", key, i+1, suffix(colourAxes, c)), T: t, Ctx: c, Layer: i}); err != nil {
						return nil, err
					}
				}
			}
			for _, c := range ctxs(geomAxes) {
				n, err := x.layers(t, c)
				if err != nil {
					return nil, err
				}
				for i := range n {
					for _, part := range shadowParts {
						it := xamlItem{Key: fmt.Sprintf("%sLayer%d%s%s", key, i+1, part, suffix(geomAxes, c)), T: t, Ctx: c, Layer: i, Part: part}
						if err := add(it); err != nil {
							return nil, err
						}
					}
				}
			}
		default:
			for _, c := range ctxs(geomAxes) {
				if err := add(xamlItem{Key: key + suffix(geomAxes, c), T: t, Ctx: c, Layer: -1}); err != nil {
					return nil, err
				}
			}
		}
	}
	slices.SortFunc(items, func(a, b xamlItem) int { return strings.Compare(a.Key, b.Key) })
	return items, nil
}

// layers is shadow t's layer count in runtime context ctx, which must be
// the same in every output and appearance (every dictionary holds the
// same keys), with no inset layer.
func (x *xamler) layers(t *tokens.Token, ctx config.Context) (int, error) {
	n, at := -1, ""
	for _, o := range x.np.Outputs() {
		for _, th := range x.themeList() {
			full := x.full(ctx, o, th.appearance)
			v, err := x.np.Value(x.p.Tokens, t.Name, full)
			if err != nil {
				return 0, fmt.Errorf("token %q%s: %w", t.Name, inContext(full), err)
			}
			l, ok := v.Lit.([]tokens.Shadow)
			if !ok {
				return 0, fmt.Errorf("token %q%s: a shadow token resolves to %T", t.Name, inContext(full), v.Lit)
			}
			for i, layer := range l {
				if layer.Inset {
					return 0, fmt.Errorf("token %q: shadow layer %d is inset, and WinUI has no inset shadow", t.Name, i+1)
				}
			}
			switch {
			case n < 0:
				n, at = len(l), inContext(full)
			case len(l) != n:
				return 0, fmt.Errorf("token %q has %d shadow layers%s and %d%s; every XAML dictionary holds the same keys",
					t.Name, n, at, len(l), inContext(full))
			}
		}
	}
	return n, nil
}

// full is runtime context ctx in output o at appearance value av ("" for
// the base).
func (x *xamler) full(ctx config.Context, o Output, av string) config.Context {
	c := maps.Clone(ctx)
	if x.np.FanOut != nil {
		c[x.np.FanOut.Name] = o.FanValue
	}
	if av != "" {
		c[x.np.Appearance.Name] = av
	}
	return c
}

// checkHighContrast checks options.highContrast, when set, against the
// emitted colour and shadow tokens: it must map each of them and nothing
// else, or a high-contrast theme would miss keys at run time.
func (x *xamler) checkHighContrast() error {
	hc := x.np.Platform.HighContrast()
	if len(hc) == 0 {
		return nil
	}
	mapped := map[string]bool{}
	for _, m := range hc {
		t, ok := x.p.Tokens.Tokens[m[0]]
		if !ok || !isColorish(t) || !slices.Contains(emitted(x.p, "xaml"), t) {
			return fmt.Errorf("highContrast maps token %q, which is not a colour or shadow token emitted to xaml", m[0])
		}
		mapped[m[0]] = true
	}
	var missing []string
	for _, t := range emitted(x.p, "xaml") {
		if isColorish(t) && !mapped[t.Name] {
			missing = append(missing, fmt.Sprintf("%q", t.Name))
		}
	}
	if len(missing) > 0 {
		noun := "token"
		if len(missing) > 1 {
			noun = "tokens"
		}
		return fmt.Errorf("highContrast leaves out %s %s; map every colour and shadow token to a system colour, or remove highContrast for a Default theme",
			noun, strings.Join(missing, ", "))
	}
	return nil
}

// file is the dictionary of output o.
func (x *xamler) file(items []xamlItem, o Output) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<ResourceDictionary xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation" xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml">` + "\n")
	b.WriteString("    <!-- " + header(x.p) + " -->\n")
	var brushes, others []xamlItem
	for _, it := range items {
		if it.brush() {
			brushes = append(brushes, it)
		} else {
			others = append(others, it)
		}
	}
	system := map[string]string{}
	for _, m := range x.np.Platform.HighContrast() {
		system[m[0]] = m[1]
	}
	if len(brushes) > 0 {
		b.WriteString("    <ResourceDictionary.ThemeDictionaries>\n")
		for _, th := range x.themeList() {
			fmt.Fprintf(&b, "        <ResourceDictionary x:Key=\"%s\">\n", th.key)
			for _, it := range brushes {
				var c string
				if th.key == "HighContrast" {
					c = "{ThemeResource " + system[it.T.Name] + "}"
				} else {
					oc, err := x.colour(it, o, th.appearance)
					if err != nil {
						return nil, err
					}
					r, g, bl := oc.ToSRGB(x.gamut)
					c = fmt.Sprintf("#%02X%02X%02X%02X", byte8(oc.Alpha), byte8(r), byte8(g), byte8(bl))
				}
				fmt.Fprintf(&b, "            <SolidColorBrush x:Key=\"%s\" Color=\"%s\" />\n", it.Key, xmlEscape(c))
			}
			b.WriteString("        </ResourceDictionary>\n")
		}
		b.WriteString("    </ResourceDictionary.ThemeDictionaries>\n")
	}
	for _, it := range others {
		el, err := x.resource(it, o)
		if err != nil {
			return nil, err
		}
		b.WriteString("    " + el + "\n")
	}
	b.WriteString("</ResourceDictionary>\n")
	return b.Bytes(), nil
}

// colour resolves brush it in output o at appearance value av.
func (x *xamler) colour(it xamlItem, o Output, av string) (color.OKLCH, error) {
	full := x.full(it.Ctx, o, av)
	v, err := x.np.Value(x.p.Tokens, it.T.Name, full)
	if err != nil {
		return color.OKLCH{}, fmt.Errorf("token %q%s: %w", it.T.Name, inContext(full), err)
	}
	switch l := v.Lit.(type) {
	case color.OKLCH:
		if it.Layer < 0 {
			return l, nil
		}
	case []tokens.Shadow:
		if it.Layer >= 0 && it.Layer < len(l) {
			return l[it.Layer].Color, nil
		}
	}
	return color.OKLCH{}, fmt.Errorf("token %q%s: a %s token resolves to %T", it.T.Name, inContext(full), it.T.Type, v.Lit)
}

// resource is non-colour resource it in output o as one XAML element.
// Non-colours do not vary on appearance (plan checks), so they resolve at
// its base.
func (x *xamler) resource(it xamlItem, o Output) (string, error) {
	t := it.T
	full := x.full(it.Ctx, o, "")
	v, err := x.np.Value(x.p.Tokens, t.Name, full)
	if err != nil {
		return "", fmt.Errorf("token %q%s: %w", t.Name, inContext(full), err)
	}
	bad := func() (string, error) {
		return "", fmt.Errorf("token %q%s: a %s token resolves to %T", t.Name, inContext(full), t.Type, v.Lit)
	}
	double := func(s string) string {
		return fmt.Sprintf("<x:Double x:Key=\"%s\">%s</x:Double>", it.Key, s)
	}
	switch t.Type {
	case "dimension":
		d, ok := v.Lit.(tokens.Dim)
		if !ok {
			return bad()
		}
		s, err := xamlEpx(d)
		if err != nil {
			return "", fmt.Errorf("token %q: %w", t.Name, err)
		}
		return double(s), nil
	case "number":
		f, ok := v.Lit.(float64)
		if !ok {
			return bad()
		}
		return double(num(f)), nil
	case "duration":
		d, ok := v.Lit.(tokens.Duration)
		if !ok {
			return bad()
		}
		switch d.Unit {
		case "ms":
			return double(num(d.V)), nil
		case "s":
			return double(num(d.V * 1000)), nil
		}
		return "", fmt.Errorf("token %q: duration unit %q has no XAML spelling", t.Name, d.Unit)
	case "cubicBezier":
		c, ok := v.Lit.([4]float64)
		if !ok {
			return bad()
		}
		return fmt.Sprintf("<KeySpline x:Key=\"%s\" ControlPoint1=\"%s,%s\" ControlPoint2=\"%s,%s\" />",
			it.Key, num(c[0]), num(c[1]), num(c[2]), num(c[3])), nil
	case "fontFamily":
		fam, ok := v.Lit.([]string)
		if !ok {
			return bad()
		}
		for _, f := range fam {
			if strings.Contains(f, ",") {
				return "", fmt.Errorf("token %q: font family %q holds a comma, which a XAML FontFamily reads as a fallback list", t.Name, f)
			}
		}
		return fmt.Sprintf("<FontFamily x:Key=\"%s\">%s</FontFamily>", it.Key, xmlEscape(strings.Join(fam, ", "))), nil
	case "fontWeight":
		w, ok := v.Lit.(float64)
		if !ok {
			return bad()
		}
		r := math.Floor(w + 0.5)
		if r < 1 || r > 999 {
			return "", fmt.Errorf("token %q%s: font weight %s is outside 1..999, the weights WinUI's FontWeight takes", t.Name, inContext(full), num(w))
		}
		return double(num(r)), nil
	case "shadow":
		l, ok := v.Lit.([]tokens.Shadow)
		if !ok || it.Layer < 0 || it.Layer >= len(l) {
			return bad()
		}
		layer := l[it.Layer]
		d := map[string]tokens.Dim{"OffsetX": layer.OffsetX, "OffsetY": layer.OffsetY, "Blur": layer.Blur, "Spread": layer.Spread}[it.Part]
		s, err := xamlEpx(d)
		if err != nil {
			return "", fmt.Errorf("token %q: %w", t.Name, err)
		}
		return double(s), nil
	}
	return "", fmt.Errorf("token %q: a %s token has no XAML resource", t.Name, t.Type)
}

// xamlEpx is a dimension in effective pixels: px as is, rem × 16.
func xamlEpx(d tokens.Dim) (string, error) {
	switch d.Unit {
	case "px":
		return num(d.V), nil
	case "rem":
		return num(d.V * 16), nil
	}
	return "", fmt.Errorf("dimension unit %q has no XAML spelling", d.Unit)
}

// xmlEscape escapes s for XML text or a double-quoted attribute.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
