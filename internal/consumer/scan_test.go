// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package consumer

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/project"
)

var update = flag.Bool("update", false, "rewrite testdata/consumers/scan.golden.json")

const fixtureDir = "../../testdata/consumers"

// fixture is testdata/consumers as a MemFS. With consumers false it holds
// only airframe.yaml and the content root, so a test can add its own
// consumer files; files then replace or add entries.
func fixture(t *testing.T, consumers bool, files map[string]string) project.MemFS {
	t.Helper()
	m := project.MemFS{}
	require.NoError(t, fs.WalkDir(os.DirFS(fixtureDir), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "scan.golden.json" {
			return err
		}
		if !consumers && p != "airframe.yaml" && !strings.HasPrefix(p, "design-system/") {
			return nil
		}
		b, err := os.ReadFile(filepath.Join(fixtureDir, filepath.FromSlash(p)))
		m[p] = string(b)
		return err
	}))
	for k, v := range files {
		m[k] = v
	}
	return m
}

func scan(t *testing.T, fsys fs.FS) Result {
	t.Helper()
	p, ds, err := project.Load(fsys)
	require.NoError(t, err)
	require.Empty(t, ds)
	res, err := Scan(p)
	require.NoError(t, err)
	return res
}

func refsIn(res Result, file string) []Reference {
	var out []Reference
	for _, r := range res.Refs {
		if r.File == file {
			out = append(out, r)
		}
	}
	return out
}

func problemsIn(res Result, file string) []Problem {
	var out []Problem
	for _, p := range res.Problems {
		if p.File == file {
			out = append(out, p)
		}
	}
	return out
}

func raws[T any](xs []T, f func(T) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

func TestScanGolden(t *testing.T) {
	res := scan(t, os.DirFS(fixtureDir))
	got, err := json.MarshalIndent(res, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	golden := filepath.Join(fixtureDir, "scan.golden.json")
	if *update {
		require.NoError(t, os.WriteFile(golden, got, 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))

	// Generated outputs and the content root are never scanned.
	for _, r := range res.Refs {
		assert.NotContains(t, r.File, "generated/", r.Raw)
		assert.NotContains(t, r.File, "Generated/", r.Raw)
		assert.False(t, strings.HasPrefix(r.File, "design-system/"), r.Raw)
	}
	assert.Equal(t, map[string]int{"android": 2, "apple": 2, "web": 3, "windows": 3}, res.Files)
}

func TestCSSLocalProperty(t *testing.T) {
	res := scan(t, fixture(t, false, map[string]string{
		"web/src/a.css": ".a { --sidebar-w: 240px; width: var(--sidebar-w); color: var(--accnet); }\n",
		"web/src/b.tsx": `const s = { "--card-pad": "12px", '--quoted' : 1 };` + "\n",
		"web/src/c.css": ".c { padding: var(--card-pad) var(--quoted); margin: var(--accent); }\n",
	}))
	assert.Equal(t, []string{"--accnet"}, raws(problemsIn(res, "web/src/a.css"), func(p Problem) string { return p.Raw }))
	assert.Equal(t, Undeclared, problemsIn(res, "web/src/a.css")[0].Kind)
	assert.Equal(t, []Reference{{Platform: "web", File: "web/src/a.css", Line: 1, Col: 62, Raw: "--accnet", Via: Direct}}, refsIn(res, "web/src/a.css"))
	assert.Empty(t, problemsIn(res, "web/src/c.css"), "properties defined in another consumer file are app-local")
	assert.Equal(t, []Reference{{Platform: "web", File: "web/src/c.css", Line: 1, Col: 58, Raw: "--accent", Token: "accent", Via: Direct}}, refsIn(res, "web/src/c.css"))
}

func TestForeignAndGeneratedSymbols(t *testing.T) {
	res := scan(t, fixture(t, false, map[string]string{
		"apple/Sources/A.swift": strings.Join([]string{
			"let t = DesignTokens(density: .compact)",
			"let d = DesignTokens.Density.compact",
			"let s = DesignTokens.Shadow.self",
			"let m = MyDesignTokens.accent",
			"let a = DesignTokens.accent",
			"let x = DesignTokens.acent",
		}, "\n") + "\n",
		"windows/App/A.xaml": strings.Join([]string{
			`<Grid Foreground="{ThemeResource SystemControlForegroundBaseHighBrush}"`,
			`  Background="{StaticResource DesignSurface}" BorderBrush="{StaticResource DesignSurfce}"`,
			`  Tag="{StaticResource Light}" />`,
		}, "\n") + "\n",
		"windows/App/A.cs": `var a = Resources["SystemAccentColor"]; var b = Resources["DesignGapCompact"];` + "\n",
		"android/src/A.kt": strings.Join([]string{
			"val c = DesignTokens.Companion",
			"val d = DesignTokens.Density.Compact",
			"val v = DesignColorValues.dark.ink",
			"val x = DesignTokens.inkk",
		}, "\n") + "\n",
	}))
	ref := func(r Reference) string { return r.Raw + "=" + r.Token }
	assert.Equal(t, []string{"DesignTokens.accent=accent", "DesignTokens.acent="}, raws(refsIn(res, "apple/Sources/A.swift"), ref))
	assert.Equal(t, []string{"DesignTokens.acent"}, raws(problemsIn(res, "apple/Sources/A.swift"), func(p Problem) string { return p.Raw }))
	assert.Equal(t, []string{"{StaticResource DesignSurface}=surface", "{StaticResource DesignSurfce}="}, raws(refsIn(res, "windows/App/A.xaml"), ref))
	assert.Len(t, problemsIn(res, "windows/App/A.xaml"), 1)
	assert.Equal(t, []string{`Resources["DesignGapCompact"]=gap`}, raws(refsIn(res, "windows/App/A.cs"), ref))
	assert.Empty(t, problemsIn(res, "windows/App/A.cs"))
	assert.Equal(t, []string{"DesignColorValues.dark.ink=ink", "DesignTokens.inkk="}, raws(refsIn(res, "android/src/A.kt"), ref))
	assert.Len(t, problemsIn(res, "android/src/A.kt"), 1)
}

func TestOurs(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, suffix string
		want                 bool
	}{
		{"DesignAccent", "Design", "", true},
		{"Design", "Design", "", false},
		{"SystemAccentColor", "Design", "", false},
		{"accent", "", "", true},
		{"AppAccentRes", "App", "Res", true},
		{"AppGapResCompact", "App", "Res", true},
		{"AppCardShadowResLayer1OffsetX", "App", "Res", true},
		{"AppResResCompact", "App", "Res", true},
		{"AppAccent", "App", "Res", false},
		{"AppRes", "App", "Res", false},
		{"AppAccentResx", "App", "Res", false},
		{"AppAccentRes2", "App", "Res", false},
		{"ApplicationRes", "App", "Res", true},
		{"MyAppAccentRes", "App", "Res", false},
	} {
		assert.Equal(t, tc.want, ours(tc.name, tc.prefix, tc.suffix), "%s with %s…%s", tc.name, tc.prefix, tc.suffix)
	}
}

func TestOursNonDefaultTemplate(t *testing.T) {
	m := fixture(t, false, map[string]string{
		"windows/App/A.xaml": strings.Join([]string{
			`<a b="{StaticResource AppGapRes}" c="{StaticResource AppGapResCompact}" />`,
			`<a b="{StaticResource AppNopeRes}" c="{StaticResource AppNopeResCompact}" />`,
			`<a b="{StaticResource AppNope}" c="{StaticResource AppNopeResx}" d="{StaticResource DesignGap}" />`,
		}, "\n") + "\n",
	})
	m["airframe.yaml"] += "naming:\n  xaml: App{Pascal}Res\n"
	res := scan(t, m)
	ref := func(r Reference) string { return r.Raw + "=" + r.Token }
	assert.Equal(t, []string{
		"{StaticResource AppGapRes}=gap", "{StaticResource AppGapResCompact}=gap",
		"{StaticResource AppNopeRes}=", "{StaticResource AppNopeResCompact}=",
	}, raws(refsIn(res, "windows/App/A.xaml"), ref))
	assert.Len(t, problemsIn(res, "windows/App/A.xaml"), 2)
}

func TestCSSFallbackNesting(t *testing.T) {
	res := scan(t, fixture(t, false, map[string]string{
		"web/src/a.css": strings.Join([]string{
			".a { box-shadow: var(--accent, rgb(0 0 0 / 0.5)); }",
			".b { margin: var(--gap, var(--hairline, 4px)); }",
			".c { margin: var( --gap , calc(1px + (2px * 3)) ) var(--hairline,1px); }",
			".d { font: var(--ink, \"a)b\"); width: myvar(--nope); x: var(--gap,); }",
		}, "\n") + "\n",
	}))
	ref := func(r Reference) string { return r.Raw + "=" + r.Token }
	assert.Equal(t, []string{
		"--accent=accent", "--gap=gap", "--hairline=hairline", "--gap=gap", "--hairline=hairline", "--ink=ink", "--gap=gap",
	}, raws(refsIn(res, "web/src/a.css"), ref))
	ps := problemsIn(res, "web/src/a.css")
	assert.Equal(t, []string{"rgb(0 0 0 / 0.5)", "4px", "calc(1px + (2px * 3))", "1px", `"a)b"`},
		raws(ps, func(p Problem) string { return p.Raw }))
	for _, p := range ps {
		assert.Equal(t, FallbackLiteral, p.Kind)
	}
	assert.Equal(t, [2]int{2, 41}, [2]int{ps[1].Line, ps[1].Col}, "the inner var's fallback")
	assert.Contains(t, ps[3].Detail, "(it equals the token's base value)", "1px is hairline's value")
	assert.NotContains(t, ps[1].Detail, "equals", "4px is not hairline's value")
}

func TestWrapperPrecedence(t *testing.T) {
	res := scan(t, fixture(t, false, map[string]string{
		"web/src/a.tsx": strings.Join([]string{
			"a(theme.space.space4, theme.space.space2, theme.space.small)",
			"b(theme.space.huge, x.theme.space.small, tokens.accent, tokens.nope)",
		}, "\n") + "\n",
		"apple/Sources/A.swift": "f(AppTheme.spacing.space4, Theme.gap, Theme.Density, AppTheme.spacing.hairlineStrong)\n",
	}))
	ref := func(r Reference) string { return r.Raw + "=" + r.Token + " " + r.Via }
	assert.Equal(t, []string{
		"theme.space.space4=gap wrapper:theme.space",
		"theme.space.space2=space-2 wrapper:theme.space",
		"theme.space.small=space-2 wrapper:theme.space",
		"theme.space.huge= wrapper:theme.space",
		"tokens.accent=accent wrapper:tokens",
		"tokens.nope= wrapper:tokens",
	}, raws(refsIn(res, "web/src/a.tsx"), ref))
	assert.Equal(t, []string{"theme.space.huge", "tokens.nope"},
		raws(problemsIn(res, "web/src/a.tsx"), func(p Problem) string { return p.Raw }))
	assert.Equal(t, []string{
		"AppTheme.spacing.space4=space-4 wrapper:AppTheme.spacing",
		"Theme.gap=gap wrapper:Theme",
		"Theme.Density= wrapper:Theme",
		"AppTheme.spacing.hairlineStrong=hairline wrapper:AppTheme.spacing",
	}, raws(refsIn(res, "apple/Sources/A.swift"), ref))
	ps := problemsIn(res, "apple/Sources/A.swift")
	require.Len(t, ps, 1)
	assert.Equal(t, WrapperUnmapped, ps[0].Kind)
	assert.Contains(t, ps[0].Detail, "not a token")
}

func TestWrapperMapsMissingToken(t *testing.T) {
	m := fixture(t, false, map[string]string{"web/src/a.tsx": "f(theme.space.gone, theme.space.space9)\n"})
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"], "space4: gap}", "space4: gap, gone: no-such-token}", 1)
	res := scan(t, m)
	assert.Equal(t, []string{
		"wrapper-unmapped theme.space.gone: wrapper theme.space's map entry gone maps gone to no-such-token, which is not a token",
		`wrapper-unmapped theme.space.space9: wrapper theme.space's pattern 1 (^space(\d+)$) maps space9 to space-9, which is not a token`,
	}, raws(problemsIn(res, "web/src/a.tsx"), func(p Problem) string { return p.Kind + " " + p.Detail }))
}

func TestExemptionMarkers(t *testing.T) {
	res := scan(t, fixture(t, false, map[string]string{
		"web/src/a.css": strings.Join([]string{
			"/* airframe-exempt fallback-literal: legacy theme */",
			".a { margin: var(--gap, 4px); }",
			".b { margin: var(--gap, 4px); } /* airframe-exempt fallback-literal */",
			".c { margin: var(--gap, 4px); } /* airframe-exempt fallback-literal:   */",
			"/* airframe-exempt primitive-read-by-consumer: one-off airframe-exempt token-owner-violation: shared */",
			".d { color: var(--blue-500); }",
			"/* airframe-exempt fallback-literal: no reference here */",
			".e { }",
		}, "\n") + "\n",
	}))
	assert.Equal(t, []Exemption{
		{Rule: "fallback-literal", Platform: "web", File: "web/src/a.css", Line: 2, MarkerLine: 1, Reason: "legacy theme"},
		{Rule: "fallback-literal", Platform: "web", File: "web/src/a.css", Line: 3, MarkerLine: 3, Reason: ""},
		{Rule: "fallback-literal", Platform: "web", File: "web/src/a.css", Line: 4, MarkerLine: 3, Reason: ""},
		{Rule: "fallback-literal", Platform: "web", File: "web/src/a.css", Line: 4, MarkerLine: 4, Reason: ""},
		{Rule: "primitive-read-by-consumer", Platform: "web", File: "web/src/a.css", Line: 6, MarkerLine: 5, Reason: "one-off"},
		{Rule: "token-owner-violation", Platform: "web", File: "web/src/a.css", Line: 6, MarkerLine: 5, Reason: "shared"},
	}, res.Exempt)
	// Scan suppresses nothing itself: the rules decide.
	assert.Len(t, problemsIn(res, "web/src/a.css"), 3)
}

func TestUnadaptedExtension(t *testing.T) {
	m := fixture(t, false, map[string]string{
		"web/src/a.css":          ".a { color: var(--accent); }\n",
		"web/src/notes.md":       "theme.space.small\n",
		"web/src/Makefile":       "x\n",
		"apple/Sources/A.swift":  "let a = DesignTokens.accent\n",
		"apple/Sources/b.kt":     "val a = DesignTokens.accent\n",
		"windows/App/A.xaml":     `<a b="{StaticResource DesignAccent}" />` + "\n",
		"android/src/A.kt":       "val a = DesignTokens.accent\n",
		"android/src/legacy.rb":  "x\n",
		"android/src/sub/b.java": "x\n",
	})
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"], `"web/src/**/*.tsx"`, `"web/src/**/*.tsx", "web/src/*.md", "web/src/Makefile"`, 1)
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"], `consumers: ["apple/**/*.swift"]`, `consumers: ["apple/**/*.swift", "apple/**/*.kt"]`, 1)
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"], `consumers: ["android/src/**/*.kt"]`, `consumers: ["android/src/**"]`, 1)
	res := scan(t, m)
	var got []string
	for _, p := range res.Problems {
		if p.Kind == SchemaInvalid {
			assert.Equal(t, "airframe.yaml", p.File)
			got = append(got, p.Detail)
		}
	}
	assert.Equal(t, []string{
		"platform android: no adapter reads 2 consumer file(s): android/src/legacy.rb (no adapter reads .rb), android/src/sub/b.java (no adapter reads .java) (platforms.android.consumers)",
		"platform apple: no adapter reads 1 consumer file(s): apple/Sources/b.kt (.kt is read through kotlin names, which platform apple does not emit) (platforms.apple.consumers)",
		"platform web: no adapter reads 2 consumer file(s): web/src/Makefile (no adapter reads a file with no extension), web/src/notes.md (no adapter reads .md) (platforms.web.consumers)",
	}, got)
	// An unadapted file is still read for wrappers; the adapted ones are scanned.
	assert.Equal(t, []string{"theme.space.small=space-2"}, raws(refsIn(res, "web/src/notes.md"), func(r Reference) string { return r.Raw + "=" + r.Token }))
	assert.Empty(t, refsIn(res, "apple/Sources/b.kt"))
	assert.Len(t, refsIn(res, "apple/Sources/A.swift"), 1)
	assert.Len(t, refsIn(res, "android/src/A.kt"), 1)
}

func TestEmptyScan(t *testing.T) {
	m := fixture(t, false, map[string]string{"web/src/a.css": ".a { color: var(--accent); }\n"})
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"], `"web/src/**/*.tsx"`, `"web/src/**/*.tssx", "web/[bad"`, 1)
	res := scan(t, m)
	kinds := map[string][]string{}
	for _, p := range res.Problems {
		kinds[p.Platform] = append(kinds[p.Platform], p.Kind+": "+p.Raw)
	}
	// A dead glob is reported even when another glob on the platform
	// matches; the design-system glob matches only excluded files.
	assert.Equal(t, []string{
		`empty-scan: design-system/**/*.css`, `empty-scan: web/src/**/*.tssx`, `schema-invalid: web/[bad`,
	}, kinds["web"], "web still scans a.css")
	assert.Equal(t, []string{`empty-scan: apple/**/*.swift`}, kinds["apple"])
	assert.Equal(t, []string{`empty-scan: android/src/**/*.kt`}, kinds["android"])
	assert.Equal(t, []string{`empty-scan: windows/App/**/*.cs`, `empty-scan: windows/App/**/*.xaml`}, kinds["windows"],
		"a platform matching no file gets one problem per glob")
	assert.Equal(t, map[string]int{"web": 1}, res.Files)
	assert.Len(t, refsIn(res, "web/src/a.css"), 1)
	for _, p := range res.Problems {
		if p.Kind == EmptyScan && p.Platform == "apple" {
			assert.Equal(t, "airframe.yaml", p.File)
			assert.Equal(t, `platform apple: consumers glob "apple/**/*.swift" matches no file (generated outputs and the design-system/ content root are not scanned); a declared input that is never scanned is a failure, not a pass`, p.Detail)
		}
	}
}

func TestDeadGlobBesideLiveOne(t *testing.T) {
	m := fixture(t, false, map[string]string{
		"web/src/a.css": ".a { color: var(--accent); }\n",
		"web/src/b.css": ".b { color: var(--ink); }\n",
	})
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"],
		`consumers: ["web/src/**/*.css", "web/src/**/*.tsx", "design-system/**/*.css"]`,
		`consumers: ["web/src/**/*.css", "web/src/b.css", "web/**/*.tssx"]`, 1)
	res := scan(t, m)
	var web []string
	for _, p := range res.Problems {
		if p.Platform == "web" {
			web = append(web, p.Kind+": "+p.Raw)
		}
	}
	// web/src/b.css is also matched by the first glob, but it matches a
	// file, so it is not dead.
	assert.Equal(t, []string{"empty-scan: web/**/*.tssx"}, web)
	assert.Equal(t, 2, res.Files["web"])
}

func TestNamesUnavailable(t *testing.T) {
	m := fixture(t, false, map[string]string{
		"web/src/a.css":         ".a { color: var(--accent); }\n",
		"apple/Sources/A.swift": "let a = DesignTokens.accent\nlet b = AppTheme.spacing.small\n",
		"android/src/A.kt":      "val a = DesignTokens.accent\n",
	})
	// Swift colours read colorsets: without them the swift emitter fails.
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"], "emit: [swift, colorsets]", "emit: [swift]", 1)
	m["airframe.yaml"] = strings.Replace(m["airframe.yaml"], ", colorsets: apple/Generated/Assets.xcassets/Tokens/", "", 1)
	p, ds, err := project.Load(m)
	require.NoError(t, err)
	require.Empty(t, ds)
	res, err := Scan(p)
	require.NoError(t, err, "an emitter failure is a problem, not an error")
	var apple []Problem
	for _, pr := range res.Problems {
		if pr.Platform == "apple" {
			apple = append(apple, pr)
		}
	}
	require.Len(t, apple, 1)
	assert.Equal(t, NamesUnavailable, apple[0].Kind)
	assert.Equal(t, "airframe.yaml", apple[0].File)
	assert.Equal(t, "swift", apple[0].Raw)
	assert.Contains(t, apple[0].Detail, "add colorsets to platform apple's emit")
	assert.Contains(t, apple[0].Detail, "cannot be checked")
	assert.Empty(t, refsIn(res, "apple/Sources/A.swift"), "no adapter or wrapper reads the platform")
	assert.NotContains(t, res.Files, "apple")
	assert.Len(t, refsIn(res, "web/src/a.css"), 1, "other platforms carry on")
	assert.Len(t, refsIn(res, "android/src/A.kt"), 1)
}

// reversedFS lists every directory in reverse order.
type reversedFS struct{ fs.FS }

func (r reversedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	ents, err := fs.ReadDir(r.FS, name)
	slices.Reverse(ents)
	return ents, err
}

func TestScanDeterministic(t *testing.T) {
	m := fixture(t, true, nil)
	want := scan(t, m)
	for range 3 {
		assert.Equal(t, want, scan(t, m))
	}
	assert.Equal(t, want, scan(t, reversedFS{m}))
}
