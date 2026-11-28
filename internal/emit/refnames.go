// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"fmt"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
)

// ReferenceNames maps every name format's emitter writes for platform pl
// to the token it stands for, or to "" for a generated symbol that is no
// token (a type, an enum or its cases, a parameter, a ColorValues val, a
// theme dictionary key). It runs the emitter itself and reads the names it
// wrote, so the two cannot drift, and it fails with the emitter's own error
// when the emitter would fail.
//
//   - css: each custom property, as --<name>.
//   - swift: the <Type> struct's members, static and instance (a backticked
//     member also unescaped); the type, Bezier and Shadow and their fields,
//     each runtime parameter's enum, cases and property, and init map to "".
//   - kotlin: the <Type> class's members and the <Type>Colors slots (a
//     member wins over a slot of the same name); <Type>, <Type>Colors,
//     <Type>ColorValues, Companion, Shadow and its fields, each parameter's
//     enum class, constants and property, colors, and the ColorValues and
//     scheme vals map to "".
//   - xaml: every resource key (base, runtime-suffixed, shadow brush and
//     part keys); the theme dictionary keys map to "".
//
// Any other format is an error.
func ReferenceNames(p *project.Project, pl config.Platform, format string) (map[string]string, error) {
	r := refs{}
	switch format {
	case "css":
		_, toks, err := buildCSS(p, pl)
		if err != nil {
			return nil, err
		}
		for _, t := range toks {
			r.token(cssProperty(t), t.Name)
		}
	case "swift":
		_, sw, err := buildSwift(p, pl)
		if err != nil {
			return nil, err
		}
		r.swift(sw)
	case "kotlin":
		_, k, err := buildKotlin(p, pl)
		if err != nil {
			return nil, err
		}
		r.kotlin(k)
	case "xaml":
		_, x, items, err := buildXAML(p, pl)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			r.token(it.Key, it.T.Name)
		}
		for _, th := range x.themeList() {
			r.symbol(th.key)
		}
	default:
		return nil, fmt.Errorf("format %s has no reference names", format)
	}
	return r, nil
}

// refs is a ReferenceNames map under construction: a token entry wins
// over a symbol one, and the first token entry for a name stands.
type refs map[string]string

func (r refs) token(name, tok string) {
	if r[name] == "" {
		r[name] = tok
	}
}

func (r refs) symbol(names ...string) {
	for _, n := range names {
		if _, ok := r[n]; !ok {
			r[n] = ""
		}
	}
}

// ident adds an escaped identifier and, when it is backticked, its bare
// spelling too.
func (r refs) ident(name, tok string) {
	r.token(name, tok)
	if bare := strings.Trim(name, "`"); bare != name {
		r.token(bare, tok)
	}
}

func (r refs) symbolIdent(names ...string) {
	for _, n := range names {
		r.symbol(n, strings.Trim(n, "`"))
	}
}

// swiftSupportFields are the stored properties of swiftSupport's Bezier
// and Shadow.
var swiftSupportFields = []string{"x1", "y1", "x2", "y2", "color", "x", "y", "blur", "spread", "inset"}

func (r refs) swift(sw *swifter) {
	for _, m := range sw.members {
		r.ident(m.name, m.t.Name)
	}
	r.symbol(sw.typ, "Bezier", "Shadow", "init")
	r.symbol(swiftSupportFields...)
	for _, a := range sw.params {
		r.symbolIdent(swiftEnum(a), swiftProp(a))
		for _, v := range a.Values {
			r.symbolIdent(swiftCase(v))
		}
	}
}

// kotlinShadowFields are the properties of kotlinShadowDecl.
var kotlinShadowFields = []string{"color", "x", "y", "blur", "spread", "inset"}

func (r refs) kotlin(k *kotliner) {
	for _, m := range k.members {
		r.ident(m.name, m.t.Name)
	}
	for _, s := range k.slotList {
		r.ident(s.Name, s.Token.Name)
	}
	r.symbol(k.typ, k.valuesType())
	if len(k.slotList) > 0 {
		r.symbol(k.colorsType(), "colors")
		for _, av := range k.appearances() {
			r.symbolIdent(kotlinValuesVal(av))
		}
	}
	if len(k.np.Platform.Material3Roles()) > 0 {
		for _, av := range k.appearances() {
			if kind, ok := kotlinSchemeKind(av); ok {
				r.symbol(kotlinSchemeVal(kind))
			}
		}
	}
	if slices.ContainsFunc(k.members, kotlinMember.static) {
		r.symbol("Companion")
	}
	if slices.ContainsFunc(k.members, func(m kotlinMember) bool { return m.t.Type == "shadow" }) {
		r.symbol("Shadow")
		r.symbol(kotlinShadowFields...)
	}
	for _, a := range k.params {
		r.symbolIdent(kotlinEnum(a), kotlinProp(a))
		for _, v := range a.Values {
			r.symbolIdent(kotlinConst(v))
		}
	}
}

// NamingAffixes are the literal parts of format's naming template around
// its placeholder ({name}, {camel} or {Pascal}): for swift and kotlin, of
// the member part after the last ".". Design{Pascal} gives ("Design", "");
// DesignTokens.{camel} gives ("", ""). A template with no placeholder is
// all prefix.
func NamingAffixes(cfg *config.Config, format string) (prefix, suffix string) {
	tmpl := cfg.NamingFor(format)
	if i := strings.LastIndex(tmpl, "."); i >= 0 {
		tmpl = tmpl[i+1:]
	}
	for _, ph := range []string{"{name}", "{camel}", "{Pascal}"} {
		if before, after, ok := strings.Cut(tmpl, ph); ok {
			return before, after
		}
	}
	return tmpl, ""
}
