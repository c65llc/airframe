// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/gittest"
)

// guideJSONCaption is the line above a json block that names its file, as
// JSON has no comments: "**`design-system/tokens/semantic.tokens.json`**",
// or "**After: `…`**" for the file as a pull request changes it.
var guideJSONCaption = regexp.MustCompile("^\\*\\*(After: )?`([A-Za-z0-9_./-]+\\.json)`\\*\\*$")

// referenceGuide is docs/guides/reference.md taken apart: the project it
// writes (before), the files a pull request changes (after), and the
// commands it shows with their quoted output.
type referenceGuide struct {
	text          string
	before, after map[string]string
	commands      []guideCommand
}

type guideCommand struct{ args, want string }

func readReferenceGuide(t *testing.T) referenceGuide {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "reference.md"))
	require.NoError(t, err)
	g := referenceGuide{text: string(b), before: map[string]string{}, after: map[string]string{}}
	for _, m := range guideFence.FindAllStringSubmatchIndex(g.text, -1) {
		lang, body := g.text[m[2]:m[3]], g.text[m[4]:m[5]]
		first, rest, _ := strings.Cut(body, "\n")
		if lang == "text" {
			if args, ok := strings.CutPrefix(first, "$ airframe "); ok {
				g.commands = append(g.commands, guideCommand{args, rest})
			}
			continue
		}
		files, name := g.before, ""
		if lang == "json" {
			before := strings.Split(strings.TrimRight(g.text[:m[0]], "\n"), "\n")
			cm := guideJSONCaption.FindStringSubmatch(before[len(before)-1])
			require.NotNil(t, cm, "every json block is captioned with its file: %q", before[len(before)-1])
			if cm[1] != "" {
				files = g.after
			}
			name = cm[2]
		} else if pm := guideFileComment.FindStringSubmatch(first); pm != nil {
			name = pm[1]
		}
		if name == "" {
			continue // a CI snippet, not a project file
		}
		_, dup := files[name]
		require.False(t, dup, "the guide writes %s twice", name)
		files[name] = body
	}
	return g
}

// TestGuideReference: the reference guide's project, written to a git
// repository as origin/main, prints exactly what the guide quotes for
// every "$ airframe find|usages|schema …" block; its pull request, committed
// on a branch, prints exactly what it quotes for every "$ airframe diff …"
// block; and the messages it quotes from stderr are the real ones.
func TestGuideReference(t *testing.T) {
	g := readReferenceGuide(t)
	for _, want := range []string{"airframe.yaml", "design-system/tokens/primitive.tokens.json", "design-system/tokens/semantic.tokens.json",
		"design-system/tokens/component.tokens.json", "design-system/components/button.yaml", "web/src/app.css", "web/src/button/Button.tsx"} {
		require.Contains(t, g.before, want)
	}
	require.Contains(t, g.after, "design-system/tokens/semantic.tokens.json")

	var diffs, others []guideCommand
	for _, c := range g.commands {
		if strings.HasPrefix(c.args, "diff ") {
			diffs = append(diffs, c)
		} else {
			others = append(others, c)
		}
	}
	require.Len(t, diffs, 2)
	require.Len(t, others, 6)

	r := gittest.New(t)
	r.Write(g.before)
	r.Commit("the design system")
	r.Git("update-ref", "refs/remotes/origin/main", "HEAD")
	t.Chdir(r.Dir) // --file is relative to the working directory

	runGuide := func(c guideCommand) {
		t.Helper()
		code, stdout, stderr := run(strings.Fields(c.args)...)
		require.Equal(t, ExitOK, code, "airframe %s: %s", c.args, stderr)
		assert.Equal(t, c.want, stdout, "the guide quotes airframe %s's output exactly", c.args)
	}
	for _, c := range others {
		runGuide(c)
	}

	// The message find prints when --file filters out every hit.
	code, _, stderr := run("find", "button", "--file", "web/src/app.css")
	assert.Equal(t, ExitFindings, code)
	assert.Contains(t, g.text, "```text\n"+stderr+"```", "the guide quotes find's filtered-out message")

	r.Git("checkout", "-q", "-b", "tidy-tokens")
	r.Write(g.after)
	r.Commit("tidy the tokens")
	for _, c := range diffs {
		runGuide(c)
	}
	code, _, _ = run("diff", "origin/main", "--exit-code")
	assert.Equal(t, ExitFindings, code, "the pull request changes the tokens")

	// A repository where the project arrives in the pull request: the
	// base has no airframe.yaml, and the note says so.
	r2 := gittest.New(t)
	r2.Write(map[string]string{"README.md": "an app\n"})
	r2.Commit("before airframe")
	r2.Git("update-ref", "refs/remotes/origin/main", "HEAD")
	r2.Git("checkout", "-q", "-b", "adopt-airframe")
	r2.Write(g.before)
	r2.Commit("adopt airframe")
	t.Chdir(r2.Dir)
	code, _, stderr = run("diff", "origin/main", "--format", "md")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, g.text, "```text\n"+stderr+"```", "the guide quotes diff's empty-side note")
}
