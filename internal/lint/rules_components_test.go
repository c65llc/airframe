// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
)

// componentRuleIDs are the rules this file covers.
var componentRuleIDs = []string{"component-unruled", "component-incomplete", "retired-used"}

// componentFS is consumerFS with every component file replaced: comps
// maps a component id to the body after its id, tokens and owners lines.
// button, chip and pill own their -bg token; any other id owns none.
func componentFS(cfg string, comps map[string]string, files map[string]string) project.MemFS {
	m := consumerFS(cfg, files)
	delete(m, "design-system/components/button.yaml")
	for id, body := range comps {
		toks := "[]"
		if id == "button" || id == "chip" || id == "pill" {
			toks = "[" + id + "-bg]"
		}
		m["design-system/components/"+id+".yaml"] = "id: " + id + "\ntokens: " + toks + "\nowners: [web/src/" + id + "/**]\n" + body
	}
	return m
}

// gantryFiles is a Gantry project next to airframe.yaml with one ruling
// of each status and one record that does not read.
var gantryFiles = map[string]string{
	"gantry.yaml":                      "version: 1\ncontent: docs/design\n",
	"docs/design/rulings/tab-strip.md": "---\nid: tab-strip\nstatus: ruled\n---\n",
	"docs/design/rulings/sheet.md":     "---\nid: sheet\nstatus: proposed\n---\n",
	"docs/design/rulings/old-tabs.md":  "---\nid: old-tabs\nstatus: superseded\nsupersededBy: tab-strip\n---\n",
	"docs/design/rulings/old-sheet.md": "---\nid: old-sheet\nstatus: superseded\n---\n",
	"docs/design/rulings/gone.md":      "---\nid: gone\nstatus: retired\n---\n",
	"docs/design/rulings/broken.md":    "---\nid: broken\nstatus: ruled\n",
	"web/src/button/Button.ts":         "export const b = 1\n",
	"web/src/chip/Chip.ts":             "export const c = 1\n",
	"web/src/pill/Pill.ts":             "export const p = 1\n",
}

const impls = "implementations: {web: web/src/button/Button.ts}\n"

func TestComponentUnruled(t *testing.T) {
	res := lintLoaded(t, componentFS(consumerConfig, map[string]string{
		"button": impls,
		"chip":   "ruling: tab-strip\n" + impls,
		"pill":   "ruling: \"@ruling:Tab_Strip\"\n" + impls,
		"card":   "ruling: \"@ruling:no-such-ruling\"\n" + impls,
	}, map[string]string{
		"design-system/retired.yaml": "- id: flush-bar\n  ruling: \"@ruling:\"\n  spellings: {css: [\"inset 3px\"]}\n" +
			"- id: old-glow\n  spellings: {css: [\"glow\"]}\n",
	}))
	got := byRule(res.Diagnostics, "component-unruled")
	require.Len(t, got, 4, messages(got))
	byFile := map[string]diag.Diagnostic{}
	for _, d := range got {
		byFile[d.File] = d
		assert.Equal(t, diag.Error, d.Severity)
	}
	b := byFile["design-system/components/button.yaml"]
	assert.Contains(t, b.Message, "component button has no ruling")
	assert.Equal(t, "ruling", b.Path)
	assert.Equal(t, 1, b.Line, "no ruling key: the record's line")
	c := byFile["design-system/components/chip.yaml"]
	assert.Contains(t, c.Message, `ruling "tab-strip" is not @ruling:<id>`)
	assert.Equal(t, 4, c.Line)
	assert.Contains(t, byFile["design-system/components/pill.yaml"].Message, `"@ruling:Tab_Strip" is not @ruling:<id>`)
	r := byFile["design-system/retired.yaml"]
	assert.Contains(t, r.Message, "retired treatment flush-bar")
	assert.Equal(t, "0.ruling", r.Path)
	// With no gantry.yaml, the check is syntax only: card's ruling is
	// well formed, so it passes, and old-glow needs no ruling.
	assert.NotContains(t, byFile, "design-system/components/card.yaml")

	tl := tallyOf(res, "component-unruled")
	assert.Equal(t, 6, tl.Items, "four components and two retired treatments")
	assert.Equal(t, 5, tl.Files)
}

func TestComponentUnruledStatus(t *testing.T) {
	files := map[string]string{
		"design-system/retired.yaml": "- id: flush-bar\n  ruling: \"@ruling:gone\"\n  spellings: {css: [\"inset 3px\"]}\n" +
			"- id: old-glow\n  ruling: \"@ruling:broken\"\n  spellings: {css: [\"glow\"]}\n",
	}
	for k, v := range gantryFiles {
		files[k] = v
	}
	res := lintLoaded(t, componentFS(consumerConfig, map[string]string{
		"button": "ruling: \"@ruling:tab-strip\"\n" + impls,
		"chip":   "ruling: \"@ruling:sheet\"\n" + impls,
		"pill":   "ruling: \"@ruling:old-tabs\"\n" + impls,
		"card":   "ruling: \"@ruling:old-sheet\"\n" + impls,
		"menu":   "ruling: \"@ruling:missing\"\n" + impls,
	}, files))
	got := byRule(res.Diagnostics, "component-unruled")
	msgs := map[string]string{}
	for _, d := range got {
		msgs[d.File] = d.Message
	}
	require.Len(t, got, 6, messages(got))
	assert.NotContains(t, msgs, "design-system/components/button.yaml", "ruled")
	assert.Contains(t, msgs["design-system/components/chip.yaml"], "ruling sheet is proposed, not yet ruled")
	assert.Contains(t, msgs["design-system/components/pill.yaml"], "ruling old-tabs is superseded by tab-strip")
	assert.Contains(t, msgs["design-system/components/pill.yaml"], "@ruling:tab-strip")
	assert.Contains(t, msgs["design-system/components/card.yaml"], "ruling old-sheet is superseded, and names no supersededBy")
	assert.Contains(t, msgs["design-system/components/menu.yaml"], "ruling missing does not exist")
	assert.Contains(t, msgs["design-system/components/menu.yaml"], "gantry.yaml")
	assert.Contains(t, msgs["design-system/retired.yaml"], "retired treatment flush-bar: ruling gone is retired")
	// The unreadable record is reported once, at the record, and the
	// treatment that cites it is not also told the ruling is missing.
	assert.Contains(t, msgs["docs/design/rulings/broken.md"], "no closing ---")
	for _, d := range got {
		assert.NotContains(t, d.Message, "ruling broken does not exist")
	}
}

// The gantry.yaml problems are reported once, whatever reads the index.
func TestComponentUnruledGantryInvalidOnce(t *testing.T) {
	cfg := consumerConfig + "gantry: tools/gantry.yaml\n"
	res := lintLoaded(t, componentFS(cfg, map[string]string{"button": "ruling: \"@ruling:tab-strip\"\n" + impls},
		map[string]string{"web/src/button/Button.ts": "export const b = 1\n"}))
	got := byRule(res.Diagnostics, "schema-invalid")
	require.Len(t, got, 1, messages(got))
	assert.Equal(t, "airframe.yaml", got[0].File)
	assert.Equal(t, "gantry", got[0].Path)
	assert.Empty(t, byRule(res.Diagnostics, "component-unruled"), "no index: syntax only")

	off := cfg + "lint:\n  rules: { component-unruled: off }\n"
	res = lintLoaded(t, componentFS(off, map[string]string{"button": "ruling: \"@ruling:tab-strip\"\n" + impls},
		map[string]string{"web/src/button/Button.ts": "export const b = 1\n"}))
	assert.Len(t, byRule(res.Diagnostics, "schema-invalid"), 1, "turning the rule off does not drop it")
}

func TestComponentUnruledNotApplicable(t *testing.T) {
	res := lintLoaded(t, componentFS(consumerConfig, nil, nil))
	assert.Equal(t, "no components or retired treatments declared", tallyOf(res, "component-unruled").NotApplicable)
	assert.Equal(t, "no components declared", tallyOf(res, "component-incomplete").NotApplicable)
}

// twoPlatforms has web and apple consumers, and a platform with none.
const twoPlatforms = `version: 1
content: design-system
platforms:
  web:
    emit: [css]
    out: {css: web/generated/tokens.css}
    consumers: ["web/src/**/*.{css,ts}"]
  apple:
    emit: [swift]
    out: {swift: apple/Generated/DesignTokens.swift}
    consumers: ["apple/**/*.swift"]
  docs:
    emit: [json]
    out: {json: docs/tokens.json}
`

func TestComponentIncomplete(t *testing.T) {
	res := lintLoaded(t, componentFS(twoPlatforms, map[string]string{
		// Complete: a file, an n/a with a reason; docs declares no consumers.
		"button": "ruling: \"@ruling:button\"\nimplementations:\n  web: web/src/button/Button.ts\n  apple: \"n/a: the system button\"\n",
		// An unknown platform, and apple missing.
		"chip": "ruling: \"@ruling:chip\"\nimplementations:\n  web: web/src/chip/Chip.ts\n  ios: apple/Chip.swift\n",
		// A missing file, n/a with no reason, an unknown host.
		"pill": "ruling: \"@ruling:pill\"\nhostedBy: toolbar\nimplementations:\n  web: web/src/pill/Gone.ts\n  apple: \"n/a:  \"\n",
		// A directory, a path outside the repo.
		"card": "ruling: \"@ruling:card\"\nimplementations:\n  web: web/src/pill\n  apple: ../Card.swift\n",
	}, map[string]string{
		"web/src/button/Button.ts": "export const b = 1\n",
		"web/src/chip/Chip.ts":     "export const c = 1\n",
		"web/src/pill/Pill.ts":     "export const p = 1\n",
		"apple/App.swift":          "let x = 1\n",
	}))
	got := byRule(res.Diagnostics, "component-incomplete")
	type at struct{ file, path string }
	found := map[at]diag.Diagnostic{}
	for _, d := range got {
		found[at{d.File, d.Path}] = d
	}
	require.Len(t, got, 7, messages(got))
	comp := func(id string) string { return "design-system/components/" + id + ".yaml" }
	assert.Contains(t, found[at{comp("chip"), "implementations.ios"}].Message, "implementations names ios, which is not a configured platform")
	assert.Equal(t, 7, found[at{comp("chip"), "implementations.ios"}].Line)
	assert.Contains(t, found[at{comp("chip"), "implementations"}].Message, "platform apple declares consumers, but implementations has no apple entry")
	assert.Contains(t, found[at{comp("pill"), "implementations.web"}].Message, "web/src/pill/Gone.ts does not exist")
	assert.Contains(t, found[at{comp("pill"), "implementations.apple"}].Message, "n/a needs a reason")
	assert.Contains(t, found[at{comp("pill"), "hostedBy"}].Message, "hostedBy toolbar is not a component")
	assert.Equal(t, 5, found[at{comp("pill"), "hostedBy"}].Line)
	assert.Contains(t, found[at{comp("card"), "implementations.web"}].Message, "is a directory")
	assert.Contains(t, found[at{comp("card"), "implementations.apple"}].Message, "is not a repo-relative file path")
	for _, d := range got {
		assert.NotEqual(t, comp("button"), d.File, d.Message)
	}

	tl := tallyOf(res, "component-incomplete")
	assert.Equal(t, 4, tl.Items)
	assert.Equal(t, "components", tl.Noun)
}

// Review Focus 4: a hostedBy cycle is reported once, naming every member,
// and a component hosted by a cycle member is not itself in the cycle.
func TestHostedByCycle(t *testing.T) {
	impl := "implementations: {web: web/src/button/Button.ts}\n"
	res := lintLoaded(t, componentFS(consumerConfig, map[string]string{
		"alpha": "hostedBy: beta\n" + impl,
		"beta":  "hostedBy: gamma\n" + impl,
		"gamma": "hostedBy: alpha\n" + impl,
		"delta": "hostedBy: beta\n" + impl,
		"solo":  "hostedBy: solo\n" + impl,
	}, map[string]string{"web/src/button/Button.ts": "export const b = 1\n"}))
	got := byRule(res.Diagnostics, "component-incomplete")
	require.Len(t, got, 2, messages(got))
	assert.Equal(t, "design-system/components/alpha.yaml", got[0].File)
	assert.Contains(t, got[0].Message, "alpha → beta → gamma → alpha")
	assert.NotContains(t, got[0].Message, "delta")
	assert.Equal(t, "design-system/components/solo.yaml", got[1].File)
	assert.Contains(t, got[1].Message, "solo → solo")
}

// retiredTokens adds old-accent (retired, read by consumers) and its
// non-retired alias.
const retiredTokens = `{
  "blue": { "$type": "color",
    "500": { "$value": { "colorSpace": "oklch", "components": [0.55, 0.2, 260] }, "$extensions": { "com.c65llc.airframe": { "tier": "primitive" } } },
    "old": { "$value": { "colorSpace": "oklch", "components": [0.5, 0.2, 260] }, "$extensions": { "com.c65llc.airframe": { "tier": "primitive", "retired": true } } } },
  "accent": { "$type": "color", "$value": "{blue.500}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic" } } },
  "old-accent": { "$type": "color", "$value": "{blue.old}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic", "retired": true } } },
  "ink": { "$type": "color", "$value": "{blue.500}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic", "modes": { "appearance:dark": "{blue.old}" } } } },
  "edge": { "$type": "color", "$value": "{blue.500}", "$extensions": { "com.c65llc.airframe": { "tier": "semantic", "modes": { "appearance:dark": "{blue.old}" }, "exempt": { "retired-used": "migrating in #40" } } } },
  "button": { "bg": { "$type": "color", "$value": "{ink}", "$extensions": { "com.c65llc.airframe": { "tier": "component", "component": "button" } } } }
}`

const retiredConfig = `version: 1
content: design-system
modes:
  appearance: {values: [light, dark], binding: runtime, web: {light: ":root", dark: ":root[data-theme='dark']"}}
platforms:
  web:
    emit: [css]
    out: {css: web/generated/tokens.css}
    consumers: ["web/src/**/*.{css,scss,ts,tsx}"]
    wrappers:
      - root: theme.color
        map: {legacy: old-accent, accent: accent}
`

func retiredFS(retiredYAML string, files map[string]string) project.MemFS {
	m := project.MemFS{
		"airframe.yaml":                        retiredConfig,
		"design-system/tokens.tokens.json":     retiredTokens,
		"design-system/components/button.yaml": "id: button\ntokens: [button-bg]\nowners: [web/src/button/**]\n",
	}
	if retiredYAML != "" {
		m["design-system/retired.yaml"] = retiredYAML
	}
	for k, v := range files {
		m[k] = v
	}
	return m
}

func TestRetiredTokenRead(t *testing.T) {
	res := lintLoaded(t, retiredFS("", map[string]string{
		"web/src/a.css": ".a { color: var(--old-accent); background: var(--accent); }\n",
		"web/src/b.ts":  "export const c = theme.color.legacy\nexport const d = theme.color.accent\n",
		"web/src/c.css": ".c { color: var(--old-accent); } /* airframe-exempt retired-used: print sheet */\n",
	}))
	got := byRule(res.Diagnostics, "retired-used")
	var consumer []diag.Diagnostic
	for _, d := range got {
		if strings.HasPrefix(d.File, "web/") {
			consumer = append(consumer, d)
		}
	}
	require.Len(t, consumer, 2, messages(got))
	assert.Equal(t, "web/src/a.css", consumer[0].File)
	assert.Equal(t, 1, consumer[0].Line)
	assert.Equal(t, 17, consumer[0].Col)
	assert.Contains(t, consumer[0].Message, "token old-accent, which is retired")
	assert.Equal(t, "web/src/b.ts", consumer[1].File)
	assert.Contains(t, consumer[1].Message, "theme.color.legacy (through wrapper theme.color)")
	assert.Contains(t, res.Exemptions, Exemption{Rule: "retired-used", File: "web/src/c.css", Line: 1, Marker: "airframe-exempt", Reason: "print sheet"})
}

func TestRetiredAlias(t *testing.T) {
	res := lintLoaded(t, retiredFS("", map[string]string{"web/src/a.css": ".a { color: var(--accent); }\n"}))
	got := byRule(res.Diagnostics, "retired-used")
	paths := map[string]string{}
	for _, d := range got {
		paths[d.Path] = d.Message
		assert.Equal(t, "design-system/tokens.tokens.json", d.File)
	}
	require.Len(t, got, 1, messages(got))
	// old-accent is itself retired, so its alias to blue.old is not a finding.
	assert.NotContains(t, paths, "old-accent")
	assert.Contains(t, paths["ink"], "token ink (appearance:dark) aliases retired token blue-old;")
	// Only the first hop counts: button-bg reaches blue-old through ink,
	// which is ink's finding, and fixing ink fixes it.
	assert.NotContains(t, paths, "button.bg")
	// edge's exempt map applies: it is recorded, not reported.
	assert.NotContains(t, paths, "edge")
	assert.Contains(t, res.Exemptions, Exemption{Rule: "retired-used", File: "design-system/tokens.tokens.json", Line: 8, Marker: "exempt", Reason: "migrating in #40"})
}

const flushBar = `- id: flush-left-bar
  ruling: "@ruling:selection"
  spellings:
    css: ["inset 3px 0 0"]
    swift: ["\\.frame\\(width: 3\\)"]
  marker: audited-bar
`

func spellingFindings(res Result) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range byRule(res.Diagnostics, "retired-used") {
		if strings.Contains(d.Message, "flush-left-bar") {
			out = append(out, d)
		}
	}
	return out
}

func TestRetiredSpelling(t *testing.T) {
	res := lintLoaded(t, retiredFS(flushBar, map[string]string{
		"web/src/a.css": ".a { color: var(--accent); }\n.b { box-shadow: inset 3px 0 0 var(--accent); }\n",
		"web/src/b.ts":  "// .frame(width: 3) is a swift spelling, not read here\nexport const x = 1\n",
	}))
	got := spellingFindings(res)
	require.Len(t, got, 1, messages(byRule(res.Diagnostics, "retired-used")))
	assert.Equal(t, "web/src/a.css", got[0].File)
	assert.Equal(t, 2, got[0].Line)
	assert.Equal(t, 18, got[0].Col)
	assert.Contains(t, got[0].Message, "retired treatment flush-left-bar (@ruling:selection)")
	assert.Contains(t, got[0].Message, `"inset 3px 0 0"`)
	tl := tallyOf(res, "retired-used")
	assert.Empty(t, tl.NotApplicable)
	assert.Positive(t, tl.Items)
}

// Review Focus 3: a spelling inside a stylesheet comment does not fire,
// because the adapters read stylesheets with their comments blanked; the
// same spelling in a .tsx string does.
func TestRetiredSpellingComments(t *testing.T) {
	res := lintLoaded(t, retiredFS(flushBar, map[string]string{
		"web/src/a.css":  "/* the old bar was inset 3px 0 0 */\n.a { color: var(--accent); }\n/*\n inset 3px 0 0\n*/\n",
		"web/src/b.tsx":  "export const s = { boxShadow: \"inset 3px 0 0 red\" }\n",
		"web/src/c.scss": ".c { content: \"/* inset 3px 0 0 */\"; }\n",
	}))
	got := spellingFindings(res)
	require.Len(t, got, 2, messages(got))
	assert.Equal(t, "web/src/b.tsx", got[0].File)
	assert.Equal(t, 1, got[0].Line)
	assert.Equal(t, 32, got[0].Col)
	assert.Equal(t, "web/src/c.scss", got[1].File, "a string is not a comment")
}

func TestRetiredMarker(t *testing.T) {
	res := lintLoaded(t, retiredFS(flushBar, map[string]string{
		"web/src/a.css": ".a { box-shadow: inset 3px 0 0 red; } /* audited-bar: print stylesheet */\n" + // 1 suppressed
			"/* audited-bar: legacy email template */\n" + // 2
			".b { box-shadow: inset 3px 0 0 red; }\n" + // 3 suppressed by line 2
			".c { box-shadow: inset 3px 0 0 red; } /* audited-bar */\n" + // 4 no reason
			".d { color: red; } /* audited-bar: not comment-only */\n" + // 5
			".e { box-shadow: inset 3px 0 0 red; }\n" + // 6 line 5 has code
			"/* airframe-exempt retired-used: vendor sheet */\n" + // 7
			".f { box-shadow: inset 3px 0 0 red; }\n" + // 8 suppressed, generic
			".g { box-shadow: inset 3px 0 0 red; } /* other-marker: wrong word */\n", // 9
	}))
	got := spellingFindings(res)
	var lines []int
	for _, d := range got {
		lines = append(lines, d.Line)
	}
	assert.Equal(t, []int{4, 6, 9}, lines, messages(got))
	require.Len(t, got, 3)
	assert.True(t, strings.HasSuffix(got[0].Message, "(audited-bar on line 4 has no reason)"), got[0].Message)
	var ex []Exemption
	for _, e := range res.Exemptions {
		if e.Rule == "retired-used" && e.File == "web/src/a.css" {
			ex = append(ex, e)
		}
	}
	assert.Equal(t, []Exemption{
		{Rule: "retired-used", File: "web/src/a.css", Line: 1, Marker: "audited-bar", Reason: "print stylesheet"},
		{Rule: "retired-used", File: "web/src/a.css", Line: 3, Marker: "audited-bar", Reason: "legacy email template"},
		{Rule: "retired-used", File: "web/src/a.css", Line: 8, Marker: "airframe-exempt", Reason: "vendor sheet"},
	}, ex)
}

// A treatment's marker counts only inside a comment: the word in code or
// a string escapes nothing, and its reason stops at the comment's closer.
func TestRetiredMarkerInCommentOnly(t *testing.T) {
	res := lintLoaded(t, retiredFS(flushBar, map[string]string{
		"web/src/a.css": ".a { box-shadow: inset 3px 0 0 red; content: \"audited-bar: in a string\"; }\n" + // 1 fires
			".b { box-shadow: inset 3px 0 0 red; } audited-bar: in code\n" + // 2 fires
			".c { box-shadow: inset 3px 0 0 red; } /* audited-bar: print */ .x { color: red; }\n", // 3 suppressed
		"web/src/b.tsx": "const s = \"inset 3px 0 0\" // audited-bar: legacy chart\n" + // 1 suppressed
			"const t = <i style={{ boxShadow: \"inset 3px 0 0\" }} /> {/* audited-bar: jsx */}\n", // 2 suppressed
	}))
	got := spellingFindings(res)
	var at []string
	for _, d := range got {
		at = append(at, fmt.Sprintf("%s:%d", d.File, d.Line))
	}
	assert.Equal(t, []string{"web/src/a.css:1", "web/src/a.css:2"}, at, messages(got))
	reasons := map[string]string{}
	for _, e := range res.Exemptions {
		reasons[fmt.Sprintf("%s:%d", e.File, e.Line)] = e.Reason
	}
	assert.Equal(t, "print", reasons["web/src/a.css:3"], "cut at the closer")
	assert.Equal(t, "legacy chart", reasons["web/src/b.tsx:1"])
	assert.Equal(t, "jsx", reasons["web/src/b.tsx:2"])
}

// A treatment's marker escapes only that treatment's spelling, never a
// retired token read on the same line.
func TestRetiredMarkerScoped(t *testing.T) {
	res := lintLoaded(t, retiredFS(flushBar, map[string]string{
		"web/src/a.css": ".a { box-shadow: inset 3px 0 0 var(--old-accent); } /* audited-bar: print */\n",
	}))
	assert.Empty(t, spellingFindings(res))
	got := byRule(res.Diagnostics, "retired-used")
	var reads []diag.Diagnostic
	for _, d := range got {
		if d.File == "web/src/a.css" {
			reads = append(reads, d)
		}
	}
	require.Len(t, reads, 1, messages(got))
	assert.Contains(t, reads[0].Message, "old-accent")
}

func TestRetiredUsedNotApplicable(t *testing.T) {
	res := lintLoaded(t, consumerFS(consumerConfig, map[string]string{"web/src/a.css": ".a { color: var(--accent); }\n"}))
	assert.Equal(t, "no retired tokens or treatments", tallyOf(res, "retired-used").NotApplicable)
}

// An implementation that is a symlink, or sits under one, is not
// followed, as the consumer scan and rulingref do not follow one.
func TestComponentIncompleteSymlink(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	write := func(p, data string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(data), 0o644))
	}
	for k, v := range componentFS(twoPlatforms, map[string]string{
		"button": "ruling: \"@ruling:button\"\nimplementations:\n  web: web/src/button/Button.ts\n  apple: apple/Linked/Button.swift\n",
	}, map[string]string{"web/src/a.ts": "export const a = 1\n", "apple/App.swift": "let x = 1\n"}) {
		write(filepath.Join(repo, k), v)
	}
	write(filepath.Join(outside, "Button.ts"), "export const b = 1\n")
	write(filepath.Join(outside, "Button.swift"), "let b = 1\n")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "web/src/button"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(outside, "Button.ts"), filepath.Join(repo, "web/src/button/Button.ts")))
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, "apple/Linked")))

	res := lintLoaded(t, os.DirFS(repo))
	msgs := map[string]string{}
	for _, d := range byRule(res.Diagnostics, "component-incomplete") {
		msgs[d.Path] = d.Message
	}
	assert.Contains(t, msgs["implementations.web"], "web/src/button/Button.ts is a symlink; not followed")
	assert.Contains(t, msgs["implementations.apple"], "is under apple/Linked, which is a symlink; not followed")
}
