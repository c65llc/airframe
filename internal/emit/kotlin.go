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
// colours are data: a shared file declares the <Type> class (see
// tokensDecl) and <Type>Colors, a data class with one Color per colour
// slot (see kotlinSlot), and each output set gets a <Type>ColorValues
// object holding a <Type>Colors per appearance value. With no colour slot
// there is no <Type>Colors and <Type>ColorValues is empty.
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
	shared, err := k.sharedDecls(slots)
	if err != nil {
		return nil, err
	}

	tmpl := outPath(p, pl, "kotlin")
	fan := ""
	if k.np.FanOut != nil {
		fan = "{" + k.np.FanOut.Name + "}"
	}
	perFile := fan != "" && strings.Contains(tmpl, fan)
	sharedOut, hasShared := pl.Option("sharedOut")
	if !hasShared && perFile {
		return nil, fmt.Errorf("out fans out per %s; set options.sharedOut for the shared DesignTokens file", k.np.FanOut.Name)
	}
	if hasShared && sharedOut == tmpl {
		return nil, fmt.Errorf("options.sharedOut and the kotlin out are both %s; the shared file needs a path of its own", sharedOut)
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
			b = k.file(append(slices.Clone(shared), values...))
		}
		if prev, ok := out[f]; ok && !bytes.Equal(prev, b) {
			return nil, fmt.Errorf("%s differs between %s %s and %s; put {%s} in the kotlin out path",
				f, k.np.FanOut.Name, by[f], o.FanValue, k.np.FanOut.Name)
		}
		out[f], by[f] = b, o.FanValue
	}
	if hasShared {
		if _, dup := out[sharedOut]; dup {
			return nil, fmt.Errorf("options.sharedOut %s is also a kotlin out file", sharedOut)
		}
		out[sharedOut] = k.file(shared)
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

// colorsDecl declares <Type>Colors, or nothing with no slots.
func (k *kotliner) colorsDecl(slots []kotlinSlot) []kotlinDecl {
	if len(slots) == 0 {
		return nil
	}
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
	if len(slots) == 0 {
		return []kotlinDecl{{body: "public object " + k.valuesType() + "\n"}}, nil
	}
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
	b.WriteString("// " + header(k.p) + "\n\npackage " + k.np.Platform.KotlinPackage() + "\n")
	if len(imports) > 0 {
		b.WriteString("\n")
	}
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

// kotlinMember is one emitted token of the <Type> class: its escaped
// Kotlin name and the axes its value switches on, in config order. A
// non-colour token switching on none is static, a companion val.
type kotlinMember struct {
	t    *tokens.Token
	name string
	axes []config.Axis
}

func (m kotlinMember) static() bool { return len(m.axes) == 0 && !isColorish(m.t) }

// kotlinReserved are the type names the shared file uses or declares; a
// nested enum of the same name would shadow them.
var kotlinReserved = []string{"Boolean", "Color", "Companion", "CubicBezierEasing", "Dp", "Duration",
	"Float", "FontWeight", "List", "Shadow", "String"}

// kotlinTypes are the Kotlin types of the token types.
var kotlinTypes = map[string]string{
	"color": "Color", "dimension": "Dp", "number": "Float", "duration": "Duration",
	"cubicBezier": "CubicBezierEasing", "fontFamily": "List<String>", "fontWeight": "FontWeight", "shadow": "List<Shadow>",
}

// kotlinImports are the imports each Kotlin type's declarations and
// literals use.
var kotlinImports = map[string][]string{
	"Color":             {kotlinColorImport},
	"Dp":                {"androidx.compose.ui.unit.Dp", "androidx.compose.ui.unit.dp"},
	"Duration":          {"kotlin.time.Duration", "kotlin.time.Duration.Companion.milliseconds"},
	"CubicBezierEasing": {"androidx.compose.animation.core.CubicBezierEasing"},
	"FontWeight":        {"androidx.compose.ui.text.font.FontWeight"},
	"List<Shadow>":      {kotlinColorImport, "androidx.compose.ui.unit.Dp", "androidx.compose.ui.unit.dp"},
}

// sharedDecls are the shared file's declarations: the <Type> class, then
// <Type>Colors.
func (k *kotliner) sharedDecls(slots []kotlinSlot) ([]kotlinDecl, error) {
	d, err := k.tokensDecl(slots)
	if err != nil {
		return nil, err
	}
	return append([]kotlinDecl{d}, k.colorsDecl(slots)...), nil
}

// prepare checks the member names and works out the axes each member
// switches on and the class's runtime parameters: each runtime axis some
// member switches on, in config order.
func (k *kotliner) prepare(slots []kotlinSlot) ([]kotlinMember, []config.Axis, error) {
	owner := map[string]string{} // Kotlin name → what declares it
	if len(slots) > 0 {
		owner["colors"] = "the colors parameter"
	}
	var members []kotlinMember
	needed := map[string]bool{}
	for _, t := range emitted(k.p, "kotlin") {
		if _, ok := kotlinTypes[t.Type]; !ok {
			return nil, nil, fmt.Errorf("token %q: a %s token has no Kotlin type", t.Name, t.Type)
		}
		raw, err := k.member(t)
		if err != nil {
			return nil, nil, err
		}
		name := KotlinIdent(raw)
		if prev, dup := owner[raw]; dup {
			return nil, nil, fmt.Errorf("%s and token %q both map to Kotlin identifier %s", prev, t.Name, name)
		}
		owner[raw] = fmt.Sprintf("token %q", t.Name)
		m, err := k.axesOf(t)
		if err != nil {
			return nil, nil, err
		}
		m.name = name
		for _, a := range m.axes {
			needed[a.Name] = true
		}
		members = append(members, m)
	}
	types := map[string]string{} // nested or used type name → what it is
	for _, n := range append(slices.Clone(kotlinReserved), k.typ, k.colorsType(), k.valuesType()) {
		types[n] = "the type " + n
	}
	var params []config.Axis
	for _, a := range k.p.Config.Axes {
		if !needed[a.Name] {
			continue
		}
		params = append(params, a)
		enum, prop := Pascal(a.Name), KotlinIdent(Camel(a.Name))
		if !kotlinIdentRE.MatchString(enum) || slices.Contains(config.KotlinKeywords, enum) {
			return nil, nil, fmt.Errorf("axis %s would be Kotlin enum %s, which is not a Kotlin type name the file can declare", a.Name, enum)
		}
		if prev, dup := types[enum]; dup {
			return nil, nil, fmt.Errorf("axis %s's Kotlin enum %s is already taken by %s", a.Name, enum, prev)
		}
		types[enum] = "axis " + a.Name
		if !validKotlin(prop) {
			return nil, nil, fmt.Errorf("axis %s has no Kotlin property name (%s)", a.Name, prop)
		}
		if p := strings.Trim(prop, "`"); owner[p] != "" {
			return nil, nil, fmt.Errorf("axis %s's Kotlin parameter %s is already taken by %s", a.Name, prop, owner[p])
		}
		owner[strings.Trim(prop, "`")] = "axis " + a.Name
		cases := map[string]string{}
		for _, v := range a.Values {
			c := KotlinIdent(Pascal(v))
			if !validKotlin(c) {
				return nil, nil, fmt.Errorf("axis %s value %q has no Kotlin enum constant name", a.Name, v)
			}
			if prev, dup := cases[c]; dup {
				return nil, nil, fmt.Errorf("axis %s values %q and %q both make Kotlin enum constant %s", a.Name, prev, v, c)
			}
			cases[c] = v
		}
	}
	return members, params, nil
}

// axesOf works out the axes t's Kotlin value switches on: each runtime
// axis it varies on, and appearance when a non-colour part of it does (a
// colour's appearance is the <Type>Colors the caller picks). The file is
// shared, so a token whose non-colour part varies per fan-out value
// cannot be written; its colours are per output, in <Type>ColorValues.
func (k *kotliner) axesOf(t *tokens.Token) (kotlinMember, error) {
	m := kotlinMember{t: t}
	varies := k.np.Varies(k.p.Tokens, t)
	np := k.np
	on := func(a *config.Axis) bool { return a != nil && slices.Contains(varies, a.Name) }
	needs := func(a *config.Axis) (bool, error) {
		switch {
		case !on(a) || t.Type == "color":
			return false, nil
		case t.Type == "shadow":
			return shadowGeometryVaries(k.p, np, t, varies, *a)
		}
		return true, nil
	}
	if per, err := needs(np.FanOut); err != nil {
		return m, err
	} else if per {
		return m, fmt.Errorf("token %q varies per %s; Kotlin non-colour tokens are shared", t.Name, np.FanOut.Name)
	}
	appearance, err := needs(np.Appearance)
	if err != nil {
		return m, err
	}
	for _, a := range k.p.Config.Axes {
		switch {
		case np.Appearance != nil && a.Name == np.Appearance.Name:
			if appearance {
				m.axes = append(m.axes, a)
			}
		case slices.ContainsFunc(np.Runtime, func(r config.Axis) bool { return r.Name == a.Name }) && on(&a):
			m.axes = append(m.axes, a)
		}
	}
	return m, nil
}

// tokensDecl declares the <Type> class: its constructor takes colors (with
// any colour slot) and one parameter per runtime axis a member switches
// on, defaulting to the axis's base; then come the axes' enums, Shadow
// when a shadow uses it, the companion object of static members, and the
// instance members, each static one forwarding to its companion val.
func (k *kotliner) tokensDecl(slots []kotlinSlot) (kotlinDecl, error) {
	members, params, err := k.prepare(slots)
	if err != nil {
		return kotlinDecl{}, err
	}
	slotOf := map[string]string{} // slotKey → slot name
	for _, s := range slots {
		slotOf[slotKey(s.Token.Name, s.Layer, s.Ctx)] = s.Name
	}
	var imports []string
	var sections []string

	var enums strings.Builder
	for i, a := range params {
		if i > 0 {
			enums.WriteString("\n")
		}
		var cs []string
		for _, v := range a.Values {
			cs = append(cs, KotlinIdent(Pascal(v)))
		}
		fmt.Fprintf(&enums, "    public enum class %s { %s }\n", Pascal(a.Name), strings.Join(cs, ", "))
	}
	if enums.Len() > 0 {
		sections = append(sections, enums.String())
	}
	if slices.ContainsFunc(members, func(m kotlinMember) bool { return m.t.Type == "shadow" }) {
		sections = append(sections, kotlinShadowDecl)
	}

	var statics, instance strings.Builder
	prevWhen := false
	for _, m := range members {
		typ := kotlinTypes[m.t.Type]
		imports = append(imports, kotlinImports[typ]...)
		if m.static() {
			expr, err := k.expr(m, config.Context{}, slotOf)
			if err != nil {
				return kotlinDecl{}, err
			}
			fmt.Fprintf(&statics, "        public val %s: %s = %s\n", m.name, typ, expr)
		}
		// One-line members run together; a blank line sets off each when.
		if instance.Len() > 0 && (prevWhen || len(m.axes) > 0) {
			instance.WriteString("\n")
		}
		prevWhen = len(m.axes) > 0
		fmt.Fprintf(&instance, "    public val %s: %s get() = ", m.name, typ)
		if m.static() {
			fmt.Fprintf(&instance, "Companion.%s\n", m.name)
			continue
		}
		if err := k.when(&instance, m, m.axes, config.Context{}, "    ", slotOf); err != nil {
			return kotlinDecl{}, err
		}
		instance.WriteString("\n")
	}
	if statics.Len() > 0 {
		sections = append(sections, "    public companion object {\n"+statics.String()+"    }\n")
	}
	if instance.Len() > 0 {
		sections = append(sections, instance.String())
	}

	var b strings.Builder
	fmt.Fprintf(&b, "public class %s", k.typ)
	var args []string
	if len(slots) > 0 {
		args = append(args, "    public val colors: "+k.colorsType()+",\n")
	}
	for _, a := range params {
		args = append(args, fmt.Sprintf("    public val %s: %s = %[2]s.%s,\n", KotlinIdent(Camel(a.Name)), Pascal(a.Name), KotlinIdent(Pascal(base(a)))))
	}
	if len(args) > 0 {
		b.WriteString("(\n" + strings.Join(args, "") + ")")
	}
	if len(sections) > 0 {
		b.WriteString(" {\n" + strings.Join(sections, "\n") + "}")
	}
	b.WriteString("\n")
	return kotlinDecl{imports: imports, body: b.String()}, nil
}

// kotlinShadowDecl declares the shadow layer type.
const kotlinShadowDecl = `    public data class Shadow(
        public val color: Color,
        public val x: Dp,
        public val y: Dp,
        public val blur: Dp,
        public val spread: Dp,
        public val inset: Boolean,
    )
`

// slotKey identifies the colour slot of a token's layer (-1 for a colour)
// in runtime context ctx.
func slotKey(token string, layer int, ctx config.Context) string {
	return fmt.Sprintf("%s#%d%s", token, layer, inContext(ctx))
}

// when writes m's value switching on axes, outer to inner, as nested
// exhaustive whens at indent, with ctx the values chosen so far.
func (k *kotliner) when(b *strings.Builder, m kotlinMember, axes []config.Axis, ctx config.Context, indent string, slotOf map[string]string) error {
	if len(axes) == 0 {
		expr, err := k.expr(m, ctx, slotOf)
		if err != nil {
			return err
		}
		b.WriteString(expr)
		return nil
	}
	a := axes[0]
	fmt.Fprintf(b, "when (%s) {\n", KotlinIdent(Camel(a.Name)))
	for _, v := range a.Values {
		c := maps.Clone(ctx)
		c[a.Name] = v
		fmt.Fprintf(b, "%s    %s.%s -> ", indent, Pascal(a.Name), KotlinIdent(Pascal(v)))
		if err := k.when(b, m, axes[1:], c, indent+"    ", slotOf); err != nil {
			return err
		}
		b.WriteString("\n")
	}
	b.WriteString(indent + "}")
	return nil
}

// expr is m's value as a Kotlin expression in switch context ctx. A colour
// reads its slot of colors, as does each shadow layer's colour.
func (k *kotliner) expr(m kotlinMember, ctx config.Context, slotOf map[string]string) (string, error) {
	t := m.t
	runtime := config.Context{}
	for _, a := range k.np.Runtime {
		if v, ok := ctx[a.Name]; ok {
			runtime[a.Name] = v
		}
	}
	slot := func(layer int) (string, error) {
		s, ok := slotOf[slotKey(t.Name, layer, runtime)]
		if !ok {
			return "", fmt.Errorf("token %q%s has no Kotlin colour slot for layer %d", t.Name, inContext(runtime), layer+1)
		}
		return "colors." + s, nil
	}
	if t.Type == "color" {
		return slot(-1)
	}
	v, err := k.np.Value(k.p.Tokens, t.Name, ctx)
	if err != nil {
		return "", fmt.Errorf("token %q%s: %w", t.Name, inContext(ctx), err)
	}
	bad := func() (string, error) {
		return "", fmt.Errorf("token %q%s: a %s token resolves to %T", t.Name, inContext(ctx), t.Type, v.Lit)
	}
	switch t.Type {
	case "dimension":
		d, ok := v.Lit.(tokens.Dim)
		if !ok {
			return bad()
		}
		s, err := kotlinDp(d)
		if err != nil {
			return "", fmt.Errorf("token %q: %w", t.Name, err)
		}
		return s, nil
	case "number":
		f, ok := v.Lit.(float64)
		if !ok {
			return bad()
		}
		return kotlinFloat(f), nil
	case "duration":
		d, ok := v.Lit.(tokens.Duration)
		if !ok {
			return bad()
		}
		ms := d.V
		switch d.Unit {
		case "ms":
		case "s":
			ms *= 1000
		default:
			return "", fmt.Errorf("token %q: duration unit %q has no Kotlin spelling", t.Name, d.Unit)
		}
		r := kotlinReceiver(ms)
		if strings.Contains(r, ".") && !strings.HasPrefix(r, "(") {
			r = "(" + r + ")"
		}
		return r + ".milliseconds", nil
	case "cubicBezier":
		c, ok := v.Lit.([4]float64)
		if !ok {
			return bad()
		}
		return fmt.Sprintf("CubicBezierEasing(%s, %s, %s, %s)", kotlinFloat(c[0]), kotlinFloat(c[1]), kotlinFloat(c[2]), kotlinFloat(c[3])), nil
	case "fontFamily":
		fam, ok := v.Lit.([]string)
		if !ok {
			return bad()
		}
		q := make([]string, len(fam))
		for i, f := range fam {
			q[i] = kotlinString(f)
		}
		return "listOf(" + strings.Join(q, ", ") + ")", nil
	case "fontWeight":
		w, ok := v.Lit.(float64)
		if !ok {
			return bad()
		}
		r := math.Floor(w + 0.5)
		if r < 1 || r > 1000 {
			return "", fmt.Errorf("token %q%s: font weight %s is outside 1..1000, the weights FontWeight takes", t.Name, inContext(ctx), num(w))
		}
		return fmt.Sprintf("FontWeight(%d)", int(r)), nil
	case "shadow":
		layers, ok := v.Lit.([]tokens.Shadow)
		if !ok {
			return bad()
		}
		parts := make([]string, len(layers))
		for i, l := range layers {
			c, err := slot(i)
			if err != nil {
				return "", err
			}
			var dims [4]string
			for j, d := range []tokens.Dim{l.OffsetX, l.OffsetY, l.Blur, l.Spread} {
				if dims[j], err = kotlinDp(d); err != nil {
					return "", fmt.Errorf("token %q: %w", t.Name, err)
				}
			}
			parts[i] = fmt.Sprintf("Shadow(color = %s, x = %s, y = %s, blur = %s, spread = %s, inset = %t)",
				c, dims[0], dims[1], dims[2], dims[3], l.Inset)
		}
		return "listOf(" + strings.Join(parts, ", ") + ")", nil
	}
	return "", fmt.Errorf("token %q: a %s token has no Kotlin type", t.Name, t.Type)
}

// kotlinDp is a dimension in dp: px as is, rem × 16.
func kotlinDp(d tokens.Dim) (string, error) {
	switch d.Unit {
	case "px":
		return kotlinReceiver(d.V) + ".dp", nil
	case "rem":
		return kotlinReceiver(d.V*16) + ".dp", nil
	}
	return "", fmt.Errorf("dimension unit %q has no Kotlin spelling", d.Unit)
}

// kotlinReceiver is v (num) as the receiver of an extension property: an
// Int or Double literal, parenthesised when negative so the minus does not
// apply to the property. A fractional duration is parenthesised too.
func kotlinReceiver(v float64) string {
	s := num(v)
	if strings.HasPrefix(s, "-") {
		return "(" + s + ")"
	}
	return s
}

// kotlinFloat is v (num) as a Float literal: 1.5f, 0f.
func kotlinFloat(v float64) string { return num(v) + "f" }

// kotlinString is s as a Kotlin string literal, $ escaped so no template
// expands.
func kotlinString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\' || r == '$':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
