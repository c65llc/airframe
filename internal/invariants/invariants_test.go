// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package invariants

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const specExample = `- id: accent-hue-stable
  kind: same-hue-across-appearances      # the accent hue never flips between themes
  tokens: ["accent", "accent-ink"]
  forEach: accentGroup
- id: danger-hue-reserved
  kind: hue-reserved
  hue: 25
  tolerance: 8
  only: ["danger", "danger-*"]
- id: selection-text-contrast
  kind: contrast
  fg: accent-ink
  bg: accent-soft
  min: 4.5
  forEach: [accentGroup, appearance]
- id: touch-floor
  kind: min-dimension
  match: "*-target*"
  min: 44
  densities: all
`

func TestInvariantsSpecExample(t *testing.T) {
	invs, ds := Parse([]byte(specExample))
	require.Empty(t, ds)
	require.Len(t, invs, 4)
	var kinds []string
	for _, i := range invs {
		kinds = append(kinds, i.Kind)
	}
	assert.Equal(t, []string{"same-hue-across-appearances", "hue-reserved", "contrast", "min-dimension"}, kinds)

	assert.Equal(t, []string{"accentGroup"}, invs[0].ForEach)
	assert.Equal(t, []string{"accent", "accent-ink"}, invs[0].Tokens)
	assert.Equal(t, 1, invs[0].Line)
	assert.Equal(t, 5, invs[1].Line)
	assert.Equal(t, 25.0, invs[1].Hue)
	assert.Equal(t, 8.0, invs[1].Tolerance)
	assert.Equal(t, []string{"danger", "danger-*"}, invs[1].Only)
	assert.Equal(t, []string{"accentGroup", "appearance"}, invs[2].ForEach)
	assert.Equal(t, "accent-ink", invs[2].FG)
	assert.Equal(t, "accent-soft", invs[2].BG)
	assert.Equal(t, 4.5, invs[2].Min)
	assert.Equal(t, "*-target*", invs[3].Match)
	assert.Equal(t, 44.0, invs[3].Min)
}

func TestInvariantUnknownKind(t *testing.T) {
	invs, ds := Parse([]byte("- id: x\n  kind: nope\n"))
	assert.Nil(t, invs)
	require.NotEmpty(t, ds)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, 2, ds[0].Line)
	assert.Equal(t, "invariants.yaml", ds[0].File)
}

func TestInvariantYAMLSyntax(t *testing.T) {
	_, ds := Parse([]byte("- id: x\n  kind: [unclosed\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "yaml-syntax", ds[0].Rule)
	assert.Positive(t, ds[0].Line)
}

func TestInvariantUnknownKey(t *testing.T) {
	_, ds := Parse([]byte("- id: x\n  kind: contrast\n  bogus: 1\n"))
	require.Len(t, ds, 1)
	assert.Equal(t, "schema-invalid", ds[0].Rule)
	assert.Equal(t, 3, ds[0].Line)
}

func TestInvariantHasRecordsPresentKeys(t *testing.T) {
	invs, ds := Parse([]byte("- id: x\n  kind: hue-reserved\n  hue: 0\n  tolerance: 4\n- id: y\n  kind: hue-reserved\n"))
	require.Empty(t, ds)
	require.Len(t, invs, 2)
	assert.True(t, invs[0].Has["hue"], "hue: 0 is present")
	assert.True(t, invs[0].Has["tolerance"])
	assert.False(t, invs[1].Has["hue"], "an absent hue is not")
}

func TestInvariantMultipleDocuments(t *testing.T) {
	invs, ds := Parse([]byte("- { id: a, kind: contrast, fg: x, bg: y, min: 3 }\n---\n- { id: b, kind: contrast, fg: x, bg: y, min: 3 }\n"))
	assert.Empty(t, invs)
	require.Len(t, ds, 1)
	assert.Equal(t, "yaml-syntax", ds[0].Rule)
	assert.Equal(t, "multiple YAML documents are not supported", ds[0].Message)
	assert.Equal(t, 2, ds[0].Line)
	assert.Equal(t, File, ds[0].File)
}
