// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
)

var (
	swiftMemberRE  = regexp.MustCompile(`(?m)^\s*public (?:static let|var) (` + "`?" + `[A-Za-z_]\w*` + "`?" + `)`)
	kotlinValRE    = regexp.MustCompile(`(?m)^\s*public val (` + "`?" + `[A-Za-z_]\w*` + "`?" + `)`)
	kotlinSlotRE   = regexp.MustCompile(`(?m)^\s+(` + "`?" + `[A-Za-z_]\w*` + "`?" + `) = Color\(`)
	kotlinBlockRE  = regexp.MustCompile(`(?s)= \w+Colors\(\n(.*?)\n\s*\)\n`)
	xamlKeyAttrRE  = regexp.MustCompile(`x:Key="([^"]+)"`)
	cssPropertyRE  = regexp.MustCompile(`(?m)^\s*(--[A-Za-z0-9_-]+):`)
	refnamesGolden = []struct {
		name, dir, platform, format string
		fsys                        func(*testing.T) fs.FS // nil: dir as it is
		files                       []string               // goldens under dir
		names                       func(string) []string
	}{
		{"apple", appleDir, "apple", "swift", nil, []string{"ios/Shared/DesignTokens.swift"}, swiftNames},
		{"apple-per-app", appleDir, "apple", "swift", func(t *testing.T) fs.FS { return perAppFS(t) },
			[]string{"swift/notes/DesignTokens.swift", "swift/tasks/DesignTokens.swift"}, swiftNames},
		{"android-shared", androidDir, "android", "kotlin", nil, []string{sharedKotlin}, func(s string) []string { return all(kotlinValRE, s) }},
		{"android-values", androidDir, "android", "kotlin", nil, []string{notesKotlin, tasksKotlin}, kotlinSlotNames},
		{"windows", windowsDir, "windows", "xaml", nil,
			[]string{"windows/notes/DesignTokens.xaml", "windows/tasks/DesignTokens.xaml"}, func(s string) []string { return all(xamlKeyAttrRE, s) }},
		{"xaml-default", xamlDefaultDir, "windows", "xaml", nil, []string{"windows/DesignTokens.xaml"}, func(s string) []string { return all(xamlKeyAttrRE, s) }},
		{"css", minimalDir, "web", "css", nil, []string{minimalCSS}, func(s string) []string { return all(cssPropertyRE, s) }},
	}
)

func all(re *regexp.Regexp, s string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

func swiftNames(s string) []string { return all(swiftMemberRE, s) }

// kotlinSlotNames are the DesignColorValues vals and the slot arguments of
// each <Type>Colors(…) in it (not a Material 3 scheme's role arguments).
func kotlinSlotNames(s string) []string {
	out := all(kotlinValRE, s)
	for _, block := range all(kotlinBlockRE, s) {
		out = append(out, all(kotlinSlotRE, block+"\n")...)
	}
	return out
}

func platformNamed(t *testing.T, p *project.Project, name string) config.Platform {
	t.Helper()
	for _, pl := range p.Config.Platforms {
		if pl.Name == name {
			return pl
		}
	}
	t.Fatalf("no platform %s", name)
	return config.Platform{}
}

func refNames(t *testing.T, dir, platform, format string) (*project.Project, map[string]string) {
	t.Helper()
	return refNamesFS(t, os.DirFS(dir), platform, format)
}

func refNamesFS(t *testing.T, fsys fs.FS, platform, format string) (*project.Project, map[string]string) {
	t.Helper()
	p := loadFS(t, fsys)
	names, err := ReferenceNames(p, platformNamed(t, p, platform), format)
	require.NoError(t, err)
	return p, names
}

func TestReferenceNamesMatchEmitted(t *testing.T) {
	for _, g := range refnamesGolden {
		t.Run(g.name, func(t *testing.T) {
			fsys := os.DirFS(g.dir)
			if g.fsys != nil {
				fsys = g.fsys(t)
			}
			p, names := refNamesFS(t, fsys, g.platform, g.format)
			for _, f := range g.files {
				b, err := os.ReadFile(filepath.Join(g.dir, filepath.FromSlash(f)))
				require.NoError(t, err)
				got := g.names(string(b))
				require.NotEmpty(t, got, "%s: the regex found no names", f)
				for _, n := range got {
					tok, ok := names[n]
					if !assert.True(t, ok, "%s: %s is emitted but not in ReferenceNames", f, n) {
						continue
					}
					if tok != "" {
						assert.Contains(t, p.Tokens.Tokens, tok, "%s: %s maps to %q, which is no token", f, n, tok)
					}
				}
			}
		})
	}
}

func TestReferenceNamesTokens(t *testing.T) {
	_, xaml := refNames(t, windowsDir, "windows", "xaml")
	assert.Equal(t, "gap", xaml["DesignGapCompact"])
	assert.Equal(t, "gap", xaml["DesignGap"])
	assert.Equal(t, "card-shadow", xaml["DesignCardShadowLayer1OffsetX"], "a shadow part key")
	assert.Equal(t, "card-shadow", xaml["DesignCardShadowLayer1"], "a shadow layer brush")
	assert.Equal(t, "", xaml["Light"])
	assert.Contains(t, xaml, "Light", "a theme dictionary key is generated, not a token")

	_, kt := refNames(t, androidDir, "android", "kotlin")
	assert.Equal(t, "surface", kt["surfaceCompact"], "a Kotlin colour slot")
	assert.Equal(t, "card-shadow", kt["cardShadowLayer1"], "a Kotlin shadow colour slot")
	assert.Equal(t, "gap", kt["gap"])
	assert.Equal(t, "blue-500", kt["blue500"])
	for _, n := range []string{"colors", "Density", "Compact", "Appearance", "Shadow", "DesignColors", "DesignColorValues", "light", "dark", "lightScheme", "Companion"} {
		v, ok := kt[n]
		assert.True(t, ok && v == "", "kotlin %s is a generated non-token symbol (%q, %v)", n, v, ok)
	}

	_, sw := refNames(t, appleDir, "apple", "swift")
	assert.Equal(t, "default", sw["`default`"])
	assert.Equal(t, "default", sw["default"], "a backticked member also matches unescaped")
	assert.Equal(t, "gap", sw["gap"])
	for _, n := range []string{"Density", "compact", "density", "Bezier", "Shadow", "init", "DesignTokens"} {
		v, ok := sw[n]
		assert.True(t, ok && v == "", "swift %s is a generated non-token symbol (%q, %v)", n, v, ok)
	}

	_, css := refNames(t, minimalDir, "web", "css")
	assert.Equal(t, "accent-ink", css["--accent-ink"])
}

// ReferenceNames fails exactly as the format's emitter does.
func TestReferenceNamesEmitErrors(t *testing.T) {
	p := loadFS(t, os.DirFS(appleDir))
	pl := platformNamed(t, p, "apple")
	pl.Emit = []string{"swift"} // Swift colours need colorsets
	_, want := Swift(p, pl)
	require.Error(t, want)
	_, got := ReferenceNames(p, pl, "swift")
	assert.Equal(t, want, got)

	p = loadFS(t, os.DirFS(androidDir))
	pl = platformNamed(t, p, "android")
	pl.Options = maps.Clone(pl.Options)
	delete(pl.Options, "sharedOut")
	_, want = Kotlin(p, pl)
	require.Error(t, want)
	_, got = ReferenceNames(p, pl, "kotlin")
	assert.Equal(t, want, got)

	p = loadFS(t, os.DirFS(windowsDir))
	pl = platformNamed(t, p, "windows")
	pl.Out = map[string]string{"xaml": "windows/{channel}/DesignTokens.xaml"}
	_, want = XAML(p, pl)
	require.Error(t, want)
	_, got = ReferenceNames(p, pl, "xaml")
	assert.Equal(t, want, got)

	_, err := ReferenceNames(p, pl, "json")
	assert.ErrorContains(t, err, "json")
}

func TestNamingAffixes(t *testing.T) {
	cfg := &config.Config{}
	for _, tc := range []struct{ format, prefix, suffix string }{
		{"css", "--", ""},
		{"swift", "", ""},
		{"kotlin", "", ""},
		{"xaml", "Design", ""},
	} {
		pre, suf := NamingAffixes(cfg, tc.format)
		assert.Equal(t, tc.prefix, pre, tc.format)
		assert.Equal(t, tc.suffix, suf, tc.format)
	}
	cfg.Naming = map[string]string{"xaml": "App{Pascal}Res", "swift": "Theme.tok_{camel}", "kotlin": "AppTokens.{name}X"}
	for _, tc := range []struct{ format, prefix, suffix string }{
		{"xaml", "App", "Res"},
		{"swift", "tok_", ""},
		{"kotlin", "", "X"},
	} {
		pre, suf := NamingAffixes(cfg, tc.format)
		assert.Equal(t, tc.prefix, pre, tc.format)
		assert.Equal(t, tc.suffix, suf, tc.format)
	}
}
