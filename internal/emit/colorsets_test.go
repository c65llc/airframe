// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"encoding/json"
	"fmt"
	"io/fs"
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
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// appleDir is the apple fixture: appearance, density, app (fanned out over
// notes and tasks) and channel (build-bound, fixed).
var appleDir = filepath.Join("..", "..", "testdata", "apple")

const (
	notesCatalog = "ios/notes/Assets.xcassets/"
	tasksCatalog = "ios/tasks/Assets.xcassets/"
)

func applePlatform(t *testing.T, p *project.Project) config.Platform {
	t.Helper()
	for _, pl := range p.Config.Platforms {
		if pl.Name == "apple" {
			return pl
		}
	}
	t.Fatal("no apple platform")
	return config.Platform{}
}

func colorsetsOf(t *testing.T, p *project.Project) map[string][]byte {
	t.Helper()
	out, err := Colorsets(p, applePlatform(t, p))
	require.NoError(t, err)
	return out
}

// colorsetDoc is the parsed shape of a colorset's Contents.json.
type colorsetDoc struct {
	Colors []struct {
		Appearances []struct{ Appearance, Value string } `json:"appearances"`
		Color       struct {
			Space      string            `json:"color-space"`
			Components map[string]string `json:"components"`
		} `json:"color"`
		Idiom string `json:"idiom"`
	} `json:"colors"`
	Info struct {
		Author  string `json:"author"`
		Version int    `json:"version"`
	} `json:"info"`
}

func colorsetAt(t *testing.T, out map[string][]byte, path string) colorsetDoc {
	t.Helper()
	b, ok := out[path]
	require.True(t, ok, "no %s among %v", path, slices.Sorted(maps.Keys(out)))
	var d colorsetDoc
	require.NoError(t, json.Unmarshal(b, &d))
	return d
}

func set(catalog, name string) string {
	return catalog + "Tokens/" + name + ".colorset/Contents.json"
}

// TestColorsetsGolden: the apple fixture's colorsets equal the committed
// goldens, file for file. -update rewrites them.
func TestColorsetsGolden(t *testing.T) {
	out := colorsetsOf(t, loadFS(t, os.DirFS(appleDir)))
	if *update {
		require.NoError(t, os.RemoveAll(filepath.Join(appleDir, "ios", "notes")))
		require.NoError(t, os.RemoveAll(filepath.Join(appleDir, "ios", "tasks")))
		for path, b := range out {
			full := filepath.Join(appleDir, filepath.FromSlash(path))
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, b, 0o644))
		}
	}
	golden := map[string][]byte{}
	require.NoError(t, fs.WalkDir(os.DirFS(appleDir), "ios", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(path, ".xcassets/") {
			return err
		}
		b, err := os.ReadFile(filepath.Join(appleDir, filepath.FromSlash(path)))
		golden[path] = b
		return err
	}))
	require.Equal(t, slices.Sorted(maps.Keys(golden)), slices.Sorted(maps.Keys(out)), "run go test ./internal/emit -update")
	for path, b := range out {
		assert.Equal(t, string(golden[path]), string(b), path)
	}
}

// TestColorsetShape: a colour varying on appearance has a universal entry
// and a luminosity-dark one, in Xcode's asset-catalog format.
func TestColorsetShape(t *testing.T) {
	out := colorsetsOf(t, loadFS(t, os.DirFS(appleDir)))
	d := colorsetAt(t, out, set(notesCatalog, "Ink"))
	require.Len(t, d.Colors, 2)
	assert.Empty(t, d.Colors[0].Appearances)
	assert.Equal(t, "universal", d.Colors[0].Idiom)
	assert.Equal(t, "display-p3", d.Colors[0].Color.Space)
	assert.Equal(t, []string{"alpha", "blue", "green", "red"}, slices.Sorted(maps.Keys(d.Colors[0].Color.Components)))
	assert.Equal(t, "1.0000", d.Colors[0].Color.Components["alpha"])
	require.Len(t, d.Colors[1].Appearances, 1)
	assert.Equal(t, "luminosity", d.Colors[1].Appearances[0].Appearance)
	assert.Equal(t, "dark", d.Colors[1].Appearances[0].Value)
	assert.NotEqual(t, d.Colors[0].Color.Components, d.Colors[1].Color.Components)
	assert.Equal(t, "airframe", d.Info.Author)
	assert.Equal(t, 1, d.Info.Version)

	assert.Len(t, colorsetAt(t, out, set(notesCatalog, "Highlight")).Colors, 1, "no dark entry when the colour does not vary on appearance")
}

func TestColorsetNamespaceFolder(t *testing.T) {
	out := colorsetsOf(t, loadFS(t, os.DirFS(appleDir)))
	for _, catalog := range []string{notesCatalog, tasksCatalog} {
		b := out[catalog+"Tokens/Contents.json"]
		require.NotNil(t, b, catalog)
		var d struct {
			Properties map[string]bool `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(b, &d))
		assert.True(t, d.Properties["provides-namespace"])
		assert.Equal(t, `{
  "info": {
    "author": "airframe",
    "version": 1
  },
  "properties": {
    "provides-namespace": true
  }
}
`, string(b))
	}
}

func TestColorsetPerRuntimeValue(t *testing.T) {
	p := loadFS(t, os.DirFS(appleDir))
	out := colorsetsOf(t, p)
	for _, v := range []string{"Compact", "Standard", "Relaxed"} {
		d := colorsetAt(t, out, set(notesCatalog, "Surface-"+v))
		assert.Len(t, d.Colors, 2, "surface varies on appearance too")
	}
	assert.NotContains(t, out, set(notesCatalog, "Surface"))
	assert.NotEqual(t, out[set(notesCatalog, "Surface-Compact")], out[set(notesCatalog, "Surface-Standard")])

	np := PlanNative(p.Config, applePlatform(t, p))
	assert.Equal(t, "Tokens/Surface-Compact", ColorsetName(np, p.Tokens.Tokens["surface"], config.Context{"density": "compact"}))
	assert.Equal(t, "Tokens/Ink", ColorsetName(np, p.Tokens.Tokens["ink"], config.Context{}))
	assert.Equal(t, "Tokens/ButtonBg", ColorsetName(np, p.Tokens.Tokens["button-bg"], config.Context{"app": "tasks", "appearance": "dark"}),
		"fan-out and appearance never name a colorset")
}

// TestFanOutThroughAlias (Review Focus 3): button-bg aliases accent, which
// varies per app; each app's catalog has its own ButtonBg.
func TestFanOutThroughAlias(t *testing.T) {
	out := colorsetsOf(t, loadFS(t, os.DirFS(appleDir)))
	notes, tasks := out[set(notesCatalog, "ButtonBg")], out[set(tasksCatalog, "ButtonBg")]
	require.NotNil(t, notes)
	require.NotNil(t, tasks)
	assert.NotEqual(t, string(notes), string(tasks))
	assert.Equal(t, string(out[set(notesCatalog, "Accent")]), string(notes))
	assert.Equal(t, string(out[set(tasksCatalog, "Accent")]), string(tasks))
	assert.Equal(t, string(out[set(notesCatalog, "Ink")]), string(out[set(tasksCatalog, "Ink")]), "a colour that does not vary per app is the same in both")

	// Swift half: the shared file reads the colorset each app's catalog
	// holds, so one static let serves both.
	assert.Contains(t, appleSwift(t), "    public static let buttonBg = Color(\"Tokens/ButtonBg\")\n")
}

func p3Components(c color.OKLCH, g color.Gamut) map[string]string {
	r, gr, b := c.ToP3(g)
	return map[string]string{"red": fmt.Sprintf("%.4f", r), "green": fmt.Sprintf("%.4f", gr), "blue": fmt.Sprintf("%.4f", b), "alpha": "1.0000"}
}

// TestColorsetGamut (Review Focus 4): an OKLCH beyond Display P3 is mapped
// by CSS Color 4 by default and clipped under gamut: clip-p3; either way
// every component is within [0,1].
func TestColorsetGamut(t *testing.T) {
	vivid := color.OKLCH{L: 0.7, C: 0.4, H: 145, Alpha: 1}
	within := func(d colorsetDoc) {
		for k, v := range d.Colors[0].Color.Components {
			var f float64
			_, err := fmt.Sscanf(v, "%g", &f)
			require.NoError(t, err)
			assert.True(t, f >= 0 && f <= 1, "%s = %s", k, v)
		}
	}

	mapped := colorsetAt(t, colorsetsOf(t, loadFS(t, os.DirFS(appleDir))), set(notesCatalog, "Highlight"))
	within(mapped)
	assert.Equal(t, p3Components(vivid, color.CSSColor4), mapped.Colors[0].Color.Components)

	fsys := memFS(t, appleDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "options: {accentColor: accent}", "options: {accentColor: accent, gamut: clip-p3}", 1)
	clipped := colorsetAt(t, colorsetsOf(t, loadFS(t, fsys)), set(notesCatalog, "Highlight"))
	within(clipped)
	assert.Equal(t, p3Components(vivid, color.Clip), clipped.Colors[0].Color.Components)
	assert.NotEqual(t, mapped.Colors[0].Color.Components, clipped.Colors[0].Color.Components)

	// Independent of ToP3: the values the gamut mapping must produce.
	assert.Equal(t, map[string]string{"red": "0.0000", "green": "0.7804", "blue": "0.0000", "alpha": "1.0000"}, mapped.Colors[0].Color.Components)
	assert.Equal(t, map[string]string{"red": "0.0000", "green": "0.8064", "blue": "0.0000", "alpha": "1.0000"}, clipped.Colors[0].Color.Components)
}

// TestColorsetUnsupportedAppearance (Review Focus 1): an appearance value
// beyond light and dark is emit-failed naming it and the colour, and the
// platform writes no colorsets.
func TestColorsetUnsupportedAppearance(t *testing.T) {
	fsys := memFS(t, appleDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"],
		`appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ["@media (prefers-color-scheme: dark) :root:not([data-theme='light'])", ":root[data-theme='dark']"]}}`,
		`appearance: {values: [light, dark, high-contrast], base: light, binding: runtime, web: {attr: data-theme}}`, 1)
	out, ds := Build(loadFS(t, fsys), []string{"colorsets"})
	ds = slices.DeleteFunc(ds, func(d diag.Diagnostic) bool { return d.Rule != "emit-failed" })
	require.Len(t, ds, 1, "%v", ds)
	assert.Contains(t, ds[0].Message, "high-contrast")
	assert.Contains(t, ds[0].Message, `"button-ink"`, "the first colour, by name, that varies on appearance")
	assert.Equal(t, "platforms.apple", ds[0].Path)
	assert.Empty(t, out)
}

func TestAccentColorAtCatalogRoot(t *testing.T) {
	out := colorsetsOf(t, loadFS(t, os.DirFS(appleDir)))
	for _, catalog := range []string{notesCatalog, tasksCatalog} {
		b := out[catalog+"AccentColor.colorset/Contents.json"]
		require.NotNil(t, b, catalog)
		assert.Equal(t, string(out[set(catalog, "Accent")]), string(b))
	}
	assert.NotEqual(t, out[notesCatalog+"AccentColor.colorset/Contents.json"], out[tasksCatalog+"AccentColor.colorset/Contents.json"])

	fsys := memFS(t, appleDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "    options: {accentColor: accent}\n", "", 1)
	for path := range colorsetsOf(t, loadFS(t, fsys)) {
		assert.NotContains(t, path, "AccentColor", "no accentColor option, no AccentColor")
	}
}

func TestAccentColorUnknownToken(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"nope", `"nope"`},
		{"gap", "not a colour"},
		{"surface", "density"},
	} {
		fsys := memFS(t, appleDir)
		fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "accentColor: accent", "accentColor: "+tc.name, 1)
		out, ds := Build(loadFS(t, fsys), []string{"colorsets"})
		ds = slices.DeleteFunc(ds, func(d diag.Diagnostic) bool { return d.Rule != "emit-failed" })
		require.Len(t, ds, 1, tc.name)
		assert.Contains(t, ds[0].Message, "accentColor", tc.name)
		assert.Contains(t, ds[0].Message, tc.want, tc.name)
		assert.Empty(t, out, tc.name)
	}
}

func TestShadowLayerColorsets(t *testing.T) {
	out := colorsetsOf(t, loadFS(t, os.DirFS(appleDir)))
	d := colorsetAt(t, out, set(notesCatalog, "CardShadowLayer1"))
	require.Len(t, d.Colors, 1)
	assert.Equal(t, "0.2000", d.Colors[0].Color.Components["alpha"])
	assert.NotContains(t, out, set(notesCatalog, "CardShadowLayer2"))
	assert.NotContains(t, out, set(notesCatalog, "CardShadow"))

	fsys := memFS(t, appleDir)
	fsys["design-system/tokens/extra.tokens.json"] = `{ "lift": { "$type": "shadow", "$value": [
    { "color": { "colorSpace": "oklch", "components": [0.2, 0, 0], "alpha": 0.1 }, "offsetX": { "value": 0, "unit": "px" }, "offsetY": { "value": 1, "unit": "px" }, "blur": { "value": 2, "unit": "px" }, "spread": { "value": 0, "unit": "px" } },
    { "color": { "colorSpace": "oklch", "components": [0.2, 0, 0], "alpha": 0.3 }, "offsetX": { "value": 0, "unit": "px" }, "offsetY": { "value": 4, "unit": "px" }, "blur": { "value": 8, "unit": "px" }, "spread": { "value": 0, "unit": "px" } }
  ], "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`
	out = colorsetsOf(t, loadFS(t, fsys))
	assert.Equal(t, "0.1000", colorsetAt(t, out, set(notesCatalog, "LiftLayer1")).Colors[0].Color.Components["alpha"])
	assert.Equal(t, "0.3000", colorsetAt(t, out, set(notesCatalog, "LiftLayer2")).Colors[0].Color.Components["alpha"])
}

// TestColorsetNameCollision: two colours whose colorset folders differ
// only in case (one folder on a case-insensitive disk) are emit-failed.
func TestColorsetNameCollision(t *testing.T) {
	fsys := memFS(t, appleDir)
	fsys["design-system/tokens/extra.tokens.json"] = `{ "inkwell": { "$type": "color", "$value": { "colorSpace": "oklch", "components": [0.5, 0, 0] },
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } },
  "ink-well": { "$type": "color", "$value": { "colorSpace": "oklch", "components": [0.5, 0, 0] },
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`
	p := loadFS(t, fsys)
	_, err := Colorsets(p, applePlatform(t, p))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"ink-well"`)
	assert.Contains(t, err.Error(), `"inkwell"`)
}

// TestColorsetSharedPathMustNotVary: without {app} in the out path, a
// colour that differs per app cannot share one catalog.
func TestColorsetSharedPathMustNotVary(t *testing.T) {
	fsys := memFS(t, appleDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "ios/{app}/Assets.xcassets/Tokens/", "ios/Shared/Assets.xcassets/Tokens/", 1)
	p := loadFS(t, fsys)
	_, err := Colorsets(p, applePlatform(t, p))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{app}")
	assert.Contains(t, err.Error(), "Accent")
}

// appearanceProject is a one-token project whose appearance axis is
// values, with base given, and whose colour bg is dark by default and
// light in mode appearance:<other>.
func appearanceProject(t *testing.T, values, base, other string) *project.Project {
	t.Helper()
	cfg, ds := config.Load([]byte(`version: 1
content: design-system
modes:
  appearance: {values: ` + values + `, base: ` + base + `, binding: runtime, web: {attr: data-theme}}
platforms:
  apple: {emit: [colorsets], out: {colorsets: "ios/Assets.xcassets/Tokens/"}}
`))
	require.Empty(t, ds)
	set, ds := tokens.Parse(cfg, map[string][]byte{"design-system/a.tokens.json": []byte(`{ "bg": { "$type": "color",
  "$value": { "colorSpace": "oklch", "components": [0.2, 0, 0] },
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive",
    "modes": { "appearance:` + other + `": { "colorSpace": "oklch", "components": [0.95, 0, 0] } } } } } }`)})
	require.Empty(t, ds)
	return &project.Project{Config: cfg, Tokens: set}
}

// TestColorsetBaseDarkAppearance: the base appearance is the universal
// entry, and the other value labels its luminosity entry by its own name.
func TestColorsetBaseDarkAppearance(t *testing.T) {
	p := appearanceProject(t, "[light, dark]", "dark", "light")
	out := colorsetsOf(t, p)
	d := colorsetAt(t, out, "ios/Assets.xcassets/Tokens/Bg.colorset/Contents.json")
	require.Len(t, d.Colors, 2)
	assert.Empty(t, d.Colors[0].Appearances)
	assert.Equal(t, p3Components(color.OKLCH{L: 0.2, Alpha: 1}, color.CSSColor4), d.Colors[0].Color.Components, "universal = the dark base")
	require.Len(t, d.Colors[1].Appearances, 1)
	assert.Equal(t, "luminosity", d.Colors[1].Appearances[0].Appearance)
	assert.Equal(t, "light", d.Colors[1].Appearances[0].Value)
	assert.Equal(t, p3Components(color.OKLCH{L: 0.95, Alpha: 1}, color.CSSColor4), d.Colors[1].Color.Components)
}

// TestColorsetAppearanceNotLuminosity: a non-base appearance value that is
// neither light nor dark labels no luminosity entry and is emit-failed.
func TestColorsetAppearanceNotLuminosity(t *testing.T) {
	p := appearanceProject(t, "[light, dim]", "light", "dim")
	out, ds := Build(p, nil)
	require.Len(t, ds, 1, "%v", ds)
	assert.Equal(t, "emit-failed", ds[0].Rule)
	assert.Contains(t, ds[0].Message, "dim")
	assert.Empty(t, out)
}

// TestColorsetNamespaceAfterFanOut: an out path whose last element is the
// fan-out placeholder namespaces each app's colours by the app.
func TestColorsetNamespaceAfterFanOut(t *testing.T) {
	fsys := memFS(t, appleDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "ios/{app}/Assets.xcassets/Tokens/", "ios/Assets.xcassets/{app}/", 1)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "    options: {accentColor: accent}\n", "", 1)
	p := loadFS(t, fsys)
	out := colorsetsOf(t, p)
	assert.Contains(t, out, "ios/Assets.xcassets/tasks/Ink.colorset/Contents.json")
	assert.Contains(t, out, "ios/Assets.xcassets/notes/Contents.json")
	np := PlanNative(p.Config, applePlatform(t, p))
	assert.Equal(t, "tasks/Ink", ColorsetName(np, p.Tokens.Tokens["ink"], config.Context{"app": "tasks"}))
	assert.Equal(t, "notes/Ink", ColorsetName(np, p.Tokens.Tokens["ink"], config.Context{}), "the fan-out's base when ctx leaves it unset")
}

// TestColorsetOutInsideCatalog: the colorsets out is a namespace folder
// inside an .xcassets catalog, checked after fan-out substitution. A bare
// folder (whose AccentColor would land at the repo root) or the catalog
// itself (AccentColor outside it, provides-namespace on its root) is
// emit-failed naming the bad value.
func TestColorsetOutInsideCatalog(t *testing.T) {
	for _, bad := range []string{"Tokens", "ios/{app}/Assets.xcassets", "ios/{app}/Tokens/"} {
		fsys := memFS(t, appleDir)
		fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "ios/{app}/Assets.xcassets/Tokens/", bad, 1)
		require.NotEqual(t, memFS(t, appleDir)["airframe.yaml"], fsys["airframe.yaml"])
		out, ds := Build(loadFS(t, fsys), []string{"colorsets"})
		ds = slices.DeleteFunc(ds, func(d diag.Diagnostic) bool { return d.Rule != "emit-failed" })
		require.Len(t, ds, 1, "%s: %v", bad, ds)
		assert.Contains(t, ds[0].Message, "colorsets out must be a folder inside an .xcassets catalog, as in App/Assets.xcassets/Tokens", bad)
		assert.Contains(t, ds[0].Message, strings.TrimSuffix(strings.ReplaceAll(bad, "{app}", "notes"), "/"), bad)
		assert.Equal(t, "platforms.apple", ds[0].Path, bad)
		assert.Empty(t, out, bad)
	}
}
