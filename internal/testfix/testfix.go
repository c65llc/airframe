// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package testfix holds project fixtures shared by more than one package's
// tests. It is an ordinary package so tests in any package can import it.
package testfix

import (
	"fmt"
	"strings"

	"github.com/c65llc/airframe/internal/project"
)

// big576Config has five loaded axes of 2, 3, 6, 8 and 2 values: 576
// contexts. swatch lists its values here rather than deferring them to a
// file, so accentGroup expands to both app and swatch.
const big576Config = `version: 1
modes:
  appearance: { values: [light, dark], binding: runtime, web: { light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"] } }
  density:    { values: [compact, standard, relaxed], binding: runtime, web: { attr: data-density, also: { relaxed: "@media (pointer: coarse)" } } }
  app:        { values: [drafts, edit, review, slides, tasks, wiki], binding: build, web: { attr: data-app }, group: accent }
  swatch:     { values: [ember, moss, tide, plum, sand, slate, rose, ink], binding: runtime, web: { attr: data-accent-swatch }, group: accent }
  channel:    { values: [stable, next], binding: build, web: { attr: data-channel } }
accentGroups: { required: [accent, accent-ink, accent-contrast, accent-on-ink, accent-soft] }
`

// big576Invariants is the spec's four invariants, verbatim.
const big576Invariants = `- id: accent-hue-stable
  kind: same-hue-across-appearances
  tokens: ["accent", "accent-ink"]
  forEach: accentGroup
- id: danger-hue-reserved
  kind: hue-reserved
  hue: 25
  tolerance: 8
  only: ["danger", "danger-*"]
- id: selection-text-contrast
  kind: contrast
  fg: accent-ink
  bg: accent-soft
  min: 4.5
  forEach: [accentGroup, appearance]
- id: touch-floor
  kind: min-dimension
  match: "*-target*"
  min: 44
  densities: all
`

// ramps are the primitive colour ramps: name, hue and chroma. gray is
// achromatic; danger is the only ramp near the reserved hue.
var ramps = []struct {
	name   string
	hue, c float64
}{
	{"gray", 250, 0.005}, {"blue", 250, 0.12}, {"teal", 190, 0.1}, {"violet", 300, 0.12},
	{"green", 145, 0.12}, {"amber", 75, 0.12}, {"danger", 25, 0.14},
}

// components each own six tokens: three colours and three dimensions.
var components = []string{
	"tabstrip", "toolbar", "row", "button", "chip", "sidebar",
	"dialog", "menu", "tooltip", "card", "input", "badge",
}

func ext(fields string) string {
	return `"$extensions": { "com.c65llc.airframe": { ` + fields + ` } }`
}

func oklch(l, c, h float64) string {
	return fmt.Sprintf(`{ "colorSpace": "oklch", "components": [%g, %g, %g] }`, l, c, h)
}

func px(v int) string { return fmt.Sprintf(`{ "value": %d, "unit": "px" }`, v) }

// object joins entries into a JSON object, one per line.
func object(entries []string) string {
	return "{\n  " + strings.Join(entries, ",\n  ") + "\n}\n"
}

func primitives() string {
	var out []string
	for _, r := range ramps {
		var steps []string
		for i := 1; i <= 10; i++ {
			l := 0.97 - float64(i-1)*0.07
			c := r.c
			if i <= 2 {
				c = min(c, 0.04)
			}
			steps = append(steps, fmt.Sprintf(`"%d": { "$value": %s, %s }`, i, oklch(l, c, r.hue), ext(`"tier": "primitive"`)))
		}
		out = append(out, fmt.Sprintf(`"%s": { "$type": "color", %s }`, r.name, strings.Join(steps, ", ")))
	}
	var space, radius []string
	for i := 1; i <= 12; i++ {
		space = append(space, fmt.Sprintf(`"%d": { "$value": %s, %s }`, i, px(4*i), ext(`"tier": "primitive"`)))
	}
	for i := 1; i <= 4; i++ {
		radius = append(radius, fmt.Sprintf(`"%d": { "$value": %s, %s }`, i, px(2*i), ext(`"tier": "primitive"`)))
	}
	out = append(out,
		`"space": { "$type": "dimension", `+strings.Join(space, ", ")+` }`,
		`"radius": { "$type": "dimension", `+strings.Join(radius, ", ")+` }`)
	return object(out)
}

// semanticColor is a semantic colour aliasing light, with modes as
// "key": "{alias}" pairs.
func semanticColor(name, light, modes string) string {
	fields := `"tier": "semantic"`
	if modes != "" {
		fields += `, "modes": { ` + modes + ` }`
	}
	return fmt.Sprintf(`"%s": { "$type": "color", "$value": "{%s}", %s }`, name, light, ext(fields))
}

func semantics() string {
	var out []string
	neutral := []struct {
		name        string
		light, dark int
	}{
		{"surface", 1, 10}, {"surface-raised", 2, 9}, {"surface-sunken", 3, 8}, {"ink", 10, 1},
		{"ink-muted", 8, 3}, {"ink-faint", 6, 5}, {"border", 4, 7}, {"border-strong", 6, 5},
	}
	for _, n := range neutral {
		out = append(out, semanticColor(n.name, fmt.Sprintf("gray.%d", n.light),
			fmt.Sprintf(`"appearance:dark": "{gray.%d}"`, n.dark)))
	}
	accent := []struct {
		name        string
		light, dark int
	}{
		{"accent", 6, 5}, {"accent-ink", 9, 2}, {"accent-contrast", 7, 4},
		{"accent-on-ink", 1, 10}, {"accent-soft", 1, 10},
	}
	for _, a := range accent {
		out = append(out, semanticColor(a.name, fmt.Sprintf("blue.%d", a.light), fmt.Sprintf(
			`"appearance:dark": "{blue.%d}", "app:tasks": "{teal.%d}", "app:tasks+appearance:dark": "{teal.%d}"`,
			a.dark, a.light, a.dark)))
	}
	status := []struct{ name, ramp string }{
		{"danger", "danger"}, {"success", "green"}, {"warning", "amber"}, {"info", "violet"},
	}
	for _, s := range status {
		for _, role := range []struct {
			suffix      string
			light, dark int
		}{{"", 6, 5}, {"-ink", 9, 2}, {"-soft", 1, 10}} {
			out = append(out, semanticColor(s.name+role.suffix, fmt.Sprintf("%s.%d", s.ramp, role.light),
				fmt.Sprintf(`"appearance:dark": "{%s.%d}"`, s.ramp, role.dark)))
		}
	}
	out = append(out, semanticColor("channel-badge", "blue.6", `"channel:next": "{amber.6}"`))
	for i := 1; i <= 12; i++ {
		out = append(out, fmt.Sprintf(`"gap-%d": { "$type": "dimension", "$value": "{space.%d}", %s }`, i, i, ext(`"tier": "semantic"`)))
	}
	for i := 1; i <= 4; i++ {
		out = append(out, fmt.Sprintf(`"corner-%d": { "$type": "dimension", "$value": "{radius.%d}", %s }`, i, i, ext(`"tier": "semantic"`)))
	}
	return object(out)
}

func componentTokens() string {
	var out []string
	for _, c := range components {
		own := fmt.Sprintf(`"tier": "component", "component": %q`, c)
		for _, col := range []struct{ suffix, alias string }{{"bg", "surface-raised"}, {"ink", "ink"}, {"border", "border"}} {
			out = append(out, fmt.Sprintf(`"%s-%s": { "$type": "color", "$value": "{%s}", %s }`, c, col.suffix, col.alias, ext(own)))
		}
		for _, d := range []struct {
			suffix                     string
			compact, standard, relaxed int
		}{{"h", 28, 32, 36}, {"target", 44, 48, 52}, {"pad", 4, 8, 12}} {
			out = append(out, fmt.Sprintf(`"%s-%s": { "$type": "dimension", "$value": %s, %s }`, c, d.suffix, px(d.standard),
				ext(own+fmt.Sprintf(`, "density": { "compact": %d, "standard": %d, "relaxed": %d }`, d.compact, d.standard, d.relaxed))))
		}
	}
	return object(out)
}

// Big576 is a project of five loaded axes (appearance 2, density 3, app 6,
// swatch 8, channel 2: 576 contexts), 200 tokens (86 primitive, 42
// semantic, 72 component) using seven distinct mode keys, and the spec's
// four invariants. It loads with no diagnostics and every invariant holds.
func Big576() project.MemFS {
	return project.MemFS{
		"airframe.yaml":                       big576Config,
		"design-system/invariants.yaml":       big576Invariants,
		"design-system/primitive.tokens.json": primitives(),
		"design-system/semantic.tokens.json":  semantics(),
		"design-system/component.tokens.json": componentTokens(),
	}
}
