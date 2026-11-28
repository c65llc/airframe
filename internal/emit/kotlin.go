// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"bytes"
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

// Kotlin writes Jetpack Compose sources. Android has no asset catalog, so
// colours are data: a shared file declares <Type>Colors, a data class with
// one Color per colour slot (see kotlinSlot), and each output set gets a
// <Type>ColorValues object holding a <Type>Colors per appearance value.
// <Type> is the type part of the kotlin naming template less a trailing
// Tokens, so the default DesignTokens.{camel} gives DesignColors and
// DesignColorValues.
//
// The values file is written to the kotlin out path, one per fan-out
// value when it contains {<fanOut>}; the shared file to options.sharedOut.
// With no sharedOut, the shared declarations open the values file, which
// then must not fan out. Colours are sRGB Color(0xAARRGGBB) or, with
// colorSpace: display-p3, Display P3 components, mapped into gamut by CSS
// Color 4 or, with gamut: clip, clipped.
func Kotlin(p *project.Project, pl config.Platform) (map[string][]byte, error) {
	k, err := newKotliner(p, pl)
	if err != nil {
		return nil, err
	}
	slots, err := k.slots()
	if err != nil {
		return nil, err
	}
	if len(slots) == 0 {
		return nil, fmt.Errorf("no colour or shadow token goes to kotlin; %s needs at least one colour", k.colorsType())
	}

	tmpl := outPath(p, pl, "kotlin")
	fan := ""
	if k.np.FanOut != nil {
		fan = "{" + k.np.FanOut.Name + "}"
	}
	perFile := fan != "" && strings.Contains(tmpl, fan)
	shared, hasShared := pl.Option("sharedOut")
	if !hasShared && perFile {
		return nil, fmt.Errorf("kotlin out fans out per %s; set options.sharedOut for the shared DesignTokens file", k.np.FanOut.Name)
	}
	if hasShared && shared == tmpl {
		return nil, fmt.Errorf("options.sharedOut and the kotlin out are both %s; the shared file needs a path of its own", shared)
	}

	out := map[string][]byte{}
	by := map[string]string{} // values file → the fan-out value that wrote it
	for _, o := range k.np.Outputs() {
		f := tmpl
		if perFile {
			f = strings.ReplaceAll(f, fan, o.FanValue)
		}
		if strings.ContainsAny(f, "{}") {
			return nil, fmt.Errorf("kotlin out path %s has a placeholder that is not the fan-out axis", tmpl)
		}
		values, err := k.values(slots, o)
		if err != nil {
			return nil, err
		}
		var b []byte
		if hasShared {
			b = k.file(values)
		} else {
			b = k.file(append(k.colorsDecl(slots), values...))
		}
		if prev, ok := out[f]; ok && !bytes.Equal(prev, b) {
			return nil, fmt.Errorf("%s differs between %s %s and %s; put {%s} in the kotlin out path",
				f, k.np.FanOut.Name, by[f], o.FanValue, k.np.FanOut.Name)
		}
		out[f], by[f] = b, o.FanValue
	}
	if hasShared {
		if _, dup := out[shared]; dup {
			return nil, fmt.Errorf("options.sharedOut %s is also a kotlin out file", shared)
		}
		out[shared] = k.file(k.colorsDecl(slots))
	}
	return out, nil
}

// kotlinSlot is one field of <Type>Colors: a colour token, or layer Layer
// (from 0) of a shadow token's colours, in Ctx, a combination of values of
// the runtime axes other than appearance that the token varies on. Layer
// is -1 for a colour token.
type kotlinSlot struct {
	Name  string
	Token *tokens.Token
	Layer int
	Ctx   config.Context
}

type kotliner struct {
	p        *project.Project
	np       NativePlan
	template string
	typ      string // the template's type part, DesignTokens by default
	space    string // srgb or display-p3
	gamut    string // css-color-4 or clip
}

// newKotliner reads pl's kotlin options and checks the template's type.
func newKotliner(p *project.Project, pl config.Platform) (*kotliner, error) {
	k := &kotliner{p: p, np: PlanNative(p.Config, pl), template: p.Config.NamingFor("kotlin"), space: "srgb", gamut: "css-color-4"}
	if v, ok := pl.Option("colorSpace"); ok {
		k.space = v
	}
	if v, ok := pl.Option("gamut"); ok {
		k.gamut = v
	}
	i := strings.LastIndex(k.template, ".")
	if i < 0 {
		return nil, fmt.Errorf("kotlin naming template %q needs a type part, as in DesignTokens.{camel}", k.template)
	}
	k.typ = k.template[:i]
	for _, typ := range []string{k.typ, k.colorsType(), k.valuesType()} {
		if !kotlinIdentRE.MatchString(typ) || slices.Contains(config.KotlinKeywords, typ) {
			return nil, fmt.Errorf("kotlin naming template %q gives type %s, which is not a Kotlin type name the file can declare", k.template, typ)
		}
	}
	return k, nil
}

// kotlinColorPrefix is the stem of the colour types for type typ: typ less
// a trailing Tokens, unless that leaves nothing.
func kotlinColorPrefix(typ string) string {
	if s := strings.TrimSuffix(typ, "Tokens"); s != "" {
		return s
	}
	return typ
}

func (k *kotliner) colorsType() string { return kotlinColorPrefix(k.typ) + "Colors" }
func (k *kotliner) valuesType() string { return kotlinColorPrefix(k.typ) + "ColorValues" }

// kotlinIdentRE is an identifier Kotlin accepts without backticks, kept to
// ASCII as for Swift.
var kotlinIdentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// KotlinIdent escapes what Kotlin reserves in an identifier: a hard
// keyword is wrapped in backticks and a leading digit gets a "_" prefix.
// It does not make any string an identifier; the emitter rejects what is
// still not one.
func KotlinIdent(s string) string {
	switch {
	case slices.Contains(config.KotlinKeywords, s):
		return "`" + s + "`"
	case s != "" && s[0] >= '0' && s[0] <= '9':
		return "_" + s
	}
	return s
}

func validKotlin(id string) bool {
	return kotlinIdentRE.MatchString(strings.TrimSuffix(strings.TrimPrefix(id, "`"), "`"))
}

// member is t's Kotlin member name, unescaped: its names.kotlin or the
// template's member part, with a leading digit prefixed.
func (k *kotliner) member(t *tokens.Token) (string, error) {
	raw := NameFor(k.p.Config, t, "kotlin")
	name := strings.Trim(KotlinIdent(raw), "`")
	if !validKotlin(name) {
		if _, own := t.Names["kotlin"]; own {
			return "", fmt.Errorf("token %q: its kotlin name %q is not a Kotlin identifier", t.Name, raw)
		}
		return "", fmt.Errorf("token %q: kotlin naming template %q gives %q, which is not a Kotlin identifier", t.Name, k.template, raw)
	}
	return name, nil
}

// slots lists <Type>Colors' fields, sorted by name: per emitted colour
// token, and per layer of an emitted shadow token, one slot for each
// combination of the runtime axes other than appearance it varies on,
// named by the member plus Pascal(value) per axis in axis order
// (surfaceCompact), with Layer<N> after a shadow's member. A shadow must
// have the same layer count in every output and appearance, as every
// <Type>Colors has the same fields. Two slots of one name are an error.
func (k *kotliner) slots() ([]kotlinSlot, error) {
	var out []kotlinSlot
	owner := map[string]string{} // slot name → token
	add := func(s kotlinSlot) error {
		if prev, dup := owner[s.Name]; dup {
			return fmt.Errorf("tokens %q and %q both make Kotlin colour slot %s", prev, s.Token.Name, s.Name)
		}
		owner[s.Name] = s.Token.Name
		out = append(out, s)
		return nil
	}
	for _, t := range emitted(k.p, "kotlin") {
		if !isColorish(t) {
			continue
		}
		member, err := k.member(t)
		if err != nil {
			return nil, err
		}
		varies := k.np.Varies(k.p.Tokens, t)
		var axes []config.Axis
		for _, a := range k.np.Runtime {
			if slices.Contains(varies, a.Name) {
				axes = append(axes, a)
			}
		}
		for _, ctx := range combos(axes) {
			suffix := ""
			for _, a := range axes {
				suffix += Pascal(ctx[a.Name])
			}
			if t.Type == "color" {
				if err := add(kotlinSlot{Name: KotlinIdent(member + suffix), Token: t, Layer: -1, Ctx: ctx}); err != nil {
					return nil, err
				}
				continue
			}
			n, err := k.layers(t, ctx)
			if err != nil {
				return nil, err
			}
			for i := range n {
				if err := add(kotlinSlot{Name: fmt.Sprintf("%sLayer%d%s", member, i+1, suffix), Token: t, Layer: i, Ctx: ctx}); err != nil {
					return nil, err
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b kotlinSlot) int {
		return strings.Compare(strings.Trim(a.Name, "`"), strings.Trim(b.Name, "`"))
	})
	return out, nil
}

// layers is shadow t's layer count in runtime context ctx, the same in
// every output and appearance.
func (k *kotliner) layers(t *tokens.Token, ctx config.Context) (int, error) {
	n, at := -1, ""
	for _, o := range k.np.Outputs() {
		for _, av := range k.appearances() {
			full := k.full(ctx, o, av)
			v, err := k.np.Value(k.p.Tokens, t.Name, full)
			if err != nil {
				return 0, fmt.Errorf("token %q%s: %w", t.Name, inContext(full), err)
			}
			l, ok := v.Lit.([]tokens.Shadow)
			if !ok {
				return 0, fmt.Errorf("token %q%s: a shadow token resolves to %T", t.Name, inContext(full), v.Lit)
			}
			switch {
			case n < 0:
				n, at = len(l), inContext(full)
			case len(l) != n:
				return 0, fmt.Errorf("token %q has %d shadow layers%s and %d%s; every %s has the same colour slots",
					t.Name, n, at, len(l), inContext(full), k.colorsType())
			}
		}
	}
	return n, nil
}

// appearances lists the appearance axis's values in axis order, or one
// "" when there is no appearance axis.
func (k *kotliner) appearances() []string {
	if k.np.Appearance == nil {
		return []string{""}
	}
	return k.np.Appearance.Values
}

// full is runtime context ctx in output o at appearance value av ("" for
// none).
func (k *kotliner) full(ctx config.Context, o Output, av string) config.Context {
	c := maps.Clone(ctx)
	if k.np.FanOut != nil {
		c[k.np.FanOut.Name] = o.FanValue
	}
	if av != "" {
		c[k.np.Appearance.Name] = av
	}
	return c
}

// kotlinDecl is one top-level declaration and the imports it uses.
type kotlinDecl struct {
	imports []string
	body    string
}

const kotlinColorImport = "androidx.compose.ui.graphics.Color"

// colorsDecl declares <Type>Colors.
func (k *kotliner) colorsDecl(slots []kotlinSlot) []kotlinDecl {
	var b strings.Builder
	fmt.Fprintf(&b, "public data class %s(\n", k.colorsType())
	for _, s := range slots {
		fmt.Fprintf(&b, "    public val %s: Color,\n", s.Name)
	}
	b.WriteString(")\n")
	return []kotlinDecl{{imports: []string{kotlinColorImport}, body: b.String()}}
}

// values declares <Type>ColorValues for output o: one <Type>Colors per
// appearance value, named by the value, or a single colors with no
// appearance axis.
func (k *kotliner) values(slots []kotlinSlot, o Output) ([]kotlinDecl, error) {
	imports := []string{kotlinColorImport}
	if k.space == "display-p3" {
		imports = append(imports, "androidx.compose.ui.graphics.colorspace.ColorSpaces")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "public object %s {\n", k.valuesType())
	seen := map[string]string{} // val name → appearance value
	for i, av := range k.appearances() {
		name := "colors"
		if av != "" {
			name = KotlinIdent(Camel(av))
			if !validKotlin(name) {
				return nil, fmt.Errorf("appearance value %q has no Kotlin name", av)
			}
			if prev, dup := seen[name]; dup {
				return nil, fmt.Errorf("appearance values %q and %q both make Kotlin val %s", prev, av, name)
			}
			seen[name] = av
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "    public val %s: %s = %s(\n", name, k.colorsType(), k.colorsType())
		for _, s := range slots {
			c, err := k.slotColor(s, o, av)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&b, "        %s = %s,\n", s.Name, kotlinColor(c, k.space, k.gamut))
		}
		b.WriteString("    )\n")
	}
	b.WriteString("}\n")
	return []kotlinDecl{{imports: imports, body: b.String()}}, nil
}

// slotColor resolves slot s in output o at appearance value av.
func (k *kotliner) slotColor(s kotlinSlot, o Output, av string) (color.OKLCH, error) {
	full := k.full(s.Ctx, o, av)
	v, err := k.np.Value(k.p.Tokens, s.Token.Name, full)
	if err != nil {
		return color.OKLCH{}, fmt.Errorf("token %q%s: %w", s.Token.Name, inContext(full), err)
	}
	switch l := v.Lit.(type) {
	case color.OKLCH:
		if s.Layer < 0 {
			return l, nil
		}
	case []tokens.Shadow:
		if s.Layer >= 0 && s.Layer < len(l) {
			return l[s.Layer].Color, nil
		}
		if s.Layer >= 0 {
			return color.OKLCH{}, fmt.Errorf("token %q%s has %d shadow layers, not %d", s.Token.Name, inContext(full), len(l), s.Layer+1)
		}
	}
	return color.OKLCH{}, fmt.Errorf("token %q%s: a %s token resolves to %T", s.Token.Name, inContext(full), s.Token.Type, v.Lit)
}

// file is a Kotlin source: the header, the package, the sorted imports
// the declarations use, then the declarations, a blank line apart.
func (k *kotliner) file(decls []kotlinDecl) []byte {
	var imports []string
	for _, d := range decls {
		imports = append(imports, d.imports...)
	}
	slices.Sort(imports)
	imports = slices.Compact(imports)
	var b strings.Builder
	b.WriteString("// " + header(k.p) + "\n\npackage " + k.np.Platform.KotlinPackage() + "\n\n")
	for _, i := range imports {
		b.WriteString("import " + i + "\n")
	}
	for _, d := range decls {
		b.WriteString("\n" + d.body)
	}
	return []byte(b.String())
}

// kotlinColor is c as a Compose Color expression. With space srgb it is
// Color(0xAARRGGBB), each 8-bit channel rounded half up, the colour
// brought into sRGB by gamut (css-color-4 or clip); with display-p3 it is
// the Display P3 components to 4 decimals.
func kotlinColor(c color.OKLCH, space, gamut string) string {
	g := color.CSSColor4
	if gamut == "clip" {
		g = color.Clip
	}
	if space == "display-p3" {
		r, gr, b := c.ToP3(g)
		return fmt.Sprintf("Color(red = %sf, green = %sf, blue = %sf, alpha = %sf, colorSpace = ColorSpaces.DisplayP3)",
			component(r), component(gr), component(b), component(c.Alpha))
	}
	r, gr, b := c.ToSRGB(g)
	return fmt.Sprintf("Color(0x%02X%02X%02X%02X)", byte8(c.Alpha), byte8(r), byte8(gr), byte8(b))
}

// byte8 is v in [0,1] as an 8-bit channel, rounded half up.
func byte8(v float64) int {
	return int(math.Floor(math.Min(1, math.Max(0, v))*255 + 0.5))
}
