// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
)

const testConfig = `version: 1
modes:
  appearance: { values: [light, dark], base: light }
  density:    { values: [compact, standard, relaxed], base: standard }
  app:        { values: [drafts, slides, wiki], base: drafts, group: accent }
  swatch:     { values: from brands/swatches.tokens.json, group: accent }
accentGroups: { required: [accent, accent-ink, accent-contrast, accent-on-ink, accent-soft] }
`

// load builds a project from token files (paths under design-system/) and
// the test config, failing on any load diagnostic.
func load(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	return loadWith(t, testConfig, files)
}

func loadWith(t *testing.T, cfg string, files map[string]string) *project.Project {
	t.Helper()
	fsys := project.MemFS{"airframe.yaml": cfg}
	for k, v := range files {
		fsys["design-system/"+k] = v
	}
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	require.NotNil(t, p)
	return p
}

// oklch is a colour literal with the given hue.
func oklch(h float64) string {
	return fmt.Sprintf(`{ "colorSpace": "oklch", "components": [0.6, 0.12, %g] }`, h)
}

// ext wraps Airframe extension fields for a token.
func ext(fields string) string {
	return `"$extensions": { "com.c65llc.airframe": { ` + fields + ` } }`
}

// ruleFindings runs one rule over p and returns its findings and tally.
func ruleFindings(t *testing.T, p *project.Project, id string) ([]diag.Diagnostic, diag.Tally) {
	t.Helper()
	for _, r := range Rules() {
		if r.ID == id {
			return r.Run(p, &Context{})
		}
	}
	t.Fatalf("no rule %s", id)
	return nil, diag.Tally{}
}

func messages(ds []diag.Diagnostic) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Message)
	}
	return out
}

// primitives is a primitive tier both other fixtures alias.
const primitives = `"moss": { "$type": "color",
    "1": { "$value": ` + `{ "colorSpace": "oklch", "components": [0.6, 0.12, 140] }` + `, ` + `"$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`

func TestTokenUnclassified(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{
  "plain": { "$type": "number", "$value": 1 },
  "box": { "$type": "number", "$value": 2, ` + ext(`"tier": "component"`) + ` },
  "ok": { "$type": "number", "$value": 3, ` + ext(`"tier": "component", "component": "tabstrip"`) + ` },
  "base": { "$type": "number", "$value": 4, ` + ext(`"tier": "primitive"`) + ` }
}`})
	ds, tally := ruleFindings(t, p, "token-unclassified")
	assert.Equal(t, []string{
		`token "plain" has no tier`,
		`component token "box" names no component`,
	}, messages(ds))
	assert.Equal(t, "design-system/a.tokens.json", ds[0].File)
	assert.Equal(t, 2, ds[0].Line)
	assert.Equal(t, "plain", ds[0].Path)
	assert.Equal(t, 4, tally.Items)
	assert.Equal(t, 1, tally.Files)
	assert.Equal(t, "tokens", tally.Noun)
}

func TestTierViolation(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		wants []string
	}{
		{"primitive_alias", `
  "moss-2": { "$type": "color", "$value": "{moss.1}", ` + ext(`"tier": "primitive"`) + ` }`,
			[]string{`primitive token "moss-2" aliases "moss-1"; a primitive must be a literal`}},
		{"primitive_literal_ok", `
  "moss-2": { "$type": "color", "$value": ` + oklch(141) + `, ` + ext(`"tier": "primitive"`) + ` }`,
			nil},
		{"semantic_reads_component", `
  "ground": { "$type": "color", "$value": "{moss.1}", ` + ext(`"tier": "semantic"`) + ` },
  "tab-bg": { "$type": "color", "$value": "{ground}", ` + ext(`"tier": "component", "component": "tabstrip"`) + ` },
  "surface": { "$type": "color", "$value": "{ground}", ` + ext(`"tier": "semantic", "modes": { "appearance:dark": "{tab.bg}" }`) + ` }`,
			[]string{`semantic token "surface" aliases "tab-bg" (component tabstrip) (mode appearance:dark); a semantic token may read primitive or semantic tokens`}},
		{"component_reads_other_component", `
  "surface": { "$type": "color", "$value": "{moss.1}", ` + ext(`"tier": "semantic"`) + ` },
  "tool-bg": { "$type": "color", "$value": "{surface}", ` + ext(`"tier": "component", "component": "toolbar"`) + ` },
  "tab-bg": { "$type": "color", "$value": "{tool.bg}", ` + ext(`"tier": "component", "component": "tabstrip"`) + ` }`,
			[]string{`component token "tab-bg" (tabstrip) aliases "tool-bg" (component toolbar); a component token may read semantic tokens, its own component's tokens or shared tokens`}},
		{"component_reads_primitive", `
  "tab-bg": { "$type": "color", "$value": "{moss.1}", ` + ext(`"tier": "component", "component": "tabstrip"`) + ` }`,
			[]string{`component token "tab-bg" (tabstrip) aliases "moss-1" (primitive); a component token may read semantic tokens, its own component's tokens or shared tokens`}},
		{"component_reads_shared_ok", `
  "surface": { "$type": "color", "$value": "{moss.1}", ` + ext(`"tier": "semantic"`) + ` },
  "tool-bg": { "$type": "color", "$value": "{surface}", ` + ext(`"tier": "component", "component": "toolbar", "shared": true`) + ` },
  "tab-bg": { "$type": "color", "$value": "{tool.bg}", ` + ext(`"tier": "component", "component": "tabstrip"`) + ` },
  "tab-ink": { "$type": "color", "$value": "{tab.bg}", ` + ext(`"tier": "component", "component": "tabstrip"`) + ` }`,
			nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := load(t, map[string]string{"a.tokens.json": "{ " + primitives + "," + tc.body + "\n}"})
			ds, tally := ruleFindings(t, p, "tier-violation")
			assert.Equal(t, tc.wants, messages(ds))
			assert.Positive(t, tally.Items)
		})
	}
}

func TestLiteralInSemantic(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{ ` + primitives + `,
  "surface": { "$type": "color", "$value": "{moss.1}", ` + ext(`"tier": "semantic", "modes": { "appearance:dark": `+oklch(140)+` }`) + ` },
  "ink": { "$type": "color", "$value": ` + oklch(140) + `, ` + ext(`"tier": "semantic"`) + ` },
  "edge": { "$type": "color", "$value": "{moss.1}", ` + ext(`"tier": "semantic"`) + ` }
}`})
	ds, tally := ruleFindings(t, p, "literal-in-semantic")
	assert.Equal(t, []string{
		`semantic token "surface" is a literal (mode appearance:dark); alias a primitive instead`,
		`semantic token "ink" is a literal; alias a primitive instead`,
	}, messages(ds))
	assert.Equal(t, 3, tally.Items, "the semantic tokens")
}

func TestDensityMissing(t *testing.T) {
	cases := []struct {
		name string
		fx   string
		want []string
	}{
		{"partial", ext(`"tier": "component", "component": "tabstrip", "density": { "compact": 32 }`),
			[]string{`component dimension "tab-h" has no density value for relaxed; give every density a value or set chrome: "<reason>"`}},
		{"base_is_value_ok", ext(`"tier": "component", "component": "tabstrip", "density": { "compact": 32, "relaxed": 40 }`), nil},
		{"none", ext(`"tier": "component", "component": "tabstrip"`),
			[]string{`component dimension "tab-h" has no density value for compact, relaxed; give every density a value or set chrome: "<reason>"`}},
		{"all_three_ok", ext(`"tier": "component", "component": "tabstrip", "density": { "compact": 32, "standard": 36, "relaxed": 40 }`), nil},
		{"chrome_reason_ok", ext(`"tier": "component", "component": "tabstrip", "chrome": "window title bar is fixed by the OS"`), nil},
		{"blank_chrome", ext(`"tier": "component", "component": "tabstrip", "chrome": "  "`),
			[]string{`component dimension "tab-h" has no density value for compact, relaxed; give every density a value or set chrome: "<reason>"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := load(t, map[string]string{"a.tokens.json": `{
  "tab-h": { "$type": "dimension", "$value": { "value": 36, "unit": "px" }, ` + tc.fx + ` },
  "gap": { "$type": "dimension", "$value": { "value": 4, "unit": "px" }, ` + ext(`"tier": "semantic"`) + ` },
  "tab-op": { "$type": "number", "$value": 1, ` + ext(`"tier": "component", "component": "tabstrip"`) + ` }
}`})
			ds, tally := ruleFindings(t, p, "density-missing")
			assert.Equal(t, tc.want, messages(ds))
			assert.Equal(t, 1, tally.Items, "only component dimensions")
		})
	}
}

func TestDensityMissingWithoutDensityAxis(t *testing.T) {
	p := loadWith(t, "version: 1\n", map[string]string{"a.tokens.json": `{
  "tab-h": { "$type": "dimension", "$value": { "value": 36, "unit": "px" }, ` + ext(`"tier": "component", "component": "tabstrip"`) + ` } }`})
	ds, tally := ruleFindings(t, p, "density-missing")
	assert.Empty(t, ds)
	assert.Equal(t, "no density axis configured", tally.NotApplicable)
}

// accentToken is a semantic colour token with the given mode values.
func accentToken(name, modes string) string {
	return fmt.Sprintf(`"%s": { "$type": "color", "$value": %s, %s }`, name, oklch(250),
		ext(`"tier": "semantic", "modes": { `+modes+` }`))
}

func TestAccentGroupPartial(t *testing.T) {
	slides := `"app:slides": ` + oklch(30)
	full := `"app:wiki": ` + oklch(300)
	both := slides + ", " + full
	p := load(t, map[string]string{"a.tokens.json": `{
  ` + accentToken("accent", both) + `,
  ` + accentToken("accent-ink", both) + `,
  ` + accentToken("accent-contrast", full) + `,
  ` + accentToken("accent-on-ink", full) + `,
  ` + accentToken("accent-soft", full+`, "app:wiki+appearance:dark": `+oklch(300)) + `,
  ` + accentToken("other", `"appearance:dark": `+oklch(1)) + `
}`})
	ds, tally := ruleFindings(t, p, "accent-group-partial")
	require.Len(t, ds, 1)
	d := ds[0]
	assert.Equal(t, "app:slides sets 2 of 5 accent roles; missing accent-contrast, accent-on-ink, accent-soft", d.Message)
	assert.Equal(t, "design-system/a.tokens.json", d.File)
	assert.Equal(t, "accent", d.Path)
	assert.Equal(t, 2, d.Line)
	assert.Equal(t, 5, tally.Items, "the required roles")
}

func TestAccentGroupPartialModeFile(t *testing.T) {
	base := `{
  ` + accentToken("accent", `"app:wiki": `+oklch(300)) + `,
  ` + accentToken("accent-ink", `"app:wiki": `+oklch(300)) + `,
  ` + accentToken("accent-contrast", `"app:wiki": `+oklch(300)) + `,
  ` + accentToken("accent-on-ink", `"app:wiki": `+oklch(300)) + `,
  ` + accentToken("accent-soft", `"app:wiki": `+oklch(300)) + `
}`
	p := load(t, map[string]string{
		"a.tokens.json": base,
		"brands/slides.tokens.json": `{ "$extensions": { "com.c65llc.airframe": { "mode": "app:slides" } },
  "accent": { "$value": ` + oklch(30) + ` } }`,
	})
	ds, _ := ruleFindings(t, p, "accent-group-partial")
	require.Len(t, ds, 1)
	assert.Equal(t, "app:slides sets 1 of 5 accent roles; missing accent-ink, accent-contrast, accent-on-ink, accent-soft", ds[0].Message)
	assert.Equal(t, "design-system/brands/slides.tokens.json", ds[0].File, "the file that set the role")
	assert.Equal(t, 2, ds[0].Line)
}

func TestAccentGroupPartialNotConfigured(t *testing.T) {
	cfg := strings.Replace(testConfig, "accentGroups: { required: [accent, accent-ink, accent-contrast, accent-on-ink, accent-soft] }\n", "", 1)
	p := loadWith(t, cfg, map[string]string{"a.tokens.json": `{ ` + accentToken("accent", `"app:slides": `+oklch(30)) + ` }`})
	ds, tally := ruleFindings(t, p, "accent-group-partial")
	assert.Empty(t, ds)
	assert.Equal(t, "no accentGroups.required configured", tally.NotApplicable)
}

func TestPrefixMultipleHues(t *testing.T) {
	cases := []struct {
		name   string
		h1, h2 float64
		want   []string
	}{
		{"spans_10", 140, 150, []string{`color tokens prefixed "moss" span 10° of hue (moss-1 140°, moss-2 150°); the limit is 8°`}},
		{"within_8", 140, 146, nil},
		{"wraps_360", 356, 2, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := load(t, map[string]string{"a.tokens.json": `{ "moss": { "$type": "color",
  "1": { "$value": ` + oklch(tc.h1) + ` },
  "2": { "$value": ` + oklch(tc.h2) + ` } },
  "fern": { "$type": "color", "$value": ` + oklch(10) + ` } }`})
			ds, tally := ruleFindings(t, p, "prefix-multiple-hues")
			assert.Equal(t, tc.want, messages(ds))
			assert.Equal(t, 3, tally.Items)
			if tc.want != nil {
				assert.Equal(t, "moss.2", ds[0].Path)
				assert.Equal(t, 3, ds[0].Line)
			}
		})
	}
}

func TestPrefixMultipleHuesIgnoresAccentModesAndGreys(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{ "moss": { "$type": "color",
  "1": { "$value": ` + oklch(140) + `, ` + ext(`"modes": { "app:slides": `+oklch(30)+`, "app:slides+appearance:dark": `+oklch(30)+` }`) + ` },
  "2": { "$value": "{moss.1}" },
  "3": { "$value": { "colorSpace": "oklch", "components": [0.5, 0.01, 300] } },
  "4": { "$value": ` + oklch(144) + `, ` + ext(`"modes": { "appearance:dark": `+oklch(146)+` }`) + ` } } }`})
	ds, _ := ruleFindings(t, p, "prefix-multiple-hues")
	assert.Empty(t, ds)

	p = load(t, map[string]string{"a.tokens.json": `{ "moss": { "$type": "color",
  "1": { "$value": ` + oklch(140) + ` },
  "4": { "$value": ` + oklch(144) + `, ` + ext(`"modes": { "appearance:dark": `+oklch(160)+` }`) + ` } } }`})
	ds, _ = ruleFindings(t, p, "prefix-multiple-hues")
	assert.Equal(t, []string{`color tokens prefixed "moss" span 20° of hue (moss-1 140°, moss-4 160° in appearance:dark); the limit is 8°`}, messages(ds))
}

func TestPrefixMultipleHuesExemptTokenLeavesTheSpan(t *testing.T) {
	files := func(h3 float64) map[string]string {
		return map[string]string{"a.tokens.json": `{ "moss": { "$type": "color",
  "1": { "$value": ` + oklch(140) + ` },
  "2": { "$value": ` + oklch(144) + ` },
  "3": { "$value": ` + fmt.Sprintf(`{ "colorSpace": "oklch", "components": [0.6, 0.12, %g] }`, h3) + `, ` +
			ext(`"exempt": { "prefix-multiple-hues": "status tint shares the ramp name" }`) + ` } } }`}
	}
	res := Run(load(t, files(30)), nil, Options{})
	assert.Empty(t, byRule(res.Diagnostics, "prefix-multiple-hues"))
	assert.Equal(t, []Exemption{{Rule: "prefix-multiple-hues", File: "design-system/a.tokens.json", Line: 4,
		Marker: "exempt", Reason: "status tint shares the ramp name"}}, res.Exemptions)

	res = Run(load(t, files(142)), nil, Options{})
	assert.Empty(t, res.Exemptions, "an exemption that suppressed nothing is not recorded")
}
