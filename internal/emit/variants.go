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

// Variant is what one emitted name stands for: Token's name ("" for a
// generated symbol that is no token) and Key, a stable descriptor of which
// of the token's names it is. The same Variant in a renamed project is the
// renamed spelling of the same name.
//
// Key is "" for the token's base name. Otherwise it is ":"-joined parts:
//
//   - density=compact: a runtime suffix, its axes in config order joined
//     by "+" (density=compact+contrast=high);
//   - layer1, layer1.offsetX: a shadow layer's colour, and one of its
//     geometry parts (offsetX, offsetY, blur, spread);
//   - slot: a Kotlin colour slot, followed by its layer and suffix parts
//     (slot:density=compact, slot:layer1:density=compact);
//   - for a symbol: values:<appearance> (a ColorValues val), scheme:<kind>
//     (a Material 3 ColorScheme val), theme:<key> (a XAML theme
//     dictionary), or symbol:<name> (any other: the type, an axis enum or
//     property, Shadow, Companion …).
type Variant struct {
	Token string
	Key   string
}

// ReferenceVariants maps every name ReferenceNames lists to its Variant.
// ReferenceNames is its projection onto Variant.Token, so the two list the
// same names and resolve collisions the same way: a token entry wins over a
// symbol one, and the first token entry for a name stands.
func ReferenceVariants(p *project.Project, pl config.Platform, format string) (map[string]Variant, error) {
	r := vrefs{m: map[string]Variant{}, axes: p.Config.Axes}
	switch format {
	case "css":
		_, toks, err := buildCSS(p, pl)
		if err != nil {
			return nil, err
		}
		for _, t := range toks {
			r.token(cssProperty(t), Variant{t.Name, ""})
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
			r.token(it.Key, Variant{it.T.Name, r.xamlKey(it)})
		}
		for _, th := range x.themeList() {
			r.symbolAs(th.key, "theme:"+th.key)
		}
	default:
		return nil, fmt.Errorf("format %s has no reference names", format)
	}
	return r.m, nil
}

// VariantNames inverts a ReferenceVariants map: each Variant's name. A Variant
// spelled more than one way (a backticked member and its bare spelling)
// takes the escaped one, which is valid wherever the name is, then the
// smallest.
func VariantNames(variants map[string]Variant) map[Variant]string {
	out := map[Variant]string{}
	for n, v := range variants {
		prev, ok := out[v]
		if !ok || better(n, prev) {
			out[v] = n
		}
	}
	return out
}

func better(n, prev string) bool {
	nb, pb := strings.HasPrefix(n, "`"), strings.HasPrefix(prev, "`")
	if nb != pb {
		return nb
	}
	return n < prev
}

// vrefs is a ReferenceVariants map under construction.
type vrefs struct {
	m    map[string]Variant
	axes []config.Axis // config order, for suffix keys
}

func (r vrefs) token(name string, v Variant) {
	if r.m[name].Token == "" {
		r.m[name] = v
	}
}

func (r vrefs) symbolAs(name, key string) {
	if _, ok := r.m[name]; !ok {
		r.m[name] = Variant{"", key}
	}
}

func (r vrefs) symbol(names ...string) {
	for _, n := range names {
		r.symbolAs(n, "symbol:"+strings.Trim(n, "`"))
	}
}

// ident adds an escaped identifier and, when it is backticked, its bare
// spelling too.
func (r vrefs) ident(name string, v Variant) {
	r.token(name, v)
	if bare := strings.Trim(name, "`"); bare != name {
		r.token(bare, v)
	}
}

func (r vrefs) symbolIdent(names ...string) {
	for _, n := range names {
		r.symbol(n, strings.Trim(n, "`"))
	}
}

// ctxKey is ctx's suffix key: axis=value for each axis ctx sets, in
// config order, joined by "+".
func (r vrefs) ctxKey(ctx config.Context) string {
	var parts []string
	for _, a := range r.axes {
		if v, ok := ctx[a.Name]; ok {
			parts = append(parts, a.Name+"="+v)
		}
	}
	return strings.Join(parts, "+")
}

func joinKey(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), ":")
}

func layerKey(layer int, part string) string {
	if layer < 0 {
		return ""
	}
	k := fmt.Sprintf("layer%d", layer+1)
	if part != "" {
		k += "." + strings.ToLower(part[:1]) + part[1:]
	}
	return k
}

func (r vrefs) xamlKey(it xamlItem) string {
	return joinKey(layerKey(it.Layer, it.Part), r.ctxKey(it.Ctx))
}

func (r vrefs) swift(sw *swifter) {
	for _, m := range sw.members {
		r.ident(m.name, Variant{m.t.Name, ""})
	}
	r.symbol(sw.typ, "Bezier", "Shadow", "init")
	for _, a := range sw.params {
		r.symbolIdent(swiftEnum(a), swiftProp(a))
	}
}

func (r vrefs) kotlin(k *kotliner) {
	for _, m := range k.members {
		r.ident(m.name, Variant{m.t.Name, ""})
	}
	for _, s := range k.slotList {
		r.ident(s.Name, Variant{s.Token.Name, joinKey("slot", layerKey(s.Layer, ""), r.ctxKey(s.Ctx))})
	}
	r.symbol(k.typ, k.valuesType())
	if len(k.slotList) > 0 {
		r.symbol(k.colorsType(), "colors")
		for _, av := range k.appearances() {
			n := kotlinValuesVal(av)
			r.symbolAs(n, "values:"+av)
			r.symbolAs(strings.Trim(n, "`"), "values:"+av)
		}
	}
	if len(k.np.Platform.Material3Roles()) > 0 {
		for _, av := range k.appearances() {
			if kind, ok := kotlinSchemeKind(av); ok {
				r.symbolAs(kotlinSchemeVal(kind), "scheme:"+kind)
			}
		}
	}
	if slices.ContainsFunc(k.members, kotlinMember.static) {
		r.symbol("Companion")
	}
	if slices.ContainsFunc(k.members, func(m kotlinMember) bool { return m.t.Type == "shadow" }) {
		r.symbol("Shadow")
	}
	for _, a := range k.params {
		r.symbolIdent(kotlinEnum(a), kotlinProp(a))
	}
}
