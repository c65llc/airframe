// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package selftest

import (
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/lint"
)

func TestSelfTestEveryRuleFlagged(t *testing.T) {
	r := Run()
	assert.Empty(t, r.Failures)
	assert.True(t, r.OK)
	assert.Equal(t, len(lint.AllIDs()), r.Flagged, "every ID fires on its flag fixture")
	assert.Equal(t, len(lint.AllIDs()), r.Ignored, "every ID stays silent on the clean fixture")
}

// Each flag fixture is named for the one ID it exists to trip, and every ID
// has one, so a failure names the rule and the directory to look in.
func TestOneFlagFixturePerID(t *testing.T) {
	ents, err := fs.ReadDir(fixtures, "fixtures/flag")
	require.NoError(t, err)
	var dirs []string
	for _, e := range ents {
		assert.True(t, e.IsDir(), e.Name())
		dirs = append(dirs, e.Name())
	}
	assert.Equal(t, lint.AllIDs(), dirs)
}

func TestSelfTestDeterministic(t *testing.T) {
	assert.Equal(t, Run(), Run())
}

// sub copies every fixture into a MapFS, minus the paths drop names.
func sub(t *testing.T, drop func(string) bool) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	err := fs.WalkDir(fixtures, "fixtures", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || drop(path) {
			return err
		}
		b, err := fs.ReadFile(fixtures, path)
		out[path] = &fstest.MapFile{Data: b}
		return err
	})
	require.NoError(t, err)
	return out
}

func TestSelfTestNamesMissedRule(t *testing.T) {
	const missing = "tier-violation"
	fsys := sub(t, func(p string) bool { return p == "fixtures/flag/"+missing+"/design-system/a.tokens.json" })
	fsys["fixtures/flag/"+missing+"/design-system/a.tokens.json"] = &fstest.MapFile{Data: []byte(`{"gap": {"$type": "number", "$value": 4}}`)}
	r := run(fsys, lint.AllIDs())
	assert.False(t, r.OK)
	assert.Equal(t, len(lint.AllIDs())-1, r.Flagged)
	assert.Equal(t, []string{"flag: tier-violation did not fire on fixtures/flag/tier-violation"}, r.Failures)
}

func TestSelfTestMissingFixtureAndOrphan(t *testing.T) {
	fsys := sub(t, func(string) bool { return false })
	ids := lint.AllIDs()
	ids[len(ids)-1] = "brand-new-rule" // yaml-syntax, the last ID, becomes an orphan fixture
	r := run(fsys, ids)
	assert.False(t, r.OK)
	assert.Contains(t, r.Failures, "flag: no fixture for brand-new-rule")
	assert.Contains(t, r.Failures, "flag: fixtures/flag/yaml-syntax names no rule ID")
}

func TestSelfTestCleanFindingFails(t *testing.T) {
	fsys := sub(t, func(p string) bool { return p == "fixtures/clean/design-system/generated/css/tokens.css" })
	r := run(fsys, lint.AllIDs())
	assert.False(t, r.OK)
	assert.Equal(t, len(lint.AllIDs())-1, r.Ignored)
	require.Len(t, r.Failures, 1)
	assert.Contains(t, r.Failures[0], "clean: emitted-stale fired: design-system/generated/css/tokens.css")
}

// The clean fixture is a copy of testdata/minimal, generated outputs
// included; regenerate both together.
func TestCleanIsMinimal(t *testing.T) {
	minimal := os.DirFS("../../testdata/minimal")
	want := map[string]string{}
	require.NoError(t, fs.WalkDir(minimal, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(minimal, p)
		want[p] = string(b)
		return err
	}))
	clean, err := fs.Sub(fixtures, cleanDir)
	require.NoError(t, err)
	got := map[string]string{}
	require.NoError(t, fs.WalkDir(clean, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(clean, p)
		got[p] = string(b)
		return err
	}))
	assert.Equal(t, want, got)
}
