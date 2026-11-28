// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usagesProject copies testdata/consumers, which has wrappers, a Kotlin
// colour slot and a component with owners, into a temp dir.
func usagesProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS("../../testdata/consumers")))
	return dir
}

func TestUsagesText(t *testing.T) {
	code, stdout, stderr := run("--root", usagesProject(t), "usages", "button-bg")
	require.Equal(t, ExitOK, code, stderr)
	assert.Empty(t, stderr)
	golden(t, "usages.text.golden", stdout)
}

func TestUsagesJSON(t *testing.T) {
	code, stdout, stderr := run("--root", usagesProject(t), "usages", "button-bg", "--json")
	require.Equal(t, ExitOK, code, stderr)
	golden(t, "usages.json.golden", stdout)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	for _, k := range []string{"name", "tier", "component", "references", "aliasedBy"} {
		assert.Contains(t, doc, k)
	}
}

// A read through a wrapper, and one through a Kotlin colour slot, are
// still reads of the token.
func TestUsagesViaWrapper(t *testing.T) {
	dir := usagesProject(t)
	code, stdout, stderr := run("--root", dir, "usages", "gap")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "android/src/main/Screen.kt:11:11  LocalTokens.current.gap  (via wrapper:LocalTokens.current)\n")
	assert.Contains(t, stdout, "web/src/button/Button.tsx:8:12  theme.space.space4  (via wrapper:theme.space)\n")

	code, stdout, stderr = run("--root", dir, "usages", "surface")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "android/src/main/Screen.kt:7:19  DesignColorValues.dark.surface\n")
}

func TestUsagesAliasedBy(t *testing.T) {
	dir := usagesProject(t)
	code, stdout, stderr := run("--root", dir, "usages", "accent")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "aliased by:\n  button-bg  default  design-system/tokens/component.tokens.json:")
	// A mode value that aliases counts, and names its key.
	code, stdout, stderr = run("--root", dir, "usages", "gray-900")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "  ink  default  ")
	assert.Contains(t, stdout, "  surface  appearance:dark  ")
}

func TestUsagesNone(t *testing.T) {
	dir := usagesProject(t)
	const unused = `{"unused": {"$type": "dimension", "$value": {"value": 3, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "design-system/tokens/unused.tokens.json"), []byte(unused), 0o644))
	code, stdout, stderr := run("--root", dir, "usages", "unused")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "no usages\n")
	code, stdout, _ = run("--root", dir, "usages", "unused", "--json")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, stdout, `"references": []`)
	assert.Contains(t, stdout, `"aliasedBy": []`)
}

func TestUsagesUnknown(t *testing.T) {
	code, stdout, stderr := run("--root", usagesProject(t), "usages", "buton-bg")
	assert.Equal(t, ExitFindings, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "unknown token buton-bg; did you mean button-bg?")
}

func TestUsagesPlatformFilter(t *testing.T) {
	dir := usagesProject(t)
	code, stdout, stderr := run("--root", dir, "usages", "ink", "--platform", "android")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "android/src/main/Screen.kt")
	assert.NotContains(t, stdout, "apple/")
	assert.NotContains(t, stdout, "web/")
	code, _, stderr = run("--root", dir, "usages", "ink", "--platform", "nope")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "unknown platform nope")
}

func TestUsagesOwner(t *testing.T) {
	code, stdout, stderr := run("--root", usagesProject(t), "usages", "button-bg")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "component: button\n")
	assert.Contains(t, stdout, "android/src/button/Button.kt:5:10  DesignTokens.buttonBg  [owner]\n")
	assert.Contains(t, stdout, "android/src/main/Screen.kt:13:16  DesignTokens.buttonBg  [not owner]\n")
}
