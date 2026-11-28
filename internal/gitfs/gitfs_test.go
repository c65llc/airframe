// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package gitfs

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/gittest"
)

func TestGitFS(t *testing.T) {
	r := gittest.New(t)
	r.Write(map[string]string{
		"README.md":                     "outside the project\n",
		"proj/airframe.yaml":            "version: 1\n",
		"proj/ds/tokens/a.tokens.json":  "{}\n",
		"proj/ds/tokens/b.tokens.json":  "{\"b\": 1}\n",
		"proj/ds/themes/dark.json":      "dark\n",
		"proj-sibling/not-included.txt": "no\n",
	})
	first := r.Commit("one")
	r.Write(map[string]string{"proj/ds/tokens/a.tokens.json": "{\"changed\": true}\n"})
	r.Remove("proj/ds/themes")
	r.Commit("two")

	repo, err := Open(filepath.Join(r.Dir, "proj"))
	require.NoError(t, err)
	assert.Equal(t, "proj/", repo.Prefix)
	sha, err := repo.Resolve("HEAD~1")
	require.NoError(t, err)
	assert.Equal(t, first, sha)

	fsys, err := repo.FS(sha)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fsys.Close() })

	// Read: the file as it was at the first commit, not as it is now.
	b, err := fs.ReadFile(fsys, "ds/tokens/a.tokens.json")
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(b))

	// Stat: a file's size and a directory.
	fi, err := fs.Stat(fsys, "ds/tokens/b.tokens.json")
	require.NoError(t, err)
	assert.Equal(t, int64(9), fi.Size())
	assert.False(t, fi.IsDir())
	fi, err = fs.Stat(fsys, "ds")
	require.NoError(t, err)
	assert.True(t, fi.IsDir())

	// A directory listing, sorted, and only under the project.
	ents, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{"airframe.yaml", "ds"}, names)
	ents, err = fs.ReadDir(fsys, "ds")
	require.NoError(t, err)
	require.Len(t, ents, 2)
	assert.Equal(t, "themes", ents[0].Name())
	assert.True(t, ents[0].IsDir())

	// A missing path, including one outside the project.
	for _, p := range []string{"nope.txt", "ds/nope", "../README.md", "README.md", "proj-sibling/not-included.txt"} {
		_, err = fs.ReadFile(fsys, p)
		assert.True(t, errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid), "%s: %v", p, err)
	}

	// The whole tree passes the standard fs.FS conformance checks.
	require.NoError(t, fstest.TestFS(fsys, "airframe.yaml", "ds/tokens/a.tokens.json", "ds/tokens/b.tokens.json", "ds/themes/dark.json"))

	// At HEAD the theme directory is gone and a.tokens.json changed.
	head, err := repo.Resolve("HEAD")
	require.NoError(t, err)
	now, err := repo.FS(head)
	require.NoError(t, err)
	t.Cleanup(func() { _ = now.Close() })
	_, err = fs.Stat(now, "ds/themes")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	b, err = fs.ReadFile(now, "ds/tokens/a.tokens.json")
	require.NoError(t, err)
	assert.Equal(t, "{\"changed\": true}\n", string(b))
}

func TestGitFSRootProject(t *testing.T) {
	r := gittest.New(t)
	r.Write(map[string]string{"airframe.yaml": "version: 1\n"})
	r.Commit("one")
	repo, err := Open(r.Dir)
	require.NoError(t, err)
	assert.Equal(t, "", repo.Prefix)
	sha, err := repo.Resolve("main")
	require.NoError(t, err)
	fsys, err := repo.FS(sha)
	require.NoError(t, err)
	defer fsys.Close()
	b, err := fs.ReadFile(fsys, "airframe.yaml")
	require.NoError(t, err)
	assert.Equal(t, "version: 1\n", string(b))
}

func TestResolveRefs(t *testing.T) {
	r := gittest.New(t)
	r.Write(map[string]string{"a.txt": "1\n"})
	one := r.Commit("one")
	r.Git("branch", "feature/slash")
	r.Git("tag", "v1.0.0")
	r.Git("tag", "-a", "-m", "annotated", "v1.0.1")
	r.Write(map[string]string{"a.txt": "2\n"})
	two := r.Commit("two")

	repo, err := Open(r.Dir)
	require.NoError(t, err)
	for ref, want := range map[string]string{
		"feature/slash": one, "v1.0.0": one, "v1.0.1": one, one[:7]: one,
		"HEAD~1": one, "HEAD": two, "main": two, "HEAD^{tree}^{}": "",
	} {
		got, err := repo.Resolve(ref)
		if want == "" {
			assert.Error(t, err, ref)
			continue
		}
		require.NoError(t, err, ref)
		assert.Equal(t, want, got, ref)
	}

	for _, ref := range []string{"--output=/tmp/x", "-h", "-", "", "ma\x00in", "nope", "HEAD~99"} {
		_, err := repo.Resolve(ref)
		var re *RefError
		require.ErrorAs(t, err, &re, "%q", ref)
		assert.Equal(t, ref, re.Ref)
	}
}

func TestOpenOutsideWorkTree(t *testing.T) {
	r := gittest.New(t)
	_, err := Open(t.TempDir())
	assert.ErrorIs(t, err, ErrNotWorkTree)
	_, err = Open(filepath.Join(r.Dir, ".git"))
	assert.ErrorIs(t, err, ErrNotWorkTree)
}

func TestOpenNoGit(t *testing.T) {
	gittest.New(t) // skips when git is absent
	t.Setenv("PATH", "")
	_, err := Open(t.TempDir())
	assert.ErrorIs(t, err, ErrNoGit)
}
