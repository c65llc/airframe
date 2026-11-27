// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/testfix"
	"github.com/c65llc/airframe/internal/tokens"
)

func TestCSSGolden(t *testing.T) {
	out := buildMinimal(t)
	got := string(out[minimalCSS])
	assert.Equal(t, string(golden(t, minimalCSS)), got)
	assert.Contains(t, got, "  --accent-ink: var(--accent);\n", "an alias stays an alias")
	assert.Contains(t, got, "@media (prefers-color-scheme: dark) {\n  :root:not([data-theme='light']) {\n    --accent: var(--amber-300);\n")
	assert.Contains(t, got, ":root[data-theme='dark'] {\n  --accent: var(--amber-300);\n")
}

func TestCSSValueFormats(t *testing.T) {
	grey := color.OKLCH{L: 0.2, C: 0.012, H: 250, Alpha: 0.2}
	for _, tc := range []struct {
		lit  any
		want string
	}{
		{color.OKLCH{L: 0.63, C: 0.13, H: 75, Alpha: 0.5}, "oklch(63% 0.13 75 / 0.5)"},
		{color.OKLCH{L: 0.63, C: 0.13, H: 75, Alpha: 1}, "oklch(63% 0.13 75)"},
		{color.OKLCH{L: 0.123456, C: 0.0000001, H: 359.99999, Alpha: 1}, "oklch(12.3456% 0 360)"},
		{tokens.Dim{V: 1.25, Unit: "rem"}, "1.25rem"},
		{tokens.Dim{V: 0, Unit: "px"}, "0px"},
		{tokens.Duration{V: 120, Unit: "ms"}, "120ms"},
		{float64(700), "700"},
		{0.4, "0.4"},
		{[]string{"IBM Plex Sans", "system-ui"}, `"IBM Plex Sans", system-ui`},
		{[]string{`Odd "Name"`, "monospace"}, `"Odd \"Name\"", monospace`},
		{[4]float64{0.2, 0, 0, 1}, "cubic-bezier(0.2, 0, 0, 1)"},
		{[]tokens.Shadow{{Color: grey, OffsetY: tokens.Dim{V: 1, Unit: "px"}, Blur: tokens.Dim{V: 2, Unit: "px"},
			OffsetX: tokens.Dim{Unit: "px"}, Spread: tokens.Dim{Unit: "px"}}}, "0 1px 2px 0 oklch(20% 0.012 250 / 0.2)"},
		{[]tokens.Shadow{
			{Color: grey, OffsetY: tokens.Dim{V: 1, Unit: "px"}, Blur: tokens.Dim{V: 2, Unit: "px"}},
			{Color: grey, Inset: true, Blur: tokens.Dim{V: 4, Unit: "rem"}},
		}, "0 1px 2px 0 oklch(20% 0.012 250 / 0.2), inset 0 0 4rem 0 oklch(20% 0.012 250 / 0.2)"},
	} {
		assert.Equal(t, tc.want, cssValue(&tokens.Value{Lit: tc.lit}), "%#v", tc.lit)
	}
	assert.Equal(t, "var(--accent)", cssValue(&tokens.Value{Alias: "accent"}))
}

// big576With is the 576-context config with one extra tokens file.
func big576With(t *testing.T, tokensJSON string) *project.Project {
	t.Helper()
	fsys := project.MemFS{
		"airframe.yaml":               testfix.Big576()["airframe.yaml"],
		"design-system/x.tokens.json": tokensJSON,
	}
	return loadFS(t, fsys)
}

func cssOf(t *testing.T, p *project.Project) string {
	t.Helper()
	out, err := CSS(p, config.Platform{Name: "web", Emit: []string{"css"}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	for _, b := range out {
		return string(b)
	}
	return ""
}

func TestScopesCrossProduct(t *testing.T) {
	css := cssOf(t, big576With(t, `{ "x": { "$type": "number", "$value": 0,
  "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:dark+app:slides": 1 } } } } }`))
	// The key's pairs are walked in its canonical (axis-name) order: app,
	// then appearance.
	assert.Contains(t, css, "@media (prefers-color-scheme: dark) {\n  :root[data-app='slides']:not([data-theme='light']) {\n    --x: 1;\n  }\n}\n")
	assert.Contains(t, css, "\n:root[data-app='slides'][data-theme='dark'] {\n  --x: 1;\n}\n")
	assert.Equal(t, 2, strings.Count(css, "--x: 1;"), "exactly two scopes")
}

func TestDensityAlsoMedia(t *testing.T) {
	css := cssOf(t, big576With(t, `{ "x": { "$type": "number", "$value": 0,
  "$extensions": { "com.c65llc.airframe": { "modes": { "density:relaxed": 2 } } } } }`))
	assert.Contains(t, css, "\n:root[data-density='relaxed'] {\n  --x: 2;\n}\n")
	assert.Contains(t, css, "\n@media (pointer: coarse) {\n  :root:not([data-density]) {\n    --x: 2;\n  }\n}\n")
}

func TestNestedMedia(t *testing.T) {
	css := cssOf(t, big576With(t, `{ "x": { "$type": "number", "$value": 0,
  "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:dark+density:relaxed": 3 } } } } }`))
	assert.Contains(t, css, "@media (prefers-color-scheme: dark) {\n  @media (pointer: coarse) {\n    :root:not([data-theme='light']):not([data-density]) {\n      --x: 3;\n    }\n  }\n}\n")
	assert.Equal(t, 4, strings.Count(css, "--x: 3;"), "2 appearance options x 2 density options")
}

func TestBlockOrder(t *testing.T) {
	css := cssOf(t, big576With(t, `{
  "b": { "$type": "number", "$value": 0, "$extensions": { "com.c65llc.airframe": { "modes": { "channel:next": 1, "app:slides+appearance:dark": 2 } } } },
  "a": { "$type": "number", "$value": 0, "$extensions": { "com.c65llc.airframe": { "modes": { "app:edit": 3 } } } } }`))
	i := func(s string) int {
		t.Helper()
		n := strings.Index(css, s)
		require.GreaterOrEqual(t, n, 0, s)
		return n
	}
	assert.Less(t, i(":root {\n  --a: 0;\n  --b: 0;\n}"), i("[data-app='edit']"))
	assert.Less(t, i("[data-app='edit']"), i("[data-channel='next']"))
	assert.Less(t, i("[data-channel='next']"), i("[data-app='slides']"))
}

// TestAlsoFallbackOnlyWhenAttrUnset: the also media condition applies only
// while the attribute is unset, so an explicit data-density always beats
// the pointer fallback, whatever other axis the key adds.
func TestAlsoFallbackOnlyWhenAttrUnset(t *testing.T) {
	css := cssOf(t, big576With(t, `{ "x": { "$type": "number", "$value": 0,
  "$extensions": { "com.c65llc.airframe": { "modes": { "density:compact": 1, "density:relaxed": 2, "appearance:dark+density:relaxed": 3 } } } } }`))
	assert.Contains(t, css, "\n@media (pointer: coarse) {\n  :root:not([data-density]) {\n    --x: 2;\n  }\n}\n")
	assert.Contains(t, css, "\n@media (pointer: coarse) {\n  :root[data-theme='dark']:not([data-density]) {\n    --x: 3;\n  }\n}\n")
	assert.NotRegexp(t, `(?m)^\s*:root( |\[data-theme='dark'\] )\{\n\s*--x: [23];`, css, "no unconditioned fallback scope")

	cfg := strings.Replace(testfix.Big576()["airframe.yaml"], "web: {attr: data-density, also:", "web: {also:", 1)
	p := loadFS(t, project.MemFS{"airframe.yaml": cfg, "design-system/x.tokens.json": `{ "x": { "$type": "number", "$value": 0,
  "$extensions": { "com.c65llc.airframe": { "modes": { "density:relaxed": 2 } } } } }`})
	_, ds := Build(p, nil)
	_, err := CSS(p, config.Platform{Name: "web", Emit: []string{"css"}})
	require.Error(t, err)
	assert.Empty(t, ds, "Big576 has no platforms")
	ce, ok := err.(*configError)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "modes.density.web.also.relaxed", ce.path)
}

// TestNoBlockForBaseValue: a key naming a base value (its only selector is
// bare :root) is rejected at parse, so CSS never writes a block for it.
func TestNoBlockForBaseValue(t *testing.T) {
	p, ds, err := project.Load(project.MemFS{
		"airframe.yaml": testfix.Big576()["airframe.yaml"],
		"design-system/x.tokens.json": `{ "x": { "$type": "number", "$value": 0,
  "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:light": 1, "app:slides+appearance:light": 2 } } } } }`,
	})
	require.NoError(t, err)
	require.Len(t, ds, 2)
	for _, d := range ds {
		assert.Equal(t, "mode-base-value", d.Rule)
	}
	css := cssOf(t, p)
	assert.Equal(t, 1, strings.Count(css, "{"), css)
	assert.Contains(t, css, ":root {\n  --x: 0;\n}\n")
}

func TestCSSAliasToUnemittedToken(t *testing.T) {
	p := big576With(t, `{
  "x": { "$type": "number", "$value": 1, "$extensions": { "com.c65llc.airframe": { "emit": ["json"] } } },
  "y": { "$type": "number", "$value": "{x}" } }`)
	_, err := CSS(p, config.Platform{Name: "web", Emit: []string{"css"}})
	require.Error(t, err)
	assert.Equal(t, "--y aliases x, which is not emitted to css", err.Error())

	fsys := memFS(t, minimalDir)
	fsys["design-system/tokens/extra.tokens.json"] = `{
  "x": { "$type": "number", "$value": 1, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "emit": ["json"] } } },
  "y": { "$type": "number", "$value": "{x}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } } }`
	out, ds := Build(loadFS(t, fsys), nil)
	require.Len(t, ds, 1)
	assert.Equal(t, "emit-failed", ds[0].Rule)
	assert.Contains(t, ds[0].Message, "--y aliases x, which is not emitted to css")
	assert.NotContains(t, out, minimalCSS)
	assert.Contains(t, out, minimalJSON)
}

func TestCSSUnformattableValueFails(t *testing.T) {
	p := big576With(t, `{ "x": { "$type": "number", "$value": 1 } }`)
	p.Tokens.Tokens["x"].Modes["appearance:dark"] = &tokens.Value{Lit: struct{}{}}
	_, err := CSS(p, config.Platform{Name: "web", Emit: []string{"css"}})
	require.Error(t, err)
	assert.Equal(t, "--x: cannot write its value as CSS (mode appearance:dark)", err.Error())
}
