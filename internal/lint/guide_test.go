// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
)

// guideBlock is a fenced block of docs/guides/consumers.md whose first line
// is a comment naming a repo-relative file: "/* web/src/app.css */",
// "// apple/Sources/ContentView.swift", "<!-- windows/App/MainPage.xaml -->"
// or "# airframe.yaml".
var guideBlock = regexp.MustCompile("(?s)```([a-z]+)\n(.*?)```")

var guidePath = regexp.MustCompile(`^(?://|/\*|<!--|#)\s*([A-Za-z0-9_./-]+\.[A-Za-z]+)\s*(?:\*/|-->)?$`)

// guideFinding is a finding line as lint's text output prints it.
var guideFinding = regexp.MustCompile(`(?m)^[A-Za-z0-9_./-]+(?::\d+:\d+)?: (?:error|warn) [a-z-]+: .*$`)

// TestGuideConsumers: the consumers guide's airframe.yaml, component file
// and consumer snippets, over the clean-consumers self-test fixture's
// tokens, lint to exactly the findings the guide quotes, so the guide's
// examples and its quoted messages cannot drift from what lint says.
func TestGuideConsumers(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "consumers.md"))
	require.NoError(t, err)
	guide := string(b)

	fsys := project.MemFS{}
	tokensDir := filepath.Join("..", "selftest", "fixtures", "clean-consumers", "design-system", "tokens")
	entries, err := os.ReadDir(tokensDir)
	require.NoError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(tokensDir, e.Name()))
		require.NoError(t, err)
		fsys["design-system/tokens/"+e.Name()] = string(data)
	}

	var quoted []string
	for _, m := range guideBlock.FindAllStringSubmatch(guide, -1) {
		lang, body := m[1], m[2]
		if lang == "text" {
			quoted = append(quoted, guideFinding.FindAllString(body, -1)...)
			continue
		}
		first, _, _ := strings.Cut(body, "\n")
		pm := guidePath.FindStringSubmatch(first)
		if lang == "yaml" {
			require.NotNil(t, pm, "every yaml block names its file on its first line: %q", first)
		}
		if pm == nil {
			continue
		}
		_, dup := fsys[pm[1]]
		require.False(t, dup, "the guide writes %s twice", pm[1])
		fsys[pm[1]] = body
	}
	require.Contains(t, fsys, "airframe.yaml")
	require.Contains(t, fsys, "design-system/components/button.yaml")
	require.NotEmpty(t, quoted)

	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.NotNil(t, p, ds)
	require.Empty(t, ds)
	res := Run(p, nil, Options{})

	var kept []diag.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Rule != "emitted-stale" { // the guide builds nothing
			kept = append(kept, d)
		}
	}
	var out strings.Builder
	diag.WriteText(&out, kept, nil)
	got := strings.Split(strings.TrimSpace(out.String()), "\n")
	slices.Sort(quoted)
	quoted = slices.Compact(quoted)
	assert.Equal(t, quoted, got, "the guide quotes exactly the findings its snippets produce")

	// Every consumer rule is quoted at least once, and its docs page linked.
	for _, rule := range []string{"reference-undeclared", "primitive-read-by-consumer", "token-owner-violation", "fallback-literal", "wrapper-unmapped"} {
		assert.Contains(t, out.String(), " "+rule+": ", "the guide's snippets trip %s", rule)
		assert.Contains(t, guide, "(../rules/"+rule+".md)", "the guide links %s's page", rule)
	}
	require.Len(t, res.Exemptions, 1, "the guide's reasoned exemption is recorded")
}
