// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package rename

import (
	"errors"
	"io/fs"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/ojson"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

const fixtureConfig = `version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ":root[data-theme='dark']"}}
  density: {values: [compact, standard], base: standard, binding: runtime, web: {attr: data-density}}
platforms:
  web:
    emit: [css]
    wrappers:
      - root: theme.space
        map: {small: space-2, wide: gap}
        patterns: [{match: '^space(\d+)$', token: 'space-{1}'}]
      - root: theme.color
        patterns: [{match: '^(\w+)Tone$', token: 'color-{1}'}]
  apple:
    emit: [swift, colorsets]
    options: {accentColor: color-accent}
  android:
    emit: [kotlin]
    options:
      package: com.example.design
      material3Scheme: {primary: color-accent, surface: color-surface}
  windows:
    emit: [xaml]
    options:
      highContrast:
        color-ink: SystemColorWindowTextColor
        color-surface: SystemColorWindowColor
`

const fixtureBase = `{
  "color": {
    "$type": "color",
    "surface": {
      "$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]},
      "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}
    },
    "ink": {
      "$value": {"colorSpace": "oklch", "components": [0.2, 0.01, 250]},
      "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}
    },
    "accent": {
      "$value": "{color.ink}",
      "$extensions": {"com.c65llc.airframe": {"tier": "semantic", "modes": {"density:compact": "{color.surface}"}}}
    }
  },
  "space": {
    "$type": "dimension",
    "2": {"$value": {"value": 2, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
    "4": {"$value": {"value": 4, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
  },
  "gap": {
    "$type": "dimension",
    "$value": "{space.4}",
    "$extensions": {"com.c65llc.airframe": {"tier": "semantic", "density": {"compact": "{space.2}"}}}
  },
  "opacity-muted": {
    "$type": "number",
    "$value": 0.6,
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}
  },
  "text": {
    "muted": {
      "$type": "color",
      "$value": "{color.ink}",
      "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}
    }
  }
}
`

const fixtureComponent = `{
  "button": {
    "bg": {
      "$type": "color",
      "$value": "{color.accent}",
      "$extensions": {"com.c65llc.airframe": {"tier": "component", "component": "button"}}
    }
  }
}
`

const fixtureDark = `{
  "$extensions": {"com.c65llc.airframe": {"mode": "appearance:dark"}},
  "color": {
    "surface": {"$value": {"colorSpace": "oklch", "components": [0.2, 0.01, 250]}},
    "ink": {"$value": "{color.surface}"}
  }
}
`

const fixtureButton = `id: button
tokens: [button-bg]
owners:
  - web/src/button/**
`

const fixtureInvariants = `- id: ink-on-surface
  kind: contrast
  fg: color-ink
  bg: color-surface
  min: 4.5
- id: accent-hue
  kind: same-hue-across-appearances
  tokens: [color-accent, color-ink]
- id: reserved
  kind: hue-reserved
  hue: 250
  tolerance: 20
  only: ["color-*", "text-muted"]
- id: touch
  kind: min-dimension
  match: "ga*"
  min: 1
`

func fixture() project.MemFS {
	return project.MemFS{
		"airframe.yaml":                              fixtureConfig,
		"design-system/tokens/base.tokens.json":      fixtureBase,
		"design-system/tokens/component.tokens.json": fixtureComponent,
		"design-system/themes/dark.tokens.json":      fixtureDark,
		"design-system/components/button.yaml":       fixtureButton,
		"design-system/invariants.yaml":              fixtureInvariants,
	}
}

func load(t *testing.T, fsys fs.FS) *project.Project {
	t.Helper()
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	return p
}

func plan(t *testing.T, fsys fs.FS, old, new, path string) *EditPlan {
	t.Helper()
	pl, err := Plan(load(t, fsys), old, new, path)
	require.NoError(t, err)
	return pl
}

func TestFixtureLoads(t *testing.T) { load(t, fixture()) }

func TestPlanTokenMove(t *testing.T) {
	pl := plan(t, fixture(), "color-surface", "color-canvas", "")
	assert.Equal(t, []Move{
		{File: "design-system/themes/dark.tokens.json", Line: 4, From: "color.surface", To: "color.canvas"},
		{File: "design-system/tokens/base.tokens.json", Line: 4, From: "color.surface", To: "color.canvas"},
	}, pl.Moves)

	after := pl.After
	require.NotNil(t, after)
	assert.NotContains(t, after.Tokens.Tokens, "color-surface")
	canvas := after.Tokens.Tokens["color-canvas"]
	require.NotNil(t, canvas)
	assert.Equal(t, []string{"color", "canvas"}, canvas.Path)
	assert.Equal(t, "semantic", canvas.Tier)
	assert.Len(t, canvas.Modes, 1, "the dark override moved with it")

	// A same-parent move keeps the token's place in its group.
	base := string(pl.Files["design-system/tokens/base.tokens.json"])
	assert.Less(t, strings.Index(base, `"canvas"`), strings.Index(base, `"ink"`))
	assert.False(t, pl.Moves[0].Reordered)

	// Every file in the plan is already formatted.
	for f, b := range pl.Files {
		want, err := format.File(f, b)
		require.NoError(t, err)
		assert.Equal(t, string(want), string(b), f)
	}

	// A move to a new group creates it, takes the inherited $type along,
	// and deletes the group the move empties.
	pl = plan(t, fixture(), "text-muted", "ink-soft", "")
	after = pl.After
	require.Contains(t, after.Tokens.Tokens, "ink-soft")
	assert.Equal(t, []string{"ink", "soft"}, after.Tokens.Tokens["ink-soft"].Path)
	base = string(pl.Files["design-system/tokens/base.tokens.json"])
	assert.NotContains(t, base, `"text"`, "the emptied group is gone")

	assert.False(t, pl.Moves[0].Reordered, "text-muted was last and stays last")

	// The order is declaration order, dark.tokens.json's first: color-surface
	// moving to the end of it puts it after color-ink.
	pl = plan(t, fixture(), "color-surface", "canvas", "")
	require.Len(t, pl.Moves, 2)
	for _, m := range pl.Moves {
		assert.True(t, m.Reordered, "%+v", m)
	}
	assert.Equal(t, []string{"color-ink", "canvas"}, pl.After.Tokens.Order[:2])
	// color-ink moves to the end of both files too, but stays second.
	pl = plan(t, fixture(), "color-ink", "ink", "")
	for _, m := range pl.Moves {
		assert.False(t, m.Reordered, "%+v", m)
	}
	ink := pl.After.Tokens.Tokens["ink"]
	require.NotNil(t, ink)
	assert.Equal(t, "color", ink.Type, "the group's $type is written onto the moved token")
	assert.Len(t, ink.Modes, 1)
}

func TestPlanAliases(t *testing.T) {
	pl := plan(t, fixture(), "color-ink", "color-text", "")
	var got []Edit
	for _, e := range pl.Edits {
		if strings.HasSuffix(e.File, ".json") {
			got = append(got, e)
		}
	}
	// base: accent's $value and text.muted's $value; dark: nothing aliases
	// color.ink there.
	assert.Equal(t, []Edit{
		{File: "design-system/tokens/base.tokens.json", Line: 13, Col: 18, Old: "{color.ink}", New: "{color.text}"},
		{File: "design-system/tokens/base.tokens.json", Line: 35, Col: 18, Old: "{color.ink}", New: "{color.text}"},
	}, got)
	after := pl.After.Tokens
	assert.Equal(t, "color-text", after.Tokens["color-accent"].Default.Alias)
	assert.Equal(t, "color-text", after.Tokens["text-muted"].Default.Alias)

	// Mode values and the density shorthand are aliases too.
	pl = plan(t, fixture(), "space-2", "space-half", "")
	assert.Equal(t, "space-half", modeAlias(t, pl.After.Tokens.Tokens["gap"]))
	pl = plan(t, fixture(), "color-surface", "color-canvas", "")
	assert.Equal(t, "color-canvas", modeAlias(t, pl.After.Tokens.Tokens["color-accent"]))
	assert.Equal(t, "color-canvas", modeAlias(t, pl.After.Tokens.Tokens["color-ink"]), "an alias in a mode file")
}

// modeAlias is the alias of tok's one mode value.
func modeAlias(t *testing.T, tok *tokens.Token) string {
	t.Helper()
	require.NotNil(t, tok)
	require.Len(t, tok.Modes, 1)
	for _, v := range tok.Modes {
		return v.Alias
	}
	return ""
}

func TestPlanConfigRefs(t *testing.T) {
	pl := plan(t, fixture(), "color-accent", "color-brand", "")
	cfg := string(pl.Files["airframe.yaml"])
	assert.Contains(t, cfg, "accentColor: color-brand")
	assert.Contains(t, cfg, "primary: color-brand")
	assert.NotContains(t, cfg, "color-accent")

	pl = plan(t, fixture(), "color-surface", "color-canvas", "")
	cfg = string(pl.Files["airframe.yaml"])
	assert.Contains(t, cfg, "surface: color-canvas", "a material3Scheme role value")
	assert.Contains(t, cfg, "color-canvas: SystemColorWindowColor", "a highContrast key")
	assert.Equal(t, "color-canvas", pl.After.Config.Platforms[3].HighContrast()[0][0])

	pl = plan(t, fixture(), "gap", "gutter", "")
	cfg = string(pl.Files["airframe.yaml"])
	assert.Contains(t, cfg, "wide: gutter", "a wrapper map value")
	for _, m := range pl.Manual {
		assert.NotEqual(t, "airframe.yaml", m.File, "no pattern makes gap: %s", m.Message)
	}

	var cfgEdits []Edit
	for _, e := range pl.Edits {
		if e.File == "airframe.yaml" {
			cfgEdits = append(cfgEdits, e)
		}
	}
	assert.Equal(t, []Edit{{File: "airframe.yaml", Line: 11, Col: 37, Old: "gap", New: "gutter"}}, cfgEdits)

	// A pattern that could resolve a member to old is the user's to check.
	pl = plan(t, fixture(), "space-4", "space-wide", "")
	require.Len(t, pl.Manual, 1)
	assert.Equal(t, "airframe.yaml", pl.Manual[0].File)
	assert.Equal(t, 12, pl.Manual[0].Line)
	assert.Contains(t, pl.Manual[0].Message, "theme.space")
	assert.Contains(t, pl.Manual[0].Message, "space-4")

	// color-{1} with (\w+) can make color-ink as well, and color-text: a
	// member that read no token would now read the renamed one.
	pl = plan(t, fixture(), "color-ink", "color-text", "")
	require.Len(t, pl.Manual, 2)
	assert.Contains(t, pl.Manual[0].Message, "theme.color pattern ^(\\w+)Tone$ → color-{1} can resolve a member to color-ink")
	assert.Contains(t, pl.Manual[1].Message, "can resolve a member to color-text, which the rename creates")

	// A pattern that can make new but not old is flagged too.
	pl = plan(t, fixture(), "gap", "space-9", "")
	require.Len(t, pl.Manual, 2, "the pattern, and the touch invariant's ga* glob")
	assert.Contains(t, pl.Manual[0].Message, "theme.space pattern ^space(\\d+)$ → space-{1} can resolve a member to space-9, which the rename creates")
}

func TestPlanComponents(t *testing.T) {
	pl := plan(t, fixture(), "button-bg", "button-fill", "")
	assert.Equal(t, "id: button\ntokens: [button-fill]\nowners:\n  - web/src/button/**\n",
		string(pl.Files["design-system/components/button.yaml"]))
	assert.Equal(t, []string{"button-fill"}, pl.After.Components["button"].Tokens)
	assert.Contains(t, pl.Edits, Edit{File: "design-system/components/button.yaml", Line: 2, Col: 10, Old: "button-bg", New: "button-fill"})
}

func TestRenameInvariantGlob(t *testing.T) {
	// Exact names are rewritten.
	pl := plan(t, fixture(), "color-ink", "color-text", "")
	inv := string(pl.Files["design-system/invariants.yaml"])
	assert.Contains(t, inv, "fg: color-text")
	assert.Contains(t, inv, "tokens: [color-accent, color-text]")
	for _, m := range pl.Manual {
		assert.NotEqual(t, "design-system/invariants.yaml", m.File, "color-* still matches: %s", m.Message)
	}

	// A glob that matched old and not new is a manual item, as is one
	// that matches new and not old.
	pl = plan(t, fixture(), "color-surface", "surface", "")
	assert.Contains(t, string(pl.Files["design-system/invariants.yaml"]), "bg: surface")
	require.Len(t, invManual(pl), 1)
	assert.Equal(t, 13, invManual(pl)[0].Line)
	assert.Contains(t, invManual(pl)[0].Message, `"color-*"`)

	pl = plan(t, fixture(), "opacity-muted", "gamma", "gamma")
	require.Len(t, invManual(pl), 1, "ga* now matches")
	assert.Contains(t, invManual(pl)[0].Message, `"ga*"`)
	assert.Equal(t, 16, invManual(pl)[0].Line)

	// An exact only entry is rewritten, not reported.
	pl = plan(t, fixture(), "text-muted", "text-soft", "")
	assert.Contains(t, string(pl.Files["design-system/invariants.yaml"]), `"text-soft"`)
	assert.Contains(t, pl.Edits, Edit{File: "design-system/invariants.yaml", Line: 13, Col: 22, Old: "text-muted", New: "text-soft"}, "a quoted scalar's column is past its quote")
	assert.Empty(t, invManual(pl))
}

func invManual(pl *EditPlan) []Manual {
	var out []Manual
	for _, m := range pl.Manual {
		if m.File == "design-system/invariants.yaml" {
			out = append(out, m)
		}
	}
	return out
}

func TestRenameCollision(t *testing.T) {
	p := load(t, fixture())
	for _, tc := range []struct{ old, new, path, want string }{
		{"color-ink", "color-surface", "", "already exists"},
		{"color-ink", "text", "", "group"},  // text.muted makes text a group
		{"color-ink", "color", "", "group"}, // color is the old token's own group, with others in it
		{"gap", "text-muted-x", "", "is a token"},
		{"nope", "x", "", "no token"},
		{"gap", "Gap", "", "not a valid token name"},
		{"gap", "gap", "", "same"},
	} {
		_, err := Plan(p, tc.old, tc.new, tc.path)
		require.Error(t, err, "%s → %s", tc.old, tc.new)
		assert.ErrorContains(t, err, tc.want, "%s → %s", tc.old, tc.new)
		var r *Refusal
		assert.True(t, errors.As(err, &r), "%s → %s is a refusal", tc.old, tc.new)
	}

	// The old token's own group is no collision: color-ink → color-ink-x
	// would sit under it, and gap → gap-x replaces gap with a group.
	pl := plan(t, fixture(), "gap", "gap-x", "")
	assert.Equal(t, []string{"gap", "x"}, pl.After.Tokens.Tokens["gap-x"].Path)
}

func TestPlanNeedsPath(t *testing.T) {
	p := load(t, fixture())
	_, err := Plan(p, "opacity-muted", "opacity-faint", "")
	assert.ErrorContains(t, err, "--path")

	pl, err := Plan(p, "opacity-muted", "opacity-faint", "opacity-faint")
	require.NoError(t, err)
	assert.Equal(t, []string{"opacity-faint"}, pl.After.Tokens.Tokens["opacity-faint"].Path)

	pl, err = Plan(p, "opacity-muted", "opacity-faint", "opacity.faint")
	require.NoError(t, err)
	assert.Equal(t, []string{"opacity", "faint"}, pl.After.Tokens.Tokens["opacity-faint"].Path)

	_, err = Plan(p, "gap", "gutter", "space.gutter")
	assert.ErrorContains(t, err, "space-gutter")
	_, err = Plan(p, "gap", "gutter", "a..b")
	assert.Error(t, err)
}

// The planner writes nothing: the project's FS is as it was.
func TestPlanWritesNothing(t *testing.T) {
	fsys := fixture()
	before := maps.Clone(fsys)
	plan(t, fsys, "color-surface", "color-canvas", "")
	assert.Equal(t, before, fsys)
}

func TestOverlay(t *testing.T) {
	base := project.MemFS{"a/x.txt": "x", "a/y.txt": "y", "b.txt": "b"}
	o := Overlay(base, map[string][]byte{"a/y.txt": []byte("Y"), "a/z.txt": []byte("z"), "c/d.txt": []byte("d")})
	b, err := fs.ReadFile(o, "a/y.txt")
	require.NoError(t, err)
	assert.Equal(t, "Y", string(b))
	b, err = fs.ReadFile(o, "a/x.txt")
	require.NoError(t, err)
	assert.Equal(t, "x", string(b))
	ents, err := fs.ReadDir(o, "a")
	require.NoError(t, err)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{"x.txt", "y.txt", "z.txt"}, names)
	ents, err = fs.ReadDir(o, ".")
	require.NoError(t, err)
	names = nil
	for _, e := range ents {
		names = append(names, e.Name()+map[bool]string{true: "/"}[e.IsDir()])
	}
	assert.Equal(t, []string{"a/", "b.txt", "c/"}, names)
	st, err := fs.Stat(o, "a/y.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(1), st.Size())
	_, err = fs.Stat(o, "nope")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// A fixed-naming format's names entry is its default spelling of the
// token's name, so it follows the rename; a custom native spelling stays.
func TestPlanNames(t *testing.T) {
	fsys := fixture()
	fsys["design-system/tokens/base.tokens.json"] = strings.Replace(fixtureBase,
		`"$extensions": {"com.c65llc.airframe": {"tier": "semantic", "modes": {"density:compact": "{color.surface}"}}}`,
		`"$extensions": {"com.c65llc.airframe": {"tier": "semantic", "modes": {"density:compact": "{color.surface}"}, "names": {"css": "--color-accent", "colorsets": "ColorAccent", "swift": "brandAccent"}}}`, 1)
	pl := plan(t, fsys, "color-accent", "color-brand", "")
	brand := pl.After.Tokens.Tokens["color-brand"]
	require.NotNil(t, brand)
	assert.Equal(t, map[string]string{"css": "--color-brand", "colorsets": "ColorBrand", "swift": "brandAccent"}, brand.Names)
	var got []string
	for _, e := range pl.Edits {
		if strings.Contains(e.Old, "Accent") || strings.HasPrefix(e.Old, "--") {
			got = append(got, e.Old+" → "+e.New)
		}
	}
	assert.Equal(t, []string{"--color-accent → --color-brand", "ColorAccent → ColorBrand"}, got)
	for _, e := range pl.Edits {
		assert.NotZero(t, e.Col, "%+v", e)
	}
}

// A fixed-naming entry that is not old's default spelling is refused
// before anything changes (the loader rejects one, so call names directly).
func TestPlanNamesRefused(t *testing.T) {
	root, err := ojson.Parse([]byte(`{"gap": {"$value": 1, "$extensions": {"com.c65llc.airframe": {"names": {"css": "--other"}}}}}`))
	require.NoError(t, err)
	r := &renamer{pl: &EditPlan{Old: "gap", New: "spacing"}}
	err = r.names("x.tokens.json", root, nil)
	var ref *Refusal
	require.ErrorAs(t, err, &ref)
	assert.Equal(t, `rename: x.tokens.json:1: gap's names.css is "--other", not "--gap", its default css spelling, which the rename would rewrite; fix it first`, ref.Msg)
}
