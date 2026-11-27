// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package project

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
