// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package retired

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/schema"
)

const file = "design-system/retired.yaml"

func mustParse(t *testing.T, src string) []Treatment {
	t.Helper()
	ts, ds := Parse(file, []byte(src))
	require.Empty(t, ds, src)
	require.NotNil(t, ts)
	return ts
}

func TestRetiredLoad(t *testing.T) {
	ts := mustParse(t, `- id: flush-left-bar
  ruling: "@ruling:selection"
  spellings:
    css: ["inset 3px 0 0", "border-left: 3px"]
    cs: ['Width="3"']
  marker: audited-not-retired
- id: other
  spellings: { swift: ["a.*b"], kotlin: [x], xaml: [y] }
`)
	require.Len(t, ts, 2)
	a := ts[0]
	assert.Equal(t, "flush-left-bar", a.ID)
	assert.Equal(t, "@ruling:selection", a.Ruling)
	assert.Equal(t, "audited-not-retired", a.Marker)
	assert.Equal(t, file, a.File)
	assert.Equal(t, 1, a.Line)
	require.Len(t, a.Spellings["css"], 2)
	assert.True(t, a.Spellings["css"][0].MatchString("x inset 3px 0 0 y"))
	assert.Len(t, a.Spellings["cs"], 1)
	assert.Equal(t, 7, ts[1].Line)
	assert.Equal(t, "", ts[1].Ruling)
	assert.Equal(t, "", ts[1].Marker)
	assert.Len(t, ts[1].Spellings, 3)

	for _, tc := range []struct{ label, src, path, msg string }{
		{"duplicate id", "- id: a\n  spellings: {css: [x]}\n- id: a\n  spellings: {css: [y]}\n", "1.id", "duplicate id"},
		{"bad id upper", "- id: Flush\n  spellings: {css: [x]}\n", "0.id", "pattern"},
		{"bad id underscore", "- id: a_b\n  spellings: {css: [x]}\n", "0.id", "pattern"},
		{"id a number", "- id: 5\n  spellings: {css: [x]}\n", "0.id", "string"},
		{"unknown adapter", "- id: a\n  spellings: {scss: [x]}\n", "0.spellings.scss", "unknown key"},
		{"empty list", "[]\n", "", "minItems"},
		{"empty file", "", "", "array"},
		{"not a list", "id: a\n", "", "array"},
		{"empty spellings", "- id: a\n  spellings: {}\n", "0.spellings", "minProperties"},
		{"missing spellings", "- id: a\n", "0", "spellings"},
		{"empty pattern list", "- id: a\n  spellings: {css: []}\n", "0.spellings.css", "minItems"},
		{"empty pattern", "- id: a\n  spellings: {css: ['']}\n", "0.spellings.css.0", ""},
		{"pattern a number", "- id: a\n  spellings: {css: [5]}\n", "0.spellings.css.0", "string"},
		{"bad regex", "- id: a\n  spellings: {css: ['(x']}\n", "0.spellings.css.0", "RE2"},
		{"bad marker", "- id: a\n  spellings: {css: [x]}\n  marker: Not Ok\n", "0.marker", "pattern"},
		{"marker a number", "- id: a\n  spellings: {css: [x]}\n  marker: 5\n", "0.marker", "string"},
		{"ruling a list", "- id: a\n  spellings: {css: [x]}\n  ruling: [a]\n", "0.ruling", "string"},
		{"unknown field", "- id: a\n  spellings: {css: [x]}\n  colour: red\n", "0.colour", "unknown key"},
		{"alias", "- id: a\n  spellings: {css: &p [x]}\n- id: b\n  spellings: {css: *p}\n", "", "alias"},
	} {
		ts, ds := Parse(file, []byte(tc.src))
		assert.Nil(t, ts, tc.label)
		require.Len(t, ds, 1, tc.label)
		assert.Equal(t, "schema-invalid", ds[0].Rule, tc.label)
		assert.Equal(t, file, ds[0].File, tc.label)
		assert.Equal(t, tc.path, ds[0].Path, tc.label)
		assert.Contains(t, ds[0].Message, tc.msg, tc.label)
		if tc.src != "" {
			assert.Positive(t, ds[0].Line, tc.label)
		}
	}

	_, ds := Parse(file, []byte("- id: [\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "yaml-syntax", ds[0].Rule)
	assert.Equal(t, file, ds[0].File)
}

func TestRetiredLoadLines(t *testing.T) {
	_, ds := Parse(file, []byte("# retired\n- id: a\n  spellings: {css: [x]}\n- id: b\n  spellings:\n    css:\n      - ok\n      - '(bad'\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, 8, ds[0].Line)
	assert.Equal(t, "1.spellings.css.1", ds[0].Path)
}

func TestSpecRetiredExampleLoads(t *testing.T) {
	data, err := os.ReadFile("../../docs/specs/2026-11-27-airframe-design.md")
	require.NoError(t, err)
	_, after, ok := strings.Cut(string(data), "**Retired** (`retired.yaml`")
	require.True(t, ok)
	_, after, ok = strings.Cut(after, "```yaml\n")
	require.True(t, ok)
	block, _, ok := strings.Cut(after, "```")
	require.True(t, ok)

	ts := mustParse(t, block)
	require.Len(t, ts, 1)
	assert.Equal(t, "flush-left-selection-bar", ts[0].ID)
	assert.Equal(t, "@ruling:selection-treatment", ts[0].Ruling)
	assert.Equal(t, "audited-not-retired-treatment", ts[0].Marker)
	assert.Len(t, ts[0].Spellings["css"], 2)
	assert.True(t, ts[0].Spellings["swift"][0].MatchString(".frame(width: 3).foregroundStyle(.accent"))
}

// The retired schema keys spellings by exactly the consumer adapter names.
func TestRetiredSchemaAdapterKeys(t *testing.T) {
	b, ok := schema.Source("retired")
	require.True(t, ok)
	var doc struct {
		Items struct {
			Properties struct {
				Spellings struct {
					Properties map[string]any `json:"properties"`
				} `json:"spellings"`
			} `json:"properties"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(b, &doc))
	var keys []string
	for k := range doc.Items.Properties.Spellings.Properties {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, config.AdapterNames, keys)
}
