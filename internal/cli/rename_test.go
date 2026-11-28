// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeHash hashes every path, mode and file's bytes under dir.
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte(filepath.ToSlash(rel) + "\x00" + fi.Mode().String() + "\x00"))
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			_, _ = h.Write(b)
		}
		return nil
	}))
	return hex.EncodeToString(h.Sum(nil))
}

func TestRenameDryRun(t *testing.T) {
	dir := usagesProject(t)
	before := treeHash(t, dir)
	code, stdout, stderr := run("--root", dir, "rename", "gap", "spacing", "--dry-run")
	require.Equal(t, ExitOK, code, stderr)
	assert.Empty(t, stderr)
	golden(t, "rename.dryrun.golden", stdout)
	assert.Equal(t, before, treeHash(t, dir), "a dry run writes nothing")

	// button-bg leaves the button group for a new bg group, appended after
	// it: it is emitted after button-ink now.
	code, stdout, stderr = run("--root", dir, "rename", "button-bg", "bg-button", "--dry-run")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "  move button.bg → bg.button (appended to its new group: the emitted order changes)\n")

	// A dry run with manual items still exits 0: the plan was printed.
	code, stdout, stderr = run("--root", dir, "rename", "space-2", "space-two", "--dry-run")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "web/src/button/Button.tsx:9  manual: platform web: theme.space.space2 reads space-2")
	assert.Equal(t, before, treeHash(t, dir))
}

func TestRenameManual(t *testing.T) {
	dir := usagesProject(t)
	before := treeHash(t, dir)
	code, stdout, stderr := run("--root", dir, "rename", "space-2", "space-two")
	assert.Equal(t, ExitFindings, code)
	assert.Contains(t, stdout, "  manual: ")
	assert.Contains(t, stderr, "2 reference(s) are left to finish by hand")
	assert.Equal(t, before, treeHash(t, dir), "manual items without --allow-manual write nothing")

	code, stdout, stderr = run("--root", dir, "rename", "space-2", "space-two", "--allow-manual")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "wrote design-system/tokens/primitive.tokens.json\n")
	assert.Contains(t, stdout, "2 reference(s) left to finish by hand")
	assert.Contains(t, readFile(t, dir, "airframe.yaml"), "small: space-two")
	assert.Contains(t, readFile(t, dir, "web/src/button/Button.tsx"), "theme.space.space2", "a pattern read is the user's")
}

func TestRenameRefusals(t *testing.T) {
	dir := usagesProject(t)
	before := treeHash(t, dir)
	for name, c := range map[string]struct {
		args []string
		code int
		msg  string
	}{
		"unknown":  {[]string{"gpa", "spacing"}, ExitFindings, "there is no token gpa"},
		"exists":   {[]string{"gap", "hairline"}, ExitFindings, "token hairline already exists"},
		"invalid":  {[]string{"gap", "Spacing!"}, ExitFindings, "is not a valid token name"},
		"no build": {[]string{"gap", "space2"}, ExitFindings, "the renamed project does not build"},
		"app-local": {[]string{"gap", "sidebar-w"}, ExitFindings,
			"airframe rename: --sidebar-w is already an app-local property of platform web, defined at web/src/app.css:2;"},
		"one arg": {[]string{"gap"}, ExitUsage, "rename takes <old> <new>"},
	} {
		code, stdout, stderr := run(append([]string{"--root", dir, "rename"}, c.args...)...)
		assert.Equal(t, c.code, code, name)
		assert.Empty(t, stdout, name)
		assert.Contains(t, stderr, c.msg, name)
	}
	assert.Equal(t, before, treeHash(t, dir))
}

func TestRenameAllOrNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only directory")
	}
	dir := usagesProject(t)
	// web/src/app.css is one of five consumer files the rename edits, and
	// sorts after the source files and the android and apple ones: they are
	// written to temporary files first, which must all be removed.
	ro := filepath.Join(dir, "web", "src")
	require.NoError(t, os.Chmod(ro, 0o555))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if f, err := os.CreateTemp(ro, "probe"); err == nil {
		// Windows (and some filesystems) ignore a read-only directory
		// mode, so there is no failing write to provoke here.
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Skip("a read-only directory mode does not stop writes here")
	}
	before := treeHash(t, dir)
	code, stdout, stderr := run("--root", dir, "rename", "gap", "spacing")
	assert.Equal(t, ExitUsage, code)
	assert.NotContains(t, stdout, "wrote ")
	assert.Contains(t, stderr, "nothing was written")
	assert.Equal(t, before, treeHash(t, dir), "a failed write leaves every file as it was, and no temporary file")
}

// lintCleanProject is init's starter project with a consumer of every
// platform reading ink: lint and build --check are clean.
func lintCleanProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	code, _, stderr := run("--root", dir, "init")
	require.Equal(t, ExitOK, code, stderr)
	for name, body := range map[string]string{
		"airframe.yaml": `version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"]}}
  density: {values: [compact, standard, relaxed], base: standard, binding: runtime, web: {attr: data-density, also: {relaxed: "@media (pointer: coarse)"}}}
platforms:
  web:
    emit: css
    consumers: ["web/**/*.css", "web/**/*.tsx"]
    wrappers:
      - root: tokens
        generated: true
  apple:
    emit: [swift, colorsets]
    out: {swift: apple/Generated/DesignTokens.swift, colorsets: apple/Generated/Assets.xcassets/Tokens/}
    consumers: ["apple/Sources/**/*.swift"]
  android:
    emit: [kotlin]
    out: {kotlin: android/generated/DesignTokens.kt}
    consumers: ["android/src/**/*.kt"]
    options: {package: com.example.design}
  windows:
    emit: [xaml]
    out: {xaml: windows/Generated/DesignTokens.xaml}
    consumers: ["windows/App/**/*.xaml"]
`,
		"web/app.css":           ".a { color: var(--ink); background: var(--surface); }\n",
		"web/page.tsx":          "export const C = () => <p style={{ color: tokens.ink }} />;\n",
		"apple/Sources/A.swift": "let a = DesignTokens.ink\n",
		"android/src/A.kt":      "val a = DesignTokens.ink\nval b = DesignColorValues.dark.ink\n",
		"windows/App/A.xaml":    "<Page><TextBlock Foreground=\"{ThemeResource DesignInk}\" /></Page>\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	code, _, stderr = run("--root", dir, "build")
	require.Equal(t, ExitOK, code, stderr)
	return dir
}

func TestRenameThenLintClean(t *testing.T) {
	dir := lintCleanProject(t)
	code, stdout, stderr := run("--root", dir, "lint")
	require.Equal(t, ExitOK, code, stdout+stderr)
	require.NotContains(t, stdout, "error ")

	before := treeHash(t, dir)
	code, stdout, stderr = run("--root", dir, "rename", "ink", "foreground", "--dry-run")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Contains(t, stdout, "would remove apple/Generated/Assets.xcassets/Tokens/Ink.colorset\n")
	assert.Equal(t, before, treeHash(t, dir))

	code, stdout, stderr = run("--root", dir, "rename", "ink", "foreground")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "removed apple/Generated/Assets.xcassets/Tokens/Ink.colorset\n")

	code, stdout, stderr = run("--root", dir, "lint")
	assert.Equal(t, ExitOK, code, stdout+stderr)
	assert.NotContains(t, stdout, "error ")
	assert.NotContains(t, stdout, "warn ")
	code, stdout, stderr = run("--root", dir, "build", "--check")
	assert.Equal(t, ExitOK, code, stdout+stderr)
	assert.Empty(t, stdout)

	// No reference to ink is left anywhere outside the generated outputs.
	for _, f := range []string{"web/app.css", "web/page.tsx", "apple/Sources/A.swift", "android/src/A.kt", "windows/App/A.xaml"} {
		got := readFile(t, dir, f)
		assert.NotRegexp(t, `(?i)(--|\.|Design|: )ink\b`, got, f)
		assert.Contains(t, strings.ToLower(got), "foreground", f)
	}
	assert.Contains(t, readFile(t, dir, "design-system/invariants.yaml"), "  fg: foreground\n")
	code, _, stderr = run("--root", dir, "usages", "ink")
	assert.Equal(t, ExitFindings, code)
	assert.Contains(t, stderr, "ink")
	code, stdout, stderr = run("--root", dir, "usages", "foreground")
	require.Equal(t, ExitOK, code, stderr)
	for _, f := range []string{"web/app.css", "web/page.tsx", "apple/Sources/A.swift", "android/src/A.kt", "windows/App/A.xaml"} {
		assert.Contains(t, stdout, f, "usages foreground lists "+f)
	}
}

func TestRenameSymlink(t *testing.T) {
	dir := usagesProject(t)
	f := filepath.Join(dir, "design-system/tokens/semantic.tokens.json")
	real := filepath.Join(dir, "semantic.json")
	require.NoError(t, os.Rename(f, real))
	require.NoError(t, os.Symlink(real, f))
	before := treeHash(t, dir)
	code, _, stderr := run("--root", dir, "rename", "gap", "spacing")
	assert.Equal(t, ExitFindings, code, stderr)
	assert.Contains(t, stderr, "design-system/tokens/semantic.tokens.json is a symbolic link")
	assert.Equal(t, before, treeHash(t, dir))
}

const inkColorset = "apple/Generated/Assets.xcassets/Tokens/Ink.colorset"

func TestRenameStaleColorsetDotfile(t *testing.T) {
	// Finder's .DS_Store in a stale colorset is removed with it.
	dir := lintCleanProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, inkColorset, ".DS_Store"), []byte("x"), 0o644))
	code, stdout, stderr := run("--root", dir, "rename", "ink", "foreground", "--dry-run")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Contains(t, stdout, "would remove "+inkColorset+"\n")
	code, stdout, stderr = run("--root", dir, "rename", "ink", "foreground")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "removed "+inkColorset+"\n")
	assert.NoDirExists(t, filepath.Join(dir, inkColorset))
	code, stdout, stderr = run("--root", dir, "build", "--check")
	assert.Equal(t, ExitOK, code, stdout+stderr)
}

func TestRenameStaleColorsetKept(t *testing.T) {
	// A file airframe did not write keeps the folder, whole, with a warning.
	dir := lintCleanProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, inkColorset, "notes.txt"), []byte("mine"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, inkColorset, ".DS_Store"), []byte("x"), 0o644))
	before := treeHash(t, dir)
	code, stdout, stderr := run("--root", dir, "rename", "ink", "foreground", "--dry-run")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Contains(t, stdout, "would keep "+inkColorset+": it holds files airframe did not write (notes.txt)\n")
	assert.NotContains(t, stdout, "would remove")
	assert.Equal(t, before, treeHash(t, dir))

	code, stdout, stderr = run("--root", dir, "rename", "ink", "foreground")
	require.Equal(t, ExitOK, code, stdout+stderr)
	assert.Contains(t, stderr, "kept "+inkColorset+": it holds files airframe did not write (notes.txt)\n")
	assert.NotContains(t, stdout, "removed")
	for _, f := range []string{"Contents.json", "notes.txt", ".DS_Store"} {
		assert.FileExists(t, filepath.Join(dir, inkColorset, f))
	}
	assert.Contains(t, readFile(t, dir, "apple/Generated/DesignTokens.swift"), "foreground")
	// lint reports it stale until it is deleted by hand.
	_, stdout, _ = run("--root", dir, "lint")
	assert.Contains(t, stdout, "Ink.colorset")
}

func TestRenameRemovalFailureStillBuilds(t *testing.T) {
	dir := lintCleanProject(t)
	was := removeStale
	t.Cleanup(func() { removeStale = was })
	removeStale = func(*os.Root, string, []string) error { return fs.ErrPermission }
	code, stdout, stderr := run("--root", dir, "rename", "ink", "foreground")
	assert.Equal(t, ExitUsage, code, stdout+stderr)
	assert.Contains(t, stderr, "remove stale colorset "+inkColorset)
	assert.Contains(t, stdout, "wrote apple/Generated/DesignTokens.swift\n", "the build still runs")
	assert.Contains(t, readFile(t, dir, "apple/Generated/DesignTokens.swift"), "foreground")
}
