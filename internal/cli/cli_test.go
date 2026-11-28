// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/selftest"
)

// run runs the command line and returns its exit status, stdout and stderr.
func run(args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := Main(args, nil, &out, &errw)
	return code, out.String(), errw.String()
}

// gateDoc is the part of the gate JSON these tests read.
type gateDoc struct {
	Gate     string `json:"gate"`
	Selftest struct {
		OK       bool     `json:"ok"`
		Failures []string `json:"failures"`
	} `json:"selftest"`
	Scanned struct {
		Files int `json:"files"`
	} `json:"scanned"`
	Findings []struct {
		Rule string `json:"rule"`
	} `json:"findings"`
}

func TestGateFailedSelftestExits1(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := run("--root", dir, "init")
	require.Equal(t, ExitOK, code, stderr)

	saved := selftestRun
	t.Cleanup(func() { selftestRun = saved })
	selftestRun = func() selftest.Result {
		return selftest.Result{Flagged: 27, Ignored: 28, Failures: []string{"flag: tier-violation did not fire"}}
	}
	code, stdout, _ := run("--root", dir, "lint", "--format", "gate")
	assert.Equal(t, ExitFindings, code)
	var doc gateDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), stdout)
	assert.Equal(t, "airframe", doc.Gate)
	assert.False(t, doc.Selftest.OK)
	assert.Equal(t, []string{"flag: tier-violation did not fire"}, doc.Selftest.Failures)
	assert.Empty(t, doc.Findings, "the project itself is clean")
	assert.Equal(t, 6, doc.Scanned.Files)
}

func TestGateWithoutProject(t *testing.T) {
	dir := t.TempDir()
	code, stdout, _ := run("--root", dir, "lint", "--format", "gate")
	assert.Equal(t, ExitFindings, code)
	var doc gateDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), stdout)
	assert.True(t, doc.Selftest.OK)
	assert.Equal(t, 0, doc.Scanned.Files)
	require.Len(t, doc.Findings, 1)
	assert.Equal(t, "config-missing", doc.Findings[0].Rule)
}

func TestWriteFileCreatesParentsAtomically(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	require.NoError(t, writeFile(root, "a/b/c.css", []byte("x\n")))
	require.NoError(t, writeFile(root, "a/b/c.css", []byte("y\n")))
	got, err := os.ReadFile(filepath.Join(dir, "a", "b", "c.css"))
	require.NoError(t, err)
	assert.Equal(t, "y\n", string(got))
	ents, err := os.ReadDir(filepath.Join(dir, "a", "b"))
	require.NoError(t, err)
	require.Len(t, ents, 1, "no temporary file left behind")
	fi, err := ents[0].Info()
	require.NoError(t, err)
	assertPerm(t, runtime.GOOS, 0o644, fi.Mode().Perm())

	// An existing file keeps its permission bits.
	require.NoError(t, os.Chmod(filepath.Join(dir, "a", "b", "c.css"), 0o600))
	require.NoError(t, writeFile(root, "a/b/c.css", []byte("z\n")))
	fi, err = os.Stat(filepath.Join(dir, "a", "b", "c.css"))
	require.NoError(t, err)
	assertPerm(t, runtime.GOOS, 0o600, fi.Mode().Perm())

	assert.Error(t, writeFile(root, "../escape.css", []byte("x\n")))
}

// assertPerm checks a file's permission bits. Windows has no Unix modes:
// Go reports 0666 for a writable file and 0444 for a read-only one, so
// there only the owner-write bit (the read-only attribute) is compared.
func assertPerm(t *testing.T, goos string, want, got os.FileMode) {
	t.Helper()
	if goos == "windows" {
		assert.Equal(t, want&0o200, got&0o200, "read-only attribute: want %v, got %v", want, got)
		return
	}
	assert.Equal(t, want, got)
}

func TestAssertPermOnWindows(t *testing.T) {
	// What Windows reports for the two files above passes there.
	assertPerm(t, "windows", 0o644, 0o666)
	assertPerm(t, "windows", 0o600, 0o666)
	ft := &testing.T{}
	assertPerm(ft, "windows", 0o644, 0o444)
	assert.True(t, ft.Failed(), "a read-only file is not a writable one")
	ft = &testing.T{}
	assertPerm(ft, "linux", 0o644, 0o666)
	assert.True(t, ft.Failed(), "elsewhere the full bits are compared")
}

// TestBuildRefusesSymlinkOutOfRoot: a generated directory that is a symlink
// to a directory outside the project stops the build, and nothing is
// written outside.
func TestBuildRefusesSymlinkOutOfRoot(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	code, _, stderr := run("--root", dir, "init")
	require.Equal(t, ExitOK, code, stderr)
	gen := filepath.Join(dir, "design-system", "generated")
	require.NoError(t, os.RemoveAll(gen))
	if err := os.Symlink(outside, gen); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	code, _, stderr = run("--root", dir, "build")
	assert.Equal(t, ExitUsage, code, stderr)
	assert.NotEmpty(t, stderr)
	ents, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, ents, "nothing written outside the root")
}

func TestLevenshtein(t *testing.T) {
	assert.Equal(t, 0, levenshtein("ink", "ink"))
	assert.Equal(t, 1, levenshtein("accent-inc", "accent-ink"))
	assert.Equal(t, 2, levenshtein("acent-in", "accent-ink"))
	assert.Equal(t, 3, levenshtein("", "abc"))
}

// TestLintConsumerReadErrorExits2: a consumer file that cannot be read
// stops lint with exit 2; it is never reported as clean or as findings.
func TestLintConsumerReadErrorExits2(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not deny reads on Windows")
	}
	dir := t.TempDir()
	files := map[string]string{
		"airframe.yaml":               "version: 1\ncontent: design-system\nplatforms:\n  web:\n    emit: [css]\n    out: {css: web/generated/tokens.css}\n    consumers: [\"web/**/*.css\"]\n",
		"design-system/a.tokens.json": `{ "gap": { "$type": "number", "$value": 4, "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } } }`,
		"web/a.css":                   ".a { margin: var(--gap); }\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	css := filepath.Join(dir, "web", "a.css")
	require.NoError(t, os.Chmod(css, 0))
	t.Cleanup(func() { _ = os.Chmod(css, 0o644) })
	if f, err := os.Open(css); err == nil {
		_ = f.Close()
		t.Skip("running with permission to read a mode-0 file")
	}
	code, stdout, stderr := run("--root", dir, "lint")
	assert.Equal(t, ExitUsage, code, stdout)
	assert.Contains(t, stderr, "web/a.css")
	assert.Empty(t, stdout)
}

func TestSchemaRetired(t *testing.T) {
	code, stdout, stderr := run("schema", "retired")
	require.Equal(t, ExitOK, code, stderr)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.Equal(t, "retired.yaml", doc["title"])

	code, _, stderr = run("schema", "nope")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, stderr, "retired")
}

// The reference commands are listed in the order a reader reaches for
// them: find a token, see its usages, diff the set.
func TestCommandsReferenceOrder(t *testing.T) {
	index := map[string]int{}
	for i, c := range Commands {
		index[c.Name] = i
	}
	assert.Less(t, index["find"], index["usages"])
	assert.Equal(t, index["find"]+1, index["usages"])
	assert.Equal(t, index["usages"]+1, index["diff"])
	assert.Contains(t, Commands[index["schema"]].Summary, "diff --format json")
}
