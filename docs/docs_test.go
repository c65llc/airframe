// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package docs

import (
	"io/fs"
	"maps"
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
// retired is the release that stopped reporting the ID, or empty.
type page struct{ severity, since, retired string }

// retiredRules are IDs no release reports any more. IDs are addresses, so
// each keeps its page, whose front matter says when it was retired.
var retiredRules = map[string]page{
	"emit-unsupported": {severity: "warn", since: "v0.1.0", retired: "v0.5.0"},
}

// laterRules are the lint IDs added after v0.1.0, by the release that
// added them.
var laterRules = map[string]string{
	"emit-axis-fixed": "v0.3.0",
	// The consumer rules.
	"fallback-literal": "v0.6.0", "primitive-read-by-consumer": "v0.6.0", "reference-undeclared": "v0.6.0",
	"token-owner-violation": "v0.6.0", "wrapper-unmapped": "v0.6.0",
	// The component and retired-treatment rules.
	"component-incomplete": "v0.7.0", "component-unruled": "v0.7.0", "retired-used": "v0.7.0",
}

// pages is every documented ID: the lint rules at their default severity,
// since v0.1.0 unless laterRules says otherwise, and the import findings at
// the severity an import reports them, since v0.2.0.
func pages() map[string]page {
	out := map[string]page{}
	for _, id := range lint.AllIDs() {
		since := "v0.1.0"
		if v, ok := laterRules[id]; ok {
			since = v
		}
		out[id] = page{severity: string(lint.DefaultSeverity(id)), since: since}
	}
	for _, id := range importer.IDs() {
		out[id] = page{severity: string(importer.Severity(id)), since: "v0.2.0"}
	}
	maps.Copy(out, retiredRules)
	return out
}

// documented is every ID that has a page, sorted: every live lint ID and
// import finding, and every retired ID.
func documented() []string {
	ids := slices.Concat(lint.AllIDs(), importer.IDs(), slices.Collect(maps.Keys(retiredRules)))
	slices.Sort(ids)
	return ids
}

// TestRetiredRules: a retired ID is no longer reported, so it is not in
// lint.AllIDs, and its page says in prose that it is retired.
func TestRetiredRules(t *testing.T) {
	for id, want := range retiredRules {
		assert.NotContains(t, lint.AllIDs(), id, "%s is retired, so nothing may report it", id)
		assert.NotContains(t, importer.IDs(), id)
		page, ok := Rule(id)
		require.True(t, ok, "docs/rules/%s.md stays: IDs are addresses", id)
		assert.Contains(t, page, "Retired in "+want.retired, id)
	}
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

			head := []string{"---", "severity: " + want[id].severity, "since: " + want[id].since}
			if want[id].retired != "" {
				head = append(head, "retired: "+want[id].retired)
			}
			head = append(head, "---", "", "# "+id, "")
			require.Greater(t, len(lines), len(head)+1)
			assert.Equal(t, head, lines[:len(head)])
			summary := lines[len(head)]
			assert.NotEmpty(t, summary, "a one-line summary under the H1")
			assert.False(t, strings.HasPrefix(summary, "#") || strings.HasPrefix(summary, "**"), summary)
			assert.Empty(t, lines[len(head)+1], "the summary is one line")

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
// Action usage, describes every emitter as built (XAML since v0.5.0),
// the consumer rules as built since v0.6.0 and components and retired
// treatments as new in v0.7.0, find, usages and diff as built for the
// next release, and plans no emitter.
func TestReadmeMatchesRelease(t *testing.T) {
	b, err := os.ReadFile("../README.md")
	require.NoError(t, err)
	readme := string(b)
	for _, want := range []string{
		"go install github.com/c65llc/airframe/cmd/airframe@latest",
		"airframe init", "airframe lint", "airframe build", "airframe fmt",
		"uses: c65llc/airframe@v0", "version: v0.7.0",
		"**Planned (not in v0.7.0)**",
	} {
		assert.Contains(t, readme, want)
	}
	_, planned, ok := strings.Cut(readme, "**Planned (not in v0.7.0)**")
	require.True(t, ok)
	built, _, _ := strings.Cut(readme, "**Planned (not in v0.7.0)**")
	planned, _, ok = strings.Cut(planned, "\n## ")
	require.True(t, ok, "the planned list ends at the next section")
	for _, emitter := range []string{"Swift", "Kotlin", "XAML"} {
		assert.Contains(t, built, emitter, "a built emitter is described before the planned list")
		assert.NotContains(t, planned, emitter, "a built emitter is not planned")
	}
	assert.Contains(t, built, "**Emits XAML/WinUI** (since v0.5.0)", "XAML is built and released")
	assert.Contains(t, built, "(docs/guides/windows.md)")
	for _, word := range []string{"emitter", "emit-unsupported", "C#"} {
		assert.NotContains(t, planned, word, "every emitter is built; the planned list names none")
	}
	// The consumer rules are built since v0.6.0, with their guide;
	// components' own rules, retired treatments and the reference surface
	// stay planned.
	assert.Contains(t, built, "**Lints consumers** (since v0.6.0)")
	assert.Contains(t, built, "(docs/guides/consumers.md)")
	for _, rule := range []string{"reference-undeclared", "primitive-read-by-consumer", "token-owner-violation", "fallback-literal", "wrapper-unmapped"} {
		assert.Contains(t, built, rule, "a built consumer rule is described before the planned list")
		assert.NotContains(t, planned, rule, "a built consumer rule is not planned")
	}
	assert.NotContains(t, planned, "Consumer rules", "the consumer rules are built")
	// Components and retired treatments are new in v0.7.0,
	// with their guide and show's component mode; the reference commands
	// and the surfaces stay planned.
	assert.Contains(t, built, "**Inventories components** (new in v0.7.0)")
	assert.Contains(t, built, "**Retires treatments** (new in v0.7.0)")
	assert.Contains(t, built, "(docs/guides/components.md)")
	assert.Contains(t, built, "airframe show <component>")
	assert.Contains(t, built, "(docs/guides/components.md#upgrading-from-v060)", "the upgrade note is linked")
	for _, rule := range []string{"component-unruled", "component-incomplete", "retired-used"} {
		assert.Contains(t, built, rule, "a built component rule is described before the planned list")
		assert.NotContains(t, planned, rule, "a built component rule is not planned")
	}
	assert.NotContains(t, planned, "Components and retired treatments", "components and retired treatments are built")
	// find, usages and diff are built for the next release, with their
	// guide; rename, retire, specimen and the surfaces stay planned.
	assert.Contains(t, built, "**Finds, lists usages of and diffs tokens** (next release)")
	assert.Contains(t, built, "(docs/guides/reference.md)")
	for _, cmd := range []string{"`airframe find <words>`", "`airframe usages <token>`", "`airframe diff <ref-a> [<ref-b>]`", "--file", "job summary"} {
		assert.Contains(t, built, cmd, "a built reference command is described before the planned list")
	}
	for _, cmd := range []string{"`find`", "`usages`", "`diff`", "find,", "usages,", "diff,"} {
		assert.NotContains(t, planned, cmd, "a built reference command is not planned")
	}
	for _, want := range []string{"`rename`", "`retire`", "`specimen`", "skills", "MCP", "PWA", "editor"} {
		assert.Contains(t, planned, want, "the remaining reference commands and surfaces stay planned")
	}
}
