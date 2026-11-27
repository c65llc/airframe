// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	assert.Equal(t, os.FileMode(0o644), fi.Mode().Perm())

	// An existing file keeps its permission bits.
	require.NoError(t, os.Chmod(filepath.Join(dir, "a", "b", "c.css"), 0o600))
	require.NoError(t, writeFile(root, "a/b/c.css", []byte("z\n")))
	fi, err = os.Stat(filepath.Join(dir, "a", "b", "c.css"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	assert.Error(t, writeFile(root, "../escape.css", []byte("x\n")))
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

func TestPageBody(t *testing.T) {
	assert.Equal(t, "# x\n\nbody\n", pageBody("---\nseverity: error\n---\n\n# x\n\nbody\n"))
	assert.Equal(t, "# x\n", pageBody("# x\n"))
	assert.Equal(t, "Tokens alias each other in a loop, so none of them has a value.", docSummary("---\na: b\n---\n\n# alias-cycle\n\nTokens alias each other in a loop, so none of them has a value.\n"))
}
