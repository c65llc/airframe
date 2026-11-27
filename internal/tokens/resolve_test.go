// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokens

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
)

func ctx(app, appearance string) config.Context {
	return config.Context{"app": app, "appearance": appearance, "density": "standard"}
}

func TestMostSpecificWins(t *testing.T) {
	s := mustParse(t, map[string]string{"a.tokens.json": `{ "x": { "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "modes": {
    "appearance:dark": 2, "appearance:dark+app:slides": 3 } } } } }`})
	for _, tc := range []struct {
		ctx  config.Context
		want float64
		key  config.ModeKey
	}{
		{ctx("slides", "dark"), 3, "app:slides+appearance:dark"},
		{ctx("wiki", "dark"), 2, "appearance:dark"},
		{ctx("wiki", "light"), 1, ""},
	} {
		v, err := s.Resolve("x", tc.ctx)
		require.NoError(t, err)
		assert.Equal(t, tc.want, v.Lit, tc.ctx)
		got, key := s.ValueFor("x", tc.ctx)
		assert.Equal(t, tc.want, got.Lit)
		assert.Equal(t, tc.key, key)
	}
	assert.Empty(t, s.Check())
}

func TestAliasResolvesInContext(t *testing.T) {
	s := mustParse(t, map[string]string{"a.tokens.json": `{
  "accent": { "$type": "color",
    "$value": { "colorSpace": "oklch", "components": [0.6, 0.1, 250] },
    "$extensions": { "com.c65llc.airframe": { "modes": {
      "appearance:dark": { "colorSpace": "oklch", "components": [0.75, 0.1, 250] } } } } },
  "accent-ink": { "$type": "color", "$value": "{accent}" }
}`})
	v, key := s.ValueFor("accent-ink", ctx("wiki", "dark"))
	assert.Equal(t, "accent", v.Alias)
	assert.Equal(t, config.ModeKey(""), key)

	v, err := s.Resolve("accent-ink", ctx("wiki", "dark"))
	require.NoError(t, err)
	assert.InDelta(t, 0.75, v.Lit.(color.OKLCH).L, 1e-9)
	v, err = s.Resolve("accent-ink", ctx("wiki", "light"))
	require.NoError(t, err)
	assert.InDelta(t, 0.6, v.Lit.(color.OKLCH).L, 1e-9)
	assert.Empty(t, s.Check())
}

func TestAliasCycle(t *testing.T) {
	s := mustParse(t, map[string]string{"a.tokens.json": `{
  "a": { "$type": "number", "$value": "{b}" },
  "b": { "$type": "number", "$value": "{a}" }
}`})
	_, err := s.Resolve("a", ctx("wiki", "light"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a → b → a")
	d := only(t, s.Check(), "alias-cycle")
	assert.Contains(t, d.Message, "a → b → a")
	assert.Equal(t, "a.tokens.json", d.File)
	assert.Equal(t, 2, d.Line)
}

// A cycle that needs two overrides from different axes at once is still
// found; one confined to contexts that never co-occur is not reported.
func TestAliasCycleContextual(t *testing.T) {
	s := mustParse(t, map[string]string{"a.tokens.json": `{
  "a": { "$type": "number", "$value": 1,
    "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:dark": "{b}" } } } },
  "b": { "$type": "number", "$value": 2,
    "$extensions": { "com.c65llc.airframe": { "modes": { "app:slides": "{a}" } } } }
}`})
	_, err := s.Resolve("a", ctx("wiki", "dark"))
	require.NoError(t, err)
	_, err = s.Resolve("a", ctx("slides", "dark"))
	require.Error(t, err)
	d := only(t, s.Check(), "alias-cycle")
	assert.Contains(t, d.Message, "a → b → a")
	assert.Contains(t, d.Message, "app:slides+appearance:dark")

	s = mustParse(t, map[string]string{"a.tokens.json": `{
  "a": { "$type": "number", "$value": 1,
    "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:dark": "{b}" } } } },
  "b": { "$type": "number", "$value": 2,
    "$extensions": { "com.c65llc.airframe": { "modes": { "appearance:light": "{a}" } } } }
}`})
	assert.Empty(t, s.Check())
}

func TestAliasUnresolved(t *testing.T) {
	s := mustParse(t, map[string]string{"a.tokens.json": `{
  "a": { "$type": "number", "$value": "{missing.thing}" }
}`})
	_, err := s.Resolve("a", ctx("wiki", "light"))
	assert.Error(t, err)
	d := only(t, s.Check(), "alias-unresolved")
	assert.Contains(t, d.Message, `"missing-thing"`)
	assert.Equal(t, 2, d.Line)
}

func TestAliasTypeMismatch(t *testing.T) {
	s := mustParse(t, map[string]string{
		"color.tokens.json": `{ "accent": { "$type": "color",
  "$value": { "colorSpace": "oklch", "components": [0.6, 0.1, 250] } } }`,
		"size.tokens.json": `{ "gap": { "$type": "dimension", "$value": "{accent}" } }`,
	})
	d := only(t, s.Check(), "alias-type-mismatch")
	assert.Equal(t, "size.tokens.json", d.File)
	assert.Contains(t, d.Message, "dimension")
	assert.Contains(t, d.Message, "color")
	assert.Contains(t, d.Message, "color.tokens.json")
}

func TestModeAmbiguous(t *testing.T) {
	s := mustParse(t, map[string]string{"a.tokens.json": `{ "x": { "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "modes": {
    "app:slides": 2, "appearance:dark": 3 } } } } }`})
	d := only(t, s.Check(), "mode-ambiguous")
	assert.Contains(t, d.Message, "app:slides")
	assert.Contains(t, d.Message, "appearance:dark")
	assert.Contains(t, d.Message, "app:slides+appearance:dark")

	v, key := s.ValueFor("x", ctx("slides", "dark"))
	assert.NotNil(t, v)
	assert.Equal(t, config.ModeKey("app:slides"), key, "a tie picks the first key in sort order")
}

func TestModeAmbiguousNotReported(t *testing.T) {
	for name, modes := range map[string]string{
		"same value":     `"app:slides": 2, "appearance:dark": 2`,
		"same axis":      `"appearance:light": 2, "appearance:dark": 3`,
		"settled":        `"app:slides": 2, "appearance:dark": 3, "app:slides+appearance:dark": 4`,
		"shared differs": `"app:slides+appearance:light": 2, "app:wiki+appearance:dark": 3`,
	} {
		s := mustParse(t, map[string]string{"a.tokens.json": `{ "x": { "$type": "number", "$value": 1,
  "$extensions": { "com.c65llc.airframe": { "modes": {` + modes + `} } } } }`})
		assert.Empty(t, s.Check(), name)
	}
}

// Each ambiguous (token, keyA, keyB) is reported once, in a stable order.
func TestModeAmbiguousOnce(t *testing.T) {
	files := map[string]string{"a.tokens.json": `{
  "y": { "$type": "number", "$value": 1,
    "$extensions": { "com.c65llc.airframe": { "modes": { "app:slides": 2, "density:compact": 3 } } } },
  "x": { "$type": "number", "$value": 1,
    "$extensions": { "com.c65llc.airframe": { "modes": { "app:slides": 2, "appearance:dark": 3 } } } }
}`}
	ds := mustParse(t, files).Check()
	assert.Equal(t, []string{"mode-ambiguous", "mode-ambiguous"}, rules(ds))
	assert.Contains(t, ds[0].Message, `"y"`)
	assert.Contains(t, ds[1].Message, `"x"`)
	for range 10 {
		assert.Equal(t, ds, mustParse(t, files).Check())
	}
}
