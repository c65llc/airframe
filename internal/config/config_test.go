// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package config

import (
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
  density:    { values: [compact, standard, relaxed], binding: runtime, web: { attr: data-density, also: { relaxed: "@media (pointer: coarse)" } } }
  app:        { values: [drafts, edit, review, slides, tasks, wiki], binding: build, web: { attr: data-app }, group: accent }
  swatch:     { values: from brands/swatches.tokens.json, binding: runtime, web: { attr: data-accent-swatch }, group: accent }
  channel:    { values: [stable, next], binding: build, web: { attr: data-channel } }
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
	c := mustLoad(t, "version: 1\nmodes:\n  a: { values: [x] }\n")
	assert.Equal(t, "design-system", c.Content)
	assert.Equal(t, "runtime", c.Axes[0].Binding)
	assert.Len(t, c.Tiers, 3)
	assert.Equal(t, []string{"owner"}, c.Tiers["component"].ReadableBy)
}

func TestPlatformOutMapAndEmitList(t *testing.T) {
	c := mustLoad(t, "version: 1\nplatforms:\n  apple:\n    emit: [swift, colorsets]\n    out: { swift: a.swift, colorsets: dir/ }\n    fanOut: app\n")
	p := c.Platforms[0]
	assert.Equal(t, []string{"swift", "colorsets"}, p.Emit)
	assert.Equal(t, map[string]string{"swift": "a.swift", "colorsets": "dir/"}, p.Out)
}

func TestScalarOutNeedsOneEmit(t *testing.T) {
	_, ds := Load([]byte("version: 1\nplatforms:\n  p:\n    emit: [a, b]\n    out: x\n"))
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
