// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	require.NoError(t, err)
	return string(b)
}

func TestRetireToken(t *testing.T) {
	dir := usagesProject(t)
	code, stdout, stderr := run("--root", dir, "retire", "gap")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "retired gap in design-system/tokens/semantic.tokens.json\n")
	assert.Contains(t, stdout, "retired-used will report:\n")
	assert.Contains(t, stdout, "  android  android/src/main/Screen.kt:11:11  LocalTokens.current.gap  (via wrapper:LocalTokens.current)\n")
	assert.Contains(t, stdout, "  web  web/src/app.css:")
	assert.Contains(t, readFile(t, dir, "design-system/tokens/semantic.tokens.json"), `"retired": true`)
	_, stdout, _ = run("--root", dir, "find", "gap")
	assert.NotContains(t, stdout, "gap  semantic", "a retired token is hidden from find")
	_, stdout, _ = run("--root", dir, "find", "gap", "--retired")
	assert.Contains(t, stdout, "gap  semantic")
}

func TestRetireTokenCreatesExtensions(t *testing.T) {
	dir := t.TempDir()
	require.Equal(t, ExitOK, func() int { c, _, _ := run("--root", dir, "init"); return c }())
	const tok = `{"loose": {"$type": "dimension", "$value": {"value": 3, "unit": "px"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "design-system/tokens/loose.tokens.json"), []byte(tok), 0o644))
	code, stdout, stderr := run("--root", dir, "retire", "loose")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Contains(t, readFile(t, dir, "design-system/tokens/loose.tokens.json"), "\"com.c65llc.airframe\": {\n        \"retired\": true")
	assert.Contains(t, stdout, "no reads or aliases")
}

func TestRetireAlready(t *testing.T) {
	dir := usagesProject(t)
	require.Equal(t, ExitOK, func() int { c, _, _ := run("--root", dir, "retire", "gap"); return c }())
	before := readFile(t, dir, "design-system/tokens/semantic.tokens.json")
	code, stdout, stderr := run("--root", dir, "retire", "gap")
	assert.Equal(t, ExitFindings, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "gap is already retired")
	assert.Equal(t, before, readFile(t, dir, "design-system/tokens/semantic.tokens.json"))
	code, _, stderr = run("--root", dir, "retire", "gpa")
	assert.Equal(t, ExitFindings, code)
	assert.Contains(t, stderr, "unknown token gpa; did you mean gap?")
}

func TestRetireTreatment(t *testing.T) {
	dir := usagesProject(t)
	code, stdout, stderr := run("--root", dir, "retire", "--treatment", "hairline-shadow",
		"--spelling", `css=0 0 0 1px`, "--spelling", `css=box-shadow:\s*none`, "--ruling", "@ruling:x", "--marker", "audited")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Contains(t, stdout, "retired treatment hairline-shadow in design-system/retired.yaml\n")
	assert.Contains(t, stdout, "retired-used will report:\n  web  web/src/app.css:")
	got := readFile(t, dir, "design-system/retired.yaml")
	assert.Contains(t, got, "- id: hairline-shadow\n  ruling: '@ruling:x'\n  marker: audited\n  spellings:\n    css:\n")

	// A second entry is appended, the first kept.
	code, _, stderr = run("--root", dir, "retire", "--treatment", "second", "--spelling", `swift=\.shadow\(`)
	require.Equal(t, ExitOK, code, stderr)
	got = readFile(t, dir, "design-system/retired.yaml")
	assert.Contains(t, got, "id: hairline-shadow")
	assert.Contains(t, got, "id: second")

	// Refusals write nothing.
	for name, args := range map[string][]string{
		"duplicate":       {"--treatment", "second", "--spelling", "css=x"},
		"bad regex":       {"--treatment", "third", "--spelling", "css=("},
		"unknown adapter": {"--treatment", "third", "--spelling", "go=x"},
		"bad id":          {"--treatment", "Bad_ID", "--spelling", "css=x"},
	} {
		code, stdout, stderr = run(append([]string{"--root", dir, "retire"}, args...)...)
		assert.Equal(t, ExitFindings, code, name+": "+stderr)
		assert.Empty(t, stdout, name)
		assert.NotEmpty(t, stderr, name)
		assert.Equal(t, got, readFile(t, dir, "design-system/retired.yaml"), name)
	}
	code, _, _ = run("--root", dir, "retire", "--treatment", "x")
	assert.Equal(t, ExitUsage, code)
	code, _, _ = run("--root", dir, "retire", "gap", "--spelling", "css=x")
	assert.Equal(t, ExitUsage, code)
	code, _, _ = run("--root", dir, "retire")
	assert.Equal(t, ExitUsage, code)
}

func TestRetireDryRun(t *testing.T) {
	dir := usagesProject(t)
	tok := readFile(t, dir, "design-system/tokens/semantic.tokens.json")
	code, stdout, stderr := run("--root", dir, "retire", "gap", "--dry-run")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "would retire gap in ")
	assert.Equal(t, tok, readFile(t, dir, "design-system/tokens/semantic.tokens.json"))

	code, stdout, stderr = run("--root", dir, "retire", "--treatment", "t", "--spelling", "css=var\\(--gap", "--dry-run")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "would retire treatment t in design-system/retired.yaml\n")
	assert.Contains(t, stdout, "web/src/app.css:")
	_, err := os.Stat(filepath.Join(dir, "design-system/retired.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestRetireThenLint(t *testing.T) {
	dir := usagesProject(t)
	code, stdout, stderr := run("--root", dir, "retire", "hairline")
	require.Equal(t, ExitOK, code, stderr)
	require.Contains(t, stdout, "  web  web/src/app.css:11:")
	code, stdout, _ = run("--root", dir, "lint")
	assert.Equal(t, ExitFindings, code)
	assert.Contains(t, stdout, "web/src/app.css:11:")
	assert.Contains(t, stdout, "error retired-used: platform web: ")
	assert.Contains(t, stdout, "token hairline, which is retired")
}

// A token file that starts with a UTF-8 byte order mark retires like any
// other.
func TestRetireTokenBOM(t *testing.T) {
	dir := usagesProject(t)
	f := filepath.Join(dir, "design-system/tokens/semantic.tokens.json")
	b, err := os.ReadFile(f)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f, append([]byte{0xEF, 0xBB, 0xBF}, b...), 0o644))
	code, stdout, stderr := run("--root", dir, "retire", "gap")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Contains(t, readFile(t, dir, "design-system/tokens/semantic.tokens.json"), `"retired": true`)
}

// A symlinked token file is refused, so the write never replaces the link
// with a regular file.
func TestRetireTokenSymlink(t *testing.T) {
	dir := usagesProject(t)
	f := filepath.Join(dir, "design-system/tokens/semantic.tokens.json")
	real := filepath.Join(dir, "semantic.json")
	require.NoError(t, os.Rename(f, real))
	require.NoError(t, os.Symlink(real, f))
	code, stdout, stderr := run("--root", dir, "retire", "gap")
	assert.Equal(t, ExitFindings, code, stdout+stderr)
	assert.Contains(t, stderr, "design-system/tokens/semantic.tokens.json is a symbolic link")
	fi, err := os.Lstat(f)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "the link is still a link")
	assert.NotContains(t, readFile(t, dir, "semantic.json"), `"retired"`)
}

func TestRetireHelpWidth(t *testing.T) {
	_, stdout, _ := run("help")
	for _, l := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(l, "  retire ") {
			assert.True(t, strings.HasPrefix(l, "  retire [--dry-run] [<token>] [--treatment ID --spelling A=RE…]  "), l)
		}
		if i := strings.Index(l, "Mark a token retired"); i >= 0 {
			assert.Less(t, i, 80, "the synopsis column is not padded out by retire")
		}
	}
	_, stdout, _ = run("retire", "--help")
	assert.Contains(t, stdout, "--spelling ADAPTER=RE2", "the command's own usage lists every flag")
}
