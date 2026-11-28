// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// Colorsets writes Xcode asset-catalog colorsets: one catalog folder per
// output set (the out path's {<fanOut>} replaced by each fan-out value),
// whose Contents.json provides a namespace, so a colour is referenced as
// "<folder>/<Name>". Every emitted colour token gets a colorset, and every
// layer of an emitted shadow token one named <Name>Layer<N>. A colour that
// varies on a runtime axis other than appearance gets one colorset per
// combination of those axes' values, named by ColorsetName. Each colorset
// has a universal entry for the appearance axis's base value and, when the
// colour varies on appearance, a luminosity-dark entry for its other
// value. Colours are Display P3, mapped into gamut by CSS Color 4 or, with
// option gamut: clip-p3, clipped. Option accentColor also writes
// AccentColor.colorset at the catalog root, the out folder's parent.
func Colorsets(p *project.Project, pl config.Platform) (map[string][]byte, error) {
	np := PlanNative(p.Config, pl)
	cs := colorsetter{p: p, np: np, gamut: color.CSSColor4}
	if g, _ := pl.Option("gamut"); g == "clip-p3" {
		cs.gamut = color.Clip
	}
	if err := cs.appearance(); err != nil {
		return nil, err
	}
	cs.plan()
	tmpl := strings.TrimSuffix(outPath(p, pl, "colorsets"), "/")
	out := map[string][]byte{}
	by := map[string]string{} // path → the fan-out value that wrote it
	for _, o := range np.Outputs() {
		dir := tmpl
		if np.FanOut != nil {
			dir = strings.ReplaceAll(dir, "{"+np.FanOut.Name+"}", o.FanValue)
		}
		if strings.ContainsAny(dir, "{}") {
			return nil, fmt.Errorf("colorsets out path %s has a placeholder that is not the fan-out axis", tmpl)
		}
		files, err := cs.catalog(dir, o)
		if err != nil {
			return nil, err
		}
		for _, f := range slices.Sorted(maps.Keys(files)) {
			if prev, ok := out[f]; ok && !bytes.Equal(prev, files[f]) {
				return nil, fmt.Errorf("%s differs between %s %s and %s; put {%s} in the colorsets out path",
					f, np.FanOut.Name, by[f], o.FanValue, np.FanOut.Name)
			}
			out[f], by[f] = files[f], o.FanValue
		}
	}
	return out, nil
}

// ColorsetName is the colorset colour token t uses in ctx, namespace
// included ("Tokens/Surface-Compact"): t's name in Pascal case, then
// "-<Value>" for each runtime axis, in axis order, that ctx sets. ctx is
// to set exactly the runtime axes t varies on (np.Varies), the way
// Colorsets names them; the fan-out, appearance and fixed axes never
// name a colorset and are ignored.
func ColorsetName(np NativePlan, t *tokens.Token, ctx config.Context) string {
	return namespace(np) + "/" + colorsetName(np, Pascal(t.Name), ctx)
}

func colorsetName(np NativePlan, base string, ctx config.Context) string {
	var b strings.Builder
	b.WriteString(base)
	for _, a := range np.Runtime {
		if v, ok := ctx[a.Name]; ok {
			b.WriteString("-" + Pascal(v))
		}
	}
	return b.String()
}

// namespace is the catalog folder's name, the last element of the out
// path.
func namespace(np NativePlan) string {
	o := np.Platform.Out["colorsets"]
	if o == "" {
		o = defaultOut["colorsets"]
	}
	return path.Base(strings.TrimSuffix(o, "/"))
}

type colorsetter struct {
	p     *project.Project
	np    NativePlan
	gamut color.Gamut
	dark  string // the appearance axis's non-base value; "" with no such axis

	entries []colorEntry
}

// colorEntry is one emitted colour or shadow token and the runtime
// contexts it has a colorset for.
type colorEntry struct {
	t            *tokens.Token
	ctxs         []config.Context // one per combination of the runtime axes it varies on
	onAppearance bool
}

// appearance finds the dark value: the appearance axis may have its base
// and one other value, the two a colorset can express.
func (cs *colorsetter) appearance() error {
	a := cs.np.Appearance
	if a == nil {
		return nil
	}
	var others []string
	for _, v := range a.Values {
		if v != base(*a) {
			others = append(others, v)
		}
	}
	if len(others) == 0 {
		return nil
	}
	dark := others[0]
	if slices.Contains(others, "dark") {
		dark = "dark"
	}
	if len(others) > 1 {
		extra := slices.DeleteFunc(slices.Clone(others), func(v string) bool { return v == dark })
		msg := fmt.Sprintf("appearance value %s cannot be expressed in a colorset, which has a base (%s) and a dark (%s) appearance only",
			strings.Join(extra, ", "), base(*a), dark)
		for _, t := range emitted(cs.p, "colorsets") {
			if isColorish(t) && slices.Contains(cs.np.Varies(cs.p.Tokens, t), a.Name) {
				msg += fmt.Sprintf("; token %q varies on appearance", t.Name)
				break
			}
		}
		return fmt.Errorf("%s", msg)
	}
	cs.dark = dark
	return nil
}

// onAppearance reports whether a colour varying on the axes listed needs
// a dark entry: it varies on an appearance axis that has a dark value.
func (cs *colorsetter) onAppearance(varies []string) bool {
	return cs.dark != "" && slices.Contains(varies, cs.np.Appearance.Name)
}

func isColorish(t *tokens.Token) bool { return t.Type == "color" || t.Type == "shadow" }

// plan lists the colorset entries: every emitted colour and shadow token
// with the runtime contexts it varies on.
func (cs *colorsetter) plan() {
	for _, t := range emitted(cs.p, "colorsets") {
		if !isColorish(t) {
			continue
		}
		varies := cs.np.Varies(cs.p.Tokens, t)
		e := colorEntry{t: t, ctxs: []config.Context{{}}}
		e.onAppearance = cs.onAppearance(varies)
		for _, a := range cs.np.Runtime {
			if !slices.Contains(varies, a.Name) {
				continue
			}
			var next []config.Context
			for _, c := range e.ctxs {
				for _, v := range a.Values {
					n := maps.Clone(c)
					n[a.Name] = v
					next = append(next, n)
				}
			}
			e.ctxs = next
		}
		cs.entries = append(cs.entries, e)
	}
}

// catalog is one output set's files under dir.
func (cs *colorsetter) catalog(dir string, o Output) (map[string][]byte, error) {
	files := map[string][]byte{}
	folder := &object{}
	folder.set("info", info())
	props := &object{}
	props.set("provides-namespace", true)
	folder.set("properties", props)
	b, err := encodeJSON(folder)
	if err != nil {
		return nil, err
	}
	files[dir+"/Contents.json"] = b

	// No two colorset folders may share a name, ignoring case: a
	// case-insensitive disk would merge them.
	owner := map[string]string{} // lower-cased colorset name → token
	put := func(name string, t *tokens.Token, b []byte) error {
		k := strings.ToLower(name)
		if prev, ok := owner[k]; ok {
			return fmt.Errorf("tokens %q and %q both make colorset %s", prev, t.Name, name)
		}
		owner[k] = t.Name
		files[dir+"/"+name+".colorset/Contents.json"] = b
		return nil
	}
	for _, e := range cs.entries {
		for _, rc := range e.ctxs {
			light, dark, err := cs.resolve(e, rc, o)
			if err != nil {
				return nil, err
			}
			if e.t.Type == "color" {
				b, err := cs.colorset(light[0], dark)
				if err != nil {
					return nil, err
				}
				if err := put(colorsetName(cs.np, Pascal(e.t.Name), rc), e.t, b); err != nil {
					return nil, err
				}
				continue
			}
			for i := range light {
				var d []color.OKLCH
				if dark != nil {
					d = dark[i : i+1]
				}
				b, err := cs.colorset(light[i], d)
				if err != nil {
					return nil, err
				}
				name := colorsetName(cs.np, fmt.Sprintf("%sLayer%d", Pascal(e.t.Name), i+1), rc)
				if err := put(name, e.t, b); err != nil {
					return nil, err
				}
			}
		}
	}

	if name, ok := cs.np.Platform.Option("accentColor"); ok {
		b, err := cs.accent(name, o)
		if err != nil {
			return nil, err
		}
		files[path.Dir(dir)+"/AccentColor.colorset/Contents.json"] = b
	}
	return files, nil
}

// resolve gives e's colours in runtime context rc of output o: one per
// colour (a shadow has one per layer) at the base appearance, and at the
// dark one when e varies on appearance (nil otherwise).
func (cs *colorsetter) resolve(e colorEntry, rc config.Context, o Output) (light, dark []color.OKLCH, err error) {
	at := func(appearance string) ([]color.OKLCH, error) {
		ctx := maps.Clone(rc)
		if cs.np.FanOut != nil {
			ctx[cs.np.FanOut.Name] = o.FanValue
		}
		if appearance != "" {
			ctx[cs.np.Appearance.Name] = appearance
		}
		v, err := cs.np.Value(cs.p.Tokens, e.t.Name, ctx)
		if err != nil {
			return nil, fmt.Errorf("token %q%s: %w", e.t.Name, inContext(ctx), err)
		}
		switch l := v.Lit.(type) {
		case color.OKLCH:
			if e.t.Type == "color" {
				return []color.OKLCH{l}, nil
			}
		case []tokens.Shadow:
			if e.t.Type == "shadow" {
				out := make([]color.OKLCH, len(l))
				for i, s := range l {
					out[i] = s.Color
				}
				return out, nil
			}
		}
		return nil, fmt.Errorf("token %q%s: a %s token resolves to %T", e.t.Name, inContext(ctx), e.t.Type, v.Lit)
	}
	if light, err = at(""); err != nil {
		return nil, nil, err
	}
	if !e.onAppearance {
		return light, nil, nil
	}
	if dark, err = at(cs.dark); err != nil {
		return nil, nil, err
	}
	if len(dark) != len(light) {
		return nil, nil, fmt.Errorf("token %q has %d shadow layers at appearance %s and %d at %s; a colorset per layer needs the same count",
			e.t.Name, len(light), base(*cs.np.Appearance), len(dark), cs.dark)
	}
	return light, dark, nil
}

// accent is output o's AccentColor colorset, from the named colour token.
func (cs *colorsetter) accent(name string, o Output) ([]byte, error) {
	t := cs.p.Tokens.Tokens[name]
	switch {
	case t == nil:
		return nil, fmt.Errorf("accentColor names token %q, which is not defined", name)
	case t.Type != "color":
		return nil, fmt.Errorf("accentColor names token %q, which is not a colour (it is a %s token)", name, t.Type)
	}
	varies := cs.np.Varies(cs.p.Tokens, t)
	for _, a := range cs.np.Runtime {
		if slices.Contains(varies, a.Name) {
			return nil, fmt.Errorf("accentColor names token %q, which varies on %s; AccentColor can vary only on appearance and %s",
				name, a.Name, fanName(cs.np))
		}
	}
	e := colorEntry{t: t, onAppearance: cs.onAppearance(varies)}
	light, dark, err := cs.resolve(e, config.Context{}, o)
	if err != nil {
		return nil, err
	}
	return cs.colorset(light[0], dark)
}

func fanName(np NativePlan) string {
	if np.FanOut == nil {
		return "no other axis"
	}
	return "the fan-out axis " + np.FanOut.Name
}

// colorset is a colorset's Contents.json: a universal entry for light and,
// when dark holds a colour, a luminosity-dark entry for it.
func (cs *colorsetter) colorset(light color.OKLCH, dark []color.OKLCH) ([]byte, error) {
	colors := []any{cs.entry(light, false)}
	if len(dark) > 0 {
		colors = append(colors, cs.entry(dark[0], true))
	}
	doc := &object{}
	doc.set("colors", colors)
	doc.set("info", info())
	return encodeJSON(doc)
}

func (cs *colorsetter) entry(c color.OKLCH, dark bool) *object {
	r, g, b := c.ToP3(cs.gamut)
	comps := &object{}
	comps.set("alpha", component(c.Alpha))
	comps.set("blue", component(b))
	comps.set("green", component(g))
	comps.set("red", component(r))
	col := &object{}
	col.set("color-space", "display-p3")
	col.set("components", comps)
	e := &object{}
	if dark {
		ap := &object{}
		ap.set("appearance", "luminosity")
		ap.set("value", "dark")
		e.set("appearances", []any{ap})
	}
	e.set("color", col)
	e.set("idiom", "universal")
	return e
}

// component is a colour channel as a 4-decimal string, never "-0.0000".
func component(v float64) string {
	s := fmt.Sprintf("%.4f", v)
	if s == "-0.0000" {
		return "0.0000"
	}
	return s
}

// info is the authorship block every Contents.json carries; JSON has no
// comments, so it stands for the generated-file header.
func info() *object {
	o := &object{}
	o.set("author", "airframe")
	o.set("version", 1)
	return o
}

// inContext is " (axis value, …)" naming ctx's axes in sorted order, or
// "" for an empty context.
func inContext(ctx config.Context) string {
	if len(ctx) == 0 {
		return ""
	}
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(ctx)) {
		parts = append(parts, k+":"+ctx[k])
	}
	return " (" + strings.Join(parts, "+") + ")"
}

// encodeJSON writes v with 2-space indents and a final newline.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
