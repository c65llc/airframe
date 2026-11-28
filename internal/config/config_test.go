// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
)

// specExample is spec §6.2 minus the apple/android/windows platforms and
// the json platform's names key (plan 3).
const specExample = `version: 1
content: design-system
tiers:
  primitive: { readableBy: [semantic] }
  semantic:  { readableBy: [component, consumers, emitters] }
  component: { readableBy: [owner] }
modes:
  appearance: { values: [light, dark], binding: runtime,  web: { light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"] } }
  density:    { values: [compact, standard, relaxed], base: standard, binding: runtime, web: { attr: data-density, also: { relaxed: "@media (pointer: coarse)" } } }
  app:        { values: [drafts, edit, review, slides, tasks, wiki], base: drafts, binding: build, web: { attr: data-app }, group: accent }
  swatch:     { values: from brands/swatches.tokens.json, binding: runtime, web: { attr: data-accent-swatch }, group: accent }
  channel:    { values: [stable, next], base: stable, binding: build, web: { attr: data-channel } }
accentGroups: { required: [accent, accent-ink, accent-contrast, accent-on-ink, accent-soft] }
platforms:
  web:     { emit: css,    out: packages/tokens/generated/tokens.css, consumers: ["apps/**/*.{css,tsx}", "packages/**/*.css"] }
  json:
    emit: [json]
    out: packages/tokens/tokens.json
naming: { css: "--{name}", swift: "DesignTokens.{camel}", kotlin: "DesignTokens.{camel}", xaml: "Design{Pascal}" }
lint:
  rules: { fallback-literal: error, contrast-below: error }
`

func mustLoad(t *testing.T, src string) *Config {
	t.Helper()
	c, ds := Load([]byte(src))
	require.Empty(t, ds)
	require.NotNil(t, c)
	return c
}

func TestLoadSpecExample(t *testing.T) {
	c := mustLoad(t, specExample)
	assert.Equal(t, 1, c.Version)
	assert.Equal(t, "design-system", c.Content)

	var names []string
	for _, a := range c.Axes {
		names = append(names, a.Name)
	}
	assert.Equal(t, []string{"appearance", "density", "app", "swatch", "channel"}, names)

	assert.Equal(t, []string{"component", "consumers", "emitters"}, c.Tiers["semantic"].ReadableBy)
	assert.Equal(t, []string{"accent", "accent-ink", "accent-contrast", "accent-on-ink", "accent-soft"}, c.AccentRequired)

	app := c.Axes[2]
	assert.Equal(t, "build", app.Binding)
	assert.Equal(t, "accent", app.Group)
	assert.Equal(t, "data-app", app.Web.Attr)

	appearance := c.Axes[0]
	assert.Equal(t, []string{":root"}, appearance.Web.Selectors["light"])
	assert.Len(t, appearance.Web.Selectors["dark"], 2)
	assert.Equal(t, "@media (pointer: coarse)", c.Axes[1].Web.Also["relaxed"])

	swatch := c.Axes[3]
	assert.True(t, swatch.Deferred())
	assert.Empty(t, swatch.Values)
	assert.Equal(t, "brands/swatches.tokens.json", swatch.Source)

	require.Len(t, c.Platforms, 2)
	assert.Equal(t, Platform{
		Name: "web", Emit: []string{"css"},
		Out:       map[string]string{"css": "packages/tokens/generated/tokens.css"},
		Consumers: []string{"apps/**/*.{css,tsx}", "packages/**/*.css"},
	}, c.Platforms[0])
	assert.Equal(t, map[string]string{"json": "packages/tokens/tokens.json"}, c.Platforms[1].Out)

	assert.Equal(t, diag.Error, c.Lint.Rules["contrast-below"])
}

func TestDefaults(t *testing.T) {
	c := mustLoad(t, "version: 1\nmodes:\n  a: { values: [x], base: x }\n")
	assert.Equal(t, "design-system", c.Content)
	assert.Equal(t, "runtime", c.Axes[0].Binding)
	assert.Len(t, c.Tiers, 3)
	assert.Equal(t, []string{"owner"}, c.Tiers["component"].ReadableBy)
}

func TestPlatformOutMapAndEmitList(t *testing.T) {
	c := mustLoad(t, "version: 1\nmodes:\n  app: { values: [a], base: a, binding: build }\nplatforms:\n  apple:\n    emit: [swift, colorsets]\n    out: { swift: a.swift, colorsets: dir/ }\n    fanOut: app\n")
	p := c.Platforms[0]
	assert.Equal(t, []string{"swift", "colorsets"}, p.Emit)
	assert.Equal(t, map[string]string{"swift": "a.swift", "colorsets": "dir/"}, p.Out)
}

func TestScalarOutNeedsOneEmit(t *testing.T) {
	_, ds := Load([]byte("version: 1\nplatforms:\n  p:\n    emit: [css, json]\n    out: x\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, 5, ds[0].Line)
}

func TestSchemaRejectsUnknownKey(t *testing.T) {
	c, ds := Load([]byte("version: 1\ncontnet: x\n"))
	assert.Nil(t, c)
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, diag.Error, ds[0].Severity)
	assert.Equal(t, 2, ds[0].Line)
	assert.Equal(t, "contnet", ds[0].Path)
	assert.Equal(t, "airframe.yaml", ds[0].File)
	assert.Equal(t, diag.DocsURL("schema-invalid"), ds[0].Docs)
}

func TestSchemaNestedLine(t *testing.T) {
	_, ds := Load([]byte("version: 1\nmodes:\n  a:\n    values: [x]\n    binding: sometimes\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, 5, ds[0].Line)
	assert.Equal(t, "modes.a.binding", ds[0].Path)
}

func TestSchemaMissingVersion(t *testing.T) {
	_, ds := Load([]byte("content: x\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	_, ds = Load(nil)
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
}

func TestYAMLSyntax(t *testing.T) {
	c, ds := Load([]byte("version: 1\nmodes: [unclosed\n"))
	assert.Nil(t, c)
	require.Len(t, ds, 1)
	assert.Equal(t, "yaml-syntax", ds[0].Rule)
	assert.Positive(t, ds[0].Line)
}

func TestValidateUnknownKind(t *testing.T) {
	ds := schema.Validate("nope", map[string]any{})
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, diag.Error, ds[0].Severity)
}

func TestValidatePlainValue(t *testing.T) {
	assert.Empty(t, schema.Validate("config", map[string]any{"version": 1}))
	ds := schema.Validate("config", map[string]any{"version": 2})
	require.Len(t, ds, 1)
	assert.Zero(t, ds[0].Line)
}

func TestUnknownKeyWithPunctuation(t *testing.T) {
	_, ds := Load([]byte("version: 1\nfoo: 1\n\"it's, odd\": 2\n"))
	require.Len(t, ds, 2)
	byPath := map[string]diag.Diagnostic{}
	for _, d := range ds {
		byPath[d.Path] = d
	}
	assert.Equal(t, 3, byPath["it's, odd"].Line)
	assert.Equal(t, 2, byPath["foo"].Line)
}

func TestNonStringKey(t *testing.T) {
	_, ds := Load([]byte("version: 1\n1: x\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, "mapping keys must be strings", ds[0].Message)
	assert.Equal(t, 2, ds[0].Line)
}

func TestDuplicateKey(t *testing.T) {
	_, ds := Load([]byte("version: 1\ncontent: a\ncontent: b\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "yaml-syntax", ds[0].Rule)
	assert.Equal(t, `duplicate key "content"`, ds[0].Message)
	assert.Equal(t, 3, ds[0].Line)
}

func TestContentIsCleaned(t *testing.T) {
	for in, want := range map[string]string{
		"design-system":   "design-system",
		"./design-system": "design-system",
		"a//b/":           "a/b",
		"a/../b":          "b",
	} {
		c := mustLoad(t, "version: 1\ncontent: "+in+"\n")
		assert.Equal(t, want, c.Content, in)
	}
}

func TestContentMustStayUnderTheRoot(t *testing.T) {
	for _, in := range []string{"../x", "a/../../x", "..", "/abs/tokens"} {
		c, ds := Load([]byte("version: 1\ncontent: " + in + "\n"))
		assert.Nil(t, c, in)
		require.Len(t, ds, 1, in)
		assert.Equal(t, "schema-invalid", ds[0].Rule, in)
		assert.Equal(t, "content", ds[0].Path, in)
		assert.Equal(t, 2, ds[0].Line, in)
		assert.Equal(t, File, ds[0].File, in)
		assert.Contains(t, ds[0].Message, in, in)
	}
}

func TestContentRejectsBackslashAndDrive(t *testing.T) {
	for _, in := range []string{`a\\b`, "C:/x", "c:x"} {
		c, ds := Load([]byte("version: 1\ncontent: '" + in + "'\n"))
		assert.Nil(t, c, in)
		require.Len(t, ds, 1, in)
		assert.Equal(t, "schema-invalid", ds[0].Rule, in)
		assert.Equal(t, "content", ds[0].Path, in)
		assert.Equal(t, 2, ds[0].Line, in)
	}
}

func TestOutEscapeRejected(t *testing.T) {
	for _, in := range []string{"../x", "/etc/x", "C:/x", `a\\b`, "a/../../x"} {
		for _, src := range []string{
			"version: 1\nplatforms:\n  web:\n    emit: css\n    out: '" + in + "'\n",
			"version: 1\nplatforms:\n  web:\n    emit: [css]\n    out:\n      css: '" + in + "'\n",
		} {
			c, ds := Load([]byte(src))
			assert.Nil(t, c, src)
			require.Len(t, ds, 1, src)
			assert.Equal(t, "schema-invalid", ds[0].Rule, src)
			assert.Contains(t, ds[0].Path, "platforms.web.out", src)
			assert.Contains(t, []int{5, 6}, ds[0].Line, src)
			assert.Contains(t, ds[0].Message, "under the project root", src)
		}
	}
}

func TestOutIsCleaned(t *testing.T) {
	c := mustLoad(t, "version: 1\nplatforms:\n  web: {emit: css, out: ./gen//tokens.css}\n")
	assert.Equal(t, "gen/tokens.css", c.Platforms[0].Out["css"])
}

// loadErr loads src and requires exactly one schema-invalid diagnostic,
// returning it.
func loadErr(t *testing.T, src string) diag.Diagnostic {
	t.Helper()
	c, ds := Load([]byte(src))
	assert.Nil(t, c)
	require.Len(t, ds, 1, src)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, File, ds[0].File)
	return ds[0]
}

func TestBaseFromBareRoot(t *testing.T) {
	c := mustLoad(t, "version: 1\nmodes:\n  appearance: { values: [dark, light], web: { light: \":root\", dark: \":root[data-theme='dark']\" } }\n")
	assert.Equal(t, "light", c.Axes[0].Base)
	assert.True(t, c.Axes[0].IsBase("light"))
	assert.False(t, c.Axes[0].IsBase("dark"))
}

func TestBaseExplicit(t *testing.T) {
	c := mustLoad(t, "version: 1\nmodes:\n  density: { values: [compact, standard], base: standard, web: { attr: data-density } }\n")
	assert.Equal(t, "standard", c.Axes[0].Base)
	assert.True(t, c.Axes[0].IsBase("standard"))
	// Agreeing with the bare-:root value is fine.
	c = mustLoad(t, "version: 1\nmodes:\n  appearance: { values: [light, dark], base: light, web: { light: \":root\", attr: data-theme } }\n")
	assert.Equal(t, "light", c.Axes[0].Base)
	// A deferred axis may declare a base; it is checked once values load.
	c = mustLoad(t, "version: 1\nmodes:\n  swatch: { values: from s.tokens.json, base: moss, web: { attr: data-s } }\n")
	assert.Equal(t, "moss", c.Axes[0].Base)
}

func TestBaseMissing(t *testing.T) {
	d := loadErr(t, "version: 1\nmodes:\n  density:\n    values: [compact, standard]\n    web: { attr: data-density }\n")
	assert.Equal(t, "axis density needs a base: value — the value rendered when no attribute is set", d.Message)
	assert.Equal(t, "modes.density", d.Path)
	assert.Equal(t, 3, d.Line)
}

func TestBaseDisagrees(t *testing.T) {
	d := loadErr(t, "version: 1\nmodes:\n  appearance:\n    values: [light, dark]\n    base: dark\n    web: { light: \":root\", attr: data-theme }\n")
	assert.Equal(t, "modes.appearance.base", d.Path)
	assert.Equal(t, 5, d.Line)
	assert.Contains(t, d.Message, "light")

	d = loadErr(t, "version: 1\nmodes:\n  density:\n    values: [compact, standard]\n    base: comfy\n    web: { attr: data-density }\n")
	assert.Equal(t, "modes.density.base", d.Path)
	assert.Equal(t, 5, d.Line)
	assert.Contains(t, d.Message, "comfy")
}

func TestContextsUseBase(t *testing.T) {
	c := mustLoad(t, `version: 1
modes:
  appearance: { values: [light, dark], web: { light: ":root", dark: ":root[data-theme='dark']" } }
  density: { values: [compact, standard, relaxed], base: standard, web: { attr: data-density } }
  app: { values: [slides, wiki], base: wiki, web: { attr: data-app } }
`)
	assert.Equal(t, []Context{{"appearance": "light", "density": "standard", "app": "wiki"}}, c.Contexts(nil))
	got := c.Contexts([]string{"density"})
	require.Len(t, got, 3)
	for i, v := range []string{"compact", "standard", "relaxed"} {
		assert.Equal(t, Context{"appearance": "light", "density": v, "app": "wiki"}, got[i])
	}
}

func TestAlsoRequiresAttrSpellings(t *testing.T) {
	d := loadErr(t, "version: 1\nmodes:\n  density:\n    values: [compact, standard, relaxed]\n    base: standard\n    web:\n      attr: data-density\n      compact: \":root[data-dense]\"\n      also: { relaxed: \"@media (pointer: coarse)\" }\n")
	assert.Equal(t, "modes.density.web.compact", d.Path)
	assert.Equal(t, 8, d.Line)
	// A bare :root base is the one explicit spelling also allows.
	c := mustLoad(t, "version: 1\nmodes:\n  density: { values: [standard, relaxed], web: { attr: data-density, standard: \":root\", also: { relaxed: \"@media (pointer: coarse)\" } } }\n")
	assert.Equal(t, "standard", c.Axes[0].Base)
}

func TestBareRootAmongSeveral(t *testing.T) {
	d := loadErr(t, "version: 1\nmodes:\n  appearance:\n    values: [light, dark]\n    web:\n      light: [\":root\", \":root[data-theme='light']\"]\n      dark: \":root[data-theme='dark']\"\n")
	assert.Equal(t, "modes.appearance.web.light", d.Path)
	assert.Equal(t, 6, d.Line)
}

func TestAlsoWithoutAttrAtLoad(t *testing.T) {
	d := loadErr(t, "version: 1\nmodes:\n  density:\n    values: [standard, relaxed]\n    base: standard\n    web:\n      also: { relaxed: \"@media (pointer: coarse)\" }\n")
	assert.Equal(t, "modes.density.web.also", d.Path)
	assert.Equal(t, 7, d.Line)
}

// TestSpec62ExampleLoads loads the airframe.yaml example in spec §6.2
// verbatim, every platform and key included, so the spec cannot drift
// from what Load accepts.
func TestSpec62ExampleLoads(t *testing.T) {
	data, err := os.ReadFile("../../docs/specs/2026-11-27-airframe-design.md")
	require.NoError(t, err)
	_, after, ok := strings.Cut(string(data), "### 6.2 ")
	require.True(t, ok)
	_, after, ok = strings.Cut(after, "```yaml\n")
	require.True(t, ok)
	block, _, ok := strings.Cut(after, "```")
	require.True(t, ok)

	c := mustLoad(t, block)
	var names []string
	for _, p := range c.Platforms {
		names = append(names, p.Name)
	}
	assert.Equal(t, []string{"web", "apple", "android", "windows", "json"}, names)
	bases := map[string]string{}
	for _, a := range c.Axes {
		bases[a.Name] = a.Base
	}
	assert.Equal(t, map[string]string{"appearance": "light", "density": "standard", "app": "drafts", "swatch": "", "channel": "stable"}, bases)
}

func TestEmitFormatUnknown(t *testing.T) {
	d := loadErr(t, "version: 1\nplatforms:\n  web:\n    emit: [css,\n      csss]\n")
	assert.Equal(t, "platforms.web.emit", d.Path)
	assert.Equal(t, 5, d.Line)
	assert.Contains(t, d.Message, "csss")
	d = loadErr(t, "version: 1\nplatforms:\n  web: {emit: csss}\n")
	assert.Equal(t, 3, d.Line)
}

func TestEmitFutureFormatsAccepted(t *testing.T) {
	c := mustLoad(t, "version: 1\nplatforms:\n  apple: {emit: [swift, colorsets]}\n  android: {emit: kotlin, options: {package: com.example.design}}\n  windows: {emit: [xaml]}\n")
	assert.Equal(t, []string{"swift", "colorsets"}, c.Platforms[0].Emit)
}

func TestOutKeyMustBeEmitted(t *testing.T) {
	d := loadErr(t, "version: 1\nplatforms:\n  web:\n    emit: css\n    out:\n      csss: x.css\n")
	assert.Equal(t, "platforms.web.out.csss", d.Path)
	assert.Equal(t, 6, d.Line)
	assert.Contains(t, d.Message, "csss")
}

// TestMultipleDocuments: a second YAML document would be ignored, so it
// is yaml-syntax at its --- line.
func TestMultipleDocuments(t *testing.T) {
	c, ds := Load([]byte("version: 1\ncontent: a\n---\ncontent: b\n"))
	assert.Nil(t, c)
	require.Len(t, ds, 1)
	assert.Equal(t, "yaml-syntax", ds[0].Rule)
	assert.Equal(t, "multiple YAML documents are not supported", ds[0].Message)
	assert.Equal(t, 3, ds[0].Line)
	assert.Equal(t, File, ds[0].File)
}

func TestWebSelectorKeyMustBeAValue(t *testing.T) {
	d := loadErr(t, "version: 1\nmodes:\n  appearance:\n    values: [light, dark]\n    web:\n      light: \":root\"\n      darkk: \":root[data-theme='dark']\"\n")
	assert.Equal(t, "modes.appearance.web.darkk is not a value of appearance", d.Message)
	assert.Equal(t, "modes.appearance.web.darkk", d.Path)
	assert.Equal(t, 7, d.Line)

	d = loadErr(t, "version: 1\nmodes:\n  density:\n    values: [standard, relaxed]\n    base: standard\n    web:\n      attr: data-density\n      also: { relaxd: \"@media (pointer: coarse)\" }\n")
	assert.Equal(t, "modes.density.web.also.relaxd is not a value of density", d.Message)
	assert.Equal(t, 8, d.Line)
}

func TestAxisValueNamesReserved(t *testing.T) {
	for _, v := range []string{"attr", "also"} {
		d := loadErr(t, "version: 1\nmodes:\n  a:\n    values: [x, "+v+"]\n    base: x\n")
		assert.Equal(t, "modes.a.values", d.Path, v)
		assert.Equal(t, 4, d.Line, v)
		assert.Contains(t, d.Message, v+" is reserved", v)
	}
}

func TestOutMustNameAPath(t *testing.T) {
	for _, in := range []string{"", ".", "./", "a/.."} {
		for _, src := range []string{
			"version: 1\nplatforms:\n  web:\n    emit: css\n    out: '" + in + "'\n",
			"version: 1\nplatforms:\n  web:\n    emit: [css]\n    out:\n      css: '" + in + "'\n",
		} {
			d := loadErr(t, src)
			assert.Contains(t, d.Path, "platforms.web.out", src)
			assert.Contains(t, d.Message, "out must name a file or directory under the root", src)
		}
	}
}

const fanOutBase = `version: 1
modes:
  density: { values: [compact, standard], base: standard, binding: runtime }
  app: { values: [a, b], base: a, binding: build }
`

func TestPlatformFanOutParsed(t *testing.T) {
	data, err := os.ReadFile("../../docs/specs/2026-11-27-airframe-design.md")
	require.NoError(t, err)
	_, after, _ := strings.Cut(string(data), "### 6.2 ")
	_, after, _ = strings.Cut(after, "```yaml\n")
	block, _, _ := strings.Cut(after, "```")
	c := mustLoad(t, block)
	got := map[string]string{}
	for _, p := range c.Platforms {
		got[p.Name] = p.FanOut
	}
	assert.Equal(t, "app", got["apple"])
	assert.Equal(t, "", got["web"])
	for _, p := range c.Platforms {
		if p.Name == "android" {
			assert.Equal(t, "com.example.design", p.KotlinPackage())
			assert.Equal(t, [][2]string{{"primary", "accent"}, {"onPrimary", "accent-contrast"}, {"primaryContainer", "accent-soft"}}, p.Material3Roles())
			v, ok := p.Option("sharedOut")
			assert.True(t, ok)
			assert.Equal(t, "android/shared/src/main/kotlin/DesignTokens.kt", v)
		}
	}
}

func TestFanOutMustBeBuildAxis(t *testing.T) {
	d := loadErr(t, fanOutBase+"platforms:\n  apple:\n    emit: [swift]\n    out: x.swift\n    fanOut: density\n")
	assert.Equal(t, "platforms.apple.fanOut", d.Path)
	assert.Contains(t, d.Message, "density")
	assert.Contains(t, d.Message, "build")
	d = loadErr(t, fanOutBase+"platforms:\n  apple:\n    emit: [swift]\n    out: x.swift\n    fanOut: nope\n")
	assert.Contains(t, d.Message, "nope")
	mustLoad(t, fanOutBase+"platforms:\n  apple:\n    emit: [swift]\n    out: x.swift\n    fanOut: app\n")
}

func TestAppleOptions(t *testing.T) {
	pre := "version: 1\nplatforms:\n  apple:\n    emit: [swift]\n    out: x.swift\n    options: "
	c := mustLoad(t, pre+"{ gamut: clip-p3, accentColor: AccentColor }\n")
	v, ok := c.Platforms[0].Option("gamut")
	assert.True(t, ok)
	assert.Equal(t, "clip-p3", v)
	_, ok = c.Platforms[0].Option("missing")
	assert.False(t, ok)
	d := loadErr(t, pre+"{ gamut: wide }\n")
	assert.Equal(t, "platforms.apple.options.gamut", d.Path)
	assert.Equal(t, 6, d.Line)
	d = loadErr(t, pre+"{ bogus: 1 }\n")
	assert.Equal(t, "platforms.apple.options.bogus", d.Path)
	loadErr(t, pre+"{ accentColor: \"\" }\n")
}

func TestKotlinOptions(t *testing.T) {
	pre := fanOutBase + "platforms:\n  droid:\n    emit: [kotlin]\n    out: x.kt\n    fanOut: app\n    options: "
	for _, tc := range []struct{ name, opts, path, msg string }{
		{"missing package", "{ gamut: clip }", "platforms.droid.options.package", "package"},
		{"digit segment", "{ package: com.2x.app }", "platforms.droid.options.package", "2x"},
		{"keyword segment", "{ package: com.in.app }", "platforms.droid.options.package", "in"},
		{"unknown key", "{ package: a.b, bogus: 1 }", "platforms.droid.options.bogus", "bogus"},
		{"colorSpace", "{ package: a.b, colorSpace: cmyk }", "platforms.droid.options.colorSpace", "cmyk"},
		{"gamut", "{ package: a.b, gamut: clip-p3 }", "platforms.droid.options.gamut", "clip-p3"},
		{"sharedOut fans out", "{ package: a.b, sharedOut: \"android/{app}/x.kt\" }", "platforms.droid.options.sharedOut", "sharedOut"},
		{"scalar scheme", "{ package: a.b, material3Scheme: true }", "platforms.droid.options.material3Scheme", "maps Material 3 roles to tokens, as in { primary: accent }"},
		{"unknown role", "{ package: a.b, material3Scheme: { primry: accent } }", "platforms.droid.options.material3Scheme.primry", "primry"},
		{"package number", "{ package: 5 }", "platforms.droid.options.package", "must be a string"},
		{"sharedOut number", "{ package: a.b, sharedOut: 5 }", "platforms.droid.options.sharedOut", "must be a string"},
		{"sharedOut escapes", "{ package: a.b, sharedOut: ../x.kt }", "platforms.droid.options.sharedOut", "sharedOut"},
		{"colorSpace mapping", "{ package: a.b, colorSpace: {} }", "platforms.droid.options.colorSpace", "colorSpace must be a string, one of"},
		{"gamut mapping", "{ package: a.b, gamut: [] }", "platforms.droid.options.gamut", "gamut must be a string, one of"},
		{"role number", "{ package: a.b, material3Scheme: { primary: 5 } }", "platforms.droid.options.material3Scheme.primary", "must be a string"},
		{"empty scheme", "{ package: a.b, material3Scheme: {} }", "platforms.droid.options.material3Scheme", "no roles"},
		{"empty token", "{ package: a.b, material3Scheme: { primary: \"\" } }", "platforms.droid.options.material3Scheme.primary", "primary"},
	} {
		d := loadErr(t, pre+tc.opts+"\n")
		assert.Equal(t, tc.path, d.Path, tc.name)
		assert.Contains(t, d.Message, tc.msg, tc.name)
	}
	for _, src := range []string{
		fanOutBase + "platforms:\n  droid:\n    emit: [kotlin]\n    out: x.kt\n",
		fanOutBase + "platforms:\n  droid:\n    emit: kotlin\n    out: x.kt\n",
	} {
		d := loadErr(t, src)
		assert.Equal(t, "platforms.droid.options.package", d.Path)
		assert.Equal(t, "platform droid: options.package is required for kotlin", d.Message)
		assert.Equal(t, 6, d.Line)
	}
	mustLoad(t, pre+"{ package: \"a.b\", sharedOut: \"x.kt\" }\n")
	c := mustLoad(t, pre+"{ package: com.example.design, sharedOut: android/shared/DesignTokens.kt, colorSpace: display-p3, gamut: clip, material3Scheme: { onPrimary: b, primary: a, surfaceDim: c } }\n")
	p := c.Platforms[0]
	assert.Equal(t, "com.example.design", p.KotlinPackage())
	assert.Equal(t, [][2]string{{"primary", "a"}, {"onPrimary", "b"}, {"surfaceDim", "c"}}, p.Material3Roles())
	assert.Nil(t, mustLoad(t, pre+"{ package: a.b }\n").Platforms[0].Material3Roles())
	assert.Equal(t, "DesignTokens.{camel}", c.NamingFor("kotlin"))
	// Other platforms' kotlin-ish options are not checked.
	mustLoad(t, "version: 1\nplatforms:\n  web:\n    emit: [css]\n    out: x.css\n    options: { package: 1 }\n")
}

func TestNamingTemplates(t *testing.T) {
	c := mustLoad(t, "version: 1\nnaming: { swift: \"Tokens.{camel}\" }\n")
	assert.Equal(t, "Tokens.{camel}", c.NamingFor("swift"))
	assert.Equal(t, "DesignTokens.{camel}", mustLoad(t, "version: 1\n").NamingFor("swift"))
	assert.Equal(t, "DesignTokens.{camel}", mustLoad(t, "version: 1\n").NamingFor("kotlin"))
	assert.Equal(t, "--{name}", mustLoad(t, "version: 1\n").NamingFor("css"))
	for _, bad := range []string{"{weird}", "plain", "{name}{camel}", "{name}-{Pascal}", "{camel}{x}"} {
		d := loadErr(t, "version: 1\nnaming: { swift: \""+bad+"\" }\n")
		assert.Equal(t, "naming.swift", d.Path, bad)
	}
}

// TestNamingUnknownFormat: a naming key that is not an emit format is
// schema-invalid at its path, so a typo is not silently ignored.
func TestNamingUnknownFormat(t *testing.T) {
	d := loadErr(t, "version: 1\nnaming: { swfit: \"X.{camel}\" }\n")
	assert.Equal(t, "naming.swfit", d.Path)
	assert.Contains(t, d.Message, `"swfit"`)
	assert.Equal(t, 2, d.Line)
}

// TestNamingFixedFormats: css, json and colorsets do not read naming yet,
// so a template other than their default is schema-invalid; the default
// itself is accepted.
func TestNamingFixedFormats(t *testing.T) {
	d := loadErr(t, "version: 1\nnaming: { css: \"--brand-{name}\" }\n")
	assert.Equal(t, "naming.css", d.Path)
	assert.Equal(t, "naming css: css does not support custom naming yet; only --{name} is accepted", d.Message)
	for format, def := range map[string]string{"css": "--{name}", "json": "{name}", "colorsets": "{Pascal}"} {
		assert.Equal(t, def, mustLoad(t, "version: 1\nnaming: { "+format+": \""+def+"\" }\n").NamingFor(format), format)
	}
	for _, format := range []string{"json", "colorsets"} {
		d := loadErr(t, "version: 1\nnaming: { "+format+": \"x{camel}\" }\n")
		assert.Equal(t, "naming."+format, d.Path)
		assert.Contains(t, d.Message, format+" does not support custom naming yet")
	}
	mustLoad(t, "version: 1\nnaming: { kotlin: \"K.{Pascal}\", xaml: \"X{camel}\" }\n")
}

// TestNamingProblemsAllReported: every bad naming entry is reported, not
// just the first.
func TestNamingProblemsAllReported(t *testing.T) {
	c, ds := Load([]byte("version: 1\nnaming: {css: \"--brand-{name}\", swfit: \"X.{camel}\", colorsets: \"{camel}\"}\n"))
	assert.Nil(t, c)
	var paths []string
	for _, d := range ds {
		assert.Equal(t, "schema-invalid", d.Rule)
		paths = append(paths, d.Path)
	}
	assert.Equal(t, []string{"naming.css", "naming.swfit", "naming.colorsets"}, paths)
}
