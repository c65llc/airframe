// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokens

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
)

func testConfig() *config.Config {
	return &config.Config{Version: 1, Axes: []config.Axis{
		{Name: "app", Values: []string{"slides", "wiki"}},
		{Name: "appearance", Values: []string{"light", "dark"}},
		{Name: "density", Values: []string{"compact", "standard", "relaxed"}},
		{Name: "swatch", Source: "swatches.yaml"},
	}}
}

func parse(t *testing.T, files map[string]string) (*Set, []diag.Diagnostic) {
	t.Helper()
	raw := map[string][]byte{}
	for k, v := range files {
		raw[k] = []byte(v)
	}
	s, ds := Parse(testConfig(), raw)
	require.NotNil(t, s)
	return s, ds
}

func mustParse(t *testing.T, files map[string]string) *Set {
	t.Helper()
	s, ds := parse(t, files)
	require.Empty(t, ds)
	return s
}

func rules(ds []diag.Diagnostic) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Rule)
	}
	return out
}

func only(t *testing.T, ds []diag.Diagnostic, rule string) diag.Diagnostic {
	t.Helper()
	require.Len(t, ds, 1, "diagnostics: %v", ds)
	d := ds[0]
	assert.Equal(t, rule, d.Rule)
	assert.Equal(t, diag.Error, d.Severity)
	assert.Equal(t, diag.DocsURL(rule), d.Docs)
	return d
}

const specToken = `{ "tabstrip": { "h": {
  "$type": "dimension",
  "$value": { "value": 36, "unit": "px" },
  "$extensions": { "com.c65llc.airframe": {
    "tier": "component", "component": "tabstrip",
    "density": { "compact": 32, "standard": 36, "relaxed": 40 },
    "emit": ["css", "swift", "kotlin", "xaml"] } } } } }
`

func TestGroupTypeInherited(t *testing.T) {
	s := mustParse(t, map[string]string{"color.tokens.json": `{
  "accent": { "$type": "color",
    "base": { "$value": { "colorSpace": "oklch", "components": [0.6, 0.1, 250] } } }
}`})
	tok := s.Tokens["accent-base"]
	require.NotNil(t, tok)
	assert.Equal(t, "color", tok.Type)
	assert.Equal(t, []string{"accent", "base"}, tok.Path)
	assert.Equal(t, color.OKLCH{L: 0.6, C: 0.1, H: 250, Alpha: 1}, tok.Default.Lit)
	assert.Equal(t, 3, tok.Default.Line)
	assert.Equal(t, 3, tok.Line)
	assert.Equal(t, "color.tokens.json", tok.File)
}

func TestNameJoin(t *testing.T) {
	s := mustParse(t, map[string]string{"t.tokens.json": specToken})
	require.Contains(t, s.Tokens, "tabstrip-h")
	assert.Equal(t, []string{"tabstrip-h"}, s.Order)
	tok := s.Tokens["tabstrip-h"]
	assert.Equal(t, "component", tok.Tier)
	assert.Equal(t, "tabstrip", tok.Component)
	assert.Equal(t, []string{"css", "swift", "kotlin", "xaml"}, tok.Emit)
}

func TestNameInvalid(t *testing.T) {
	s, ds := parse(t, map[string]string{"t.tokens.json": `{
  "Tab Strip": { "h": { "$type": "number", "$value": 1 } }
}`})
	d := only(t, ds, "token-name-invalid")
	assert.Equal(t, "t.tokens.json", d.File)
	assert.Equal(t, 2, d.Line)
	assert.Contains(t, d.Message, `"Tab Strip-h"`)
	assert.Empty(t, s.Tokens)
}

func TestDensityShorthand(t *testing.T) {
	s := mustParse(t, map[string]string{"t.tokens.json": specToken})
	tok := s.Tokens["tabstrip-h"]
	assert.Equal(t, Dim{36, "px"}, tok.Default.Lit)
	require.Contains(t, tok.Modes, config.ModeKey("density:compact"))
	assert.Equal(t, Dim{32, "px"}, tok.Modes["density:compact"].Lit)
	assert.Equal(t, Dim{40, "px"}, tok.Modes["density:relaxed"].Lit)
	assert.Equal(t, 6, tok.Modes["density:compact"].Line)
}

func TestFileLevelMode(t *testing.T) {
	s := mustParse(t, map[string]string{
		"base.tokens.json": `{ "surface": { "$type": "color",
  "bg": { "$value": { "colorSpace": "oklch", "components": [0.98, 0, 0] } },
  "fg": { "$value": { "colorSpace": "oklch", "components": [0.2, 0, 0] } } } }`,
		"themes/dark.tokens.json": `{
  "$extensions": { "com.c65llc.airframe": { "mode": "appearance:dark" } },
  "surface": {
    "bg": { "$value": { "colorSpace": "oklch", "components": [0.15, 0, 0] } },
    "fg": { "$value": { "colorSpace": "oklch", "components": [0.9, 0, 0] } } } }`,
	})
	assert.Equal(t, []string{"surface-bg", "surface-fg"}, s.Order)
	for _, name := range s.Order {
		tok := s.Tokens[name]
		assert.Equal(t, "base.tokens.json", tok.File)
		assert.Equal(t, "color", tok.Type, "type comes from the base file")
		require.Contains(t, tok.Modes, config.ModeKey("appearance:dark"), name)
	}
	assert.InDelta(t, 0.15, s.Tokens["surface-bg"].Modes["appearance:dark"].Lit.(color.OKLCH).L, 1e-9)
}

func TestModeOverrideConflict(t *testing.T) {
	_, ds := parse(t, map[string]string{
		"a.tokens.json": `{ "x": { "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:dark": 2 } } } } }`,
		"themes/dark.tokens.json": `{
  "$extensions": { "com.c65llc.airframe": { "mode": "appearance:dark" } },
  "x": { "$value": 3 } }`,
	})
	d := only(t, ds, "mode-override-conflict")
	assert.Contains(t, d.Message, "a.tokens.json")
	assert.Contains(t, d.Message, "themes/dark.tokens.json")
	assert.Contains(t, d.Message, "appearance:dark")
}

func TestModeUnknown(t *testing.T) {
	_, ds := parse(t, map[string]string{"a.tokens.json": `{ "x": { "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:sepia": 2 } } } } }`})
	d := only(t, ds, "mode-unknown")
	assert.Contains(t, d.Message, "appearance:sepia")
	assert.Contains(t, d.Message, `unknown value "sepia" for axis appearance`)
	assert.Equal(t, 2, d.Line)
}

func TestTypeUnsupported(t *testing.T) {
	s, ds := parse(t, map[string]string{"a.tokens.json": `{ "g": { "$type": "gradient", "$value": [] } }`})
	d := only(t, ds, "type-unsupported")
	assert.Contains(t, d.Message, "gradient")
	assert.Empty(t, s.Tokens)
}

func TestBadColorShape(t *testing.T) {
	_, ds := parse(t, map[string]string{"a.tokens.json": `{ "c": { "$type": "color",
  "$value": { "colorSpace": "srgb", "components": [1, 0, 0] } } }`})
	d := only(t, ds, "schema-invalid")
	assert.Equal(t, `colorSpace must be "oklch"`, d.Message)
	assert.Equal(t, 2, d.Line)
	assert.Equal(t, "c.$value.colorSpace", d.Path)
}

func TestJSONSyntaxAndBOM(t *testing.T) {
	_, ds := parse(t, map[string]string{"a.tokens.json": "{\n  \"x\": 1,\n  \"x\": 2\n}"})
	d := only(t, ds, "json-syntax")
	assert.Equal(t, 3, d.Line)
	assert.Equal(t, `duplicate key "x"`, d.Message)

	s := mustParse(t, map[string]string{"b.tokens.json": "\xef\xbb\xbf{ \"x\": { \"$type\": \"number\", \"$value\": 2 } }"})
	assert.Equal(t, 2.0, s.Tokens["x"].Default.Lit)
}

func TestLiteralShapes(t *testing.T) {
	s := mustParse(t, map[string]string{"a.tokens.json": `{
  "font": { "$type": "fontFamily", "$value": ["Inter", "system-ui"] },
  "mono": { "$type": "fontFamily", "$value": "Menlo" },
  "weight": { "$type": "fontWeight", "$value": "semi-bold" },
  "fast": { "$type": "duration", "$value": { "value": 120, "unit": "ms" } },
  "ease": { "$type": "cubicBezier", "$value": [0.2, 0, 0, 1] },
  "lift": { "$type": "shadow", "$value": {
    "color": { "colorSpace": "oklch", "components": [0, 0, 0], "alpha": 0.2 },
    "offsetX": { "value": 0, "unit": "px" }, "offsetY": { "value": 1, "unit": "px" },
    "blur": { "value": 2, "unit": "px" }, "spread": { "value": 0, "unit": "px" } } },
  "body": { "$type": "fontFamily", "$value": "{font}" }
}`})
	assert.Equal(t, []string{"Inter", "system-ui"}, s.Tokens["font"].Default.Lit)
	assert.Equal(t, []string{"Menlo"}, s.Tokens["mono"].Default.Lit)
	assert.Equal(t, 600.0, s.Tokens["weight"].Default.Lit)
	assert.Equal(t, Duration{120, "ms"}, s.Tokens["fast"].Default.Lit)
	assert.Equal(t, [4]float64{0.2, 0, 0, 1}, s.Tokens["ease"].Default.Lit)
	sh := s.Tokens["lift"].Default.Lit.([]Shadow)
	require.Len(t, sh, 1)
	assert.Equal(t, color.OKLCH{Alpha: 0.2}, sh[0].Color)
	assert.Equal(t, Dim{2, "px"}, sh[0].Blur)
	assert.Equal(t, "font", s.Tokens["body"].Default.Alias)
}

func TestExtensionFields(t *testing.T) {
	s, ds := parse(t, map[string]string{"a.tokens.json": `{
  "x": { "$type": "number", "$value": 1, "$description": "the x",
    "$extensions": { "com.c65llc.airframe": {
      "shared": true, "retired": true, "chrome": "toolbar",
      "exempt": { "touch-floor": "decorative", "other": "", "bad": 1 } } } }
}`})
	d := only(t, ds, "schema-invalid")
	assert.Equal(t, "exemption from bad needs a reason string", d.Message)
	tok := s.Tokens["x"]
	assert.True(t, tok.Shared)
	assert.True(t, tok.Retired)
	assert.Equal(t, "toolbar", tok.Chrome)
	assert.Equal(t, "the x", tok.Description)
	assert.Equal(t, map[string]string{"touch-floor": "decorative", "other": ""}, tok.Exempt,
		"an empty reason is kept for lint to report as exemption-without-reason")
	assert.Equal(t, "a.tokens.json", s.FileOf(tok.Default))
}

func TestOrderAcrossFiles(t *testing.T) {
	s := mustParse(t, map[string]string{
		"b.tokens.json": `{ "z": { "$type": "number", "$value": 1 }, "a": { "$type": "number", "$value": 1 } }`,
		"a.tokens.json": `{ "m": { "$type": "number", "$value": 1 } }`,
	})
	assert.Equal(t, []string{"m", "z", "a"}, s.Order)
}

func TestDiagnosticsDeterministic(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		files[n+".tokens.json"] = `{ "Bad": { "$type": "number", "$value": 1 } }`
	}
	_, first := parse(t, files)
	for range 10 {
		_, again := parse(t, files)
		assert.Equal(t, first, again)
	}
	var names []string
	for _, d := range first {
		names = append(names, d.File)
	}
	assert.True(t, strings.Join(names, ",") == "a.tokens.json,b.tokens.json,c.tokens.json,d.tokens.json,e.tokens.json", names)
}

func TestSchemaDescribesTokens(t *testing.T) {
	var doc any
	require.NoError(t, json.Unmarshal([]byte(specToken), &doc))
	assert.Empty(t, schema.Validate("tokens", doc))

	var dark any
	require.NoError(t, json.Unmarshal([]byte(`{
  "$extensions": { "com.c65llc.airframe": { "mode": "appearance:dark" } },
  "surface": { "$type": "color",
    "bg": { "$value": { "colorSpace": "oklch", "components": [0.15, 0, 0], "alpha": 0.5 } },
    "fg": { "$value": "{surface.ink}",
      "$extensions": { "com.c65llc.airframe": { "exempt": { "touch-floor": "icon only" },
        "modes": { "app:slides+density:compact": "{x}" } } } } } }`), &dark))
	assert.Empty(t, schema.Validate("tokens", dark))

	var badDoc any
	require.NoError(t, json.Unmarshal([]byte(`{ "c": { "$type": "color",
  "$value": { "colorSpace": "srgb", "components": [1, 0, 0] },
  "$extensions": { "com.c65llc.airframe": { "exempt": { "x": "" }, "tierz": "a" } } } }`), &badDoc))
	assert.NotEmpty(t, schema.Validate("tokens", badDoc))
}

func TestModeFileEdges(t *testing.T) {
	s, ds := parse(t, map[string]string{
		"base.tokens.json": `{ "x": { "$type": "number", "$value": 1 }, "x-y": { "$type": "number", "$value": 1 },
  "y": { "$type": "number", "$value": 1 } }`,
		"dup.tokens.json": `{ "x": { "y": { "$type": "number", "$value": 2 } } }`,
		"themes/bad.tokens.json": `{ "$extensions": { "com.c65llc.airframe": { "mode": "appearance:sepia" } },
  "x": { "$value": 9 } }`,
		"themes/dark.tokens.json": `{ "$extensions": { "com.c65llc.airframe": { "mode": "appearance:dark" } },
  "y": { "$type": "dimension", "$value": { "value": 1, "unit": "px" } },
  "only": { "$type": "number", "$value": 5 } }`,
	})
	assert.ElementsMatch(t, []string{"mode-override-conflict", "mode-unknown", "schema-invalid", "schema-invalid"}, rules(ds))
	assert.Empty(t, s.Tokens["x"].Modes, "a file with an unknown mode contributes nothing")
	assert.Empty(t, s.Tokens["y"].Modes, "a type clash is not merged")
	var noBase []diag.Diagnostic
	for _, d := range ds {
		if strings.Contains(d.Message, "no $value") {
			noBase = append(noBase, d)
		}
	}
	require.Len(t, noBase, 1, "a token with mode values but no base $value is reported")
	assert.Equal(t, `token "only" has mode values but no $value`, noBase[0].Message)
	assert.Equal(t, "themes/dark.tokens.json", noBase[0].File)
	assert.Equal(t, 3, noBase[0].Line)
	assert.Equal(t, []string{"x", "x-y", "y", "only"}, s.Order)
}

func TestOverrideDescriptiveFields(t *testing.T) {
	s, ds := parse(t, map[string]string{
		"base.tokens.json": `{ "x": { "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "tier": "core" } } } }`,
		"themes/dark.tokens.json": `{ "$extensions": { "com.c65llc.airframe": { "mode": "appearance:dark" } },
  "x": { "$value": 2,
    "$extensions": { "com.c65llc.airframe": {
      "exempt": { "touch-floor": "icon" },
      "tier": "component",
      "modes": { "app:slides": 3 } } } } }`,
	})
	require.Equal(t, []string{"schema-invalid", "schema-invalid"}, rules(ds))
	for i, line := range []int{4, 5} {
		assert.Equal(t, `only $value, modes and density may appear on an override of "x"`, ds[i].Message)
		assert.Equal(t, "themes/dark.tokens.json", ds[i].File)
		assert.Equal(t, line, ds[i].Line)
	}
	assert.Equal(t, "x.$extensions.com.c65llc.airframe.exempt", ds[0].Path)
	assert.Equal(t, "x.$extensions.com.c65llc.airframe.tier", ds[1].Path)
	tok := s.Tokens["x"]
	assert.Equal(t, "core", tok.Tier)
	assert.Nil(t, tok.Exempt)
	assert.Equal(t, 2.0, tok.Modes["appearance:dark"].Lit)
	assert.Equal(t, 3.0, tok.Modes["app:slides+appearance:dark"].Lit)
}

// TestModeBaseValue: a value whose only web selector is bare :root is the
// axis base; a mode key naming it is rejected at the key's line.
func TestModeBaseValue(t *testing.T) {
	cfg, cds := config.Load([]byte(`version: 1
modes:
  appearance: { values: [light, dark], web: { light: ":root", dark: [":root[data-theme='dark']"] } }
  app: { values: [drafts, slides, wiki], base: drafts, web: { attr: data-app } }
`))
	require.Empty(t, cds)
	s, ds := Parse(cfg, map[string][]byte{"a.tokens.json": []byte(`{ "x": { "$type": "number", "$value": 0,
  "$extensions": { "com.c65llc.airframe": { "modes": {
    "app:slides+appearance:light": 1,
    "appearance:light": 2,
    "appearance:dark": 3 } } } } }`)})
	require.Len(t, ds, 2)
	assert.Equal(t, "mode-base-value", ds[0].Rule)
	assert.Equal(t, 3, ds[0].Line)
	assert.Equal(t, "appearance:light is the base appearance; write the key without it", ds[0].Message)
	assert.Equal(t, "mode-base-value", ds[1].Rule)
	assert.Equal(t, 4, ds[1].Line)
	assert.Equal(t, []config.ModeKey{"appearance:dark"}, sortedKeys(s.Tokens["x"].Modes), "rejected keys are dropped")
}

// baseDensityConfig has a density axis whose base is standard.
func baseDensityConfig() *config.Config {
	return &config.Config{Version: 1, Axes: []config.Axis{
		{Name: "density", Values: []string{"compact", "standard", "relaxed"}, Base: "standard"},
	}}
}

// TestDensityShorthandBaseEqualAllowed: the density shorthand may list the
// base value when it equals $value; it is dropped, not kept as a mode key.
func TestDensityShorthandBaseEqualAllowed(t *testing.T) {
	s, ds := Parse(baseDensityConfig(), map[string][]byte{"t.tokens.json": []byte(specToken)})
	require.Empty(t, ds)
	assert.Equal(t, []config.ModeKey{"density:compact", "density:relaxed"}, sortedKeys(s.Tokens["tabstrip-h"].Modes))

	// An alias equal to the $value alias is equal too.
	s, ds = Parse(baseDensityConfig(), map[string][]byte{"t.tokens.json": []byte(`{
  "gap": { "$type": "dimension", "$value": { "value": 4, "unit": "px" } },
  "pad": { "$type": "dimension", "$value": "{gap}",
    "$extensions": { "com.c65llc.airframe": { "density": { "standard": "{gap}", "relaxed": { "value": 8, "unit": "px" } } } } } }`)})
	require.Empty(t, ds)
	assert.Equal(t, []config.ModeKey{"density:relaxed"}, sortedKeys(s.Tokens["pad"].Modes))
}

// TestDensityShorthandBaseDiffers: a base entry that differs from $value
// is mode-base-value at its line, and is dropped.
func TestDensityShorthandBaseDiffers(t *testing.T) {
	s, ds := Parse(baseDensityConfig(), map[string][]byte{"t.tokens.json": []byte(`{ "h": {
  "$type": "dimension",
  "$value": { "value": 40, "unit": "px" },
  "$extensions": { "com.c65llc.airframe": { "density": {
    "compact": 44,
    "standard": 48 } } } } }`)})
	d := only(t, ds, "mode-base-value")
	assert.Equal(t, 6, d.Line)
	assert.Equal(t, "h.$extensions.com.c65llc.airframe.density.standard", d.Path)
	assert.Contains(t, d.Message, "density:standard is the base density")
	assert.Equal(t, []config.ModeKey{"density:compact"}, sortedKeys(s.Tokens["h"].Modes))

	// An explicit modes key naming the base stays an error even when equal.
	_, ds = Parse(baseDensityConfig(), map[string][]byte{"t.tokens.json": []byte(`{ "h": {
  "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "modes": { "density:standard": 1 } } } } }`)})
	only(t, ds, "mode-base-value")
}

func TestTokenNamesParsed(t *testing.T) {
	s := mustParse(t, map[string]string{
		"a.tokens.json": `{ "x": { "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "names": { "swift": "spacingXs", "kotlin": "SpacingXs" } } } } }`,
	})
	assert.Equal(t, map[string]string{"swift": "spacingXs", "kotlin": "SpacingXs"}, s.Tokens["x"].Names)
}

func TestTokenNamesBadValue(t *testing.T) {
	for name, names := range map[string]string{
		"empty":   `{ "swift": "" }`,
		"number":  `{ "swift": 3 }`,
		"unknown": `{ "swfit": "x" }`,
		"notobj":  `["swift"]`,
	} {
		t.Run(name, func(t *testing.T) {
			s, ds := parse(t, map[string]string{
				"a.tokens.json": "{ \"x\": { \"$type\": \"number\", \"$value\": 1,\n  \"$extensions\": { \"com.c65llc.airframe\": { \"names\": " + names + " } } } }",
			})
			d := only(t, ds, "schema-invalid")
			assert.Equal(t, 2, d.Line)
			assert.Empty(t, s.Tokens["x"].Names)
		})
	}
}

func TestTokenNamesOnOverrideRejected(t *testing.T) {
	s, ds := parse(t, map[string]string{
		"base.tokens.json": `{ "x": { "$type": "number", "$value": 1 } }`,
		"themes/dark.tokens.json": `{ "$extensions": { "com.c65llc.airframe": { "mode": "appearance:dark" } },
  "x": { "$value": 2,
    "$extensions": { "com.c65llc.airframe": { "names": { "swift": "x2" } } } } }`,
	})
	d := only(t, ds, "schema-invalid")
	assert.Equal(t, `only $value, modes and density may appear on an override of "x"`, d.Message)
	assert.Equal(t, 4, d.Line)
	assert.Nil(t, s.Tokens["x"].Names)
}
