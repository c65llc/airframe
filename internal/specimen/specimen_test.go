// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package specimen

import (
	"bytes"
	"encoding/xml"
	"flag"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

var update = flag.Bool("update", false, "rewrite the SVG goldens under testdata")

func loadApple(t *testing.T) *project.Project {
	t.Helper()
	p, ds, err := project.Load(os.DirFS("../../testdata/apple"))
	require.NoError(t, err)
	require.NotNil(t, p, "%v", ds)
	return p
}

// memProject loads an in-memory project: cfg is airframe.yaml, toks the
// one token file.
func memProject(t *testing.T, cfg, toks string) *project.Project {
	t.Helper()
	p, ds, err := project.Load(project.MemFS{
		"airframe.yaml":               cfg,
		"design-system/a.tokens.json": toks,
	})
	require.NoError(t, err)
	require.NotNil(t, p, "%v", ds)
	return p
}

func TestSpecimenGolden(t *testing.T) {
	sheets, err := Render(loadApple(t))
	require.NoError(t, err)
	// The apple fixture fans out over app: notes and tasks.
	require.Equal(t, []string{
		"colors-notes.svg", "colors-tasks.svg", "spacing-notes.svg", "spacing-tasks.svg", "type-notes.svg", "type-tasks.svg",
	}, slices.Sorted(maps.Keys(sheets)))
	for name, got := range sheets {
		path := filepath.Join("testdata", name)
		if *update {
			require.NoError(t, os.MkdirAll("testdata", 0o755))
			require.NoError(t, os.WriteFile(path, got, 0o644))
			continue
		}
		want, err := os.ReadFile(path)
		require.NoError(t, err, "run go test ./internal/specimen -update")
		assert.Equal(t, string(want), string(got), name)
	}
}

const shapesCfg = `version: 1
content: design-system
modes:
  appearance: {values: [light, dark, contrast], binding: runtime, web: {light: ":root", dark: "[data-theme=dark]", contrast: "[data-theme=contrast]"}}
platforms:
  web: {emit: css}
`

const noAppearanceCfg = `version: 1
content: design-system
modes:
  density: {values: [compact, standard], base: standard, binding: runtime, web: {attr: data-density}}
platforms:
  web: {emit: css}
`

const shapesToks = `{
  "bg": {"$type": "color", "$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]},
    "$description": "Page <background> & more",
    "$extensions": {"com.c65llc.airframe": {"tier": "semantic", "modes": {
      "appearance:dark": {"colorSpace": "oklch", "components": [0.2, 0.01, 250]},
      "appearance:contrast": {"colorSpace": "oklch", "components": [1, 0, 0]}}}}},
  "neon": {"$type": "color", "$value": {"colorSpace": "oklch", "components": [0.75, 0.4, 150]},
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive", "retired": true}}},
  "font-body": {"$type": "fontFamily", "$value": ["Inter", "Helvetica Neue"],
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "weight-bold": {"$type": "fontWeight", "$value": 700,
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "font-size-lg": {"$type": "dimension", "$value": {"value": 1.5, "unit": "rem"},
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "gap": {"$type": "dimension", "$value": {"value": 4, "unit": "rem"},
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "wide": {"$type": "dimension", "$value": {"value": 2000, "unit": "px"},
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
}`

func TestSpecimenShapes(t *testing.T) {
	// A third appearance: three swatch columns, and the out-of-gamut colour
	// is clamped into a valid hex.
	sheets, err := Render(memProject(t, shapesCfg, shapesToks))
	require.NoError(t, err)
	require.Equal(t, []string{"colors.svg", "spacing.svg", "type.svg"}, slices.Sorted(maps.Keys(sheets)))
	colors := string(sheets["colors.svg"])
	assert.Equal(t, 3+3, strings.Count(colors, "<rect x=")) // 2 tokens x 3 appearances
	assert.Regexp(t, `fill="#[0-9a-f]{6}"`, colors)
	assert.NotContains(t, colors, "NaN")
	assert.Contains(t, colors, "neon (retired)")
	for _, a := range []string{">light<", ">dark<", ">contrast<"} {
		assert.Contains(t, colors, a)
	}
	// The bar is capped at 480 drawn; the label keeps the real value.
	spacing := string(sheets["spacing.svg"])
	assert.Contains(t, spacing, `width="480"`)
	assert.Contains(t, spacing, "2000px")
	assert.Contains(t, spacing, "4rem = 64px")
	assert.NotContains(t, spacing, "font-size-lg")
	typ := string(sheets["type.svg"])
	assert.Contains(t, typ, "font-size-lg")
	assert.Contains(t, typ, `font-size="24"`)
	assert.Contains(t, typ, "&apos;Inter&apos;, &apos;Helvetica Neue&apos;, sans-serif")
	assert.Contains(t, typ, `font-weight="700"`)

	// No appearance axis: a single swatch column, no column header.
	sheets, err = Render(memProject(t, noAppearanceCfg, shapesToks))
	require.NoError(t, err)
	colors = string(sheets["colors.svg"])
	assert.Equal(t, 2, strings.Count(colors, "<rect x="))
	assert.NotContains(t, colors, ">light<")

	// Fan-out: one set per value, named by it.
	sheets, err = Render(loadApple(t))
	require.NoError(t, err)
	assert.Contains(t, sheets, "colors-notes.svg")
	assert.Contains(t, sheets, "colors-tasks.svg")
}

func TestSpecimenOutOfGamutClamped(t *testing.T) {
	h := hex(color.OKLCH{L: 0.75, C: 0.4, H: 150, Alpha: 1})
	assert.Regexp(t, `^#[0-9a-f]{6}$`, h)
	assert.Equal(t, "#00d451", h)
}

func TestSpecimenDeterministic(t *testing.T) {
	a, err := Render(loadApple(t))
	require.NoError(t, err)
	for range 3 {
		b, err := Render(loadApple(t))
		require.NoError(t, err)
		require.Equal(t, a, b)
	}
}

func TestSpecimenEscaping(t *testing.T) {
	// A hand-built project: the loader would refuse such a name.
	nasty := `a<&>"b'`
	set := &tokens.Set{
		Tokens: map[string]*tokens.Token{},
	}
	add := func(name, typ string, lit any) {
		set.Order = append(set.Order, name)
		set.Tokens[name] = &tokens.Token{Name: name, Type: typ, Tier: "semantic", Description: "d <&> \"q\" 'x'",
			Default: &tokens.Value{Lit: lit}}
	}
	add(nasty+"-c", "color", nil)
	add(nasty+"-d", "dimension", tokens.Dim{V: 4, Unit: "px"})
	add(nasty+"-f", "fontFamily", []string{nasty})
	p := &project.Project{Config: &config.Config{Content: nasty}, Tokens: set}
	sheets, err := Render(p)
	require.NoError(t, err)
	for name, b := range sheets {
		s := string(b)
		assert.NotContains(t, s, "a<&>", name)
		assert.Contains(t, s, "a&lt;&amp;&gt;", name)
		assertWellFormed(t, name, b)
	}
}

func assertWellFormed(t *testing.T, name string, b []byte) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	root := ""
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, name)
		if se, ok := tok.(xml.StartElement); ok && root == "" {
			root = se.Name.Local
		}
	}
	assert.Equal(t, "svg", root, name)
}

func TestSpecimenWellFormed(t *testing.T) {
	for _, p := range []*project.Project{
		loadApple(t),
		memProject(t, shapesCfg, shapesToks),
		memProject(t, noAppearanceCfg, shapesToks),
	} {
		sheets, err := Render(p)
		require.NoError(t, err)
		for name, b := range sheets {
			assertWellFormed(t, name, b)
		}
	}
}
