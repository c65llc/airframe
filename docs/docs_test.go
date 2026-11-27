// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package docs

import (
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/lint"
)

// defaults is each rule ID's default severity.
func defaults() map[string]string {
	out := map[string]string{}
	for _, id := range lint.AllIDs() {
		out[id] = string(lint.DefaultSeverity(id))
	}
	return out
}

func TestEveryRuleHasDocs(t *testing.T) {
	sev := defaults()
	for _, id := range lint.AllIDs() {
		t.Run(id, func(t *testing.T) {
			page, ok := Rule(id)
			require.True(t, ok, "docs/rules/%s.md", id)
			lines := strings.Split(strings.TrimSuffix(page, "\n"), "\n")
			assert.True(t, strings.HasSuffix(page, "\n") && !strings.HasSuffix(page, "\n\n"), "one final newline")
			assert.NotContains(t, page, "\r")
			assert.GreaterOrEqual(t, len(lines), 15, "short, not empty")
			assert.LessOrEqual(t, len(lines), 40, "short")

			require.GreaterOrEqual(t, len(lines), 7)
			assert.Equal(t, []string{"---", "severity: " + sev[id], "since: v0.1.0", "---", "", "# " + id, ""}, lines[:7])
			summary := lines[7]
			assert.NotEmpty(t, summary, "a one-line summary under the H1")
			assert.False(t, strings.HasPrefix(summary, "#") || strings.HasPrefix(summary, "**"), summary)
			assert.Empty(t, lines[8], "the summary is one line")

			assert.Contains(t, lines, "**Why**")
			assert.Contains(t, lines, "**Fix**")
			assert.Contains(t, page, "\n```", "an example")
		})
	}
}

func TestNoOrphanPages(t *testing.T) {
	names, err := fs.Glob(rules, "rules/*")
	require.NoError(t, err)
	var ids []string
	for _, n := range names {
		ids = append(ids, strings.TrimSuffix(path.Base(n), ".md"))
	}
	assert.Equal(t, lint.AllIDs(), ids, "one page per rule ID, no others")
}

func TestRuleUnknown(t *testing.T) {
	_, ok := Rule("no-such-rule")
	assert.False(t, ok)
	_, ok = Rule("../embed")
	assert.False(t, ok)
}

// TestReadmeMatchesRelease guards the README against drifting from what
// the release does: it carries the install line, the quick start and the
// Action usage, and names the unbuilt emitters only as planned.
func TestReadmeMatchesRelease(t *testing.T) {
	b, err := os.ReadFile("../README.md")
	require.NoError(t, err)
	readme := string(b)
	for _, want := range []string{
		"go install github.com/c65llc/airframe/cmd/airframe@latest",
		"airframe init", "airframe lint", "airframe build", "airframe fmt",
		"uses: c65llc/airframe@v0", "version: v0.1.2",
		"**Planned (not in v0.1.2)**",
	} {
		assert.Contains(t, readme, want)
	}
	_, planned, ok := strings.Cut(readme, "**Planned (not in v0.1.2)**")
	require.True(t, ok)
	built, _, _ := strings.Cut(readme, "**Planned (not in v0.1.2)**")
	for _, unbuilt := range []string{"Swift", "Kotlin", "XAML"} {
		assert.NotContains(t, built, unbuilt, "an unbuilt emitter is described before the planned list")
		assert.Contains(t, planned, unbuilt)
	}
}
