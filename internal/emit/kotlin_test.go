// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// androidDir is the android fixture: appearance, density, app (fanned out
// over notes and tasks) and channel (build-bound, fixed), the apple token
// set, a shared DesignTokens.kt and one DesignColorValues.kt per app.
var androidDir = filepath.Join("..", "..", "testdata", "android")

const (
	sharedKotlin = "android/shared/src/main/kotlin/DesignTokens.kt"
	notesKotlin  = "android/notes/src/main/kotlin/DesignColorValues.kt"
	tasksKotlin  = "android/tasks/src/main/kotlin/DesignColorValues.kt"
)

func androidPlatform(t *testing.T, p *project.Project) config.Platform {
	t.Helper()
	for _, pl := range p.Config.Platforms {
		if pl.Name == "android" {
			return pl
		}
	}
	t.Fatal("no android platform")
	return config.Platform{}
}

func kotlinOf(t *testing.T, p *project.Project) map[string][]byte {
	t.Helper()
	out, err := Kotlin(p, androidPlatform(t, p))
	require.NoError(t, err)
	return out
}

func androidKotlin(t *testing.T) map[string][]byte {
	t.Helper()
	return kotlinOf(t, loadFS(t, os.DirFS(androidDir)))
}

// androidFS is the android fixture as a MemFS with its options line
// rewritten by edit.
func androidFS(t *testing.T, edit func(yaml string) string) project.MemFS {
	t.Helper()
	fsys := memFS(t, androidDir)
	fsys["airframe.yaml"] = edit(fsys["airframe.yaml"])
	return fsys
}

// kotlinEdgesDir is a scratch project for Kotlin's edges: members named
// in, object, when and 2xl, a negative dimension, a fractional duration in
// seconds, a fractional font weight, a quoted font family and a shadow
// whose geometry varies on appearance and density. It does not fan out,
// so its one file holds the shared declarations and the values.
var kotlinEdgesDir = filepath.Join("..", "..", "testdata", "kotlin-edges")

const edgesKotlin = "android/src/main/kotlin/Design.kt"

// kotlinGolden checks dir's kotlin output against the .kt goldens under
// dir/android; -update rewrites them.
func kotlinGolden(t *testing.T, dir string, out map[string][]byte) {
	t.Helper()
	if *update {
		require.NoError(t, os.RemoveAll(filepath.Join(dir, "android")))
		for path, b := range out {
			full := filepath.Join(dir, filepath.FromSlash(path))
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
			require.NoError(t, os.WriteFile(full, b, 0o644))
		}
	}
	golden := kotlinGoldenFiles(t, dir)
	require.Equal(t, slices.Sorted(maps.Keys(golden)), slices.Sorted(maps.Keys(out)), "run go test ./internal/emit -update")
	for path, b := range out {
		assert.Equal(t, string(golden[path]), string(b), path)
	}
}

// kotlinGoldenFiles reads the .kt goldens under dir/android.
func kotlinGoldenFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	golden := map[string][]byte{}
	require.NoError(t, fs.WalkDir(os.DirFS(dir), "android", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".kt") {
			return err
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		golden[path] = b
		return err
	}))
	return golden
}

// TestKotlinGolden: the android fixture's shared file and both apps'
// DesignColorValues.kt, and the edges project's one file, equal the
// committed goldens. -update rewrites them.
func TestKotlinGolden(t *testing.T) {
	out := androidKotlin(t)
	require.Equal(t, []string{notesKotlin, sharedKotlin, tasksKotlin}, slices.Sorted(maps.Keys(out)))
	kotlinGolden(t, androidDir, out)
	edges := kotlinOf(t, loadFS(t, os.DirFS(kotlinEdgesDir)))
	require.Equal(t, []string{edgesKotlin}, slices.Sorted(maps.Keys(edges)))
	kotlinGolden(t, kotlinEdgesDir, edges)

	// The shared file: the class with its runtime parameters, enums,
	// Shadow, companion and members, then DesignColors.
	s := string(out[sharedKotlin])
	for _, want := range []string{
		"public class DesignTokens(\n    public val colors: DesignColors,\n    public val appearance: Appearance = Appearance.Light,\n    public val density: Density = Density.Standard,\n) {\n",
		"    public enum class Appearance { Light, Dark }\n",
		"    public enum class Density { Compact, Standard, Relaxed }\n",
		"    public data class Shadow(\n        public val color: Color,\n        public val x: Dp,\n        public val y: Dp,\n        public val blur: Dp,\n        public val spread: Dp,\n        public val inset: Boolean,\n    )\n",
		"    public companion object {\n",
		"        public val radius: Dp = 4.dp\n",
		"        public val opacityMuted: Float = 0.6f\n",
		"        public val durationFast: Duration = 150.milliseconds\n",
		"        public val easeStandard: CubicBezierEasing = CubicBezierEasing(0.2f, 0f, 0f, 1f)\n",
		"        public val fontBody: List<String> = listOf(\"Inter\", \"system-ui\")\n",
		"        public val weightStrong: FontWeight = FontWeight(650)\n",
		"    public val accent: Color get() = colors.accent\n",
		"    public val elevation: List<Shadow> get() = listOf(Shadow(color = colors.elevationLayer1, x = 0.dp, y = 1.dp, blur = 2.dp, spread = 0.dp, inset = false))\n",
		"    public val gap: Dp get() = when (density) {\n        Density.Compact -> 4.dp\n        Density.Standard -> 4.dp\n        Density.Relaxed -> 8.dp\n    }\n",
		"    public val surface: Color get() = when (density) {\n        Density.Compact -> colors.surfaceCompact\n        Density.Standard -> colors.surfaceStandard\n        Density.Relaxed -> colors.surfaceRelaxed\n    }\n",
		"    public val hairline: Dp get() = when (appearance) {\n        Appearance.Light -> when (density) {\n            Density.Compact -> 0.5.dp\n",
		"public data class DesignColors(\n",
	} {
		assert.Contains(t, s, want)
	}
	assert.Less(t, strings.Index(s, "public class DesignTokens("), strings.Index(s, "public data class DesignColors("))
	for _, imp := range []string{"androidx.compose.animation.core.CubicBezierEasing", "androidx.compose.ui.graphics.Color",
		"androidx.compose.ui.text.font.FontWeight", "androidx.compose.ui.unit.Dp", "androidx.compose.ui.unit.dp",
		"kotlin.time.Duration", "kotlin.time.Duration.Companion.milliseconds"} {
		assert.Contains(t, s, "\nimport "+imp+"\n")
	}
}

// TestKotlinStaticAndInstance: a non-colour token varying on nothing is a
// companion val, with an instance val forwarding to it so every member
// reads alike on an instance (as Swift's static let does); a colour or
// shadow is always an instance val, as it reads colors.
func TestKotlinStaticAndInstance(t *testing.T) {
	s := string(androidKotlin(t)[sharedKotlin])
	assert.Contains(t, s, "        public val space1: Dp = 4.dp\n")
	assert.Contains(t, s, "    public val space1: Dp get() = Companion.space1\n")
	assert.Contains(t, s, "    public val ink: Color get() = colors.ink\n")
	companion := s[strings.Index(s, "    public companion object {"):]
	companion = companion[:strings.Index(companion, "\n    }\n")]
	assert.NotContains(t, companion, "Color", "no colour is static")
	assert.NotContains(t, companion, "gap", "a varying token is no static")
}

// TestKotlinFileShape: every file starts with the header and the package,
// lists only the imports it uses, and ends in one newline; the shared file
// holds DesignTokens and DesignColors, the per-app file DesignColorValues.
func TestKotlinFileShape(t *testing.T) {
	out := androidKotlin(t)
	head := "// GENERATED by airframe from design-system/ — do not edit. Regenerate with `airframe build`.\n\npackage com.example.design\n\n"
	for path, b := range out {
		s := string(b)
		assert.True(t, strings.HasPrefix(s, head+"import "), "%s:\n%s", path, s)
		assert.True(t, strings.HasSuffix(s, ")\n") || strings.HasSuffix(s, "}\n"), path)
		assert.False(t, strings.HasSuffix(s, "\n\n"), path)
		assert.NotContains(t, s, "\r", path)
		assert.NotContains(t, s, "\n\n\n", path)
		assert.NotContains(t, s, "ColorSpaces", "srgb needs no colour space: %s", path)
		var imports []string
		for _, m := range regexp.MustCompile(`(?m)^import (.+)$`).FindAllStringSubmatch(s, -1) {
			imports = append(imports, m[1])
		}
		assert.True(t, slices.IsSorted(imports), "%s: %v", path, imports)
		for _, imp := range imports {
			short := imp[strings.LastIndex(imp, ".")+1:]
			assert.Regexp(t, `\b`+short+`\b`, s[strings.Index(s, "\n\npublic"):], "%s imports %s but does not use it", path, imp)
		}
	}
	shared := string(out[sharedKotlin])
	assert.Contains(t, shared, "public data class DesignColors(\n")
	assert.Contains(t, shared, "public class DesignTokens(\n")
	assert.NotContains(t, shared, "DesignColorValues")
	notes := string(out[notesKotlin])
	assert.True(t, strings.HasPrefix(notes, head+"import androidx.compose.material3.ColorScheme\nimport androidx.compose.material3.darkColorScheme\n"+
		"import androidx.compose.material3.lightColorScheme\nimport androidx.compose.ui.graphics.Color\n\npublic object DesignColorValues {\n"), notes)
	assert.Contains(t, notes, "    public val light: DesignColors = DesignColors(\n")
	assert.Contains(t, notes, "    public val dark: DesignColors = DesignColors(\n")
	assert.Less(t, strings.Index(notes, "val light"), strings.Index(notes, "val dark"), "appearance axis order")
	assert.NotContains(t, notes, "data class")
	assert.NotContains(t, notes, "class DesignTokens")
}

// TestKotlinColorSlots (Review Focus 1): surface, varying on density, has
// one slot per density value, named in axis order, with each value's
// colour; shadow layers have <member>Layer<N> slots; slots are sorted.
func TestKotlinColorSlots(t *testing.T) {
	out := androidKotlin(t)
	shared := string(out[sharedKotlin])
	shared = shared[strings.Index(shared, "public data class DesignColors("):]
	for _, s := range []string{"surfaceCompact", "surfaceStandard", "surfaceRelaxed", "cardShadowLayer1", "elevationLayer1", "shadowSmLayer1", "buttonBg", "ink"} {
		assert.Contains(t, shared, "    public val "+s+": Color,\n")
	}
	assert.NotContains(t, shared, "public val surface: Color", "a colour varying on density has no unsuffixed slot")
	assert.NotContains(t, shared, "Notes", "the fan-out axis names no slot")
	assert.NotContains(t, shared, "Stable", "a fixed axis names no slot")
	assert.NotContains(t, shared, "Light", "appearance names no slot")

	var slots []string
	for _, m := range regexp.MustCompile(`(?m)^    public val (\w+): Color,$`).FindAllStringSubmatch(shared, -1) {
		slots = append(slots, m[1])
	}
	assert.True(t, slices.IsSorted(slots), "%v", slots)

	p := loadFS(t, os.DirFS(androidDir))
	k, err := newKotliner(p, androidPlatform(t, p))
	require.NoError(t, err)
	got, err := k.slots()
	require.NoError(t, err)
	byName := map[string]kotlinSlot{}
	for _, s := range got {
		byName[s.Name] = s
	}
	for _, d := range []string{"compact", "standard", "relaxed"} {
		s := byName["surface"+Pascal(d)]
		require.NotNil(t, s.Token, d)
		assert.Equal(t, "surface", s.Token.Name)
		assert.Equal(t, -1, s.Layer)
		assert.Equal(t, config.Context{"density": d}, s.Ctx)
	}
	assert.Equal(t, 0, byName["elevationLayer1"].Layer)
	assert.Equal(t, config.Context{}, byName["ink"].Ctx)

	// Values: light compact is gray.200, light relaxed gray.50, dark
	// compact gray.800, dark relaxed gray.900.
	gray := func(l, c float64) string {
		return kotlinColor(color.OKLCH{L: l, C: c, H: 250, Alpha: 1}, "srgb", "css-color-4")
	}
	light, dark := block(t, string(out[notesKotlin]), "light"), block(t, string(out[notesKotlin]), "dark")
	assert.Contains(t, light, "        surfaceCompact = "+gray(0.9, 0.008)+",\n")
	assert.Contains(t, light, "        surfaceStandard = "+gray(0.98, 0.004)+",\n")
	assert.Contains(t, light, "        surfaceRelaxed = "+gray(0.98, 0.004)+",\n")
	assert.Contains(t, dark, "        surfaceCompact = "+gray(0.3, 0.01)+",\n")
	assert.Contains(t, dark, "        surfaceStandard = "+gray(0.2, 0.012)+",\n")
	assert.Contains(t, dark, "        surfaceRelaxed = "+gray(0.2, 0.012)+",\n")
}

// block is the DesignColors(…) argument list of the named val.
func block(t *testing.T, file, val string) string {
	t.Helper()
	start := strings.Index(file, "    public val "+val+": ")
	require.GreaterOrEqual(t, start, 0, "no val %s in\n%s", val, file)
	end := strings.Index(file[start:], "\n    )\n")
	require.GreaterOrEqual(t, end, 0)
	return file[start : start+end+1]
}

// TestKotlinFanOutThroughAlias (Review Focus 3): button-bg aliases accent,
// which varies per app, so each app's DesignColorValues has its own; the
// shared file is the same bytes whichever app is built.
func TestKotlinFanOutThroughAlias(t *testing.T) {
	out := androidKotlin(t)
	notes, tasks := block(t, string(out[notesKotlin]), "light"), block(t, string(out[tasksKotlin]), "light")
	blue := kotlinColor(color.OKLCH{L: 0.6, C: 0.15, H: 255, Alpha: 1}, "srgb", "css-color-4")
	green := kotlinColor(color.OKLCH{L: 0.65, C: 0.15, H: 150, Alpha: 1}, "srgb", "css-color-4")
	assert.Contains(t, notes, "        buttonBg = "+blue+",\n")
	assert.Contains(t, tasks, "        buttonBg = "+green+",\n")
	assert.Contains(t, notes, "        accent = "+blue+",\n")
	assert.Contains(t, tasks, "        accent = "+green+",\n")
	assert.NotEqual(t, string(out[notesKotlin]), string(out[tasksKotlin]))

	// Built with tasks first, the shared bytes and each
	// app's values are the same: neither depends on which app comes first.
	fsys := androidFS(t, func(y string) string {
		return strings.Replace(y, "app: {values: [notes, tasks], base: notes,", "app: {values: [tasks, notes], base: notes,", 1)
	})
	swapped := kotlinOf(t, loadFS(t, fsys))
	for _, f := range []string{sharedKotlin, notesKotlin, tasksKotlin} {
		assert.Equal(t, string(out[f]), string(swapped[f]), f)
	}
}

// TestKotlinGamut (Review Focus 4): highlight, an OKLCH beyond sRGB and
// Display P3, is CSS Color 4 mapped by default and clipped under gamut:
// clip; display-p3 writes ColorSpaces.DisplayP3 components in [0,1].
func TestKotlinGamut(t *testing.T) {
	vivid := color.OKLCH{L: 0.7, C: 0.4, H: 145, Alpha: 1}
	hex := func(r, g, b float64) string {
		c := func(v float64) int { return int(math.Floor(v*255 + 0.5)) }
		return fmt.Sprintf("Color(0xFF%02X%02X%02X)", c(r), c(g), c(b))
	}
	highlight := func(file string) string {
		m := regexp.MustCompile(`(?m)^        highlight = (.+),$`).FindStringSubmatch(block(t, file, "light"))
		require.NotNil(t, m, file)
		return m[1]
	}

	mapped := highlight(string(androidKotlin(t)[notesKotlin]))
	assert.Equal(t, hex(vivid.ToSRGB(color.CSSColor4)), mapped)

	withOpts := func(extra string) map[string][]byte {
		return kotlinOf(t, loadFS(t, androidFS(t, func(y string) string {
			return strings.Replace(y, "      package: com.example.design\n", "      package: com.example.design\n"+extra, 1)
		})))
	}
	clipped := highlight(string(withOpts("      gamut: clip\n")[notesKotlin]))
	assert.Equal(t, hex(vivid.ToSRGB(color.Clip)), clipped)
	assert.NotEqual(t, mapped, clipped)
	// Pinned bytes. The clipped green (0xD2) was checked by hand from the
	// OKLab → linear sRGB matrices; the mapped one keeps lightness and hue
	// at lower chroma, so it is darker.
	assert.Equal(t, "Color(0xFF00C300)", mapped)
	assert.Equal(t, "Color(0xFF00D200)", clipped)

	for _, g := range []string{"css-color-4", "clip"} {
		out := withOpts("      colorSpace: display-p3\n      gamut: " + g + "\n")
		file := string(out[notesKotlin])
		assert.Contains(t, file, "import androidx.compose.ui.graphics.colorspace.ColorSpaces\n", g)
		assert.NotContains(t, string(out[sharedKotlin]), "ColorSpaces", "the shared file holds no values")
		p3 := highlight(file)
		m := regexp.MustCompile(`^Color\(red = ([0-9.]+)f, green = ([0-9.]+)f, blue = ([0-9.]+)f, alpha = ([0-9.]+)f, colorSpace = ColorSpaces\.DisplayP3\)$`).FindStringSubmatch(p3)
		require.NotNil(t, m, p3)
		for _, c := range m[1:] {
			assert.Regexp(t, `^\d\.\d{4}$`, c)
			f, err := strconv.ParseFloat(c, 64)
			require.NoError(t, err)
			assert.True(t, f >= 0 && f <= 1, "%s in %s", c, p3)
		}
		gm := color.CSSColor4
		if g == "clip" {
			gm = color.Clip
		}
		r, gr, b := vivid.ToP3(gm)
		assert.Equal(t, fmt.Sprintf("Color(red = %.4ff, green = %.4ff, blue = %.4ff, alpha = 1.0000f, colorSpace = ColorSpaces.DisplayP3)", r, gr, b), p3)
	}
}

// TestKotlinColorFormat: alpha and channels round half up to 8 bits.
func TestKotlinColorFormat(t *testing.T) {
	assert.Equal(t, "Color(0xFFFFFFFF)", kotlinColor(color.OKLCH{L: 1, Alpha: 1}, "srgb", "css-color-4"))
	assert.Equal(t, "Color(0x80000000)", kotlinColor(color.OKLCH{L: 0, Alpha: 0.5}, "srgb", "css-color-4"), "127.5 rounds up")
	assert.Equal(t, "Color(0x00000000)", kotlinColor(color.OKLCH{L: 0, Alpha: 0}, "srgb", "clip"))
	assert.Equal(t, "Color(red = 0.0000f, green = 0.0000f, blue = 0.0000f, alpha = 0.2000f, colorSpace = ColorSpaces.DisplayP3)",
		kotlinColor(color.OKLCH{L: 0, Alpha: 0.2}, "display-p3", "clip"))
}

// TestKotlinShadowLayerAlpha: a shadow layer's slot keeps its alpha.
func TestKotlinShadowLayerAlpha(t *testing.T) {
	want := kotlinColor(color.OKLCH{L: 0.2, C: 0.012, H: 250, Alpha: 0.2}, "srgb", "css-color-4")
	assert.True(t, strings.HasPrefix(want, "Color(0x33"), want)
	assert.Contains(t, block(t, string(androidKotlin(t)[notesKotlin]), "light"), "        shadowSmLayer1 = "+want+",\n")
}

// TestKotlinSharedOut (Review Focus 5): with no sharedOut, a kotlin out
// that fans out is emit-failed naming the option; one that does not holds
// the shared declarations and the values in one file.
func TestKotlinSharedOut(t *testing.T) {
	noShared := func(y string) string {
		return strings.Replace(y, "      sharedOut: android/shared/src/main/kotlin/DesignTokens.kt\n", "", 1)
	}
	p := loadFS(t, androidFS(t, noShared))
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	d := failure(androidPlatform(t, p), "kotlin", err)
	assert.Equal(t, "emit-failed", d.Rule)
	assert.Equal(t, "platform android: kotlin: out fans out per app; set options.sharedOut for the shared DesignTokens file", d.Message)

	// No fan-out: one file, shared declarations first, imports once.
	fsys := androidFS(t, func(y string) string {
		y = noShared(y)
		y = strings.Replace(y, `out: {kotlin: "android/{app}/src/main/kotlin/DesignColorValues.kt"}`, "out: {kotlin: android/app/src/main/kotlin/Design.kt}", 1)
		return strings.Replace(y, "    fanOut: app\n", "", 1)
	})
	out := kotlinOf(t, loadFS(t, fsys))
	require.Equal(t, []string{"android/app/src/main/kotlin/Design.kt"}, slices.Sorted(maps.Keys(out)))
	s := string(out["android/app/src/main/kotlin/Design.kt"])
	assert.Equal(t, 1, strings.Count(s, "// GENERATED"))
	assert.Equal(t, 1, strings.Count(s, "package com.example.design\n"))
	assert.Equal(t, 1, strings.Count(s, "import androidx.compose.ui.graphics.Color\n"))
	assert.Less(t, strings.Index(s, "public data class DesignColors("), strings.Index(s, "public object DesignColorValues {"))
	shared := string(androidKotlin(t)[sharedKotlin])
	assert.Contains(t, s, shared[strings.Index(shared, "public class DesignTokens"):], "the shared part is the same declarations")
	assert.Less(t, strings.Index(s, "public class DesignTokens("), strings.Index(s, "public data class DesignColors("))
}

// TestKotlinSharedOutCleanedCollides: a sharedOut spelled with ./ or ..
// is written at its cleaned path, so the cross-platform collision check
// catches another platform's out that names the same file.
func TestKotlinSharedOutCleanedCollides(t *testing.T) {
	for _, spelling := range []string{"./" + sharedKotlin, "android/x/../shared/src/main/kotlin/DesignTokens.kt"} {
		fsys := androidFS(t, func(y string) string {
			y = strings.Replace(y, "sharedOut: "+sharedKotlin, "sharedOut: "+spelling, 1)
			return strings.Replace(y, "platforms:\n", "platforms:\n  data: {emit: json, out: "+sharedKotlin+"}\n", 1)
		})
		out, ds := Build(loadFS(t, fsys), nil)
		var msgs []string
		for _, d := range ds {
			if d.Severity == diag.Error {
				msgs = append(msgs, d.Message)
			}
		}
		assert.Equal(t, []string{"data.json and android.kotlin both write " + sharedKotlin}, msgs, spelling)
		for f := range out {
			assert.True(t, path.Clean(f) == f, "%s: output path %q is not clean", spelling, f)
		}
	}
}

// TestKotlinOnePerAppFileMustNotVary: a per-app out without {app} would
// be one file for two apps whose colours differ.
func TestKotlinOnePerAppFileMustNotVary(t *testing.T) {
	p := loadFS(t, androidFS(t, func(y string) string {
		return strings.Replace(y, `"android/{app}/src/main/kotlin/DesignColorValues.kt"`, "android/all/DesignColorValues.kt", 1)
	}))
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differs between app notes and tasks; put {app} in the kotlin out path")
}

// TestKotlinSlotCollision: two slots of one name are emit-failed naming
// both tokens.
func TestKotlinSlotCollision(t *testing.T) {
	fsys := memFS(t, androidDir)
	fsys["design-system/tokens/extra.tokens.json"] = `{ "surface-compact": { "$type": "color", "$value": "{gray.50}",
  "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } } }`
	p := loadFS(t, fsys)
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"surface"`)
	assert.Contains(t, err.Error(), `"surface-compact"`)
	assert.Contains(t, err.Error(), "surfaceCompact")
}

// TestKotlinColorTypeNames: the colour types take the type part of the
// kotlin naming template, less a Tokens suffix.
func TestKotlinColorTypeNames(t *testing.T) {
	for template, want := range map[string]string{"DesignTokens.{camel}": "Design", "Theme.{camel}": "Theme", "AppTokens.{camel}": "App"} {
		assert.Equal(t, want, kotlinColorPrefix(template[:strings.LastIndex(template, ".")]), template)
	}
	fsys := androidFS(t, func(y string) string {
		return strings.Replace(y, "platforms:", "naming: {kotlin: \"Theme.{camel}\"}\nplatforms:", 1)
	})
	out := kotlinOf(t, loadFS(t, fsys))
	assert.Contains(t, string(out[sharedKotlin]), "public data class ThemeColors(\n")
	assert.Contains(t, string(out[notesKotlin]), "public object ThemeColorValues {\n    public val light: ThemeColors = ThemeColors(\n")
}

// TestKotlinNoAppearance: with no appearance axis the values object has a
// single colors val.
func TestKotlinNoAppearance(t *testing.T) {
	out := kotlinOf(t, loadFS(t, noAppearanceFS(t)))
	s := string(out[notesKotlin])
	assert.Contains(t, s, "public object DesignColorValues {\n    public val colors: DesignColors = DesignColors(\n")
	assert.NotContains(t, s, "val light:")
}

// noAppearanceFS is the android fixture with no appearance axis and its
// appearance modes removed.
func noAppearanceFS(t *testing.T) project.MemFS {
	t.Helper()
	fsys := memFS(t, androidDir)
	fsys["airframe.yaml"] = regexp.MustCompile(`(?m)^  appearance: .*\n`).ReplaceAllString(fsys["airframe.yaml"], "")
	delete(fsys, "design-system/themes/dark.tokens.json")
	fsys["design-system/tokens/scale.tokens.json"] = strings.NewReplacer(
		`"appearance:dark": {
            "value": 1.5,
            "unit": "px"
          },`, "",
		`,
          "appearance:dark+density:compact": {
            "value": 1,
            "unit": "px"
          }`, "").Replace(fsys["design-system/tokens/scale.tokens.json"])
	fsys["design-system/tokens/semantic.tokens.json"] = strings.NewReplacer(
		`,
          "appearance:dark+density:compact": "{gray.800}",
          "appearance:dark+density:relaxed": "{gray.900}"`, "").Replace(fsys["design-system/tokens/semantic.tokens.json"])
	return fsys
}

// TestKotlinIdentifiers (Review Focus 2): members named after hard
// keywords are backticked, a leading digit gets a _ prefix, and both
// compile (TestKotlinTypechecks); space-4 and space4 are emit-failed
// naming both.
func TestKotlinIdentifiers(t *testing.T) {
	s := string(kotlinOf(t, loadFS(t, os.DirFS(kotlinEdgesDir)))[edgesKotlin])
	for _, want := range []string{
		"        public val `in`: Float = 1f\n",
		"    public val `in`: Float get() = Companion.`in`\n",
		"    public val `object`: Color get() = colors.`object`\n",
		"    public val `object`: Color,\n",
		"        `object` = Color(",
		"    public val `when`: Dp get() = when (density) {\n",
		"        public val _2xl: Dp = 24.dp\n",
	} {
		assert.Contains(t, s, want)
	}

	fsys := memFS(t, kotlinEdgesDir)
	fsys["design-system/tokens/extra.tokens.json"] = `{ "space4": { "$type": "number", "$value": 4,
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } },
  "space": { "4": { "$type": "number", "$value": 4,
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } } }`
	p := loadFS(t, fsys)
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	d := failure(androidPlatform(t, p), "kotlin", err)
	assert.Equal(t, "emit-failed", d.Rule)
	assert.Contains(t, d.Message, `"space-4"`)
	assert.Contains(t, d.Message, `"space4"`)
	assert.Contains(t, d.Message, "Kotlin identifier space4")

	// A token taking an axis's parameter name, or colors, collides too.
	for _, name := range []string{"density", "colors"} {
		fsys := memFS(t, kotlinEdgesDir)
		fsys["design-system/tokens/extra.tokens.json"] = `{ "` + name + `": { "$type": "number", "$value": 4,
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`
		p := loadFS(t, fsys)
		_, err := Kotlin(p, androidPlatform(t, p))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), `"`+name+`"`)
	}
}

// TestKotlinTypeNameKeyword: a naming template whose type part is a hard
// keyword is emit-failed naming the template.
func TestKotlinTypeNameKeyword(t *testing.T) {
	fsys := androidFS(t, func(y string) string {
		return strings.Replace(y, "platforms:", "naming: {kotlin: \"object.{camel}\"}\nplatforms:", 1)
	})
	p := loadFS(t, fsys)
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	d := failure(androidPlatform(t, p), "kotlin", err)
	assert.Equal(t, "emit-failed", d.Rule)
	assert.Contains(t, d.Message, "object.{camel}")

	// An axis whose enum would shadow a type the file uses.
	fsys = memFS(t, kotlinEdgesDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "  density: {", "  color: {", 1)
	for path, src := range fsys {
		if strings.HasSuffix(path, ".json") {
			fsys[path] = strings.ReplaceAll(src, "density:", "color:")
		}
	}
	p = loadFS(t, fsys)
	_, err = Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "axis color")
}

// TestKotlinUnresolvableSlot: a colour aliasing a token that does not
// exist is emit-failed naming it.
func TestKotlinUnresolvableSlot(t *testing.T) {
	fsys := memFS(t, androidDir)
	fsys["design-system/tokens/extra.tokens.json"] = `{ "lone": { "$type": "color", "$value": "{nowhere}",
  "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } } }`
	p := loadFS(t, fsys)
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	d := failure(androidPlatform(t, p), "kotlin", err)
	assert.Equal(t, "emit-failed", d.Rule)
	assert.Contains(t, d.Message, `"lone"`)
}

// TestKotlinFanOutNonColour: the DesignTokens file is shared, so a
// non-colour token, or a shadow's geometry, that varies per app is
// emit-failed; a shadow whose colour alone varies per app is not.
func TestKotlinFanOutNonColour(t *testing.T) {
	fsys := memFS(t, androidDir)
	fsys["design-system/tokens/gutter.tokens.json"] = gutterTokens
	p := loadFS(t, fsys)
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	d := failure(androidPlatform(t, p), "kotlin", err)
	assert.Equal(t, "emit-failed", d.Rule)
	assert.Equal(t, `platform android: kotlin: token "gutter" varies per app; Kotlin non-colour tokens are shared`, d.Message)

	shadow := func(y int, alpha float64) string {
		return fmt.Sprintf(`{ "color": { "colorSpace": "oklch", "components": [0.2, 0.01, 250], "alpha": %v },
  "offsetX": { "value": 0, "unit": "px" }, "offsetY": { "value": %d, "unit": "px" },
  "blur": { "value": 2, "unit": "px" }, "spread": { "value": 0, "unit": "px" } }`, alpha, y)
	}
	glow := func(tasks string) string {
		return `{ "glow": { "$type": "shadow", "$value": ` + shadow(1, 0.2) + `,
  "$extensions": { "com.c65llc.airframe": { "tier": "semantic", "modes": { "app:tasks": ` + tasks + ` } } } } }`
	}
	fsys = memFS(t, androidDir)
	fsys["design-system/tokens/glow.tokens.json"] = glow(shadow(3, 0.2))
	p = loadFS(t, fsys)
	_, err = Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `token "glow" varies per app; Kotlin non-colour tokens are shared`)

	fsys["design-system/tokens/glow.tokens.json"] = glow(shadow(1, 0.5))
	out := kotlinOf(t, loadFS(t, fsys))
	assert.Contains(t, string(out[sharedKotlin]), "    public val glow: List<Shadow> get() = listOf(Shadow(color = colors.glowLayer1, x = 0.dp, y = 1.dp,")
	assert.NotEqual(t, block(t, string(out[notesKotlin]), "light"), block(t, string(out[tasksKotlin]), "light"))
}

// noColourFS is the android fixture with only non-colour tokens and no
// material3Scheme.
func noColourFS(t *testing.T) project.MemFS {
	t.Helper()
	fsys := androidFS(t, func(y string) string {
		return regexp.MustCompile(`(?m)^      material3Scheme: .*\n`).ReplaceAllString(y, "")
	})
	for path := range fsys {
		if strings.HasPrefix(path, "design-system/") {
			delete(fsys, path)
		}
	}
	fsys["design-system/tokens/scale.tokens.json"] = `{ "gap": { "$type": "dimension", "$value": { "value": 4, "unit": "px" },
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "modes": { "density:relaxed": { "value": 8, "unit": "px" } } } } },
  "radius": { "$type": "dimension", "$value": { "value": 4, "unit": "px" },
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`
	return fsys
}

// TestKotlinNoColourSlots: with no colour or shadow token there is no
// DesignColors and no colors parameter, and each app still gets an empty
// DesignColorValues.
func TestKotlinNoColourSlots(t *testing.T) {
	out := kotlinOf(t, loadFS(t, noColourFS(t)))
	require.Equal(t, []string{notesKotlin, sharedKotlin, tasksKotlin}, slices.Sorted(maps.Keys(out)))
	shared := string(out[sharedKotlin])
	assert.NotContains(t, shared, "DesignColors")
	assert.NotContains(t, shared, "colors")
	assert.NotContains(t, shared, "graphics.Color")
	assert.Contains(t, shared, "public class DesignTokens(\n    public val density: Density = Density.Standard,\n) {\n")
	head := "// GENERATED by airframe from design-system/ — do not edit. Regenerate with `airframe build`.\n\npackage com.example.design\n\n"
	for _, f := range []string{notesKotlin, tasksKotlin} {
		assert.Equal(t, head+"public object DesignColorValues\n", string(out[f]), f)
	}
}

// TestKotlinValueFormats: dimensions, durations and weights spell as the
// plan's types table says.
func TestKotlinValueFormats(t *testing.T) {
	s := string(kotlinOf(t, loadFS(t, os.DirFS(kotlinEdgesDir)))[edgesKotlin])
	for _, want := range []string{
		"        public val nudge: Dp = (-1).dp\n",
		"        public val settle: Duration = (37.5).milliseconds\n",
		"        public val weightBook: FontWeight = FontWeight(450)\n",
		"        public val fontMono: List<String> = listOf(\"JetBrains \\\"Mono\\\"\", \"\\$mono\")\n",
		"        Density.Compact -> 1.5.dp\n",
		"        public val vast: Dp = 3000000000.0.dp\n",
		"        public val abyss: Dp = (-3000000000.0).dp\n",
	} {
		assert.Contains(t, s, want)
	}

	// Load keeps a weight within 1..1000; rounding stays there too.
	fsys := memFS(t, kotlinEdgesDir)
	fsys["design-system/tokens/extra.tokens.json"] = `{ "weight-odd": { "$type": "fontWeight", "$value": 999.6,
  "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } } }`
	assert.Contains(t, string(kotlinOf(t, loadFS(t, fsys))[edgesKotlin]), "        public val weightOdd: FontWeight = FontWeight(1000)\n")
}

// kotlinUse is a Kotlin source using every member of the DesignTokens
// class in src, with its declared type: companion members on the type,
// instance members on an instance built for every value of every axis,
// from the first <Type>Colors of values ("" when the class takes none).
func kotlinUse(t *testing.T, src, values string) string {
	t.Helper()
	pkg := regexp.MustCompile(`(?m)^package (.+)$`).FindStringSubmatch(src)
	typ := regexp.MustCompile(`(?m)^public class (\w+)`).FindStringSubmatch(src)
	require.NotNil(t, pkg)
	require.NotNil(t, typ)
	class := src[strings.Index(src, typ[0]):]
	class = class[:strings.Index(class, "\n}\n")]
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg[1])
	for _, m := range regexp.MustCompile(`(?m)^import .+$`).FindAllString(src, -1) {
		b.WriteString(m + "\n")
	}
	b.WriteString("\nfun useAll() {\n")
	params := regexp.MustCompile(`(?m)^    public val (\S+): (\w+) = \w+\.\w+,$`).FindAllStringSubmatch(class, -1)
	var args []string
	if regexp.MustCompile(`(?m)^    public val colors: \w+,$`).MatchString(class) {
		v := regexp.MustCompile(`(?m)^    public val (\S+): \w+Colors = `).FindStringSubmatch(values)
		require.NotNil(t, v, "no colours in\n%s", values)
		args = append(args, "colors = "+regexp.MustCompile(`(?m)^public object (\w+)`).FindStringSubmatch(values)[1]+"."+v[1])
	}
	for i, p := range params {
		fmt.Fprintf(&b, "    for (v%d in %s.%s.entries) {\n", i, typ[1], p[2])
		args = append(args, fmt.Sprintf("%s = v%d", p[1], i))
	}
	fmt.Fprintf(&b, "    val tokens = %s(%s)\n", typ[1], strings.Join(args, ", "))
	qualify := func(decl string) string {
		return regexp.MustCompile(`\bShadow\b`).ReplaceAllString(decl, typ[1]+".Shadow")
	}
	n := 0
	for _, m := range regexp.MustCompile(`(?m)^    public val (\S+): (.+?) get\(\) = `).FindAllStringSubmatch(class, -1) {
		fmt.Fprintf(&b, "    run { val v: %s = tokens.%s }\n", qualify(m[2]), m[1])
		n++
	}
	for _, m := range regexp.MustCompile(`(?m)^        public val (\S+): (.+?) = `).FindAllStringSubmatch(class, -1) {
		fmt.Fprintf(&b, "    run { val v: %s = %s.%s }\n", qualify(m[2]), typ[1], m[1])
		n++
	}
	require.Positive(t, n)
	b.WriteString(strings.Repeat("    }\n", len(params)) + "}\n")
	return b.String()
}

// kotlinSets are the sources TestKotlinTypechecks compiles, by name: each
// golden's shared file with one app's values, the edges file, and the
// no-colour, display-p3 and no-appearance variants.
func kotlinSets(t *testing.T) map[string][]string {
	t.Helper()
	android := kotlinGoldenFiles(t, androidDir)
	edges := kotlinGoldenFiles(t, kotlinEdgesDir)
	noColour := kotlinOf(t, loadFS(t, noColourFS(t)))
	p3 := kotlinOf(t, loadFS(t, androidFS(t, func(y string) string {
		return strings.Replace(y, "      package: com.example.design\n", "      package: com.example.design\n      colorSpace: display-p3\n", 1)
	})))
	noAppearance := kotlinOf(t, loadFS(t, noAppearanceFS(t)))
	return map[string][]string{
		"android-notes": {string(android[sharedKotlin]), string(android[notesKotlin])},
		"android-tasks": {string(android[sharedKotlin]), string(android[tasksKotlin])},
		"edges":         {string(edges[edgesKotlin]), string(edges[edgesKotlin])},
		"no-colour":     {string(noColour[sharedKotlin]), string(noColour[notesKotlin])},
		"display-p3":    {string(p3[sharedKotlin]), string(p3[notesKotlin])},
		"m3-no-appear":  {string(noAppearance[sharedKotlin]), string(noAppearance[notesKotlin])},
	}
}

// TestKotlinTypechecks: every golden set, with a source using every
// member, compiles with kotlinc against the Compose stubs in
// testdata/kotlin-stubs. Skipped without kotlinc on PATH.
func TestKotlinTypechecks(t *testing.T) {
	kotlinc, err := exec.LookPath("kotlinc")
	if err != nil {
		t.Skip("kotlinc is not on PATH")
	}
	stubs, err := filepath.Glob(filepath.Join("testdata", "kotlin-stubs", "*.kt"))
	require.NoError(t, err)
	require.NotEmpty(t, stubs)
	for name, set := range kotlinSets(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := []string{filepath.Join(dir, "Shared.kt"), filepath.Join(dir, "Use.kt")}
			require.NoError(t, os.WriteFile(files[0], []byte(set[0]), 0o644))
			if set[1] != set[0] {
				files = append(files, filepath.Join(dir, "Values.kt"))
				require.NoError(t, os.WriteFile(files[2], []byte(set[1]), 0o644))
			}
			use := kotlinUse(t, set[0], set[1])
			require.NoError(t, os.WriteFile(files[1], []byte(use), 0o644))
			args := append([]string{"-nowarn"}, files...)
			args = append(args, stubs...)
			b, err := exec.Command(kotlinc, append(args, "-d", filepath.Join(dir, "out"))...).CombinedOutput()
			require.NoError(t, err, "kotlinc %s:\n%s\nuse:\n%s", name, b, use)
			t.Logf("kotlinc %s: ok%s", name, strings.TrimRight("\n"+string(b), "\n"))
		})
	}
}

// TestKotlinDpRange: an integral dp outside Int's range is a Double
// receiver, as Compose has no Long.dp.
func TestKotlinDpRange(t *testing.T) {
	for _, c := range []struct {
		d    tokens.Dim
		want string
	}{
		{tokens.Dim{V: 2147483647, Unit: "px"}, "2147483647.dp"},
		{tokens.Dim{V: 2147483648, Unit: "px"}, "2147483648.0.dp"},
		{tokens.Dim{V: 3000000000, Unit: "px"}, "3000000000.0.dp"},
		{tokens.Dim{V: -2147483647, Unit: "px"}, "(-2147483647).dp"},
		{tokens.Dim{V: -2147483648, Unit: "px"}, "(-2147483648).dp"},
		{tokens.Dim{V: -2147483649, Unit: "px"}, "(-2147483649.0).dp"},
		{tokens.Dim{V: -3000000000, Unit: "px"}, "(-3000000000.0).dp"},
		{tokens.Dim{V: 200000000, Unit: "rem"}, "3200000000.0.dp"},
		{tokens.Dim{V: 1.5, Unit: "px"}, "1.5.dp"},
		{tokens.Dim{V: 1e15, Unit: "px"}, "1000000000000000.0.dp"},
	} {
		got, err := kotlinDp(c.d)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, "%v", c.d)
	}
}

// material3FS is the android fixture with its material3Scheme mapping
// replaced by scheme.
func material3FS(t *testing.T, scheme string) project.MemFS {
	t.Helper()
	return androidFS(t, func(y string) string {
		const fixture = "material3Scheme: {primary: accent, onPrimary: button-ink, tertiary: highlight}"
		require.Contains(t, y, fixture)
		return strings.Replace(y, fixture, "material3Scheme: "+scheme, 1)
	})
}

// kotlinFailure is the emit diagnostic for the android platform of fsys,
// which must fail.
func kotlinFailure(t *testing.T, fsys project.MemFS) diag.Diagnostic {
	t.Helper()
	p := loadFS(t, fsys)
	_, err := Kotlin(p, androidPlatform(t, p))
	require.Error(t, err)
	return failure(androidPlatform(t, p), "kotlin", err)
}

// TestMaterial3Golden: with material3Scheme set, each app's values object
// ends with lightScheme and darkScheme, lightColorScheme and
// darkColorScheme with the named roles in role-list order, each role the
// colour its token has in that app and appearance.
func TestMaterial3Golden(t *testing.T) {
	out := androidKotlin(t)
	slot := func(file, val, name string) string {
		m := regexp.MustCompile(`(?m)^        ` + name + ` = (.+),$`).FindStringSubmatch(block(t, file, val))
		require.NotNil(t, m, "no %s in %s", name, val)
		return m[1]
	}
	for _, f := range []string{notesKotlin, tasksKotlin} {
		s := string(out[f])
		for _, imp := range []string{"androidx.compose.material3.ColorScheme", "androidx.compose.material3.darkColorScheme",
			"androidx.compose.material3.lightColorScheme"} {
			assert.Contains(t, s, "\nimport "+imp+"\n", f)
		}
		for _, av := range []string{"light", "dark"} {
			want := fmt.Sprintf("\n    public val %sScheme: ColorScheme = %sColorScheme(\n"+
				"        primary = %s,\n        onPrimary = %s,\n        tertiary = %s,\n    )\n",
				av, av, slot(s, av, "accent"), slot(s, av, "buttonInk"), slot(s, av, "highlight"))
			assert.Contains(t, s, want, f)
		}
		assert.Less(t, strings.Index(s, "public val dark: DesignColors"), strings.Index(s, "public val lightScheme:"), f)
		assert.Less(t, strings.Index(s, "public val lightScheme:"), strings.Index(s, "public val darkScheme:"), f)
		assert.True(t, strings.HasSuffix(s, "    )\n}\n"), f)
	}
	assert.NotEqual(t, block(t, string(out[notesKotlin]), "lightScheme"), block(t, string(out[tasksKotlin]), "lightScheme"))
	assert.NotContains(t, string(out[sharedKotlin]), "material3")

	// Roles come out in role-list order whatever order the YAML gives.
	s := string(kotlinOf(t, loadFS(t, material3FS(t, "{surface: banner, primary: accent, onSurface: ink}")))[notesKotlin])
	assert.Regexp(t, `lightColorScheme\(\n        primary = .+,\n        surface = .+,\n        onSurface = .+,\n    \)`, s)

	// Without material3Scheme there is no scheme and no material3 import.
	plain := string(kotlinOf(t, loadFS(t, androidFS(t, func(y string) string {
		return regexp.MustCompile(`(?m)^      material3Scheme: .*\n`).ReplaceAllString(y, "")
	})))[notesKotlin])
	assert.NotContains(t, plain, "Scheme")
	assert.NotContains(t, plain, "material3")
}

// TestMaterial3RoleVaries (Review Focus 1): surface varies on density, so
// it has no single value for a role and is emit-failed, not given its
// base; a role token varying per app alone is fine (accent).
func TestMaterial3RoleVaries(t *testing.T) {
	d := kotlinFailure(t, material3FS(t, "{primary: accent, surface: surface}"))
	assert.Equal(t, "emit-failed", d.Rule)
	assert.Contains(t, d.Message, "material3Scheme role surface")
	assert.Contains(t, d.Message, `"surface"`)
	assert.Contains(t, d.Message, "density")
}

// TestMaterial3BadRole: a role naming an unknown token, a non-colour one
// (a dimension, a shadow), or a colour not emitted to kotlin is
// emit-failed naming the role and token.
func TestMaterial3BadRole(t *testing.T) {
	for scheme, want := range map[string][]string{
		"{primary: nowhere}":    {"material3Scheme role primary", `"nowhere"`, "no such token"},
		"{primary: radius}":     {"material3Scheme role primary", `"radius"`, "dimension"},
		"{scrim: elevation}":    {"material3Scheme role scrim", `"elevation"`, "shadow"},
		"{primary: brand}":      {"material3Scheme role primary", `"brand"`, "is not emitted to kotlin"},
		"{onSurface: gray-800}": nil,
	} {
		fsys := material3FS(t, scheme)
		// brand is a colour emitted to css alone.
		fsys["design-system/tokens/brand.tokens.json"] = `{ "brand": { "$type": "color",
  "$value": { "colorSpace": "oklch", "components": [0.6, 0.15, 255] },
  "$extensions": { "com.c65llc.airframe": { "tier": "semantic", "emit": ["css"] } } } }`
		if want == nil {
			kotlinOf(t, loadFS(t, fsys))
			continue
		}
		d := kotlinFailure(t, fsys)
		assert.Equal(t, "emit-failed", d.Rule, scheme)
		for _, w := range want {
			assert.Contains(t, d.Message, w, scheme)
		}
	}
}

// TestMaterial3Appearance: an appearance value other than light and dark
// has no Material 3 scheme and is emit-failed naming it; with no
// appearance axis there is lightScheme alone, after colors.
func TestMaterial3Appearance(t *testing.T) {
	fsys := memFS(t, androidDir)
	fsys["airframe.yaml"] = strings.Replace(fsys["airframe.yaml"], "appearance: {values: [light, dark],", "appearance: {values: [light, dark, dim],", 1)
	d := kotlinFailure(t, fsys)
	assert.Equal(t, "emit-failed", d.Rule)
	assert.Contains(t, d.Message, `appearance value "dim"`)
	assert.Contains(t, d.Message, "material3Scheme")

	s := string(kotlinOf(t, loadFS(t, noAppearanceFS(t)))[notesKotlin])
	assert.Contains(t, s, "\n    public val lightScheme: ColorScheme = lightColorScheme(\n        primary = ")
	assert.NotContains(t, s, "darkScheme")
	assert.NotContains(t, s, "darkColorScheme")
	assert.Less(t, strings.Index(s, "public val colors: DesignColors"), strings.Index(s, "public val lightScheme:"))
}

// TestGuideKotlinCompiles: every ```kotlin block in docs/guides/android.md
// is a complete file that compiles with kotlinc against the android
// fixture's goldens (the shared file and one app's values, for each app)
// and the Compose stubs, so the guide cannot drift from what is emitted.
// Skipped without kotlinc on PATH.
func TestGuideKotlinCompiles(t *testing.T) {
	kotlinc, err := exec.LookPath("kotlinc")
	if err != nil {
		t.Skip("kotlinc is not on PATH")
	}
	guide, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "android.md"))
	require.NoError(t, err)
	blocks := regexp.MustCompile("(?s)```kotlin\n(.*?)```").FindAllStringSubmatch(string(guide), -1)
	require.NotEmpty(t, blocks)
	stubs, err := filepath.Glob(filepath.Join("testdata", "kotlin-stubs", "*.kt"))
	require.NoError(t, err)
	require.NotEmpty(t, stubs)
	golden := kotlinGoldenFiles(t, androidDir)
	for _, app := range []string{notesKotlin, tasksKotlin} {
		t.Run(strings.Split(app, "/")[1], func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			files := []string{filepath.Join(dir, "DesignTokens.kt"), filepath.Join(dir, "DesignColorValues.kt")}
			require.NoError(t, os.WriteFile(files[0], golden[sharedKotlin], 0o644))
			require.NoError(t, os.WriteFile(files[1], golden[app], 0o644))
			for i, m := range blocks {
				f := filepath.Join(dir, fmt.Sprintf("Guide%d.kt", i))
				require.NoError(t, os.WriteFile(f, []byte(m[1]), 0o644))
				files = append(files, f)
			}
			args := append([]string{"-nowarn"}, files...)
			args = append(args, stubs...)
			b, err := exec.Command(kotlinc, append(args, "-d", filepath.Join(dir, "out"))...).CombinedOutput()
			require.NoError(t, err, "kotlinc of the guide's %d kotlin blocks:\n%s", len(blocks), b)
			t.Logf("kotlinc: %d guide blocks ok%s", len(blocks), strings.TrimRight("\n"+string(b), "\n"))
		})
	}
}

// TestGuideAndroidConfig: the guide's §1 airframe.yaml, over the android
// fixture's tokens, builds exactly the fixture's Kotlin goldens (the
// files its kotlin blocks compile against) and every warning the guide
// quotes, so its emit-axis-fixed section names an axis §1 declares.
func TestGuideAndroidConfig(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "android.md"))
	require.NoError(t, err)
	yamls := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(guide), -1)
	require.NotEmpty(t, yamls)
	fsys := memFS(t, androidDir)
	fsys["airframe.yaml"] = "version: 1\ncontent: design-system\n" + yamls[0][1]
	out, ds := Build(loadFS(t, fsys), nil)
	var warns []string
	for _, d := range ds {
		require.NotEqual(t, diag.Error, d.Severity, "%v", d)
		warns = append(warns, d.File+": "+string(d.Severity)+" "+d.Rule+": "+d.Message)
	}
	assert.Equal(t, kotlinGoldenFiles(t, androidDir), out)
	quoted := regexp.MustCompile(`(?m)^airframe\.yaml: warn .*$`).FindAllString(string(guide), -1)
	require.NotEmpty(t, quoted)
	for _, q := range quoted {
		assert.Contains(t, warns, q)
	}
}
