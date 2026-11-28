// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package docs

import (
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/importer"
	"github.com/c65llc/airframe/internal/lint"
)

// page is what a rule ID's docs page must declare in its front matter.
type page struct{ severity, since string }

// pages is every documented ID: the lint rules at their default severity,
// since v0.1.0, and the import findings at the severity an import reports
// them, since v0.2.0.
func pages() map[string]page {
	out := map[string]page{}
	for _, id := range lint.AllIDs() {
		out[id] = page{string(lint.DefaultSeverity(id)), "v0.1.0"}
	}
	for _, id := range importer.IDs() {
		out[id] = page{string(importer.Severity(id)), "v0.2.0"}
	}
	return out
}

// documented is every ID that has a page, sorted.
func documented() []string {
	ids := slices.Concat(lint.AllIDs(), importer.IDs())
	slices.Sort(ids)
	return ids
}

func TestEveryRuleHasDocs(t *testing.T) {
	want := pages()
	for _, id := range documented() {
		t.Run(id, func(t *testing.T) {
			page, ok := Rule(id)
			require.True(t, ok, "docs/rules/%s.md", id)
			lines := strings.Split(strings.TrimSuffix(page, "\n"), "\n")
			assert.True(t, strings.HasSuffix(page, "\n") && !strings.HasSuffix(page, "\n\n"), "one final newline")
			assert.NotContains(t, page, "\r")
			assert.GreaterOrEqual(t, len(lines), 15, "short, not empty")
			assert.LessOrEqual(t, len(lines), 40, "short")

			require.GreaterOrEqual(t, len(lines), 7)
			assert.Equal(t, []string{"---", "severity: " + want[id].severity, "since: " + want[id].since, "---", "", "# " + id, ""}, lines[:7])
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
	assert.Equal(t, documented(), ids, "one page per rule ID and import finding, no others")
	assert.Len(t, slices.Compact(documented()), len(documented()), "no import ID is also a lint ID")
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
