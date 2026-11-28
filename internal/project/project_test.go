// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package project

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
)

const (
	cfg  = "version: 1\n"
	tokA = `{ "color": { "$type": "color", "ink": { "$value": { "colorSpace": "oklch", "components": [0.2, 0, 0] } } } }`
	tokB = `{ "space": { "$type": "dimension", "s": { "$value": { "value": 4, "unit": "px" } } } }`
	invs = "- id: a\n  kind: contrast\n  fg: x\n  bg: y\n  min: 4.5\n" +
		"- id: b\n  kind: hue-reserved\n  hue: 25\n  tolerance: 8\n" +
		"- id: c\n  kind: min-dimension\n  match: \"*\"\n  min: 44\n" +
		"- id: d\n  kind: same-hue-across-appearances\n  tokens: [x]\n  forEach: g\n"
)

func TestLoadMinimal(t *testing.T) {
	fsys := MemFS{
		"airframe.yaml":                     cfg,
		"design-system/b/space.tokens.json": tokB,
		"design-system/color.tokens.json":   tokA,
		"design-system/invariants.yaml":     invs,
		"other/ignored.tokens.json":         tokA,
	}
	p, ds, err := Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	require.NotNil(t, p)
	assert.Equal(t, []string{"design-system/b/space.tokens.json", "design-system/color.tokens.json"}, p.TokenFiles)
	assert.Len(t, p.Invariants, 4)
	assert.NotNil(t, p.Tokens)
	assert.Contains(t, p.Sources, "design-system/color.tokens.json")
	assert.Contains(t, p.Sources, "airframe.yaml")
}

func TestLoadNoInvariantsFileIsFine(t *testing.T) {
	p, ds, err := Load(MemFS{"airframe.yaml": cfg, "design-system/c.tokens.json": tokA})
	require.NoError(t, err)
	require.Empty(t, ds)
	assert.Empty(t, p.Invariants)
}

func TestLoadNoTokens(t *testing.T) {
	p, ds, err := Load(MemFS{"airframe.yaml": cfg})
	require.NoError(t, err)
	assert.Nil(t, p)
	require.Len(t, ds, 1)
	assert.Equal(t, "no-tokens", ds[0].Rule)
	assert.Equal(t, "error", string(ds[0].Severity))
	assert.Equal(t, "no *.tokens.json under design-system/ (content:)", ds[0].Message)
	assert.NotEmpty(t, ds[0].Docs)
}

func TestLoadConfigMissing(t *testing.T) {
	p, ds, err := Load(MemFS{})
	require.NoError(t, err)
	assert.Nil(t, p)
	require.Len(t, ds, 1)
	assert.Equal(t, "config-missing", ds[0].Rule)
	assert.Equal(t, "no airframe.yaml at the project root", ds[0].Message)
	assert.NotEmpty(t, ds[0].Docs)
}

func TestLoadCustomContentDir(t *testing.T) {
	p, ds, err := Load(MemFS{"airframe.yaml": "version: 1\ncontent: tokens\n", "tokens/c.tokens.json": tokA})
	require.NoError(t, err)
	require.Empty(t, ds)
	assert.Equal(t, []string{"tokens/c.tokens.json"}, p.TokenFiles)
}

func TestLoadInvariantErrorsCarryPath(t *testing.T) {
	p, ds, err := Load(MemFS{
		"airframe.yaml": cfg, "design-system/c.tokens.json": tokA,
		"design-system/invariants.yaml": "- id: x\n  kind: nope\n",
	})
	require.NoError(t, err)
	require.NotNil(t, p, "an invariants error is a partial load")
	assert.Nil(t, p.Invariants)
	assert.NotNil(t, p.Tokens)
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, "design-system/invariants.yaml", ds[0].File)
	assert.Equal(t, 2, ds[0].Line)
}

func TestLoadBadConfig(t *testing.T) {
	p, ds, err := Load(MemFS{"airframe.yaml": "version: 2\n"})
	require.NoError(t, err)
	assert.Nil(t, p)
	require.NotEmpty(t, ds)
}

func TestMemFS(t *testing.T) {
	m := MemFS{"a/b/c.txt": "hi", "a/d.txt": "yo"}
	b, err := fs.ReadFile(m, "a/b/c.txt")
	require.NoError(t, err)
	assert.Equal(t, "hi", string(b))
	ents, err := fs.ReadDir(m, "a")
	require.NoError(t, err)
	require.Len(t, ents, 2)
	assert.Equal(t, "b", ents[0].Name())
	assert.True(t, ents[0].IsDir())
	_, err = fs.ReadFile(m, "nope")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestLoadPartialTokenError(t *testing.T) {
	p, ds, err := Load(MemFS{
		"airframe.yaml":               cfg,
		"design-system/a.tokens.json": "{ not json",
		"design-system/b.tokens.json": tokB,
	})
	require.NoError(t, err)
	require.NotNil(t, p, "a token-file error is a partial load")
	require.Len(t, ds, 1)
	assert.Equal(t, "json-syntax", ds[0].Rule)
	assert.Equal(t, "design-system/a.tokens.json", ds[0].File)
	require.NotNil(t, p.Tokens)
	assert.Contains(t, p.Tokens.Tokens, "space-s")
	assert.Equal(t, []string{"design-system/a.tokens.json", "design-system/b.tokens.json"}, p.TokenFiles)
}

// tokTabs holds two tabstrip-owned tokens, one owned by a component with no
// file, and a semantic one.
const tokTabs = `{
  "tabstrip": { "$type": "dimension",
    "h":   { "$value": { "value": 36, "unit": "px" }, "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "tabstrip" } } },
    "gap": { "$value": { "value": 4, "unit": "px" }, "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "tabstrip" } } }
  },
  "sidebar": { "w": { "$type": "dimension", "$value": { "value": 240, "unit": "px" }, "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "sidebar" } } } },
  "gutter": { "$type": "dimension", "$value": { "value": 8, "unit": "px" }, "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } }
}`

const tabsFile = "design-system/components/tabstrip.yaml"

func loadTabs(t *testing.T, component string, extra MemFS) (*Project, []diag.Diagnostic) {
	t.Helper()
	fsys := MemFS{"airframe.yaml": cfg, "design-system/tabs.tokens.json": tokTabs, tabsFile: component}
	for k, v := range extra {
		fsys[k] = v
	}
	p, ds, err := Load(fsys)
	require.NoError(t, err)
	require.NotNil(t, p)
	return p, ds
}

func TestLoadComponents(t *testing.T) {
	p, ds := loadTabs(t, "id: tabstrip\ntokens: [tabstrip-h, tabstrip-gap]\nowners: [web/tabs/**]\n", MemFS{
		"design-system/components/nested/ignored.yaml": "id: ignored\n",
		"design-system/components/notes.md":            "not a component",
	})
	require.Empty(t, ds)
	require.Len(t, p.Components, 1)
	c := p.Components["tabstrip"]
	require.NotNil(t, c)
	assert.Equal(t, tabsFile, c.File)
	assert.Equal(t, []string{tabsFile}, p.ComponentFiles)
	assert.Contains(t, p.Sources, tabsFile)
}

func TestLoadNoComponents(t *testing.T) {
	p, ds, err := Load(MemFS{"airframe.yaml": cfg, "design-system/c.tokens.json": tokA})
	require.NoError(t, err)
	require.Empty(t, ds)
	assert.Empty(t, p.Components)
	assert.Empty(t, p.ComponentFiles)
}

func TestLoadComponentErrorIsPartial(t *testing.T) {
	p, ds := loadTabs(t, "id: tabs\nowners: [x/**]\n", nil)
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, tabsFile, ds[0].File)
	assert.Equal(t, "id", ds[0].Path)
	assert.Empty(t, p.Components)
	assert.NotNil(t, p.Tokens)
	// The file is still a source (fmt formats it); and an unloaded
	// component file does not also report its tokens as unlisted.
	assert.Equal(t, []string{tabsFile}, p.ComponentFiles)
}

func TestComponentConsistency(t *testing.T) {
	for _, tc := range []struct {
		label, src, path, msg string
		line                  int
	}{
		{"listed token does not exist", "id: tabstrip\ntokens: [tabstrip-h, tabstrip-gap, tabstrip-max-w]\nowners: [x/**]\n",
			"tokens.2", "tabstrip-max-w does not exist", 2},
		{"listed token owned elsewhere", "id: tabstrip\ntokens: [tabstrip-h, tabstrip-gap, sidebar-w]\nowners: [x/**]\n",
			"tokens.2", "sidebar-w belongs to component sidebar, not tabstrip", 2},
		{"component token not listed", "id: tabstrip\ntokens: [tabstrip-h]\nowners: [x/**]\n",
			"tokens", "tabstrip-gap", 2},
		{"component token not listed, at the tokens key", "# tabs\nid: tabstrip\ntokens: [tabstrip-gap]\nowners: [x/**]\n",
			"tokens", "tabstrip-h", 3},
	} {
		_, ds := loadTabs(t, tc.src, nil)
		require.Len(t, ds, 1, tc.label)
		d := ds[0]
		assert.Equal(t, "schema-invalid", d.Rule, tc.label)
		assert.Equal(t, tabsFile, d.File, tc.label)
		assert.Equal(t, tc.path, d.Path, tc.label)
		assert.Equal(t, tc.line, d.Line, tc.label)
		assert.Contains(t, d.Message, tc.msg, tc.label)
	}

	// A semantic token listed as owned.
	_, ds := loadTabs(t, "id: tabstrip\ntokens: [tabstrip-h, tabstrip-gap, gutter]\nowners: [x/**]\n", nil)
	require.Len(t, ds, 1)
	assert.Equal(t, "tokens.2", ds[0].Path)
	assert.Contains(t, ds[0].Message, "gutter is tier semantic, not component")

	// Both unlisted tokens are reported, in token order, one finding each.
	_, ds = loadTabs(t, "id: tabstrip\nowners: [x/**]\n", nil)
	require.Len(t, ds, 2)
	assert.Equal(t, 1, ds[0].Line, "with no tokens key, at the record")
	assert.Contains(t, ds[0].Message, "tabstrip-h")
	assert.Contains(t, ds[1].Message, "tabstrip-gap")
	assert.Contains(t, ds[0].Message, "design-system/tabs.tokens.json")

	// sidebar-w's component has no file: not a load error.
	_, ds = loadTabs(t, "id: tabstrip\ntokens: [tabstrip-h, tabstrip-gap]\nowners: [x/**]\n", nil)
	assert.Empty(t, ds)
}
