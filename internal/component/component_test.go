// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package component

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const file = "design-system/components/tabstrip.yaml"

func mustParse(t *testing.T, src string) *Component {
	t.Helper()
	c, ds := Parse(file, []byte(src))
	require.Empty(t, ds, src)
	require.NotNil(t, c)
	return c
}

func TestComponentLoad(t *testing.T) {
	c := mustParse(t, `id: tabstrip
noun: Tab strip
tokens: [tabstrip-h, tabstrip-gap]
owners: [web/src/tabstrip/**, "ios/**/TabStrip*.swift"]
hostedBy: shell
implementations: { web: web/src/tabstrip/TabStrip.tsx, android: "n/a: phone only" }
`)
	assert.Equal(t, "tabstrip", c.ID)
	assert.Equal(t, file, c.File)
	assert.Equal(t, 1, c.Line)
	assert.Equal(t, []string{"tabstrip-h", "tabstrip-gap"}, c.Tokens)
	assert.Equal(t, []string{"web/src/tabstrip/**", "ios/**/TabStrip*.swift"}, c.Owners)
	assert.False(t, c.Shared)
	assert.Equal(t, "Tab strip", c.Noun)
	assert.Equal(t, "shell", c.HostedBy)
	assert.Equal(t, map[string]string{"web": "web/src/tabstrip/TabStrip.tsx", "android": "n/a: phone only"}, c.Implementations)

	// shared: true needs no owners; hostedBy may be null.
	c = mustParse(t, "id: tabstrip\nshared: true\nhostedBy: null\n")
	assert.True(t, c.Shared)
	assert.Empty(t, c.Owners)
	assert.Equal(t, "", c.HostedBy)

	for _, tc := range []struct{ label, src, path, msg string }{
		{"id is not the basename", "id: tabs\nowners: [x/**]\n", "id", "must equal the file's basename (tabstrip)"},
		{"id missing", "owners: [x/**]\n", "", "id"},
		{"id number", "id: 5\nowners: [x/**]\n", "id", "string"},
		{"owners missing", "id: tabstrip\n", "owners", "owners is required"},
		{"owners empty", "id: tabstrip\nowners: []\n", "owners", "owners is required"},
		{"owners missing, shared false", "id: tabstrip\nshared: false\n", "owners", "owners is required"},
		{"shared string", "id: tabstrip\nshared: 'true'\nowners: [x/**]\n", "shared", "boolean"},
		{"unknown field", "id: tabstrip\nowners: [x/**]\ncolour: red\n", "colour", "unknown key"},
		{"implementations a list", "id: tabstrip\nowners: [x/**]\nimplementations: [web]\n", "implementations", "object"},
		{"implementations value a number", "id: tabstrip\nowners: [x/**]\nimplementations: { web: 5 }\n", "implementations.web", "string"},
		{"implementations value a timestamp", "id: tabstrip\nowners: [x/**]\nimplementations: { web: 2026-01-01 }\n", "implementations.web", "must be a string"},
		{"noun a number", "id: tabstrip\nowners: [x/**]\nnoun: 5\n", "noun", "string"},
		{"ruling a list", "id: tabstrip\nowners: [x/**]\nruling: [a]\n", "ruling", "string"},
		{"hostedBy a bool", "id: tabstrip\nowners: [x/**]\nhostedBy: true\n", "hostedBy", ""},
		{"tokens a string", "id: tabstrip\nowners: [x/**]\ntokens: tabstrip-h\n", "tokens", "array"},
		{"tokens duplicate", "id: tabstrip\nowners: [x/**]\ntokens: [a, a]\n", "tokens", ""},
		{"owner escapes the root", "id: tabstrip\nowners: [../x/**]\n", "owners.0", "repo-relative glob"},
		{"owner absolute", "id: tabstrip\nowners: [/x/**]\n", "owners.0", "repo-relative glob"},
		{"owner backslash", "id: tabstrip\nowners: ['x\\y']\n", "owners.0", "repo-relative glob"},
		{"owner bad glob", "id: tabstrip\nowners: ['x/[a']\n", "owners.0", "not a valid glob"},
		{"empty file", "", "", "object"},
		{"a list", "- id: tabstrip\n", "", "object"},
	} {
		c, ds := Parse(file, []byte(tc.src))
		assert.Nil(t, c, tc.label)
		require.Len(t, ds, 1, tc.label)
		assert.Equal(t, "schema-invalid", ds[0].Rule, tc.label)
		assert.Equal(t, file, ds[0].File, tc.label)
		assert.Equal(t, tc.path, ds[0].Path, tc.label)
		assert.Contains(t, ds[0].Message, tc.msg, tc.label)
	}

	_, ds := Parse(file, []byte("id: [\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "yaml-syntax", ds[0].Rule)
	assert.Equal(t, file, ds[0].File)
}

func TestComponentLoadLines(t *testing.T) {
	_, ds := Parse(file, []byte("# a tab strip\nid: tabs\nowners: [x/**]\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, 2, ds[0].Line)
	_, ds = Parse(file, []byte("id: tabstrip\nowners:\n  - x/**\n  - ../y\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, 4, ds[0].Line)
}

func TestSpec63ComponentExampleLoads(t *testing.T) {
	data, err := os.ReadFile("../../docs/specs/2026-11-27-airframe-design.md")
	require.NoError(t, err)
	_, after, ok := strings.Cut(string(data), "**Component** (`components/tabstrip.yaml`")
	require.True(t, ok)
	_, after, ok = strings.Cut(after, "```yaml\n")
	require.True(t, ok)
	block, _, ok := strings.Cut(after, "```")
	require.True(t, ok)

	c := mustParse(t, block)
	assert.Equal(t, "tabstrip", c.ID)
	assert.Equal(t, "@ruling:tab-strip", c.Ruling)
	assert.Equal(t, []string{"tabstrip-h", "tabstrip-max-w", "tabstrip-gap"}, c.Tokens)
	assert.Len(t, c.Owners, 2)
	assert.Equal(t, "", c.HostedBy)
	assert.Len(t, c.Implementations, 4)
}
