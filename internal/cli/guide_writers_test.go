// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writerVerbs are the reference commands that write files. The reference
// guide's blocks for them run in a fresh copy of its project each, as one
// would change what the next one reads.
var writerVerbs = []string{"rename", "retire", "specimen"}

// isWriter reports whether a guide command's args start with a writer verb.
func isWriter(args string) bool {
	verb, _, _ := strings.Cut(args, " ")
	return slices.Contains(writerVerbs, verb)
}

// shellFields splits a quoted command line as a POSIX shell would for the
// guide's purposes: on spaces, with single quotes keeping their contents
// literal.
func shellFields(s string) []string {
	var out []string
	var b strings.Builder
	in, quoted := false, false
	for _, r := range s {
		switch {
		case r == '\'':
			quoted, in = !quoted, true
		case r == ' ' && !quoted:
			if in {
				out = append(out, b.String())
				b.Reset()
				in = false
			}
		default:
			b.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, b.String())
	}
	return out
}

func TestShellFields(t *testing.T) {
	assert.Equal(t, []string{"retire", "--spelling", `css=a\{ b`, "x"}, shellFields(`retire --spelling 'css=a\{ b' x`))
	assert.Equal(t, []string{"specimen"}, shellFields("specimen"))
}

// guideProject writes the reference guide's project to a new directory,
// makes it the working directory, and returns it.
func guideProject(t *testing.T, g referenceGuide) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range g.before {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	t.Chdir(dir)
	return dir
}

// TestGuideReferenceWriters: every "$ airframe rename|retire|specimen …"
// block of the reference guide, run in a fresh copy of the guide's
// project, exits 0 and prints exactly what the guide quotes; a --dry-run
// writes nothing, and a rename that writes leaves a project that builds
// clean with no reference to the old name. The refusals the guide quotes
// from stderr are the real ones, and write nothing.
func TestGuideReferenceWriters(t *testing.T) {
	g := readReferenceGuide(t)
	require.Contains(t, g.before, "design-system/invariants.yaml")
	var writers []guideCommand
	for _, c := range g.commands {
		if isWriter(c.args) {
			writers = append(writers, c)
		}
	}
	var verbs []string
	for _, c := range writers {
		verbs = append(verbs, strings.Fields(c.args)[0])
	}
	assert.Equal(t, []string{"rename", "rename", "rename", "rename", "rename", "retire", "retire", "specimen"}, verbs,
		"the guide quotes five renames (two dry runs of the main example and its real run, a --path and a manual item), two retires and a specimen")

	for _, c := range writers {
		t.Run(c.args, func(t *testing.T) {
			dir := guideProject(t, g)
			before := treeHash(t, dir)
			code, stdout, stderr := run(shellFields(c.args)...)
			require.Equal(t, ExitOK, code, "airframe %s: %s", c.args, stderr)
			assert.Equal(t, c.want, stdout, "the guide quotes airframe %s's output exactly", c.args)
			if strings.Contains(c.args, "--dry-run") {
				assert.Equal(t, before, treeHash(t, dir), "a dry run writes nothing")
			}
		})
	}

	t.Run("the rename writes a project that builds clean", func(t *testing.T) {
		guideProject(t, g)
		code, _, stderr := run("rename", "surface", "canvas")
		require.Equal(t, ExitOK, code, stderr)
		code, stdout, stderr := run("build", "--check")
		assert.Equal(t, ExitOK, code, "%s%s", stdout, stderr)
		for _, name := range []string{"web/src/app.css", "design-system/invariants.yaml", "design-system/tokens/component.tokens.json"} {
			b, err := os.ReadFile(filepath.FromSlash(name))
			require.NoError(t, err)
			assert.NotContains(t, string(b), "surface", "%s reads no surface any more", name)
			assert.Contains(t, string(b), "canvas", name)
		}
	})

	// refused runs a command the guide quotes the refusal of: it exits 1,
	// writes nothing, and its stderr is in the guide as a text block.
	refused := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		before := treeHash(t, dir)
		code, _, stderr := run(args...)
		assert.Equal(t, ExitFindings, code, "airframe %v: %s", args, stderr)
		assert.Contains(t, g.text, "```text\n"+stderr+"```", "the guide quotes airframe %v's refusal", args)
		assert.Equal(t, before, treeHash(t, dir), "a refusal writes nothing")
	}
	t.Run("refusals", func(t *testing.T) {
		dir := guideProject(t, g)
		refused(t, dir, "rename", "ink-muted", "ink-subtle")
		refused(t, dir, "rename", "gap", "row-gap")
		refused(t, dir, "retire", "legacy-ink")
	})
	t.Run("symlink", func(t *testing.T) {
		dir := guideProject(t, g)
		real := filepath.Join(dir, "design-system", "semantic.json")
		link := filepath.Join(dir, "design-system", "tokens", "semantic.tokens.json")
		require.NoError(t, os.Rename(link, real))
		if err := os.Symlink(filepath.Join("..", "semantic.json"), link); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
		refused(t, dir, "retire", "ink")
	})
}
