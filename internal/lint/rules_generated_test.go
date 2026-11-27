// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
)

var minimalDir = filepath.Join("..", "..", "testdata", "minimal")

const minimalCSS = "design-system/generated/css/tokens.css"

// minimal copies testdata/minimal into a MemFS so a test can change it.
func minimal(t *testing.T) project.MemFS {
	t.Helper()
	m := project.MemFS{}
	require.NoError(t, fs.WalkDir(os.DirFS(minimalDir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(filepath.Join(minimalDir, filepath.FromSlash(path)))
		m[path] = string(b)
		return err
	}))
	return m
}

func lintFS(t *testing.T, fsys fs.FS, opts Options) Result {
	t.Helper()
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	return Run(p, nil, opts)
}

func tallyOf(res Result, rule string) diag.Tally {
	for _, tl := range res.Tallies {
		if tl.Rule == rule {
			return tl
		}
	}
	return diag.Tally{}
}

func TestMinimalLintsClean(t *testing.T) {
	res := lintFS(t, os.DirFS(minimalDir), Options{})
	assert.Empty(t, res.Diagnostics)
	assert.Equal(t, diag.Tally{Rule: "emitted-stale", Files: 2, Items: 2, Noun: "files"}, tallyOf(res, "emitted-stale"))
}

func TestEmittedStale(t *testing.T) {
	fsys := minimal(t)
	orig := fsys[minimalCSS]
	i := strings.Index(orig, "--accent:")
	fsys[minimalCSS] = orig[:i] + "--accemt:" + orig[i+len("--accent:"):]

	got := byRule(lintFS(t, fsys, Options{}).Diagnostics, "emitted-stale")
	require.Len(t, got, 1)
	assert.Equal(t, minimalCSS, got[0].File)
	assert.Equal(t, diag.Error, got[0].Severity)
	assert.Equal(t, minimalCSS+" is stale — run `airframe build`", got[0].Message)

	fsys[minimalCSS] = orig
	assert.Empty(t, lintFS(t, fsys, Options{}).Diagnostics)

	delete(fsys, minimalCSS)
	got = byRule(lintFS(t, fsys, Options{}).Diagnostics, "emitted-stale")
	require.Len(t, got, 1, "a missing file is stale")
	assert.Equal(t, minimalCSS, got[0].File)
}

func TestEmittedStaleIgnoresCRLFOnDisk(t *testing.T) {
	fsys := minimal(t)
	fsys[minimalCSS] = strings.ReplaceAll(fsys[minimalCSS], "\n", "\r\n")
	assert.Empty(t, lintFS(t, fsys, Options{}).Diagnostics)
}

func TestEmittedStaleUsesGenerated(t *testing.T) {
	p, ds, err := project.Load(os.DirFS(minimalDir))
	require.NoError(t, err)
	require.Empty(t, ds)
	gen, bds := emit.Build(p, nil)
	require.Empty(t, bds)
	gen[minimalCSS] = append([]byte(nil), gen[minimalCSS]...)
	gen[minimalCSS][0] = '#'
	got := byRule(Run(p, nil, Options{Generated: gen}).Diagnostics, "emitted-stale")
	require.Len(t, got, 1, "lint compares against Options.Generated, not its own build")
	assert.Equal(t, minimalCSS, got[0].File)
}

func TestEmittedStaleNoPlatforms(t *testing.T) {
	res := Run(load(t, map[string]string{"a.tokens.json": `{}`}), nil, Options{})
	assert.Empty(t, byRule(res.Diagnostics, "emitted-stale"))
	assert.Equal(t, "no platforms configured", tallyOf(res, "emitted-stale").NotApplicable)
}

func TestEmittedStaleReportsBuildFailure(t *testing.T) {
	fsys := minimal(t)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], `":root[data-theme='dark']"`, `"html[data-theme='dark']"`, 1)
	res := lintFS(t, fsys, Options{})
	got := byRule(res.Diagnostics, "schema-invalid")
	require.Len(t, got, 1, "the build's own diagnostic is reported, never a clean read")
	assert.Equal(t, "airframe.yaml", got[0].File)
}
