// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package find

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/project"
)

// rankToken is one token of the Rank fixture: a top-level name (dashes
// stand for nesting), tier, component, and description.
type rankToken struct{ name, tier, component, desc string }

var rankTokens = []rankToken{
	{"muted", "semantic", "", ""},                   // whole segment
	{"mutedness", "semantic", "", ""},               // a segment starts with the word
	{"plain", "semantic", "", "Muted body text."},   // a whole description word
	{"other", "semantic", "", "Mutedly coloured."},  // a description word starts with it
	{"subtle", "semantic", "", ""},                  // a synonym of muted, one lower
	{"both-muted", "semantic", "", "Muted, twice."}, // segment and description add up
	{"tie", "primitive", "", ""},
	{"b-tie", "semantic", "", ""},
	{"a-tie", "semantic", "", ""},
	{"widget-tie", "component", "widget", ""}, // the noun "widget" earns 1 too
	{"lone-tie", "component", "panel", ""},
}

func rankProject(t *testing.T) *project.Project {
	t.Helper()
	var leaves []string
	for _, tk := range rankTokens {
		ext := fmt.Sprintf(`"tier": %q`, tk.tier)
		if tk.component != "" {
			ext += fmt.Sprintf(`, "component": %q`, tk.component)
		}
		desc := ""
		if tk.desc != "" {
			desc = fmt.Sprintf(`"$description": %q, `, tk.desc)
		}
		leaf := fmt.Sprintf(`{ "$type": "dimension", %s"$value": { "value": 1, "unit": "px" }, "$extensions": { "com.c65llc.airframe": { %s } } }`, desc, ext)
		parts := strings.Split(tk.name, "-")
		for i := len(parts) - 1; i > 0; i-- {
			leaf = fmt.Sprintf(`{ %q: %s }`, parts[i], leaf)
		}
		leaves = append(leaves, fmt.Sprintf(`%q: %s`, parts[0], leaf))
	}
	fsys := project.MemFS{
		"airframe.yaml":                   "version: 1\ncontent: design-system\n",
		"design-system/all.tokens.json":   "{\n" + strings.Join(leaves, ",\n") + "\n}\n",
		"design-system/components/x.yaml": "id: widget\nnoun: Widget\ntokens: [widget-tie]\nshared: true\n",
		"design-system/components/y.yaml": "id: panel\nnoun: Panel\ntokens: [lone-tie]\nshared: true\n",
	}
	p, _, err := project.Load(fsys)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.NotNil(t, p.Tokens)
	return p
}

// scores maps each hit's name to its score.
func scores(hits []Hit) map[string]int {
	m := map[string]int{}
	for _, h := range hits {
		m[h.Name] = h.Score
	}
	return m
}

func TestRankWeights(t *testing.T) {
	p := rankProject(t)
	got := scores(Rank(p, "muted", Options{Limit: 100}))
	assert.Equal(t, map[string]int{
		"muted":      wholeSegment,             // 3
		"mutedness":  segmentStart,             // 2
		"plain":      wholeDesc,                // 2
		"other":      descStart,                // 1
		"subtle":     wholeSegment - 1,         // 2: synonym, one lower
		"both-muted": wholeSegment + wholeDesc, // 5
	}, got)

	// The component noun earns nounWord on top of a segment match.
	got = scores(Rank(p, "widget", Options{Limit: 100}))
	assert.Equal(t, map[string]int{"widget-tie": wholeSegment + nounWord}, got)
}

func TestRankTieOrder(t *testing.T) {
	p := rankProject(t)
	var names []string
	for _, h := range Rank(p, "tie", Options{Limit: 100}) {
		names = append(names, h.Name)
	}
	// Equal scores: semantic, then component, then primitive; then name.
	assert.Equal(t, []string{"a-tie", "b-tie", "lone-tie", "widget-tie", "tie"}, names)
}

func TestRankStopwords(t *testing.T) {
	p := rankProject(t)
	assert.Equal(t, []string{"muted"}, Words("the muted of a"))
	assert.Equal(t, Rank(p, "muted", Options{}), Rank(p, "the muted for a", Options{}))
	assert.Empty(t, Words("the of a"))
	assert.Nil(t, Rank(p, "the of a", Options{}))
}

func TestRankLimit(t *testing.T) {
	p := rankProject(t)
	assert.Len(t, Rank(p, "tie", Options{Limit: 2}), 2)
	assert.Len(t, Rank(p, "tie muted", Options{}), DefaultLimit) // 11 match
}
