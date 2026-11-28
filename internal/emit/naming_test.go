// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/tokens"
)

func TestCamelPascal(t *testing.T) {
	for _, tc := range []struct{ in, camel, pascal string }{
		{"accent-ink", "accentInk", "AccentInk"},
		{"space-4", "space4", "Space4"},
		{"gap", "gap", "Gap"},
		{"tabstrip-max-w", "tabstripMaxW", "TabstripMaxW"},
		{"2x-gap", "2xGap", "2xGap"},
	} {
		assert.Equal(t, tc.camel, Camel(tc.in), tc.in)
		assert.Equal(t, tc.pascal, Pascal(tc.in), tc.in)
	}
}

func TestSwiftIdent(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"default", "`default`"},
		{"case", "`case`"},
		{"Type", "`Type`"},
		{"self", "`self`"},
		{"2x", "_2x"},
		{"accentInk", "accentInk"},
		{"defaultGap", "defaultGap"},
		{"type", "type"},
	} {
		assert.Equal(t, tc.want, SwiftIdent(tc.in), tc.in)
	}
}

func TestApply(t *testing.T) {
	for _, tc := range []struct{ tmpl, typ, member string }{
		{"DesignTokens.{camel}", "DesignTokens", "accentInk"},
		{"Design{Pascal}", "", "DesignAccentInk"},
		{"--{name}", "", "--accent-ink"},
		{"Theme.{Pascal}", "Theme", "AccentInk"},
	} {
		typ, member := Apply(tc.tmpl, "accent-ink")
		assert.Equal(t, tc.typ, typ, tc.tmpl)
		assert.Equal(t, tc.member, member, tc.tmpl)
	}
}

func TestNameForOverride(t *testing.T) {
	cfg, ds := config.Load([]byte("version: 1\nnaming: { swift: \"Theme.{Pascal}\" }\n"))
	require.Empty(t, ds)
	plain := &tokens.Token{Name: "accent-ink"}
	assert.Equal(t, "AccentInk", NameFor(cfg, plain, "swift"))
	assert.Equal(t, "accentInk", NameFor(cfg, plain, "kotlin"), "the default template")
	named := &tokens.Token{Name: "accent-ink", Names: map[string]string{"swift": "brandInk"}}
	assert.Equal(t, "brandInk", NameFor(cfg, named, "swift"))
	assert.Equal(t, "accentInk", NameFor(cfg, named, "kotlin"), "a name for one format only")
}
