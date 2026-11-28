// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokendiff

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func markdown(t *testing.T, d Diff) string {
	t.Helper()
	var b strings.Builder
	require.NoError(t, WriteMarkdown(&b, d, Side{Ref: "v1", Commit: strings.Repeat("a", 40)}, Side{}))
	return b.String()
}

// A colour cell is the swatch, then the hex and the oklch as two code
// spans, so neither reads as part of the other.
func TestMarkdownColourCell(t *testing.T) {
	md := markdown(t, Diff{Added: []Token{{Name: "ink", Type: "color", Value: Value{Value: "oklch(20% 0.01 250)", Hex: "#12171b"}}}})
	assert.Contains(t, md, "| ■ `#12171b` `oklch(20% 0.01 250)` |")
}

// Descriptions are prose: HTML and Markdown in them are shown, not
// rendered, and a pipe does not end the cell.
func TestMarkdownEscapesDescriptions(t *testing.T) {
	md := markdown(t, Diff{Changed: []Changed{{Name: "ink", Changes: []Change{
		{Kind: KindDescription, From: "Plain.", To: "<b>Bold</b> & a|b *x* _y_ `z` \\n"},
	}}}})
	assert.Contains(t, md, "| *description* | Plain. | &lt;b&gt;Bold&lt;/b&gt; &amp; a\\|b \\*x\\* \\_y\\_ \\`z\\` \\\\n |\n")
	assert.NotContains(t, md, "<b>")
}

// Past 50 changed tokens the per-token tables fold into one <details>; the
// added and removed lists stay as they are.
func TestMarkdownFoldsManyChanges(t *testing.T) {
	changed := func(n int) Diff {
		d := Diff{Added: []Token{{Name: "new", Type: "dimension", Value: Value{Value: "1px"}}}}
		for i := range n {
			d.Changed = append(d.Changed, Changed{Name: fmt.Sprintf("t%03d", i), Changes: []Change{
				{Kind: KindValue, Before: Value{Value: "1px"}, After: Value{Value: "2px"}},
			}})
		}
		return d
	}
	md := markdown(t, changed(50))
	assert.NotContains(t, md, "<details>")

	md = markdown(t, changed(51))
	assert.Contains(t, md, "### Changed (51)\n\n<details><summary>51 changed tokens</summary>\n\n#### `t000`\n")
	assert.True(t, strings.HasSuffix(md, "| base | `1px` | `2px` |\n\n</details>\n"), md[len(md)-80:])
	assert.Less(t, strings.Index(md, "### Added (1)"), strings.Index(md, "<details>"))
}
