// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package docs

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/lint"
)

// defaults is each rule ID's default severity: a registered rule's own,
// "error" for a load-time ID.
func defaults() map[string]string {
	out := map[string]string{}
	for _, id := range lint.AllIDs() {
		out[id] = "error"
	}
	for _, r := range lint.Rules() {
		out[r.ID] = string(r.Default)
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
