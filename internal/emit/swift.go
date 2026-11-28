// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"fmt"
	"maps"
	"math"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// Swift writes a SwiftUI source file declaring one Sendable struct, named
// by the type part of the swift naming template (DesignTokens by default),
// whose members are the emitted tokens. The out path is one shared file,
// or one file per fan-out value when it contains {<fanOut>}; a shared file
// refuses a non-colour token that differs per fan-out value, and refuses
// to be shared when the colorset namespace differs per value.
//
// The runtime axes some emitted token varies on become nested String
// enums, stored properties and init parameters defaulting to each axis's
// base; appearance is one only when a non-colour token varies on it, as a
// colour's colorset handles appearance itself. A token varying on none of
// them is a static let, with an instance property forwarding to it so
// every member reads alike on an instance; one that does is an instance
// property whose exhaustive switch covers every combination of their
// values. Colours
// (and shadow colours) read Task 4's colorsets by ColorsetName, so they
// need colorsets emitted on the same platform.
func Swift(p *project.Project, pl config.Platform) (map[string][]byte, error) {
	np := PlanNative(p.Config, pl)
	sw := &swifter{p: p, np: np, template: p.Config.NamingFor("swift")}
	tmpl := outPath(p, pl, "swift")
	fan := ""
	if np.FanOut != nil {
		fan = "{" + np.FanOut.Name + "}"
	}
	sw.perFile = fan != "" && strings.Contains(tmpl, fan)
	if fan != "" && !sw.perFile && slices.Contains(pl.Emit, "colorsets") {
		if last := path.Base(strings.TrimSuffix(outPath(p, pl, "colorsets"), "/")); strings.Contains(last, fan) {
			return nil, fmt.Errorf("the colorset namespace differs per %[1]s; put {%[1]s} in the swift out path", np.FanOut.Name)
		}
	}
	if err := sw.prepare(); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, o := range np.Outputs() {
		f := tmpl
		if sw.perFile {
			f = strings.ReplaceAll(f, fan, o.FanValue)
		}
		if strings.ContainsAny(f, "{}") {
			return nil, fmt.Errorf("swift out path %s has a placeholder that is not the fan-out axis", tmpl)
		}
		b, err := sw.file(o)
		if err != nil {
			return nil, err
		}
		out[f] = b
		if !sw.perFile {
			break // a shared file is written once
		}
	}
	return out, nil
}

// swiftReserved are the type names the generated file uses; a nested
// type of the same name would shadow them.
var swiftReserved = []string{"Bezier", "Bool", "CGFloat", "CaseIterable", "Color", "Double", "Equatable",
	"Font", "Sendable", "Shadow", "String", "SwiftUI", "TimeInterval"}

// swiftIdentRE is an identifier Swift accepts without backticks. Airframe
// keeps to ASCII, which every Swift toolchain and code review reads alike.
var swiftIdentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// swiftTypeName reports whether id can name a declared type: an
// identifier that is no keyword (a backticked one still cannot be a type)
// nor one of the names Swift gives a meaning in type position.
func swiftTypeName(id string) bool {
	return swiftIdentRE.MatchString(id) && !swiftKeywords[id] && !slices.Contains([]string{"Any", "Protocol", "Self", "Type"}, id)
}

func validSwift(id string) bool {
	return swiftIdentRE.MatchString(strings.TrimSuffix(strings.TrimPrefix(id, "`"), "`"))
}

type swifter struct {
	p        *project.Project
	np       NativePlan
	template string
	perFile  bool

	typ     string
	params  []config.Axis // the struct's runtime parameters, config order
	members []swiftMember
}

// swiftMember is one emitted token: its Swift name and the axes its value
// switches on (none for a static let), in config order.
type swiftMember struct {
	t      *tokens.Token
	name   string
	axes   []config.Axis
	varies []string
}

// prepare checks the type and member names and works out which axes each
// token, and so the struct, needs.
func (sw *swifter) prepare() error {
	i := strings.LastIndex(sw.template, ".")
	if i < 0 {
		return fmt.Errorf("swift naming template %q needs a type part, as in DesignTokens.{camel}", sw.template)
	}
	typ := sw.template[:i]
	if !swiftTypeName(typ) || slices.Contains(swiftReserved, typ) {
		return fmt.Errorf("swift naming template %q names type %s, which is not a Swift type name the file can declare", sw.template, typ)
	}
	sw.typ = typ

	colorsets := slices.Contains(sw.np.Platform.Emit, "colorsets")
	inColorsets := map[string]bool{}
	for _, t := range emitted(sw.p, "colorsets") {
		inColorsets[t.Name] = true
	}
	owner := map[string]string{} // Swift name → what declares it
	for _, n := range swiftReserved {
		owner[n] = "the type " + n
	}
	needed := map[string]bool{}
	for _, t := range emitted(sw.p, "swift") {
		raw := NameFor(sw.p.Config, t, "swift")
		name := SwiftIdent(raw)
		if !validSwift(name) {
			if _, own := t.Names["swift"]; own {
				return fmt.Errorf("token %q: its swift name %q is not a Swift identifier", t.Name, raw)
			}
			return fmt.Errorf("token %q: swift naming template %q gives %q, which is not a Swift identifier", t.Name, sw.template, raw)
		}
		key := strings.Trim(name, "`")
		if prev, dup := owner[key]; dup {
			return fmt.Errorf("%s and token %q both map to Swift identifier %s", prev, t.Name, name)
		}
		owner[key] = fmt.Sprintf("token %q", t.Name)
		if isColorish(t) {
			if !colorsets {
				return fmt.Errorf("token %q is a %s token, and Swift colours read colorsets; add colorsets to platform %s's emit",
					t.Name, t.Type, sw.np.Platform.Name)
			}
			if !inColorsets[t.Name] {
				return fmt.Errorf("token %q goes to swift but not to colorsets, where its Swift colour is read from", t.Name)
			}
		}
		m, err := sw.axesOf(t)
		if err != nil {
			return err
		}
		m.name = name
		for _, a := range m.axes {
			needed[a.Name] = true
		}
		sw.members = append(sw.members, m)
	}
	for _, a := range sw.p.Config.Axes {
		if !needed[a.Name] {
			continue
		}
		sw.params = append(sw.params, a)
		enum, prop := Pascal(a.Name), SwiftIdent(Camel(a.Name))
		if !validSwift(prop) {
			return fmt.Errorf("axis %s has no Swift property name (%s)", a.Name, prop)
		}
		if !swiftTypeName(enum) {
			return fmt.Errorf("axis %s would be Swift enum %s, which is not a Swift type name the file can declare", a.Name, enum)
		}
		for _, n := range []string{enum, strings.Trim(prop, "`")} {
			if prev, dup := owner[n]; dup {
				return fmt.Errorf("axis %s's Swift name %s is already taken by %s", a.Name, n, prev)
			}
			owner[n] = "axis " + a.Name
		}
		cases := map[string]string{}
		for _, v := range a.Values {
			c := SwiftIdent(Camel(v))
			if !validSwift(c) {
				return fmt.Errorf("axis %s value %q has no Swift case name", a.Name, v)
			}
			if prev, dup := cases[c]; dup {
				return fmt.Errorf("axis %s values %q and %q both make Swift case %s", a.Name, prev, v, c)
			}
			cases[c] = v
		}
	}
	return nil
}

// axesOf works out the axes t's Swift value switches on: each runtime axis
// it varies on, and appearance when a non-colour part of it does. With a
// shared file, a token whose non-colour part varies per fan-out value
// cannot be written.
func (sw *swifter) axesOf(t *tokens.Token) (swiftMember, error) {
	m := swiftMember{t: t, varies: sw.np.Varies(sw.p.Tokens, t)}
	np := sw.np
	on := func(a *config.Axis) bool { return a != nil && slices.Contains(m.varies, a.Name) }
	needs := func(a *config.Axis) (bool, error) {
		switch {
		case !on(a) || t.Type == "color":
			return false, nil
		case t.Type == "shadow":
			return sw.geometryVaries(m, *a)
		}
		return true, nil
	}
	if !sw.perFile {
		if per, err := needs(np.FanOut); err != nil {
			return m, err
		} else if per {
			return m, fmt.Errorf("token %q varies per %[2]s; put {%[2]s} in the swift out path", t.Name, np.FanOut.Name)
		}
	}
	appearance, err := needs(np.Appearance)
	if err != nil {
		return m, err
	}
	for _, a := range sw.p.Config.Axes {
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

// geometryVaries reports whether a shadow's offsets, blur, spread, inset
// or layer count change with axis x, its colours aside (their colorsets
// carry appearance, and every app's catalog its own colour).
func (sw *swifter) geometryVaries(m swiftMember, x config.Axis) (bool, error) {
	var axes []config.Axis
	for _, a := range sw.allAxes() {
		if slices.Contains(m.varies, a.Name) {
			axes = append(axes, a)
		}
	}
	for _, ctx := range combos(axes) {
		ref, err := sw.geometry(m.t, ctx)
		if err != nil {
			return false, err
		}
		for _, v := range x.Values {
			c := maps.Clone(ctx)
			c[x.Name] = v
			g, err := sw.geometry(m.t, c)
			if err != nil {
				return false, err
			}
			if g != ref {
				return true, nil
			}
		}
	}
	return false, nil
}

// allAxes are the axes a Swift value can depend on: fan-out, appearance
// and runtime.
func (sw *swifter) allAxes() []config.Axis {
	var out []config.Axis
	if sw.np.FanOut != nil {
		out = append(out, *sw.np.FanOut)
	}
	if sw.np.Appearance != nil {
		out = append(out, *sw.np.Appearance)
	}
	return append(out, sw.np.Runtime...)
}

func (sw *swifter) geometry(t *tokens.Token, ctx config.Context) (string, error) {
	v, err := sw.np.Value(sw.p.Tokens, t.Name, ctx)
	if err != nil {
		return "", fmt.Errorf("token %q%s: %w", t.Name, inContext(ctx), err)
	}
	layers, ok := v.Lit.([]tokens.Shadow)
	if !ok {
		return "", fmt.Errorf("token %q%s: a shadow token resolves to %T", t.Name, inContext(ctx), v.Lit)
	}
	var b strings.Builder
	for _, l := range layers {
		fmt.Fprintf(&b, "%v %v %v %v %v;", l.OffsetX, l.OffsetY, l.Blur, l.Spread, l.Inset)
	}
	return b.String(), nil
}

// combos lists every combination of the axes' values, the first axis
// varying slowest, values in config order. No axes give one empty context.
func combos(axes []config.Axis) []config.Context {
	out := []config.Context{{}}
	for _, a := range axes {
		var next []config.Context
		for _, c := range out {
			for _, v := range a.Values {
				n := maps.Clone(c)
				n[a.Name] = v
				next = append(next, n)
			}
		}
		out = next
	}
	return out
}

// file is the Swift source of output o.
func (sw *swifter) file(o Output) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// " + header(sw.p) + "\n\nimport SwiftUI\n\n")
	fmt.Fprintf(&b, "public struct %s: Sendable {\n", sw.typ)
	for _, a := range sw.params {
		fmt.Fprintf(&b, "    public enum %s: String, CaseIterable, Sendable {\n", Pascal(a.Name))
		for _, v := range a.Values {
			c := SwiftIdent(Camel(v))
			if strings.Trim(c, "`") == v {
				fmt.Fprintf(&b, "        case %s\n", c)
			} else {
				fmt.Fprintf(&b, "        case %s = %s\n", c, swiftString(v))
			}
		}
		b.WriteString("    }\n\n")
	}
	b.WriteString(swiftSupport)
	if len(sw.params) > 0 {
		var args []string
		for _, a := range sw.params {
			fmt.Fprintf(&b, "    public var %s: %s\n", SwiftIdent(Camel(a.Name)), Pascal(a.Name))
			args = append(args, fmt.Sprintf("%s: %s = .%s", SwiftIdent(Camel(a.Name)), Pascal(a.Name), SwiftIdent(Camel(base(a)))))
		}
		fmt.Fprintf(&b, "\n    public init(%s) {\n", strings.Join(args, ", "))
		for _, a := range sw.params {
			p := SwiftIdent(Camel(a.Name))
			fmt.Fprintf(&b, "        self.%s = %s\n", strings.Trim(p, "`"), p)
		}
		b.WriteString("    }\n")
	} else {
		b.WriteString("    public init() {}\n")
	}
	// Static lets run together; a blank line sets off each switch.
	prevSwitch := true
	for _, m := range sw.members {
		if prevSwitch || len(m.axes) > 0 {
			b.WriteString("\n")
		}
		prevSwitch = len(m.axes) > 0
		if err := sw.member(&b, m, o); err != nil {
			return nil, err
		}
	}
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

// swiftSupport declares the value types the members use.
const swiftSupport = `    /// A cubic Bézier timing curve's control points.
    public struct Bezier: Sendable, Equatable {
        public let x1: Double
        public let y1: Double
        public let x2: Double
        public let y2: Double

        public init(x1: Double, y1: Double, x2: Double, y2: Double) {
            self.x1 = x1
            self.y1 = y1
            self.x2 = x2
            self.y2 = y2
        }
    }

    /// One layer of a shadow, in points.
    public struct Shadow: Sendable, Equatable {
        public let color: Color
        public let x: CGFloat
        public let y: CGFloat
        public let blur: CGFloat
        public let spread: CGFloat
        public let inset: Bool

        public init(color: Color, x: CGFloat, y: CGFloat, blur: CGFloat, spread: CGFloat, inset: Bool) {
            self.color = color
            self.x = x
            self.y = y
            self.blur = blur
            self.spread = spread
            self.inset = inset
        }
    }

`

// member writes m's declaration for output o.
func (sw *swifter) member(b *strings.Builder, m swiftMember, o Output) error {
	typ, ok := swiftTypes[m.t.Type]
	if !ok {
		return fmt.Errorf("token %q: a %s token has no Swift type", m.t.Name, m.t.Type)
	}
	if len(m.axes) == 0 {
		expr, err := sw.expr(m, config.Context{}, o)
		if err != nil {
			return err
		}
		if m.t.Type == "color" {
			fmt.Fprintf(b, "    public static let %s = %s\n", m.name, expr)
		} else {
			fmt.Fprintf(b, "    public static let %s: %s = %s\n", m.name, typ, expr)
		}
		// The instance forwarder lets tokens.x read every member alike,
		// whether or not it varies.
		fmt.Fprintf(b, "    public var %s: %s { Self.%s }\n", m.name, typ, m.name)
		return nil
	}
	var subject []string
	for _, a := range m.axes {
		subject = append(subject, SwiftIdent(Camel(a.Name)))
	}
	fmt.Fprintf(b, "    public var %s: %s {\n", m.name, typ)
	if len(subject) == 1 {
		fmt.Fprintf(b, "        switch %s {\n", subject[0])
	} else {
		fmt.Fprintf(b, "        switch (%s) {\n", strings.Join(subject, ", "))
	}
	for _, ctx := range combos(m.axes) {
		expr, err := sw.expr(m, ctx, o)
		if err != nil {
			return err
		}
		var pats []string
		for _, a := range m.axes {
			pats = append(pats, "."+SwiftIdent(Camel(ctx[a.Name])))
		}
		pat := pats[0]
		if len(pats) > 1 {
			pat = "(" + strings.Join(pats, ", ") + ")"
		}
		fmt.Fprintf(b, "        case %s: return %s\n", pat, expr)
	}
	b.WriteString("        }\n    }\n")
	return nil
}

var swiftTypes = map[string]string{
	"color": "Color", "dimension": "CGFloat", "number": "Double", "duration": "TimeInterval",
	"cubicBezier": "Bezier", "fontFamily": "[String]", "fontWeight": "Font.Weight", "shadow": "[Shadow]",
}

// expr is m's value as a Swift expression in switch context ctx of output
// o. Colours name their colorset: ColorsetName gets the runtime axes m
// varies on, and the output's fan-out value when the file is per value.
func (sw *swifter) expr(m swiftMember, ctx config.Context, o Output) (string, error) {
	full := maps.Clone(ctx)
	csCtx := config.Context{}
	for _, a := range sw.np.Runtime {
		if v, ok := ctx[a.Name]; ok {
			csCtx[a.Name] = v
		}
	}
	if sw.perFile {
		full[sw.np.FanOut.Name] = o.FanValue
		csCtx[sw.np.FanOut.Name] = o.FanValue
	}
	v, err := sw.np.Value(sw.p.Tokens, m.t.Name, full)
	if err != nil {
		return "", fmt.Errorf("token %q%s: %w", m.t.Name, inContext(full), err)
	}
	bad := func() (string, error) {
		return "", fmt.Errorf("token %q%s: a %s token resolves to %T", m.t.Name, inContext(full), m.t.Type, v.Lit)
	}
	switch m.t.Type {
	case "color":
		if _, ok := v.Lit.(color.OKLCH); !ok {
			return bad()
		}
		return fmt.Sprintf("Color(%s)", swiftString(ColorsetName(sw.np, m.t, csCtx))), nil
	case "dimension":
		d, ok := v.Lit.(tokens.Dim)
		if !ok {
			return bad()
		}
		return swiftPoints(d)
	case "number":
		f, ok := v.Lit.(float64)
		if !ok {
			return bad()
		}
		return swiftFloat(f), nil
	case "duration":
		d, ok := v.Lit.(tokens.Duration)
		if !ok {
			return bad()
		}
		s := d.V
		if d.Unit == "ms" {
			s /= 1000
		} else if d.Unit != "s" {
			return "", fmt.Errorf("token %q: duration unit %q has no Swift spelling", m.t.Name, d.Unit)
		}
		return swiftFloat(s), nil
	case "cubicBezier":
		c, ok := v.Lit.([4]float64)
		if !ok {
			return bad()
		}
		return fmt.Sprintf("Bezier(x1: %s, y1: %s, x2: %s, y2: %s)", swiftFloat(c[0]), swiftFloat(c[1]), swiftFloat(c[2]), swiftFloat(c[3])), nil
	case "fontFamily":
		fam, ok := v.Lit.([]string)
		if !ok {
			return bad()
		}
		q := make([]string, len(fam))
		for i, f := range fam {
			q[i] = swiftString(f)
		}
		return "[" + strings.Join(q, ", ") + "]", nil
	case "fontWeight":
		w, ok := v.Lit.(float64)
		if !ok {
			return bad()
		}
		return swiftWeight(w), nil
	case "shadow":
		layers, ok := v.Lit.([]tokens.Shadow)
		if !ok {
			return bad()
		}
		ns := namespace(sw.np, csCtx)
		parts := make([]string, len(layers))
		for i, l := range layers {
			name := ns + "/" + colorsetName(sw.np, fmt.Sprintf("%sLayer%d", Pascal(m.t.Name), i+1), csCtx)
			var dims [4]string
			for j, d := range []tokens.Dim{l.OffsetX, l.OffsetY, l.Blur, l.Spread} {
				if dims[j], err = swiftPoints(d); err != nil {
					return "", fmt.Errorf("token %q: %w", m.t.Name, err)
				}
			}
			parts[i] = fmt.Sprintf("Shadow(color: Color(%s), x: %s, y: %s, blur: %s, spread: %s, inset: %t)",
				swiftString(name), dims[0], dims[1], dims[2], dims[3], l.Inset)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	return "", fmt.Errorf("token %q: a %s token has no Swift type", m.t.Name, m.t.Type)
}

// swiftPoints is a dimension in points: px as is, rem × 16.
func swiftPoints(d tokens.Dim) (string, error) {
	switch d.Unit {
	case "px":
		return swiftFloat(d.V), nil
	case "rem":
		return swiftFloat(d.V * 16), nil
	}
	return "", fmt.Errorf("dimension unit %q has no Swift spelling", d.Unit)
}

// swiftFloat is v rounded to 4 decimals (num), always with a decimal
// point, so a literal reads as floating point wherever it stands.
func swiftFloat(v float64) string {
	s := num(v)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// swiftWeights are Font.Weight's members by CSS weight, 100 to 900.
var swiftWeights = []string{".ultraLight", ".thin", ".light", ".regular", ".medium", ".semibold", ".bold", ".heavy", ".black"}

// swiftWeight snaps a CSS font weight to the nearest Font.Weight (a
// weight halfway between two takes the heavier).
func swiftWeight(w float64) string {
	i := int(math.Floor(w/100+0.5)) - 1
	return swiftWeights[max(0, min(i, len(swiftWeights)-1))]
}

// swiftString is s as a Swift string literal.
func swiftString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u{%x}`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
