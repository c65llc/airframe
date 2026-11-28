// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A token's version changes with its own node or one of its mode values,
// not with another token or a change fmt would undo.
func TestRecordVersion(t *testing.T) {
	root := fixture(t, "consumers")
	version := func(name string) string {
		p, _ := loaded(t, root)
		tok := p.Tokens.Tokens[name]
		require.NotNil(t, tok, name)
		return tokenVersion(p, tok)
	}
	surface, accent := version("surface"), version("accent")
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, surface)
	assert.NotEqual(t, surface, accent)

	file := "design-system/tokens/semantic.tokens.json"
	src := read(t, root, file)

	// Reindenting the file is not a change.
	write(t, root, file, strings.ReplaceAll(src, "\n", "\n  "))
	assert.Equal(t, surface, version("surface"))
	write(t, root, file, src)

	// Editing another token is not a change to this one.
	p, _ := loaded(t, root)
	require.NotEqual(t, p.Tokens.Tokens["accent"].File, "", "accent has a file")
	write(t, root, file, strings.Replace(src, `"surface": {`, `"surface": {"$description": "Page background.",`, 1))
	assert.NotEqual(t, surface, version("surface"))
	assert.Equal(t, accent, version("accent"))

	// A component's version is its file's.
	p, _ = loaded(t, root)
	button := componentVersion(p, p.Components["button"])
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, button)
	comp := "design-system/components/button.yaml"
	write(t, root, comp, read(t, root, comp)+"noun: button\n")
	p, _ = loaded(t, root)
	assert.NotEqual(t, button, componentVersion(p, p.Components["button"]))
}
