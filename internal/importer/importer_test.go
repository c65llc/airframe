// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// smallConfig has one attribute-spelled axis with a bare-:root base.
const smallConfig = `version: 1
content: ds
modes:
  appearance: {values: [light, dark], web: {light: ":root", dark: ":root[data-theme='dark']"}}
platforms:
  web: {emit: css}
`

func loadConfig(t *testing.T, src string) *config.Config {
	t.Helper()
	cfg, ds := config.Load([]byte(src))
	require.Empty(t, ds)
	return cfg
}

func run(t *testing.T, cfgSrc, cssSrc string) Result {
	t.Helper()
	return ImportSources(loadConfig(t, cfgSrc), []Source{{File: "theme.css", Data: []byte(cssSrc)}})
}

func byRule(ds []diag.Diagnostic, rule string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range ds {
		if d.Rule == rule {
			out = append(out, d)
		}
	}
	return out
}

func errorsIn(ds []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range ds {
		if d.Severity == diag.Error {
			out = append(out, d)
		}
	}
	return out
}

// tokenFile decodes one written token file into name -> raw token object.
func tokenFile(t *testing.T, r Result, path string) map[string]map[string]any {
	t.Helper()
	data, ok := r.Files[path]
	require.True(t, ok, "missing %s in %v", path, slices.Sorted(maps.Keys(r.Files)))
	var out map[string]map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func airframeExt(t *testing.T, tok map[string]any) map[string]any {
	t.Helper()
	ext, ok := tok["$extensions"].(map[string]any)
	require.True(t, ok, "%v", tok)
	af, ok := ext[tokens.Namespace].(map[string]any)
	require.True(t, ok, "%v", ext)
	return af
}

func fixture(t *testing.T) ([]byte, Result) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "import")
	cfgData, err := os.ReadFile(filepath.Join(dir, "airframe.yaml"))
	require.NoError(t, err)
	cssData, err := os.ReadFile(filepath.Join(dir, "theme.css"))
	require.NoError(t, err)
	cfg := loadConfig(t, string(cfgData))
	return cfgData, ImportSources(cfg, []Source{{File: "theme.css", Data: cssData}})
}

func TestImportFixture(t *testing.T) {
	cfgData, r := fixture(t)
	require.Empty(t, errorsIn(r.Diagnostics), "%v", r.Diagnostics)
	assert.Empty(t, r.Diagnostics, "the fixture is clean: no warnings either")
	assert.Equal(t, 25, r.Tokens)
	assert.Equal(t, 38, r.Declarations)
	assert.Equal(t, 1, r.Ignored)

	prim := "design-system/tokens/primitive.tokens.json"
	sem := "design-system/tokens/semantic.tokens.json"
	assert.Len(t, r.Files, 2)
	for _, p := range []string{prim, sem} {
		got, err := format.File(p, r.Files[p])
		require.NoError(t, err)
		assert.Equal(t, string(r.Files[p]), string(got), "%s is canonical", p)
	}

	semToks := tokenFile(t, r, sem)
	ai := semToks["accent-ink"]
	require.NotNil(t, ai)
	assert.Equal(t, "color", ai["$type"])
	assert.Equal(t, "{accent}", ai["$value"])
	ext := airframeExt(t, ai)
	assert.Equal(t, "semantic", ext["tier"])
	modes := ext["modes"].(map[string]any)
	assert.Equal(t, "{gray-900}", modes["appearance:dark"])

	ink := semToks["ink"]
	assert.Equal(t, "The main text colour: body copy and headings.", ink["$description"])

	primToks := tokenFile(t, r, prim)
	// Colours are rounded to 6 decimals; a near-zero chroma is achromatic.
	assert.Equal(t, map[string]any{"colorSpace": "oklch", "components": []any{0.979129, 0.0, 0.0}},
		primToks["gray-50"]["$value"])
	sp := primToks["space-4"]
	require.NotNil(t, sp)
	assert.Equal(t, "dimension", sp["$type"])
	assert.Equal(t, map[string]any{"value": 1.0, "unit": "rem"}, sp["$value"])
	assert.Equal(t, "primitive", airframeExt(t, sp)["tier"])
	_, hasModes := airframeExt(t, sp)["modes"]
	assert.False(t, hasModes)

	gap := airframeExt(t, semToks["gap"])["modes"].(map[string]any)
	assert.Equal(t, map[string]any{"density:compact": "{space-1}", "density:relaxed": "{space-4}"}, gap)
	assert.Equal(t, "{blue-500}", airframeExt(t, semToks["accent"])["modes"].(map[string]any)["app:slides"])

	// First-appearance order within each file.
	assert.Equal(t, []string{"surface", "ink", "accent", "accent-ink", "gap", "border-w"}, keyOrder(t, r.Files[sem]))
	assert.Equal(t, "gray-50", keyOrder(t, r.Files[prim])[0])

	// The written files load as a project with no diagnostics.
	fsys := project.MemFS{config.File: string(cfgData)}
	for p, b := range r.Files {
		fsys[p] = string(b)
	}
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	require.NotNil(t, p)
	assert.Len(t, p.Tokens.Order, 25)
	tok := p.Tokens.Tokens["space-4"]
	require.NotNil(t, tok)
	assert.Equal(t, tokens.Dim{V: 1, Unit: "rem"}, tok.Default.Lit)
	assert.Equal(t, "primitive", tok.Tier)
	ai2 := p.Tokens.Tokens["accent-ink"]
	assert.Equal(t, "accent", ai2.Default.Alias)
	assert.Equal(t, "gray-900", ai2.Modes["appearance:dark"].Alias)
}

// keyOrder lists the top-level keys of a JSON object in file order.
func keyOrder(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var out []string
	depth := 0
	for {
		tk, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tk.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				depth++
			} else {
				depth--
			}
		case string:
			if depth == 1 && dec.More() {
				out = append(out, v)
				var skip json.RawMessage
				require.NoError(t, dec.Decode(&skip))
			}
		}
	}
	return out
}

func TestImportSelectorListSplits(t *testing.T) {
	r := run(t, smallConfig, ":root, :root[data-theme='dark'] { --x: 1px }\n")
	require.Empty(t, r.Diagnostics)
	x := tokenFile(t, r, "ds/tokens/primitive.tokens.json")["x"]
	assert.Equal(t, map[string]any{"value": 1.0, "unit": "px"}, x["$value"])
	assert.Equal(t, map[string]any{"appearance:dark": map[string]any{"value": 1.0, "unit": "px"}}, airframeExt(t, x)["modes"])
	_, hasSem := r.Files["ds/tokens/semantic.tokens.json"]
	assert.False(t, hasSem, "an empty file is not written")
}

func TestImportReportsUnsupportedSelector(t *testing.T) {
	r := run(t, smallConfig, ":root { --b: 1 }\n\n.dark { --a: 1 }\n")
	got := byRule(r.Diagnostics, "import-unsupported-selector")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, 3, got[0].Line)
	assert.Equal(t, "theme.css", got[0].File)
	assert.Equal(t, diag.Error, got[0].Severity)
	assert.Contains(t, got[0].Message, ".dark")
	assert.Equal(t, diag.DocsURL("import-unsupported-selector"), got[0].Docs)
	assert.Empty(t, r.Files)
}

func TestImportReportsUnsupportedMedia(t *testing.T) {
	r := run(t, smallConfig, ":root { --b: 1 }\n@media (min-width: 40em) { :root { --b: 2 } }\n")
	got := byRule(r.Diagnostics, "import-unsupported-media")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, 2, got[0].Line)
	assert.Empty(t, r.Files)
}

func TestImportUnmappedScope(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: 1 }\n:root[data-theme='sepia'] { --a: 2 }\n")
	got := byRule(r.Diagnostics, "import-unmapped-scope")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, 2, got[0].Line)
	assert.Contains(t, got[0].Message, ":root[data-theme='sepia']")
	assert.Contains(t, got[0].Message, "airframe.yaml")
	assert.Empty(t, r.Files)
}

func TestImportUnmappedScopeWithMedia(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: 1 }\n@media (prefers-contrast: more) { :root { --a: 2 } }\n")
	got := byRule(r.Diagnostics, "import-unmapped-scope")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Contains(t, got[0].Message, `"@media (prefers-contrast: more) :root"`)
}

func TestImportNoBase(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: 1 }\n:root[data-theme='dark'] { --b: 2 }\n")
	got := byRule(r.Diagnostics, "import-no-base")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, 2, got[0].Line)
	assert.Contains(t, got[0].Message, "--b")
	assert.Empty(t, r.Files)
}

func TestImportTypeConflict(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: 1px }\n:root[data-theme='dark'] { --a: #fff }\n")
	got := byRule(r.Diagnostics, "import-type-conflict")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Contains(t, got[0].Message, "dimension")
	assert.Contains(t, got[0].Message, "color")
	assert.Equal(t, 2, got[0].Line)
	assert.Empty(t, r.Files)
}

func TestImportTypeConflictThroughAlias(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: 1px; --c: #fff; --b: var(--a) }\n:root[data-theme='dark'] { --b: var(--c) }\n")
	got := byRule(r.Diagnostics, "import-type-conflict")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Contains(t, got[0].Message, "--b")
}

func TestImportAliasCycle(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: var(--b); --b: var(--c); --c: var(--a); --d: 1px }\n")
	got := byRule(r.Diagnostics, "import-type-conflict")
	require.Len(t, got, 1, "one report per cycle: %v", r.Diagnostics)
	assert.Contains(t, got[0].Message, "--a → --b → --c → --a")
	assert.Empty(t, r.Files)
}

func TestImportAliasChainTakesTargetType(t *testing.T) {
	r := run(t, smallConfig, ":root { --c: var(--b); --b: var(--a); --a: 2px }\n")
	require.Empty(t, r.Diagnostics)
	sem := tokenFile(t, r, "ds/tokens/semantic.tokens.json")
	assert.Equal(t, "dimension", sem["c"]["$type"])
	assert.Equal(t, "{b}", sem["c"]["$value"])
	assert.Equal(t, "dimension", sem["b"]["$type"])
}

func TestImportAliasMissingTarget(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: var(--nope) }\n")
	got := byRule(r.Diagnostics, "import-unsupported-value")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, "var(--nope) refers to a property the imported CSS does not declare", got[0].Message)
	assert.Empty(t, r.Files)
}

func TestImportUnsupportedValue(t *testing.T) {
	r := run(t, smallConfig, ":root {\n  --a: calc(1px + 2px);\n}\n")
	got := byRule(r.Diagnostics, "import-unsupported-value")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, 2, got[0].Line)
	assert.Empty(t, r.Files)
}

func TestImportNameInvalid(t *testing.T) {
	r := run(t, smallConfig, ":root { --Accent_Ink: #fff }\n")
	got := byRule(r.Diagnostics, "import-name-invalid")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Contains(t, got[0].Message, "--Accent_Ink")
	assert.Empty(t, r.Files)
}

func TestImportDuplicateWarns(t *testing.T) {
	r := run(t, smallConfig, ":root {\n  --a: 1px;\n  --a: 2px;\n}\n")
	got := byRule(r.Diagnostics, "import-duplicate")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, diag.Warn, got[0].Severity)
	assert.Equal(t, 3, got[0].Line)
	assert.Contains(t, got[0].Message, "theme.css:2")
	assert.Contains(t, got[0].Message, "theme.css:3")
	a := tokenFile(t, r, "ds/tokens/primitive.tokens.json")["a"]
	assert.Equal(t, map[string]any{"value": 2.0, "unit": "px"}, a["$value"], "the later one wins")
}

// Two spellings of one mode key carry the same value: that is the emitter's
// own shape, not a duplicate. Different values under them are reported.
func TestImportSpellingsOfOneKey(t *testing.T) {
	cfg := `version: 1
content: ds
modes:
  appearance: {values: [light, dark], web: {light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"]}}
`
	same := ":root { --a: #000 }\n@media (prefers-color-scheme: dark) { :root:not([data-theme='light']) { --a: #fff } }\n:root[data-theme='dark'] { --a: #ffffff }\n"
	r := run(t, cfg, same)
	assert.Empty(t, r.Diagnostics)
	differ := strings.Replace(same, "#ffffff", "#eee", 1)
	r = run(t, cfg, differ)
	got := byRule(r.Diagnostics, "import-duplicate")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, 3, got[0].Line)
	assert.NotEmpty(t, r.Files)
}

func TestImportFallbackDropped(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: 1px; --b: var(--a, 2px) }\n")
	got := byRule(r.Diagnostics, "import-fallback-dropped")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, diag.Warn, got[0].Severity)
	b := tokenFile(t, r, "ds/tokens/semantic.tokens.json")["b"]
	assert.Equal(t, "{a}", b["$value"])
}

func TestImportNothing(t *testing.T) {
	r := run(t, smallConfig, ":root { color: red }\n")
	got := byRule(r.Diagnostics, "import-nothing")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, diag.Error, got[0].Severity)
	assert.Empty(t, r.Files)
	assert.Equal(t, 1, r.Ignored)
}

func TestImportParseDiagnosticsPassThrough(t *testing.T) {
	r := run(t, smallConfig, ":root { --a: 1px }\n@layer x { :root { --b: 1px } }\n")
	got := byRule(r.Diagnostics, "import-unsupported-at-rule")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, 2, got[0].Line)
	assert.Empty(t, r.Files)
}

func TestImportDescriptionOnlyFromBase(t *testing.T) {
	r := run(t, smallConfig, ":root {\n  /* base note */\n  --a: 1px;\n}\n:root[data-theme='dark'] {\n  /* dark note */\n  --a: 2px;\n  --b: 1px;\n}\n:root { --b: 3px }\n")
	require.Empty(t, r.Diagnostics)
	prim := tokenFile(t, r, "ds/tokens/primitive.tokens.json")
	assert.Equal(t, "base note", prim["a"]["$description"])
	_, has := prim["b"]["$description"]
	assert.False(t, has)
}

func TestImportDeterministic(t *testing.T) {
	_, a := fixture(t)
	for range 5 {
		_, b := fixture(t)
		assert.Equal(t, a.Files, b.Files)
		assert.Equal(t, a.Diagnostics, b.Diagnostics)
	}
}

func TestIDs(t *testing.T) {
	ids := IDs()
	assert.Len(t, ids, 14)
	assert.True(t, slices.IsSorted(ids))
	assert.Equal(t, []string{
		"import-css-syntax", "import-duplicate", "import-fallback-dropped", "import-name-invalid",
		"import-no-base", "import-not-equivalent", "import-nothing", "import-too-many-environments",
		"import-type-conflict", "import-unmapped-scope", "import-unsupported-at-rule",
		"import-unsupported-media", "import-unsupported-selector", "import-unsupported-value",
	}, ids)
}

func TestIndexCoversEveryKey(t *testing.T) {
	data, _ := fixture(t)
	idx, err := buildIndex(loadConfig(t, string(data)))
	require.NoError(t, err)
	// appearance (2 choices: absent, dark), density (3: absent, compact,
	// relaxed), app (2: absent, slides): 12 keys. dark has 2 spellings and
	// relaxed 2, so scopes are (1+2)*(1+1+2)*(1+1) = 24.
	assert.Len(t, idx, 24)
	assert.Equal(t, config.ModeKey(""), idx["|:root"])
	assert.Equal(t, config.ModeKey("appearance:dark+density:relaxed"),
		idx["(pointer: coarse) and (prefers-color-scheme: dark)|:root:not([data-density]):not([data-theme='light'])"])
}

func TestIndexRejectsCollidingSpellings(t *testing.T) {
	cfg := loadConfig(t, `version: 1
content: ds
modes:
  appearance: {values: [light, dark], web: {light: ":root", dark: ":root[data-x='1']"}}
  contrast: {values: [normal, high], web: {normal: ":root", high: ":root[data-x='1']"}}
`)
	_, err := buildIndex(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "appearance:dark")
	assert.Contains(t, err.Error(), "contrast:high")

	r := ImportSources(cfg, []Source{{File: "theme.css", Data: []byte(":root { --a: 1px }\n")}})
	got := byRule(r.Diagnostics, "import-unmapped-scope")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, config.File, got[0].File)
	assert.Empty(t, r.Files)
}

func TestRoundColor(t *testing.T) {
	got := roundColor(color.OKLCH{L: 0.12345678, C: 0.0000004, H: 123.4567891, Alpha: 0.33333333})
	assert.Equal(t, color.OKLCH{L: 0.123457, C: 0, H: 0, Alpha: 0.333333}, got)
	got = roundColor(color.OKLCH{L: 0.5, C: 0.1234567, H: 10.0000004, Alpha: 1})
	assert.Equal(t, color.OKLCH{L: 0.5, C: 0.123457, H: 10, Alpha: 1}, got)
}

// Shadow-layer colours are rounded too, before the duplicate comparison.
func TestImportRoundsShadowColours(t *testing.T) {
	r := run(t, smallConfig, ":root { --s: 0 1px 2px #f8f8f8 }\n")
	require.Empty(t, r.Diagnostics)
	s := tokenFile(t, r, "ds/tokens/primitive.tokens.json")["s"]["$value"].([]any)
	c := s[0].(map[string]any)["color"].(map[string]any)["components"].([]any)
	assert.Equal(t, []any{0.979129, 0.0, 0.0}, c)
}

func TestImportFormatFailureIsReported(t *testing.T) {
	old := formatFile
	formatFile = func(string, []byte) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { formatFile = old })
	r := run(t, smallConfig, ":root { --a: 1px }\n")
	got := byRule(r.Diagnostics, "import-unsupported-value")
	require.Len(t, got, 1, "%v", r.Diagnostics)
	assert.Equal(t, "internal: could not format ds/tokens/primitive.tokens.json: boom", got[0].Message)
	assert.Empty(t, r.Files)
}
