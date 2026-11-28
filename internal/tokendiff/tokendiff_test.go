// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokendiff

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
)

const testConfig = `version: 1
content: ds
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ":root[data-theme='dark']"}}
platforms:
  web: {emit: css}
`

func load(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	m := project.MemFS{"airframe.yaml": testConfig}
	for k, v := range files {
		m[k] = v
	}
	p, ds, err := project.Load(m)
	require.NoError(t, err)
	for _, d := range ds {
		require.NotEqual(t, diag.Error, d.Severity, "%+v", d)
	}
	require.NotNil(t, p)
	return p
}

const before = `{
  "gray": {
    "$type": "color",
    "50": {"$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "900": {"$value": {"colorSpace": "oklch", "components": [0.2, 0.012, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "surface": {"$type": "color", "$value": "{gray.50}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink": {"$type": "color", "$value": "{gray.900}", "$description": "Body text.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "legacy": {"$type": "color", "$value": "{gray.900}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "old-gap": {"$type": "dimension", "$value": {"value": 8, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "gone": {"$type": "dimension", "$value": {"value": 3, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}
}
`

const beforeDark = `{
  "$extensions": {"com.c65llc.airframe": {"mode": "appearance:dark"}},
  "surface": {"$value": "{gray.900}"}
}
`

const after = `{
  "gray": {
    "$type": "color",
    "50": {"$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "700": {"$value": {"colorSpace": "oklch", "components": [0.38, 0.01, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "900": {"$value": {"colorSpace": "oklch", "components": [0.2, 0.012, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "surface": {"$type": "color", "$value": "{gray.50}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink": {"$type": "color", "$value": "{gray.900}", "$description": "Body text | prose.", "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "legacy": {"$type": "color", "$value": "{gray.900}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic", "retired": true}}},
  "gap": {"$type": "dimension", "$value": {"value": 8, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}
}
`

const afterDark = `{
  "$extensions": {"com.c65llc.airframe": {"mode": "appearance:dark"}},
  "surface": {"$value": "{gray.700}"}
}
`

// Fixtures are the before and after projects TestCompare and the format
// tests share.
func Fixtures(t *testing.T) (*project.Project, *project.Project) {
	a := load(t, map[string]string{"ds/tokens/base.tokens.json": before, "ds/themes/dark.tokens.json": beforeDark})
	b := load(t, map[string]string{"ds/tokens/base.tokens.json": after, "ds/themes/dark.tokens.json": afterDark})
	return a, b
}

func TestCompare(t *testing.T) {
	a, b := Fixtures(t)
	d := Compare(a, b)

	names := func(ts []Token) []string {
		var out []string
		for _, t := range ts {
			out = append(out, t.Name)
		}
		return out
	}
	assert.Equal(t, []string{"gap", "gray-700"}, names(d.Added))
	assert.Equal(t, []string{"gone", "old-gap"}, names(d.Removed))
	assert.Equal(t, []Rename{{From: "old-gap", To: "gap"}}, d.Renamed, "same type and value in every context")
	assert.Equal(t, "#3e4348", d.Added[1].Value.Hex)
	assert.Equal(t, "oklch(38% 0.01 250)", d.Added[1].Value.Value)

	require.Len(t, d.Changed, 3)
	byName := map[string][]Change{}
	for _, c := range d.Changed {
		byName[c.Name] = c.Changes
	}
	assert.Equal(t, []string{"ink", "legacy", "surface"}, []string{d.Changed[0].Name, d.Changed[1].Name, d.Changed[2].Name})

	// A value change in one mode only: the base is unchanged.
	require.Len(t, byName["surface"], 1)
	sc := byName["surface"][0]
	assert.Equal(t, KindValue, sc.Kind)
	assert.Equal(t, "appearance:dark", string(sc.Context))
	assert.Equal(t, "oklch(20% 0.012 250)", sc.Before.Value)
	assert.Equal(t, "oklch(38% 0.01 250)", sc.After.Value)
	assert.NotEmpty(t, sc.Before.Hex)

	assert.Equal(t, []Change{
		{Kind: KindTier, From: "semantic", To: "primitive"},
		{Kind: KindDescription, From: "Body text.", To: "Body text | prose."},
	}, byName["ink"])
	assert.Equal(t, []Change{{Kind: KindRetired, From: "false", To: "true"}}, byName["legacy"])
	assert.False(t, d.Empty())

	assert.True(t, Compare(a, a).Empty(), "a project against itself")
}

func TestCompareAliasRetargetSameValue(t *testing.T) {
	// The alias changes but the resolved value does not: no change.
	a := load(t, map[string]string{"ds/tokens/base.tokens.json": before})
	b := load(t, map[string]string{"ds/tokens/base.tokens.json": `{
  "gray": {
    "$type": "color",
    "50": {"$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "900": {"$value": {"colorSpace": "oklch", "components": [0.2, 0.012, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "surface": {"$type": "color", "$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "ink": {"$type": "color", "$value": "{gray.900}", "$description": "Body text.", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "legacy": {"$type": "color", "$value": "{ink}", "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "old-gap": {"$type": "dimension", "$value": {"value": 8, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "gone": {"$type": "dimension", "$value": {"value": 3, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}}
}`})
	assert.True(t, Compare(a, b).Empty(), "%+v", Compare(a, b))
}

func TestCompareBaseValueChange(t *testing.T) {
	a := load(t, map[string]string{"ds/tokens/base.tokens.json": before, "ds/themes/dark.tokens.json": beforeDark})
	b := load(t, map[string]string{"ds/tokens/base.tokens.json": before, "ds/themes/dark.tokens.json": `{
  "$extensions": {"com.c65llc.airframe": {"mode": "appearance:dark"}},
  "surface": {"$value": "{gray.900}"},
  "ink": {"$value": "{gray.50}"}
}`})
	d := Compare(a, b)
	require.Len(t, d.Changed, 1)
	assert.Equal(t, "ink", d.Changed[0].Name)
	require.Len(t, d.Changed[0].Changes, 1, "a new mode value is a change in that mode only")
	assert.Equal(t, "appearance:dark", string(d.Changed[0].Changes[0].Context))
}

// renameTok is a dimension token of tier tier (and component comp) of
// the given px value.
func renameTok(name, tier, comp string, px int) string {
	ext := `"tier": "` + tier + `"`
	if comp != "" {
		ext += `, "component": "` + comp + `"`
	}
	return `"` + name + `": {"$type": "dimension", "$value": {"value": ` + strconv.Itoa(px) + `, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {` + ext + `}}}`
}

func renameProject(t *testing.T, toks ...string) *project.Project {
	t.Helper()
	return load(t, map[string]string{"ds/t.tokens.json": "{" + strings.Join(toks, ",") + "}"})
}

func TestRenamesNeedSameTierAndComponent(t *testing.T) {
	a := renameProject(t,
		renameTok("old-semantic", "semantic", "", 4),
		renameTok("old-primitive", "primitive", "", 6),
	)
	b := renameProject(t,
		renameTok("new-primitive-four", "primitive", "", 4), // same value, other tier
		renameTok("new-semantic-six", "semantic", "", 6),    // same value, other tier
	)
	assert.Empty(t, Compare(a, b).Renamed)

	// The same value, tier and type, but another component: no pair.
	a = load(t, map[string]string{"ds/t.tokens.json": "{" + renameTok("button-pad", "component", "button", 4) + "}",
		"ds/components/button.yaml": "id: button\ntokens: [button-pad]\nshared: true\n"})
	b = load(t, map[string]string{"ds/t.tokens.json": "{" + renameTok("card-pad", "component", "card", 4) + "}",
		"ds/components/card.yaml": "id: card\ntokens: [card-pad]\nshared: true\n"})
	assert.Empty(t, Compare(a, b).Renamed)
}

// Several candidates: each removed token pairs with the added one that
// shares most name segments, not the first in name order, and no token
// pairs twice.
func TestRenamesPreferSharedSegments(t *testing.T) {
	a := renameProject(t,
		renameTok("card-gap", "semantic", "", 8),
		renameTok("button-gap", "semantic", "", 8),
		renameTok("lone", "semantic", "", 8),
	)
	b := renameProject(t,
		renameTok("aa-card-gap", "semantic", "", 8),  // shares card, gap with card-gap
		renameTok("button-space", "semantic", "", 8), // shares button with button-gap
		renameTok("zz-unrelated", "semantic", "", 8), // shares nothing
	)
	assert.Equal(t, []Rename{
		{From: "button-gap", To: "button-space"},
		{From: "card-gap", To: "aa-card-gap"},
		{From: "lone", To: "zz-unrelated"},
	}, Compare(a, b).Renamed)

	// Cross-pairing: name order would give a→x and leave b without its
	// better match; segments decide instead.
	a = renameProject(t,
		renameTok("a-gap", "semantic", "", 2),
		renameTok("b-gap-wide", "semantic", "", 2),
	)
	b = renameProject(t,
		renameTok("x-gap-wide", "semantic", "", 2), // shares gap, wide with b-gap-wide
		renameTok("y-gap", "semantic", "", 2),      // shares gap with both
	)
	assert.Equal(t, []Rename{
		{From: "a-gap", To: "y-gap"},
		{From: "b-gap-wide", To: "x-gap-wide"},
	}, Compare(a, b).Renamed)

	// Equal segments: name order, each once.
	a = renameProject(t, renameTok("p", "semantic", "", 1), renameTok("q", "semantic", "", 1))
	b = renameProject(t, renameTok("r", "semantic", "", 1), renameTok("s", "semantic", "", 1), renameTok("t", "semantic", "", 1))
	assert.Equal(t, []Rename{{From: "p", To: "r"}, {From: "q", To: "s"}}, Compare(a, b).Renamed)
}
