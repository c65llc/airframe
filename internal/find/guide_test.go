// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package find

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGuideSynonyms: the reference guide's synonym table lists exactly the
// synonym groups find uses, one row per group, and its weights table
// carries find's weights.
func TestGuideSynonyms(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "reference.md"))
	require.NoError(t, err)
	guide := string(b)
	_, table, ok := strings.Cut(guide, "| Words that stand for one another |\n| --- |\n")
	require.True(t, ok, "the guide has the synonym table")
	table, _, _ = strings.Cut(table, "\n\n")
	var want []string
	for _, g := range synonymGroups {
		want = append(want, "| `"+strings.Join(g, "`, `")+"` |")
	}
	assert.Equal(t, want, strings.Split(table, "\n"), "the guide's synonym table is find's, in order")

	for _, row := range []struct {
		what   string
		weight int
	}{
		{"is a whole word of the name", wholeSegment},
		{"starts a word of the name", segmentStart},
		{"is a whole word of the description", wholeDesc},
		{"starts a word of the description", descStart},
		{"is a word of the component's noun", nounWord},
	} {
		re := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(row.what) + ` \| (\d+) \|$`)
		m := re.FindStringSubmatch(guide)
		if assert.NotNil(t, m, "the weights table has %q", row.what) {
			assert.Equal(t, fmt.Sprint(row.weight), m[1], row.what)
		}
	}
	for w := range stopwords {
		assert.Contains(t, guide, "`"+w+"`", "the guide lists the stopword %q", w)
	}
}
