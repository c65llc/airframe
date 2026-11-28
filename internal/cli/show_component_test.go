// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite testdata/show_component.*.golden")

const showConfig = `version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"]}}
  density: {values: [compact, standard, relaxed], base: standard, binding: runtime, web: {attr: data-density, also: {relaxed: "@media (pointer: coarse)"}}}
platforms:
  web:
    emit: css
    consumers: ["web/**/*.css", "web/**/*.tsx"]
  json: {emit: [json]}
  apple:
    emit: [swift, colorsets]
    out: {swift: apple/Generated/DesignTokens.swift, colorsets: apple/Generated/Assets.xcassets/Tokens/}
    consumers: ["apple/Sources/**/*.swift"]
`

var showFiles = map[string]string{
	"design-system/components/button.yaml": `id: button
noun: Button
ruling: "@ruling:button"
tokens: [button-target, button-gap, button-ink, button-bg]
owners: ["web/button/**", "apple/Sources/Button/**"]
implementations:
  web: web/button/Button.tsx
  apple: apple/Sources/Button/PrimaryButton.swift
`,
	"design-system/components/split-button.yaml": `id: split-button
ruling: "@ruling:split-button"
hostedBy: button
owners: ["web/button/**"]
`,
	"web/button/Button.tsx": `export const Button = () => <button style={{ background: "var(--button-bg)", color: "var(--button-bg)", gap: "var(--button-gap)" }} />;
`,
	"apple/Sources/Button/PrimaryButton.swift": "import SwiftUI\n\nlet primaryBackground = DesignTokens.buttonBg\n",
	"apple/Sources/ContentView.swift":          "import SwiftUI\n\nlet ink = DesignTokens.ink\n",
}

// showProject writes an initialised project with the button components
// and extra files, and returns its root.
func showProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	code, _, stderr := run("--root", dir, "init")
	require.Equal(t, ExitOK, code, stderr)
	files := map[string]string{"airframe.yaml": showConfig}
	for k, v := range showFiles {
		files[k] = v
	}
	for k, v := range extra {
		files[k] = v
	}
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	code, _, stderr = run("--root", dir, "build")
	require.Equal(t, ExitOK, code, stderr)
	return dir
}

// golden compares got with testdata/<name>, rewriting it under -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

func TestShowComponentText(t *testing.T) {
	dir := showProject(t, nil)
	code, stdout, stderr := run("--root", dir, "show", "button")
	require.Equal(t, ExitOK, code, stderr)
	assert.Empty(t, stderr)
	golden(t, "show_component.text.golden", stdout)
}

func TestShowComponentJSON(t *testing.T) {
	dir := showProject(t, nil)
	code, stdout, stderr := run("--root", dir, "show", "button", "--json")
	require.Equal(t, ExitOK, code, stderr)
	golden(t, "show_component.json.golden", stdout)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	for _, k := range []string{"id", "noun", "ruling", "shared", "hostedBy", "hosts", "tokens", "owners", "implementations", "consumers"} {
		assert.Contains(t, doc, k)
	}
}

func TestShowComponentGantryStatus(t *testing.T) {
	dir := showProject(t, map[string]string{
		"gantry.yaml":                       "version: 1\ncontent: docs/design\nplatforms:\n  web: {label: Web}\n",
		"docs/design/rulings/button.md":     "---\nid: button\nstatus: superseded\nsupersededBy: new-button\n---\n",
		"docs/design/rulings/new-button.md": "---\nid: new-button\nstatus: ruled\n---\n",
	})
	code, stdout, stderr := run("--root", dir, "show", "button")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "ruling: @ruling:button (superseded by new-button)\n")
	// JSON names the replacement too, so text and JSON carry the same facts.
	code, stdout, stderr = run("--root", dir, "show", "button", "--json")
	require.Equal(t, ExitOK, code, stderr)
	golden(t, "show_component_superseded.json.golden", stdout)
	var doc struct {
		Ruling map[string]string `json:"ruling"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.Equal(t, map[string]string{"ref": "@ruling:button", "status": "superseded", "supersededBy": "new-button"}, doc.Ruling)
	code, stdout, _ = run("--root", dir, "show", "split-button", "--json")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, stdout, `"status": "missing"`)
	assert.NotContains(t, stdout, "supersededBy", "omitted when empty")
}

func TestShowAmbiguous(t *testing.T) {
	dir := showProject(t, map[string]string{
		"design-system/components/accent-ink.yaml": "id: accent-ink\nruling: \"@ruling:accent-ink\"\nowners: [\"web/x/**\"]\n",
	})
	// The token wins, with a hint on stderr that keeps --json one document.
	code, stdout, stderr := run("--root", dir, "show", "accent-ink")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "tier: semantic\n")
	assert.Equal(t, "(also a component: airframe show --component accent-ink)\n", stderr)
	code, stdout, stderr = run("--root", dir, "show", "accent-ink", "--json")
	require.Equal(t, ExitOK, code)
	assert.Contains(t, stderr, "also a component")
	assert.Contains(t, stdout, `"tier": "semantic"`)
	// --component forces the component, with no hint.
	code, stdout, stderr = run("--root", dir, "show", "--component", "accent-ink")
	require.Equal(t, ExitOK, code, stderr)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "accent-ink\nnoun: -\n")
}

func TestShowUnknown(t *testing.T) {
	dir := showProject(t, nil)
	code, _, stderr := run("--root", dir, "show", "zzzzzzzzzz")
	assert.Equal(t, ExitFindings, code)
	assert.Equal(t, "airframe show: unknown token zzzzzzzzzz\n", stderr)
	code, _, stderr = run("--root", dir, "show", "--component", "accent-ink")
	assert.Equal(t, ExitFindings, code)
	assert.Equal(t, "airframe show: unknown component accent-ink\n", stderr)
}

func TestShowComponentScanFailureExits2(t *testing.T) {
	dir := showProject(t, nil)
	// An unreadable consumer file is an I/O failure, not a finding.
	if os.Getuid() == 0 {
		t.Skip("root reads anything")
	}
	bad := filepath.Join(dir, "web", "button", "Button.tsx")
	require.NoError(t, os.Chmod(bad, 0))
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
	code, stdout, stderr := run("--root", dir, "show", "button")
	assert.Equal(t, ExitUsage, code, stderr)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "airframe show: scan consumers:")
}
