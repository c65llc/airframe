// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
)

func readFixture(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("..", "..", "testdata", "import", name))
}

// sheet parses one stylesheet that must parse cleanly.
func sheet(t *testing.T, file, src string) *css.Sheet {
	t.Helper()
	sh, ds := css.Parse(file, []byte(src))
	require.Empty(t, ds)
	return sh
}

// checkCSS imports src under cfgSrc and checks the result against src.
func checkCSS(t *testing.T, cfgSrc, src string) CheckResult {
	t.Helper()
	cfg := loadConfig(t, cfgSrc)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(src)}})
	require.Empty(t, errorsIn(r.Diagnostics), "%v", r.Diagnostics)
	return Check(cfg, []byte(cfgSrc), r.Files, []*css.Sheet{sheet(t, "theme.css", src)})
}

// editEmitted replaces the emitted CSS with edit applied to it.
func editEmitted(t *testing.T, edit func(string) string) {
	t.Helper()
	old := emitCSS
	emitCSS = func(p *project.Project, pl config.Platform) (map[string][]byte, error) {
		out, err := old(p, pl)
		for k, v := range out {
			out[k] = []byte(edit(string(v)))
		}
		return out, err
	}
	t.Cleanup(func() { emitCSS = old })
}

func TestCheckFixtureEquivalent(t *testing.T) {
	cfgData, r := fixture(t)
	require.Empty(t, r.Diagnostics)
	cfg := loadConfig(t, string(cfgData))
	cssData, err := readFixture("theme.css")
	require.NoError(t, err)
	res := Check(cfg, cfgData, r.Files, []*css.Sheet{sheet(t, "theme.css", string(cssData))})
	assert.Empty(t, res.Diagnostics)
	assert.Positive(t, res.Environments)
	assert.Equal(t, 25, res.Properties)
	assert.Equal(t, res.Environments*res.Properties, res.Comparisons)
}

func TestCheckHexEqualsEmittedOKLCH(t *testing.T) {
	res := checkCSS(t, smallConfig, ":root { --a: #3366cc; }\n")
	assert.Empty(t, res.Diagnostics)
	assert.Equal(t, 1, res.Properties)
}

func TestCheckDetectsCascadeDifference(t *testing.T) {
	cfgData, r := fixture(t)
	cfg := loadConfig(t, string(cfgData))
	cssData, err := readFixture("theme.css")
	require.NoError(t, err)
	dark := regexp.MustCompile(`(?m)^:root\[data-theme='dark'\] \{\n(?:  .*\n)*\}\n`)
	editEmitted(t, func(s string) string {
		out := dark.ReplaceAllString(s, "")
		require.NotEqual(t, s, out, "the dark block was dropped")
		return out
	})
	res := Check(cfg, cfgData, r.Files, []*css.Sheet{sheet(t, "theme.css", string(cssData))})
	got := byRule(res.Diagnostics, RuleNotEquivalent)
	require.NotEmpty(t, got)
	var surface *diag.Diagnostic
	for i := range got {
		assert.Equal(t, diag.Error, got[i].Severity)
		assert.Equal(t, diag.DocsURL(RuleNotEquivalent), got[i].Docs)
		if strings.HasPrefix(got[i].Message, "--surface ") {
			surface = &got[i]
		}
	}
	require.NotNil(t, surface, "%v", got)
	assert.Contains(t, surface.Message, "data-theme=dark")
	assert.Contains(t, surface.Message, "more environments")
	assert.Equal(t, "theme.css", surface.File)
	assert.Equal(t, 26, surface.Line, "the first declaration of --surface")
}

func TestCheckInvalidOnBothSidesIsEqual(t *testing.T) {
	src := ":root { --c: 1px; }\n"
	cycle := "\n:root {\n  --a: var(--b);\n  --b: var(--a);\n  --m: var(--zz);\n}\n"
	old := src + ":root { --a: var(--b); --b: var(--a); --m: var(--zz); }\n"
	cfg := loadConfig(t, smallConfig)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(src)}})
	require.Empty(t, r.Diagnostics)

	t.Run("both invalid", func(t *testing.T) {
		editEmitted(t, func(s string) string { return s + cycle })
		res := Check(cfg, []byte(smallConfig), r.Files, []*css.Sheet{sheet(t, "theme.css", old)})
		assert.Empty(t, res.Diagnostics)
		assert.Equal(t, 4, res.Properties)
	})
	t.Run("generated fixes it", func(t *testing.T) {
		editEmitted(t, func(s string) string { return s + "\n:root {\n  --a: 1px;\n  --b: var(--a);\n  --m: 2px;\n}\n" })
		res := Check(cfg, []byte(smallConfig), r.Files, []*css.Sheet{sheet(t, "theme.css", old)})
		got := byRule(res.Diagnostics, RuleNotEquivalent)
		require.Len(t, got, 3, "%v", res.Diagnostics)
		assert.Equal(t, "--a differs with no attributes or media features set: the original CSS gives (invalid), the generated CSS gives 1px", got[0].Message)
		assert.Equal(t, 2, got[0].Line)
	})
}

const densityConfig = `version: 1
content: ds
modes:
  density: {values: [compact, standard, relaxed], base: standard, binding: runtime, web: {attr: data-density, also: {relaxed: "@media (pointer: coarse)"}}}
platforms:
  web: {emit: css}
`

func TestCheckAlsoFallback(t *testing.T) {
	src := `:root { --gap: 8px; }
:root[data-density='relaxed'] { --gap: 12px; }
@media (pointer: coarse) {
  :root:not([data-density]) { --gap: 12px; }
}
`
	res := checkCSS(t, densityConfig, src)
	assert.Empty(t, res.Diagnostics)
	// data-density unset, relaxed or "*", by pointer unset or coarse.
	assert.Equal(t, 6, res.Environments)
}

func TestCheckGeneratedOnlyProperty(t *testing.T) {
	cfg := loadConfig(t, smallConfig)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(":root { --a: 1px; --b: 2px; }\n:root[data-theme='dark'] { --b: 3px; }\n")}})
	require.Empty(t, r.Diagnostics)
	res := Check(cfg, []byte(smallConfig), r.Files, []*css.Sheet{sheet(t, "theme.css", ":root { --a: 1px; }\n")})
	got := byRule(res.Diagnostics, RuleNotEquivalent)
	require.Len(t, got, 1, "%v", res.Diagnostics)
	assert.Equal(t, config.File, got[0].File)
	assert.Equal(t, 0, got[0].Line)
	assert.Equal(t, "--b differs with no attributes or media features set: the original CSS gives (not declared), the generated CSS gives 2px (+2 more environments)", got[0].Message)
	assert.Equal(t, 2, res.Properties)
	assert.Equal(t, 3, res.Environments, "data-theme unset, dark or another value")
}

func TestCheckCapsReports(t *testing.T) {
	var gen, old strings.Builder
	gen.WriteString(":root {\n")
	old.WriteString(":root {\n")
	for i := range 25 {
		fmt.Fprintf(&gen, "  --p-%02d: 2px;\n", i)
		fmt.Fprintf(&old, "  --p-%02d: 1px;\n", i)
	}
	gen.WriteString("}\n")
	old.WriteString("}\n")
	cfg := loadConfig(t, smallConfig)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(gen.String())}})
	require.Empty(t, r.Diagnostics)
	res := Check(cfg, []byte(smallConfig), r.Files, []*css.Sheet{sheet(t, "theme.css", old.String())})
	got := byRule(res.Diagnostics, RuleNotEquivalent)
	require.Len(t, got, 21)
	assert.True(t, strings.HasPrefix(got[0].Message, "--p-00 "), got[0].Message)
	assert.True(t, strings.HasPrefix(got[19].Message, "--p-19 "), got[19].Message)
	assert.Equal(t, "… and 5 more properties differ", got[20].Message)
	assert.Equal(t, 25, res.Properties)
}

func TestCheckNoCSSPlatform(t *testing.T) {
	cfgSrc := strings.Replace(smallConfig, "emit: css", "emit: json", 1)
	cfg := loadConfig(t, cfgSrc)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(":root { --a: 1px; }\n")}})
	require.Empty(t, r.Diagnostics)
	res := Check(cfg, []byte(cfgSrc), r.Files, []*css.Sheet{sheet(t, "theme.css", ":root { --a: 1px; }\n")})
	require.Len(t, res.Diagnostics, 1)
	d := res.Diagnostics[0]
	assert.Equal(t, RuleNothing, d.Rule)
	assert.Equal(t, config.File, d.File)
	assert.Equal(t, "no platform emits css; add one to airframe.yaml so the import can be checked", d.Message)
	assert.Zero(t, res.Comparisons)
}

func TestCheckFirstCSSPlatformWins(t *testing.T) {
	cfgSrc := strings.Replace(smallConfig, "  web: {emit: css}\n",
		"  data: {emit: [json]}\n  web: {emit: [json, css]}\n  other: {emit: [css], out: {css: x/other.css}}\n", 1)
	cfg := loadConfig(t, cfgSrc)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(":root { --a: 1px; }\n")}})
	require.Empty(t, r.Diagnostics)
	var used []string
	old := emitCSS
	emitCSS = func(p *project.Project, pl config.Platform) (map[string][]byte, error) {
		used = append(used, pl.Name)
		return old(p, pl)
	}
	t.Cleanup(func() { emitCSS = old })
	res := Check(cfg, []byte(cfgSrc), r.Files, []*css.Sheet{sheet(t, "theme.css", ":root { --a: 1px; }\n")})
	assert.Empty(t, res.Diagnostics)
	assert.Equal(t, []string{"web"}, used)
}

func TestCheckSurfacesLoadDiagnostics(t *testing.T) {
	cfg := loadConfig(t, smallConfig)
	files := map[string][]byte{"ds/tokens/primitive.tokens.json": []byte("{not json")}
	res := Check(cfg, []byte(smallConfig), files, []*css.Sheet{sheet(t, "theme.css", ":root { --a: 1px; }\n")})
	require.NotEmpty(t, res.Diagnostics)
	assert.Empty(t, byRule(res.Diagnostics, RuleNotEquivalent))
	assert.Zero(t, res.Comparisons)

	res = Check(cfg, []byte(smallConfig), map[string][]byte{}, []*css.Sheet{sheet(t, "theme.css", ":root { --a: 1px; }\n")})
	require.NotEmpty(t, res.Diagnostics, "no token files: loading stops")
	assert.Zero(t, res.Comparisons)
}

func TestCheckEmitFailureIsReported(t *testing.T) {
	cfg := loadConfig(t, smallConfig)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(":root { --a: 1px; }\n")}})
	old := emitCSS
	emitCSS = func(*project.Project, config.Platform) (map[string][]byte, error) {
		return nil, fmt.Errorf("boom")
	}
	t.Cleanup(func() { emitCSS = old })
	res := Check(cfg, []byte(smallConfig), r.Files, []*css.Sheet{sheet(t, "theme.css", ":root { --a: 1px; }\n")})
	require.Len(t, res.Diagnostics, 1)
	assert.Equal(t, "emit-failed", res.Diagnostics[0].Rule)
	assert.Equal(t, "platform web: css: boom", res.Diagnostics[0].Message)
}

func TestCheckNothingCompared(t *testing.T) {
	cfg := loadConfig(t, smallConfig)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(":root { --a: 1px; }\n")}})
	editEmitted(t, func(string) string { return "/* empty */\n" })
	res := Check(cfg, []byte(smallConfig), r.Files, []*css.Sheet{sheet(t, "theme.css", "")})
	require.Len(t, res.Diagnostics, 1)
	assert.Equal(t, RuleNothing, res.Diagnostics[0].Rule)
	assert.Zero(t, res.Comparisons)
	assert.Equal(t, 1, res.Environments)
}

func TestCheckTooManyEnvironments(t *testing.T) {
	var b strings.Builder
	b.WriteString(":root { --a: 1px; }\n")
	for i := range 10 {
		for _, v := range []string{"x", "y", "z"} {
			fmt.Fprintf(&b, ":root[data-a%d='%s'] { --a: 2px; }\n", i, v)
		}
	}
	cfg := loadConfig(t, smallConfig)
	r := importSources(cfg, []source{{File: "theme.css", Data: []byte(":root { --a: 1px; }\n")}})
	res := Check(cfg, []byte(smallConfig), r.Files, []*css.Sheet{sheet(t, "theme.css", b.String())})
	require.Len(t, res.Diagnostics, 1, "%v", res.Diagnostics)
	assert.Equal(t, css.RuleTooManyEnvironments, res.Diagnostics[0].Rule)
	assert.Zero(t, res.Comparisons)
}

func TestRenderEnv(t *testing.T) {
	assert.Equal(t, "no attributes or media features set", renderEnv(css.Env{}))
	assert.Equal(t, "data-a=<other>, data-b=x; pointer=coarse, prefers-color-scheme=dark", renderEnv(css.Env{
		Attrs: map[string]string{"data-b": "x", "data-a": "*"},
		Media: map[string]string{"prefers-color-scheme": "dark", "pointer": "coarse"},
	}))
	assert.Equal(t, "pointer=coarse", renderEnv(css.Env{Media: map[string]string{"pointer": "coarse"}}))
}

func TestCheckFiveAxesIsFast(t *testing.T) {
	var cfgSrc, src strings.Builder
	cfgSrc.WriteString("version: 1\ncontent: ds\nmodes:\n")
	src.WriteString(":root {\n")
	for i := range 5 {
		fmt.Fprintf(&cfgSrc, "  a%d: {values: [x, y, z], base: x, binding: runtime, web: {attr: data-a%d}}\n", i, i)
		fmt.Fprintf(&src, "  --p-%d: %dpx;\n  --q-%d: var(--p-%d);\n", i, i, i, i)
	}
	cfgSrc.WriteString("platforms:\n  web: {emit: css}\n")
	src.WriteString("}\n")
	for i := range 5 {
		for _, v := range []string{"y", "z"} {
			fmt.Fprintf(&src, ":root[data-a%d='%s'] { --p-%d: %d%dpx; --q-%d: var(--p-%d); }\n", i, v, i, i, len(v), (i+1)%5, i)
		}
	}
	start := time.Now()
	res := checkCSS(t, cfgSrc.String(), src.String())
	elapsed := time.Since(start)
	assert.Empty(t, res.Diagnostics)
	assert.Equal(t, 1024, res.Environments) // (unset, y, z, *)^5
	assert.Less(t, elapsed, 2*time.Second)
}
