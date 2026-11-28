// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guideFence is a fenced block of a guide: its language and body.
var guideFence = regexp.MustCompile("(?s)```([a-z]+)\n(.*?)```")

// guideFileComment is a block's first line when it names a repo-relative
// file: "/* web/src/app.css */", "// web/src/row.tsx" or "# airframe.yaml".
var guideFileComment = regexp.MustCompile(`^(?://|/\*|<!--|#)\s*([A-Za-z0-9_./-]+\.[A-Za-z]+)\s*(?:\*/|-->)?$`)

// guideCaption is the line above a markdown block that names its file,
// since a ruling record must start with its front matter: "**`docs/design/rulings/button.md`**".
var guideCaption = regexp.MustCompile("^\\*\\*`([A-Za-z0-9_./-]+\\.md)`\\*\\*$")

// guideFindingLine is a finding line as lint's text output prints it.
var guideFindingLine = regexp.MustCompile(`(?m)^[A-Za-z0-9_./-]+(?::\d+(?::\d+)?)?: (?:error|warn) [a-z-]+: .*$`)

// TestGuideComponents: the components guide's files (airframe.yaml, the
// Gantry project and its rulings, the component files, retired.yaml and
// the consumer snippets), over the clean-consumers self-test fixture's
// tokens, lint to exactly the findings the guide quotes; and every command
// it shows ("$ airframe show …") prints exactly the output it quotes. So
// the guide's examples cannot drift from what Airframe says.
func TestGuideComponents(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "components.md"))
	require.NoError(t, err)
	guide := string(b)

	files := map[string]string{}
	tokensDir := filepath.Join("..", "selftest", "fixtures", "clean-consumers", "design-system", "tokens")
	entries, err := os.ReadDir(tokensDir)
	require.NoError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(tokensDir, e.Name()))
		require.NoError(t, err)
		files["design-system/tokens/"+e.Name()] = string(data)
	}

	var quoted []string
	type command struct{ args, want string }
	var commands []command
	for _, m := range guideFence.FindAllStringSubmatchIndex(guide, -1) {
		lang, body := guide[m[2]:m[3]], guide[m[4]:m[5]]
		first, rest, _ := strings.Cut(body, "\n")
		if lang == "text" {
			if args, ok := strings.CutPrefix(first, "$ airframe "); ok {
				commands = append(commands, command{args, rest})
				continue
			}
			quoted = append(quoted, guideFindingLine.FindAllString(body, -1)...)
			continue
		}
		name := ""
		if lang == "markdown" {
			before := strings.Split(strings.TrimRight(guide[:m[0]], "\n"), "\n")
			cm := guideCaption.FindStringSubmatch(before[len(before)-1])
			require.NotNil(t, cm, "every markdown block is captioned with its file: %q", before[len(before)-1])
			name = cm[1]
		} else if pm := guideFileComment.FindStringSubmatch(first); pm != nil {
			name = pm[1]
		}
		if lang == "yaml" {
			require.NotEmpty(t, name, "every yaml block names its file on its first line: %q", first)
		}
		if name == "" {
			continue
		}
		_, dup := files[name]
		require.False(t, dup, "the guide writes %s twice", name)
		files[name] = body
	}
	for _, want := range []string{"airframe.yaml", "gantry.yaml", "design-system/retired.yaml", "design-system/components/button.yaml", "docs/design/rulings/button.md"} {
		require.Contains(t, files, want)
	}
	require.NotEmpty(t, quoted)
	require.Len(t, commands, 2)

	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}

	code, stdout, stderr := run("--root", dir, "lint")
	assert.Equal(t, ExitFindings, code, stderr)
	var got []string
	for _, l := range guideFindingLine.FindAllString(stdout, -1) {
		// The guide builds nothing.
		if !strings.Contains(l, " emitted-stale: ") {
			got = append(got, l)
		}
	}
	slices.Sort(got)
	slices.Sort(quoted)
	quoted = slices.Compact(quoted)
	assert.Equal(t, quoted, got, "the guide quotes exactly the findings its snippets produce")

	for _, rule := range []string{"component-unruled", "component-incomplete", "retired-used"} {
		assert.Contains(t, stdout, " "+rule+": ", "the guide's snippets trip %s", rule)
		assert.Contains(t, guide, "(../rules/"+rule+".md)", "the guide links %s's page", rule)
	}

	// The reasoned treatment marker is honoured and listed in the gate.
	code, stdout, stderr = run("--root", dir, "lint", "--format", "gate")
	assert.Equal(t, ExitFindings, code, stderr)
	var doc struct {
		Exemptions []struct {
			File, Marker, Reason, Rule string
		} `json:"exemptions"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.Len(t, doc.Exemptions, 1)
	assert.Equal(t, "web/src/mail.css", doc.Exemptions[0].File)
	assert.Equal(t, "audited-selection-bar", doc.Exemptions[0].Marker)
	assert.Equal(t, "retired-used", doc.Exemptions[0].Rule)
	assert.NotEmpty(t, doc.Exemptions[0].Reason)

	for _, c := range commands {
		code, stdout, stderr := run(append([]string{"--root", dir}, strings.Fields(c.args)...)...)
		require.Equal(t, ExitOK, code, "airframe %s: %s", c.args, stderr)
		assert.Equal(t, c.want, stdout, "the guide quotes airframe %s's output exactly", c.args)
	}
}
