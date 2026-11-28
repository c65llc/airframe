// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package rulingref

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
)

func loadConfig(t *testing.T, src string) *config.Config {
	t.Helper()
	c, ds := config.Load([]byte(src))
	require.Empty(t, ds)
	return c
}

const gantryYAML = "version: 1\ncontent: docs/design\nplatforms:\n  web: {label: Web}\n"

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func lookup(t *testing.T, ix *Index, id, wantStatus, wantBy string) {
	t.Helper()
	status, by, ok := ix.Lookup(id)
	require.True(t, ok, id)
	assert.Equal(t, wantStatus, status, id)
	assert.Equal(t, wantBy, by, id)
}

func TestResolveDefaultLocation(t *testing.T) {
	fsys := os.DirFS(filepath.Join("..", "..", "testdata", "gantry-project"))
	ix, ds := Resolve(fsys, loadConfig(t, "version: 1\n"))
	require.Empty(t, ds)
	require.NotNil(t, ix)
	assert.Equal(t, "gantry.yaml", ix.File())
	lookup(t, ix, "tab-strip", "ruled", "")
	lookup(t, ix, "old-tabs", "superseded", "tab-strip")
	lookup(t, ix, "sheet", "proposed", "")
	_, _, ok := ix.Lookup("nope")
	assert.False(t, ok)
	assert.False(t, ix.Unreadable("nope"))
}

func TestResolveConfiguredPath(t *testing.T) {
	fsys := fstest.MapFS{
		"airframe.yaml": file("version: 1\ngantry: design/gantry.yaml\n"),
		// a root gantry.yaml is ignored when the key names another
		"gantry.yaml":                        file("version: 1\ncontent: elsewhere\n"),
		"design/gantry.yaml":                 file("version: 1\ncontent: records\n"),
		"design/records/rulings/sheet.md":    file("---\nid: sheet\nstatus: retired\n---\n"),
		"design/records/rulings/notes.txt":   file("not a ruling"),
		"design/records/rulings/sub/deep.md": file("---\nid: deep\nstatus: ruled\n---\n"),
		"elsewhere/rulings/other.md":         file("---\nid: other\nstatus: ruled\n---\n"),
	}
	ix, ds := Resolve(fsys, loadConfig(t, "version: 1\ngantry: design/gantry.yaml\n"))
	require.Empty(t, ds)
	require.NotNil(t, ix)
	assert.Equal(t, "design/gantry.yaml", ix.File())
	lookup(t, ix, "sheet", "retired", "")
	for _, id := range []string{"other", "deep", "notes"} {
		_, _, ok := ix.Lookup(id)
		assert.False(t, ok, id)
	}
}

func TestResolveContentDefaultsAndMissingRulings(t *testing.T) {
	// content defaults to Gantry's docs/design; no rulings directory is an
	// empty project, not an error.
	ix, ds := Resolve(fstest.MapFS{"gantry.yaml": file("version: 1\n")}, loadConfig(t, "version: 1\n"))
	require.Empty(t, ds)
	require.NotNil(t, ix)
	_, _, ok := ix.Lookup("x")
	assert.False(t, ok)

	ix, ds = Resolve(fstest.MapFS{
		"gantry.yaml":                  file("version: 1\n"),
		"docs/design/rulings/a-b-c.md": file("---\nid: a-b-c\nstatus: ruled\n---\n"),
	}, loadConfig(t, "version: 1\n"))
	require.Empty(t, ds)
	lookup(t, ix, "a-b-c", "ruled", "")
}

func TestResolveMissingConfigured(t *testing.T) {
	fsys := fstest.MapFS{"gantry.yaml": file(gantryYAML)}
	ix, ds := Resolve(fsys, loadConfig(t, "version: 1\ngantry: tools/gantry.yaml\n"))
	assert.Nil(t, ix)
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, diag.Error, ds[0].Severity)
	assert.Equal(t, config.File, ds[0].File)
	assert.Equal(t, "gantry", ds[0].Path)
	assert.Contains(t, ds[0].Message, "tools/gantry.yaml")
}

func TestResolveNoGantry(t *testing.T) {
	ix, ds := Resolve(fstest.MapFS{"airframe.yaml": file("version: 1\n")}, loadConfig(t, "version: 1\n"))
	assert.Nil(t, ix)
	assert.Empty(t, ds)
	// a nil Index answers like an empty one
	_, _, ok := ix.Lookup("tab-strip")
	assert.False(t, ok)
	assert.False(t, ix.Unreadable("tab-strip"))
}

func TestResolveMalformed(t *testing.T) {
	const dir = "docs/design/rulings/"
	fsys := fstest.MapFS{
		"gantry.yaml":         file(gantryYAML),
		dir + "crlf.md":       file("---\r\nid: crlf\r\nstatus: ruled\r\n---\r\nbody\r\n"),
		dir + "bom.md":        file("\uFEFF---\nid: bom\nstatus: superseded\nsupersededBy: crlf\n---\n"),
		dir + "bom-crlf.md":   file("\uFEFF---\r\nid: bom-crlf\r\nstatus: ruled\r\n---"),
		dir + "unclosed.md":   file("---\nid: unclosed\nstatus: ruled\n"),
		dir + "no-fm.md":      file("# just a heading\n"),
		dir + "bad-yaml.md":   file("---\nid: bad-yaml\nstatus: [ruled\n---\n"),
		dir + "mismatch.md":   file("---\nid: other-id\nstatus: ruled\n---\n"),
		dir + "alias.md":      file("---\nid: &x alias\nstatus: ruled\nnoun: *x\n---\n"),
		dir + "merge.md":      file("---\nid: merge\nstatus: ruled\n<<: {noun: x}\n---\n"),
		dir + "bad-status.md": file("---\nid: bad-status\nstatus: maybe\n---\n"),
		dir + "no-status.md":  file("---\nid: no-status\n---\n"),
		dir + "list.md":       file("---\n- id\n---\n"),
		dir + "empty.md":      file("---\n---\n"),
		dir + "num-id.md":     file("---\nid: 7\nstatus: ruled\n---\n"),
		dir + "bad-by.md":     file("---\nid: bad-by\nstatus: superseded\nsupersededBy: [a]\n---\n"),
		dir + "null-by.md":    file("---\nid: null-by\nstatus: ruled\nsupersededBy:\n---\n"),
		dir + "dup.md":        file("---\nid: dup\nstatus: ruled\nstatus: retired\n---\n"),
	}
	ix, ds := Resolve(fsys, loadConfig(t, "version: 1\n"))
	require.NotNil(t, ix, "a broken ruling does not discard the project")

	lookup(t, ix, "crlf", "ruled", "")
	lookup(t, ix, "bom", "superseded", "crlf")
	lookup(t, ix, "bom-crlf", "ruled", "")
	lookup(t, ix, "null-by", "ruled", "")

	type want struct {
		line int
		msg  string
	}
	wants := map[string]want{
		"unclosed.md":   {1, "no closing ---"},
		"no-fm.md":      {1, "does not start with ---"},
		"bad-yaml.md":   {0, ""},
		"mismatch.md":   {2, `id "other-id" does not match the file name (mismatch)`},
		"alias.md":      {0, "anchors"},
		"merge.md":      {4, "merge keys"},
		"bad-status.md": {3, `status "maybe" is not one of proposed, ruled, superseded, retired`},
		"no-status.md":  {1, "status is required"},
		"list.md":       {2, "must be a mapping"},
		"empty.md":      {1, "must be a mapping"},
		"num-id.md":     {2, "id must be a string"},
		"bad-by.md":     {4, "supersededBy must be a string"},
		"dup.md":        {4, `duplicate key "status"`},
	}
	got := map[string]diag.Diagnostic{}
	for _, d := range ds {
		assert.Equal(t, "component-unruled", d.Rule, d.File)
		assert.Equal(t, diag.Error, d.Severity, d.File)
		assert.Equal(t, diag.DocsURL("component-unruled"), d.Docs, d.File)
		require.NotContains(t, got, filepath.Base(d.File), "one diagnostic per file")
		got[filepath.Base(d.File)] = d
	}
	require.Len(t, got, len(wants), "%v", ds)
	for name, w := range wants {
		d, ok := got[name]
		require.True(t, ok, name)
		assert.Equal(t, dir+name, d.File, name)
		if w.line != 0 {
			assert.Equal(t, w.line, d.Line, name)
		} else {
			assert.NotZero(t, d.Line, name)
		}
		assert.Contains(t, d.Message, w.msg, name)
		id := name[:len(name)-len(".md")]
		_, _, ok = ix.Lookup(id)
		assert.False(t, ok, "%s is not in the index", id)
		assert.True(t, ix.Unreadable(id), id)
	}
	// diagnostics come in file-name order
	for i := 1; i < len(ds); i++ {
		assert.Less(t, ds[i-1].File, ds[i].File)
	}
}

func TestResolveGantryYAMLInvalid(t *testing.T) {
	for name, tc := range map[string]struct {
		src, rule, path string
		line            int
	}{
		"content escapes the repo":    {"version: 1\ncontent: ../../outside\n", "schema-invalid", "content", 2},
		"content escapes the project": {"version: 1\ncontent: ../outside\n", "schema-invalid", "content", 2},
		"content is absolute":         {"version: 1\ncontent: /etc\n", "schema-invalid", "content", 2},
		"content has a backslash":     {"version: 1\ncontent: 'a\\b'\n", "schema-invalid", "content", 2},
		"content has a drive":         {"version: 1\ncontent: 'C:/x'\n", "schema-invalid", "content", 2},
		"content is not a string":     {"version: 1\ncontent: [a]\n", "schema-invalid", "content", 2},
		"not a mapping":               {"- a\n", "schema-invalid", "", 1},
		"alias":                       {"version: 1\nx: &a docs\ncontent: *a\n", "schema-invalid", "", 2},
		"syntax":                      {"version: 1\ncontent: [docs\n", "yaml-syntax", "", 0},
	} {
		fsys := fstest.MapFS{"tools/gantry.yaml": file(tc.src)}
		ix, ds := Resolve(fsys, loadConfig(t, "version: 1\ngantry: tools/gantry.yaml\n"))
		assert.Nil(t, ix, name)
		require.Len(t, ds, 1, name)
		assert.Equal(t, tc.rule, ds[0].Rule, name)
		assert.Equal(t, "tools/gantry.yaml", ds[0].File, name)
		assert.Equal(t, tc.path, ds[0].Path, name)
		if tc.line != 0 {
			assert.Equal(t, tc.line, ds[0].Line, name)
		}
	}
}

// symlinkRepo builds a repo in a temp dir with a valid Gantry project and
// an outside directory holding a ruling, then lets link add a symlink.
func symlinkRepo(t *testing.T, link func(repo, outside string) error) fs.FS {
	t.Helper()
	base := t.TempDir()
	repo, outside := filepath.Join(base, "repo"), filepath.Join(base, "outside")
	rulings := filepath.Join(repo, "docs", "design", "rulings")
	require.NoError(t, os.MkdirAll(rulings, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "rulings"), 0o755))
	write := func(p, s string) { require.NoError(t, os.WriteFile(p, []byte(s), 0o644)) }
	write(filepath.Join(repo, "gantry.yaml"), gantryYAML)
	write(filepath.Join(outside, "gantry.yaml"), gantryYAML)
	write(filepath.Join(rulings, "tab-strip.md"), "---\nid: tab-strip\nstatus: ruled\n---\n")
	write(filepath.Join(outside, "secret.md"), "---\nid: secret\nstatus: ruled\n---\n")
	write(filepath.Join(outside, "rulings", "secret.md"), "---\nid: secret\nstatus: ruled\n---\n")
	if err := link(repo, outside); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks not permitted: %v", err)
		}
		require.NoError(t, err)
	}
	return os.DirFS(repo)
}

func TestResolveRefusesSymlinks(t *testing.T) {
	cfg := loadConfig(t, "version: 1\n")

	t.Run("ruling file", func(t *testing.T) {
		fsys := symlinkRepo(t, func(repo, outside string) error {
			return os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(repo, "docs", "design", "rulings", "secret.md"))
		})
		ix, ds := Resolve(fsys, cfg)
		require.NotNil(t, ix)
		require.Len(t, ds, 1)
		assert.Equal(t, "component-unruled", ds[0].Rule)
		assert.Equal(t, "docs/design/rulings/secret.md", ds[0].File)
		assert.Contains(t, ds[0].Message, "symlink")
		_, _, ok := ix.Lookup("secret")
		assert.False(t, ok)
		assert.True(t, ix.Unreadable("secret"))
		lookup(t, ix, "tab-strip", "ruled", "")
	})

	for name, link := range map[string]func(repo, outside string) error{
		"content dir": func(repo, outside string) error {
			d := filepath.Join(repo, "docs", "design")
			if err := os.RemoveAll(d); err != nil {
				return err
			}
			return os.Symlink(outside, d)
		},
		"rulings dir": func(repo, outside string) error {
			d := filepath.Join(repo, "docs", "design", "rulings")
			if err := os.RemoveAll(d); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(outside, "rulings"), d)
		},
		"gantry.yaml": func(repo, outside string) error {
			g := filepath.Join(repo, "gantry.yaml")
			if err := os.Remove(g); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(outside, "gantry.yaml"), g)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ix, ds := Resolve(symlinkRepo(t, link), cfg)
			assert.Nil(t, ix)
			require.Len(t, ds, 1)
			assert.Equal(t, "schema-invalid", ds[0].Rule)
			assert.Equal(t, "gantry.yaml", ds[0].File)
			assert.Contains(t, ds[0].Message, "symlink")
		})
	}
}
