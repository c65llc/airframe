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

	"github.com/c65llc/airframe/internal/schema"
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

// A shared component's tokens may be read from anywhere: each read says
// [shared], and its JSON carries shared: true in place of owner.
func TestUsagesShared(t *testing.T) {
	dir := usagesProject(t)
	f := filepath.Join(dir, "design-system/components/button.yaml")
	body, err := os.ReadFile(f)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f, append(body, "shared: true\n"...), 0o644))

	code, stdout, stderr := run("--root", dir, "usages", "button-bg")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "android/src/main/Screen.kt:13:16  DesignTokens.buttonBg  [shared]\n")
	assert.NotContains(t, stdout, "owner]")

	code, stdout, stderr = run("--root", dir, "usages", "button-bg", "--json")
	require.Equal(t, ExitOK, code, stderr)
	var doc struct {
		References []map[string]any `json:"references"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.NotEmpty(t, doc.References)
	for _, r := range doc.References {
		assert.Equal(t, true, r["shared"], r)
		assert.NotContains(t, r, "owner")
	}
	var v any
	require.NoError(t, json.Unmarshal([]byte(stdout), &v))
	assert.Empty(t, schema.Validate("usages", v))
}

func TestUsagesJSONSchema(t *testing.T) {
	dir := usagesProject(t)
	for _, name := range []string{"button-bg", "gap", "accent", "gray-900"} {
		code, stdout, stderr := run("--root", dir, "usages", name, "--json")
		require.Equal(t, ExitOK, code, stderr)
		var v any
		require.NoError(t, json.Unmarshal([]byte(stdout), &v))
		assert.Empty(t, schema.Validate("usages", v), name)
	}
	// The schema is served, and rejects an owner that is not a boolean.
	code, served, _ := run("schema", "usages")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, served, "airframe usages --json")
	bad := map[string]any{"name": "x", "tier": "component", "component": "c", "aliasedBy": []any{},
		"references": []any{map[string]any{"platform": "web", "file": "f", "line": 1, "col": 1, "raw": "r", "via": "direct", "owner": "shared"}}}
	assert.NotEmpty(t, schema.Validate("usages", bad))
}
