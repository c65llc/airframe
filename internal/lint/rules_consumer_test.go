// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
)

// consumerConfig is one css platform with consumers and two wrappers.
const consumerConfig = `version: 1
content: design-system
platforms:
  web:
    emit: [css]
    out: {css: web/generated/tokens.css}
    consumers: ["web/src/**/*.{css,ts}"]
    wrappers:
      - root: theme.space
        map: {small: space-2}
      - root: theme.button
        map: {bg: button-bg}
`

const consumerTokens = `{
  "space": { "$type": "dimension",
    "2": { "$value": { "value": 8, "unit": "px" }, ` + `"$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } },
  "blue": { "$type": "color",
    "500": { "$value": { "colorSpace": "oklch", "components": [0.55, 0.2, 260] }, "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } },
  "accent": { "$type": "color", "$value": "{blue.500}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } },
  "ink": { "$type": "color", "$value": "{blue.500}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } },
  "button": { "bg": { "$type": "color", "$value": "{accent}", "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "button" } } } },
  "chip": { "bg": { "$type": "color", "$value": "{accent}", "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "chip" } } } },
  "pill": { "bg": { "$type": "color", "$value": "{accent}", "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "pill" } } } }
}`

const buttonComponent = "id: button\ntokens: [button-bg]\nowners: [web/src/button/**]\n"

// consumerFS is a project with consumerConfig, consumerTokens, the button
// component, and the given consumer files (repo-relative).
func consumerFS(cfg string, files map[string]string) project.MemFS {
	m := project.MemFS{
		"airframe.yaml":                        cfg,
		"design-system/tokens.tokens.json":     consumerTokens,
		"design-system/components/button.yaml": buttonComponent,
	}
	for k, v := range files {
		m[k] = v
	}
	return m
}

// lintLoaded loads fsys and lints it, passing any load diagnostics through.
func lintLoaded(t *testing.T, fsys fs.FS) Result {
	t.Helper()
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.NotNil(t, p, ds)
	return Run(p, ds, Options{})
}

func lintConsumers(t *testing.T, files map[string]string) Result {
	t.Helper()
	p, ds, err := project.Load(consumerFS(consumerConfig, files))
	require.NoError(t, err)
	require.Empty(t, ds)
	return Run(p, nil, Options{})
}

func TestReferenceUndeclared(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/a.css": ".a { color: var(--accnet); }\n",
		"web/src/b.css": ".b { --sidebar-w: 240px; width: var(--sidebar-w); color: var(--accent); }\n",
	})
	got := byRule(res.Diagnostics, "reference-undeclared")
	require.Len(t, got, 1, messages(got))
	d := got[0]
	assert.Equal(t, "web/src/a.css", d.File)
	assert.Equal(t, 1, d.Line)
	assert.Equal(t, 17, d.Col)
	assert.Equal(t, diag.Error, d.Severity)
	assert.Contains(t, d.Message, "--accnet")
	assert.Equal(t, diag.DocsURL("reference-undeclared"), d.Docs)
}

// A platform whose emitter cannot render its names is reported, never
// read as clean.
func TestNamesUnavailable(t *testing.T) {
	cfg := `version: 1
content: design-system
platforms:
  apple:
    emit: [swift]
    out: {swift: apple/Generated/DesignTokens.swift}
    consumers: ["apple/**/*.swift"]
`
	res := lintLoaded(t, consumerFS(cfg, map[string]string{"apple/A.swift": "let a = DesignTokens.accent\n"}))
	got := byRule(res.Diagnostics, "reference-undeclared")
	require.Len(t, got, 1, messages(got))
	assert.Equal(t, "platforms.apple.emit", got[0].Path)
	assert.Contains(t, got[0].Message, "cannot be checked until it builds")
}

// A consumers file no adapter reads is schema-invalid, reported once
// however many consumer rules read the scan.
func TestConsumerSchemaInvalidOnce(t *testing.T) {
	cfg := strings.Replace(consumerConfig, `*.{css,ts}"]`, `*.{css,ts}", "web/src/**/*.md"]`, 1)
	res := lintLoaded(t, consumerFS(cfg, map[string]string{
		"web/src/a.css":     ".a { color: var(--accent); }\n",
		"web/src/README.md": "notes\n",
	}))
	got := byRule(res.Diagnostics, "schema-invalid")
	require.Len(t, got, 1, messages(got))
	assert.Equal(t, "airframe.yaml", got[0].File)
	assert.Equal(t, "platforms.web.consumers", got[0].Path)
	assert.Contains(t, got[0].Message, "web/src/README.md")

	// Turning the reporting rule off does not drop it.
	off := cfg + "lint:\n  rules: { reference-undeclared: off }\n"
	res = lintLoaded(t, consumerFS(off, map[string]string{
		"web/src/a.css":     ".a { color: var(--accent); }\n",
		"web/src/README.md": "notes\n",
	}))
	assert.Len(t, byRule(res.Diagnostics, "schema-invalid"), 1)
}

func TestPrimitiveReadByConsumer(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/a.css":        ".a { color: var(--blue-500); background: var(--accent); }\n",
		"web/src/b.ts":         "const s = theme.space.small\n",
		"web/src/button/b.css": ".b { color: var(--button-bg); }\n",
	})
	got := byRule(res.Diagnostics, "primitive-read-by-consumer")
	require.Len(t, got, 2, messages(got))
	assert.Equal(t, "web/src/a.css", got[0].File)
	assert.Contains(t, got[0].Message, "blue-500")
	assert.Contains(t, got[0].Message, "primitive")
	assert.Equal(t, "web/src/b.ts", got[1].File)
	assert.Contains(t, got[1].Message, "space-2")
	assert.Contains(t, got[1].Message, "wrapper theme.space", "a wrapper read names its root")
}

// A tier whose readableBy names consumers may be read; a custom config
// can open the primitives up.
func TestPrimitiveReadableWhenTierAllows(t *testing.T) {
	cfg := consumerConfig + "tiers:\n  primitive: {readableBy: [semantic, consumers]}\n  semantic: {readableBy: [component, consumers, emitters]}\n  component: {readableBy: [owner]}\n"
	res := lintLoaded(t, consumerFS(cfg, map[string]string{"web/src/a.css": ".a { color: var(--blue-500); }\n"}))
	assert.Empty(t, byRule(res.Diagnostics, "primitive-read-by-consumer"))
}

func TestTokenOwnerViolation(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/app.css":       ".a { color: var(--button-bg); }\n",
		"web/src/button/b.css":  ".b { color: var(--button-bg); }\n",
		"web/src/chip.css":      ".c { color: var(--chip-bg); }\n",
		"web/src/button/c.css":  ".d { color: var(--chip-bg); }\n",
		"web/src/plain.css":     ".e { color: var(--accent); }\n",
		"web/src/pill/pill.css": "",
	})
	got := byRule(res.Diagnostics, "token-owner-violation")
	require.Len(t, got, 3, messages(got))
	assert.Equal(t, "web/src/app.css", got[0].File)
	assert.Contains(t, got[0].Message, "button-bg")
	assert.Contains(t, got[0].Message, "web/src/button/**", "names the owners")
	assert.Equal(t, "web/src/button/c.css", got[1].File)
	assert.Contains(t, got[1].Message, "component chip declares no owners (no components/chip.yaml)")
	assert.Equal(t, "web/src/chip.css", got[2].File)
	assert.Contains(t, got[2].Message, "component chip declares no owners (no components/chip.yaml)")
}

func TestTokenOwnerSharedComponent(t *testing.T) {
	res := lintLoaded(t, consumerFS(consumerConfig, map[string]string{
		"design-system/components/chip.yaml": "id: chip\ntokens: [chip-bg]\nshared: true\n",
		"web/src/app.css":                    ".a { color: var(--chip-bg); }\n",
	}))
	assert.Empty(t, byRule(res.Diagnostics, "token-owner-violation"))
}

// A component whose file failed to load is already a schema-invalid
// finding: the owner rule says nothing more about its tokens.
func TestTokenOwnerBrokenComponentFile(t *testing.T) {
	res := lintLoaded(t, consumerFS(consumerConfig, map[string]string{
		"design-system/components/chip.yaml": "id: chip\ntokens: [chip-bg]\nowners: 7\n",
		"web/src/app.css":                    ".a { color: var(--chip-bg); }\n",
	}))
	assert.NotEmpty(t, byRule(res.Diagnostics, "schema-invalid"))
	assert.Empty(t, byRule(res.Diagnostics, "token-owner-violation"), messages(res.Diagnostics))
}

// Review Focus 4: a wrapper read of a component token from a file that
// does not own it is a violation; from an owner it is not.
func TestOwnerViaWrapper(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/app.ts":       "const c = theme.button.bg\n",
		"web/src/button/b.ts":  "const c = theme.button.bg\n",
		"web/src/plain.css":    ".e { color: var(--accent); }\n",
		"web/src/button/x.css": "",
	})
	got := byRule(res.Diagnostics, "token-owner-violation")
	require.Len(t, got, 1, messages(got))
	assert.Equal(t, "web/src/app.ts", got[0].File)
	assert.Equal(t, 11, got[0].Col)
	assert.Contains(t, got[0].Message, "button-bg")
	assert.Contains(t, got[0].Message, "wrapper theme.button")
}

func TestFallbackLiteral(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/a.css": ".a { color: var(--accent, #fff); }\n",
		"web/src/b.css": ".b { color: var(--accent, var(--ink)); }\n",
	})
	got := byRule(res.Diagnostics, "fallback-literal")
	require.Len(t, got, 1, messages(got))
	assert.Equal(t, "web/src/a.css", got[0].File)
	assert.Equal(t, diag.Warn, got[0].Severity, "fallback-literal defaults to warn")
	assert.Contains(t, got[0].Message, "#fff")
}

func TestWrapperUnmapped(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/a.ts":  "const a = theme.space.large\n",
		"web/src/b.ts":  "const b = theme.space.small + theme.button.bg\n",
		"web/src/c.css": ".c { color: var(--accent); }\n",
	})
	got := byRule(res.Diagnostics, "wrapper-unmapped")
	require.Len(t, got, 1, messages(got))
	assert.Equal(t, "web/src/a.ts", got[0].File)
	assert.Contains(t, got[0].Message, "theme.space")
	assert.Contains(t, got[0].Message, "large")
}

// Every wrapper map entry whose token does not exist is reported at
// airframe.yaml, read or not; a read through it points at the entry.
func TestWrapperMapEntryDangling(t *testing.T) {
	cfg := strings.Replace(consumerConfig, "map: {small: space-2}", "map: {small: space-2, wide: space-9, huge: space-99}", 1)
	res := lintLoaded(t, consumerFS(cfg, map[string]string{
		"web/src/a.ts":  "const a = theme.space.wide\n",
		"web/src/c.css": ".c { color: var(--accent); }\n",
	}))
	got := byRule(res.Diagnostics, "wrapper-unmapped")
	require.Len(t, got, 3, messages(got))
	assert.Equal(t, "airframe.yaml", got[0].File)
	assert.Equal(t, "platforms.web.wrappers.0.map.huge", got[0].Path, "reported though nothing reads it")
	assert.Contains(t, got[0].Message, "space-99")
	assert.Equal(t, "platforms.web.wrappers.0.map.wide", got[1].Path)
	assert.Equal(t, "web/src/a.ts", got[2].File)
	assert.Contains(t, got[2].Message, "see platforms.web.wrappers.0.map.wide")
	assert.NotContains(t, got[2].Message, "space-9", "the entry's finding names the token, once")
}

// The primitive hint names the tiers consumers may read, from config.
func TestPrimitiveReadHint(t *testing.T) {
	res := lintConsumers(t, map[string]string{"web/src/a.css": ".a { color: var(--blue-500); }\n"})
	got := byRule(res.Diagnostics, "primitive-read-by-consumer")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "read a token of tier semantic instead")

	cfg := consumerConfig + "tiers:\n  primitive: {readableBy: [semantic]}\n  semantic: {readableBy: [component, emitters]}\n  component: {readableBy: [owner]}\n"
	res = lintLoaded(t, consumerFS(cfg, map[string]string{"web/src/a.css": ".a { color: var(--accent); }\n"}))
	got = byRule(res.Diagnostics, "primitive-read-by-consumer")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "no tier's readableBy lists consumers")
}

// A file two platforms read counts once per platform in every rule.
func TestConsumerFilesPerPlatform(t *testing.T) {
	cfg := consumerConfig + `  web2:
    emit: [css]
    out: {css: web/generated2/tokens.css}
    consumers: ["web/src/**/*.css"]
`
	res := lintLoaded(t, consumerFS(cfg, map[string]string{"web/src/a.css": ".a { color: var(--accent); }\n"}))
	for _, id := range consumerRuleIDs {
		assert.Equal(t, 2, tallyOf(res, id).Files, id)
	}
}

var consumerRuleIDs = []string{
	"fallback-literal", "primitive-read-by-consumer", "reference-undeclared", "token-owner-violation", "wrapper-unmapped",
}

func TestConsumerTallies(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/a.css":       ".a { color: var(--accent); margin: var(--space-2); padding: var(--nope); }\n",
		"web/src/b.ts":        "const b = theme.space.small + theme.space.large\n",
		"web/src/button/c.ts": "const c = theme.button.bg\n",
	})
	want := map[string]diag.Tally{
		// every reference (three css, three wrapper reads) and the one glob
		"reference-undeclared": {Files: 3, Items: 7, Noun: "references and globs"},
		// the resolved ones
		"primitive-read-by-consumer": {Files: 3, Items: 4},
		"token-owner-violation":      {Files: 3, Items: 4},
		// the css references (.ts is read through css names too)
		"fallback-literal": {Files: 3, Items: 3},
		// the wrapper reads and the two map entries
		"wrapper-unmapped": {Files: 3, Items: 5, Noun: "references and map entries"},
	}
	for _, id := range consumerRuleIDs {
		tl := tallyOf(res, id)
		noun := want[id].Noun
		if noun == "" {
			noun = "references"
		}
		assert.Equal(t, noun, tl.Noun, id)
		assert.Empty(t, tl.NotApplicable, id)
		assert.Equal(t, want[id].Files, tl.Files, id)
		assert.Equal(t, want[id].Items, tl.Items, id)
	}
}

func TestConsumerNotApplicable(t *testing.T) {
	// No platform declares consumers.
	res := lintLoaded(t, project.MemFS{
		"airframe.yaml":                    "version: 1\ncontent: design-system\nplatforms:\n  web: {emit: [css]}\n",
		"design-system/tokens.tokens.json": consumerTokens,
	})
	for _, id := range consumerRuleIDs {
		assert.Equal(t, "no platform declares consumers", tallyOf(res, id).NotApplicable, id)
	}

	// Consumers, but no wrappers and no css consumer file.
	cfg := `version: 1
content: design-system
platforms:
  apple:
    emit: [swift, colorsets]
    out: {swift: apple/Generated/DesignTokens.swift, colorsets: apple/Generated/Assets.xcassets/Tokens/}
    consumers: ["apple/**/*.swift"]
`
	res = lintLoaded(t, consumerFS(cfg, map[string]string{"apple/A.swift": "let a = DesignTokens.accent\n"}))
	assert.Equal(t, 2, tallyOf(res, "reference-undeclared").Items, "one reference, one glob")
	assert.Equal(t, "no platform declares wrappers", tallyOf(res, "wrapper-unmapped").NotApplicable)
	assert.Equal(t, "no css consumer files", tallyOf(res, "fallback-literal").NotApplicable)
}

func TestConsumerExemption(t *testing.T) {
	res := lintConsumers(t, map[string]string{
		"web/src/a.css": "/* airframe-exempt primitive-read-by-consumer: legacy banner, see #12 */\n" +
			".a { color: var(--blue-500); }\n" +
			".b { color: var(--blue-500); } /* airframe-exempt primitive-read-by-consumer */\n" +
			".c { color: var(--accnet); } /* airframe-exempt reference-undeclared: vendor sheet */\n",
	})
	got := byRule(res.Diagnostics, "primitive-read-by-consumer")
	require.Len(t, got, 1, messages(got))
	assert.Equal(t, 3, got[0].Line)
	assert.True(t, strings.HasSuffix(got[0].Message, "(airframe-exempt on line 3 has no reason)"), got[0].Message)
	assert.Empty(t, byRule(res.Diagnostics, "reference-undeclared"))
	assert.Equal(t, []Exemption{
		{Rule: "primitive-read-by-consumer", File: "web/src/a.css", Line: 2, Marker: "airframe-exempt", Reason: "legacy banner, see #12"},
		{Rule: "reference-undeclared", File: "web/src/a.css", Line: 4, Marker: "airframe-exempt", Reason: "vendor sheet"},
	}, res.Exemptions)
}

// failFS fails every read of one file, as a disk error would.
type failFS struct {
	fs.FS
	bad string
}

func (f failFS) Open(name string) (fs.File, error) {
	if name == f.bad {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("input/output error")}
	}
	return f.FS.Open(name)
}

// An I/O failure while scanning is the run's error (exit 2), never a
// clean result.
func TestConsumerScanErrorIsFatal(t *testing.T) {
	p, ds, err := project.Load(consumerFS(consumerConfig, map[string]string{"web/src/a.css": ".a { color: var(--accent); }\n"}))
	require.NoError(t, err)
	require.Empty(t, ds)
	p.FS = failFS{FS: p.FS, bad: "web/src/a.css"}
	res := Run(p, nil, Options{})
	require.Error(t, res.Err)
	assert.Contains(t, res.Err.Error(), "web/src/a.css")
}
