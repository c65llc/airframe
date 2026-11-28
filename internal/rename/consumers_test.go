// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package rename

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/project"
)

func consumers() fs.FS { return os.DirFS("../../testdata/consumers") }

func mustCodemod(t *testing.T, fsys fs.FS, old, new string) *EditPlan {
	t.Helper()
	pl, err := Codemod(load(t, fsys), old, new, "")
	require.NoError(t, err)
	return pl
}

// edits are pl's edits as file:line:col old → new lines.
func edits(pl *EditPlan) []string {
	var out []string
	for _, e := range pl.Edits {
		out = append(out, e.File+":"+strconv.Itoa(e.Line)+":"+strconv.Itoa(e.Col)+"  "+e.Old+" → "+e.New)
	}
	return out
}

// reads are the references of the project after pl, at file, as
// raw → token.
func reads(t *testing.T, pl *EditPlan, file string) map[string]string {
	t.Helper()
	res, err := consumer.Scan(pl.After)
	require.NoError(t, err)
	out := map[string]string{}
	for _, r := range res.Refs {
		if r.File == file {
			out[r.Raw] = r.Token
		}
	}
	return out
}

func TestRenameVariants(t *testing.T) {
	// gap varies on density: XAML's DesignGapCompact follows it.
	pl := mustCodemod(t, consumers(), "gap", "spacing")
	assert.Equal(t, []string{
		"airframe.yaml:13:65  gap → spacing",
		"android/src/main/Screen.kt:11:31  gap → spacing",
		"apple/Sources/ContentView.swift:8:35  gap → spacing",
		"web/src/app.css:12:15  --gap → --spacing",
		"windows/App/MainPage.xaml:2:77  DesignGapCompact → DesignSpacingCompact",
	}, edits(pl))
	assert.Contains(t, string(pl.Consumers["windows/App/MainPage.xaml"]), `Padding="{StaticResource DesignSpacingCompact}"`)
	assert.Contains(t, string(pl.Consumers["apple/Sources/ContentView.swift"]), ".padding(DesignTokens.spacing)\n")
	assert.Contains(t, string(pl.Consumers["web/src/app.css"]), "margin: var(--spacing, var(--hairline, 4px));\n")
	assert.Contains(t, string(pl.Consumers["android/src/main/Screen.kt"]), "val gap = LocalTokens.current.spacing\n")
	assert.Equal(t, "spacing", reads(t, pl, "windows/App/MainPage.xaml")["{StaticResource DesignSpacingCompact}"])

	// surface has an appearance mode: Kotlin's ColorValues member follows it,
	// and so does the alias in button-ink.
	pl = mustCodemod(t, consumers(), "surface", "canvas")
	assert.Equal(t, []string{
		"android/src/main/Screen.kt:7:42  surface → canvas",
		"design-system/tokens/component.tokens.json:10:18  {surface} → {canvas}",
		"web/src/app.css:8:19  --surface → --canvas",
		"windows/App/MainPage.xaml:2:36  DesignSurface → DesignCanvas",
	}, edits(pl))
	assert.Contains(t, string(pl.Consumers["android/src/main/Screen.kt"]), "val darkSurface = DesignColorValues.dark.canvas\n")
	assert.Contains(t, string(pl.Consumers["windows/App/MainPage.xaml"]), `"{ThemeResource DesignCanvas}"`)
	// Edits are exact splices: every other byte is as it was.
	src, err := fs.ReadFile(consumers(), "web/src/app.css")
	require.NoError(t, err)
	assert.Equal(t, strings.Replace(string(src), "var(--surface,", "var(--canvas,", 1), string(pl.Consumers["web/src/app.css"]))
	assert.Equal(t, src, pl.ConsumerSources["web/src/app.css"])
}

// A Kotlin colour slot with a runtime suffix, and two edits of different
// lengths on one line.
func TestRenameVariantsKotlinSlot(t *testing.T) {
	fsys := project.MemFS{
		"airframe.yaml": `version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ":root[data-theme='dark']"}}
  density: {values: [compact, standard], base: standard, binding: runtime, web: {attr: data-density}}
platforms:
  android:
    emit: [kotlin]
    out: {kotlin: android/generated/DesignTokens.kt}
    consumers: ["android/src/**/*.kt"]
    options: {package: com.example.design}
`,
		"design-system/tokens/base.tokens.json": `{
  "surface": {"$type": "color", "$value": {"colorSpace": "oklch", "components": [0.98, 0.004, 250]},
    "$extensions": {"com.c65llc.airframe": {"tier": "semantic", "modes": {
      "appearance:dark": {"colorSpace": "oklch", "components": [0.2, 0.004, 250]},
      "density:compact": {"colorSpace": "oklch", "components": [0.9, 0.004, 250]}}}}}
}`,
		"android/src/A.kt": "val a = DesignColorValues.light.surfaceCompact\nval b = DesignColorValues.dark.surface; val c = DesignTokens.surface\n",
	}
	pl := mustCodemod(t, fsys, "surface", "canvas")
	assert.Equal(t, "val a = DesignColorValues.light.canvasCompact\nval b = DesignColorValues.dark.canvas; val c = DesignTokens.canvas\n",
		string(pl.Consumers["android/src/A.kt"]))
}

func TestRenameWrappers(t *testing.T) {
	t.Run("map", func(t *testing.T) {
		// web's theme.space maps space4 to gap: only the config changes.
		pl := mustCodemod(t, consumers(), "gap", "spacing")
		assert.NotContains(t, pl.Consumers, "web/src/button/Button.tsx")
		assert.Contains(t, string(pl.Files["airframe.yaml"]), "space4: spacing")
		assert.Equal(t, "spacing", reads(t, pl, "web/src/button/Button.tsx")["theme.space.space4"])
	})
	t.Run("generated", func(t *testing.T) {
		// tokens (web, css names) and Theme (apple, swift names) are generated.
		pl := mustCodemod(t, consumers(), "accent", "brand")
		assert.Contains(t, string(pl.Consumers["web/src/page.tsx"]), "{tokens.brand}")
		assert.Contains(t, string(pl.Consumers["apple/Sources/ContentView.swift"]), ".tint(Theme.brand)")
		assert.Contains(t, string(pl.Consumers["windows/App/MainPage.xaml.cs"]), `Resources["DesignBrand"]`)
		assert.Equal(t, "brand", reads(t, pl, "web/src/page.tsx")["tokens.brand"])
		assert.Empty(t, pl.Manual)
	})
	t.Run("pattern", func(t *testing.T) {
		// theme.space.space2 reads space-2 through ^space(\d+)$: a manual item
		// where it is read, besides the config-level one for the pattern. No
		// read resolves through apple's AppTheme.spacing pattern to space-2
		// (airframe.yaml:24), so that one is dropped.
		pl := mustCodemod(t, consumers(), "space-2", "space-two")
		assert.NotContains(t, pl.Consumers, "web/src/button/Button.tsx")
		var at []string
		for _, m := range pl.Manual {
			at = append(at, m.File+":"+strconv.Itoa(m.Line))
		}
		assert.Equal(t, []string{"airframe.yaml:14", "web/src/button/Button.tsx:9"}, at)
		assert.Contains(t, pl.Manual[1].Message, "theme.space.space2 reads space-2 through a pattern of wrapper theme.space")

		// A pattern that can make new is kept with no read: space-4 → space-8
		// could now be read as space8 through both patterns.
		pl = mustCodemod(t, consumers(), "gap", "space-8")
		var msgs []string
		for _, m := range pl.Manual {
			msgs = append(msgs, m.File+":"+strconv.Itoa(m.Line)+" "+m.Message[:strings.Index(m.Message, ":")])
		}
		assert.Equal(t, []string{"airframe.yaml:14 platform web", "airframe.yaml:24 platform apple"}, msgs)
	})
}

// staleFS serves name's bytes as they are for the first after reads, and
// changed by edit on every later one: a file edited between the scan and
// the splice.
type staleFS struct {
	fs.FS
	name         string
	reads, after int
	edit         func([]byte) []byte
}

func (s *staleFS) Open(name string) (fs.File, error) {
	f, err := s.FS.Open(name)
	if err != nil || name != s.name {
		return f, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if s.reads++; s.reads > s.after {
		b = s.edit(b)
	}
	return &memFile{Reader: bytes.NewReader(b), name: name, size: int64(len(b))}, nil
}

type memFile struct {
	*bytes.Reader
	name string
	size int64
}

func (m *memFile) Stat() (fs.FileInfo, error) { return m, nil }
func (m *memFile) Close() error               { return nil }
func (m *memFile) Name() string               { return m.name[strings.LastIndex(m.name, "/")+1:] }
func (m *memFile) Size() int64                { return m.size }
func (m *memFile) Mode() fs.FileMode          { return 0o644 }
func (m *memFile) ModTime() time.Time         { return time.Time{} }
func (m *memFile) IsDir() bool                { return false }
func (m *memFile) Sys() any                   { return nil }

func TestRenameStaleSplice(t *testing.T) {
	for name, edit := range map[string]func([]byte) []byte{
		// the line shifted right: the bytes at the column are no longer --gap
		"shifted": func(b []byte) []byte {
			return bytes.Replace(b, []byte("margin: var(--gap"), []byte("margin:  var(--gap"), 1)
		},
		// the reference itself changed
		"renamed": func(b []byte) []byte { return bytes.Replace(b, []byte("var(--gap,"), []byte("var(--gab,"), 1) },
	} {
		t.Run(name, func(t *testing.T) {
			// Count the reads of a clean run: the splice's is the last, as the
			// validating scan reads the file from the overlay.
			clean := &staleFS{FS: consumers(), name: "web/src/app.css", after: 1 << 30}
			mustCodemod(t, clean, "gap", "spacing")
			fsys := &staleFS{FS: consumers(), name: "web/src/app.css", after: clean.reads - 1, edit: edit}
			_, err := Codemod(load(t, fsys), "gap", "spacing", "")
			var r *Refusal
			require.True(t, errors.As(err, &r), "%v", err)
			assert.Contains(t, r.Msg, "web/src/app.css:12:15 no longer reads --gap")
		})
	}

	// Directly: a splice whose bytes do not match changes nothing.
	src := []byte("a { margin: var(--gap); }\n")
	e := consumerEdit{Edit: Edit{File: "x.css", Line: 1, Col: 17, Old: "--gap", New: "--spacing"}, Raw: "--gap", RefCol: 17}
	out, err := splice("x.css", src, []consumerEdit{e})
	require.NoError(t, err)
	assert.Equal(t, "a { margin: var(--spacing); }\n", string(out))
	e.Col, e.RefCol = 16, 16
	_, err = splice("x.css", src, []consumerEdit{e})
	assert.ErrorAs(t, err, new(*Refusal))
	assert.Equal(t, "a { margin: var(--gap); }\n", string(src))
}

func TestRenameValidation(t *testing.T) {
	// space2 is a token name of its own, but every native emitter spells it
	// as space-2 already is: the renamed project would not build.
	_, err := Codemod(load(t, consumers()), "gap", "space2", "")
	var r *Refusal
	require.True(t, errors.As(err, &r), "%v", err)
	assert.Contains(t, r.Msg, "the renamed project does not build")
	var msgs []string
	for _, d := range r.Diagnostics {
		msgs = append(msgs, d.Message)
	}
	assert.Contains(t, strings.Join(msgs, "\n"), `tokens "space-2" and "space2" both make XAML key DesignSpace2`)

	// A project that does not build is refused before anything is planned.
	fsys := project.MemFS{}
	for _, f := range []string{"airframe.yaml", "design-system/tokens/semantic.tokens.json", "design-system/tokens/primitive.tokens.json"} {
		b, err := fs.ReadFile(consumers(), f)
		require.NoError(t, err)
		fsys[f] = string(b)
	}
	fsys["design-system/tokens/extra.tokens.json"] = `{"space2": {"$type": "dimension", "$value": {"value": 2, "unit": "px"}, "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}}`
	_, err = Codemod(load(t, fsys), "gap", "spacing", "")
	require.True(t, errors.As(err, &r), "%v", err)
	assert.Equal(t, "rename: the project does not build; fix it first", r.Msg)
}
