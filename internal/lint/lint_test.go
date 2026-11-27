// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
)

// flagAll is a project on which every rule fires at least once.
var flagAll = map[string]string{
	"a.tokens.json": `{
  "plain": { "$type": "number", "$value": 1 },
  "moss": { "$type": "color",
    "1": { "$value": ` + oklch(140) + `, ` + ext(`"tier": "primitive"`) + ` },
    "2": { "$value": "{moss.1}", ` + ext(`"tier": "primitive", "exempt": { "token-unclassified": " " }`) + ` },
    "3": { "$value": ` + oklch(150) + `, ` + ext(`"tier": "primitive"`) + ` } },
  "surface": { "$type": "color", "$value": ` + oklch(150) + `, ` + ext(`"tier": "semantic"`) + ` },
  "tab-h": { "$type": "dimension", "$value": { "value": 36, "unit": "px" }, ` + ext(`"tier": "component", "component": "tabstrip"`) + ` },
  "glow": { "$type": "color", "$value": ` + oklch(150) + `, ` + ext(`"modes": { "appearance:dark": `+oklch(160)+` }`) + ` },
  ` + accentToken("accent", `"app:slides": `+oklch(30)) + `
}`,
	"invariants.yaml": `- { id: c, kind: contrast, fg: surface, bg: moss-1, min: 21 }
- { id: h, kind: hue-reserved, hue: 150, tolerance: 2, only: ["moss-*"] }
- { id: a, kind: same-hue-across-appearances, tokens: [glow] }
- { id: d, kind: min-dimension, match: "tab-*", min: 44 }
`,
}

// invariantRules count checks, read invariants.yaml as well as tokens.
var invariantRules = []string{"accent-flip", "contrast-below", "dimension-below", "hue-reserved"}

func ruleIDs() []string {
	var out []string
	for _, r := range Rules() {
		out = append(out, r.ID)
	}
	return out
}

func byRule(ds []diag.Diagnostic, rule string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range ds {
		if d.Rule == rule {
			out = append(out, d)
		}
	}
	return out
}

// without drops one rule's findings, for fixtures written by hand that are
// not in canonical form.
func without(ds []diag.Diagnostic, rule string) []diag.Diagnostic {
	return slices.DeleteFunc(slices.Clone(ds), func(d diag.Diagnostic) bool { return d.Rule == rule })
}

func TestRulesStableOrder(t *testing.T) {
	ids := ruleIDs()
	assert.True(t, slices.IsSorted(ids), ids)
	assert.Equal(t, []string{
		"accent-flip", "accent-group-partial", "contrast-below", "density-missing", "dimension-below",
		"emitted-stale", "exemption-without-reason", "hue-reserved", "literal-in-semantic", "not-formatted",
		"prefix-multiple-hues", "tier-violation", "token-unclassified",
	}, ids)
	for _, r := range Rules() {
		assert.Equal(t, diag.Error, r.Default, r.ID)
		assert.NotEmpty(t, r.Summary, r.ID)
		assert.NotNil(t, r.Run, r.ID)
	}
}

func TestTalliesNameEveryRule(t *testing.T) {
	res := Run(loadWith(t, testConfig+"platforms:\n  json: { emit: json }\n", flagAll), nil, Options{})
	var named []string
	for _, tl := range res.Tallies {
		named = append(named, tl.Rule)
		assert.Positive(t, tl.Items, tl.Rule)
		switch {
		case tl.Rule == "emitted-stale":
			assert.Equal(t, "files", tl.Noun, tl.Rule)
			assert.Equal(t, 1, tl.Files, tl.Rule)
		case tl.Rule == "not-formatted":
			assert.Equal(t, "files", tl.Noun, tl.Rule)
			assert.Equal(t, 3, tl.Files, "airframe.yaml, the token file and invariants.yaml")
		case slices.Contains(invariantRules, tl.Rule):
			assert.Equal(t, "checks", tl.Noun, tl.Rule)
			assert.Equal(t, 2, tl.Files, tl.Rule)
		default:
			assert.Equal(t, "tokens", tl.Noun, tl.Rule)
			assert.Equal(t, 1, tl.Files, tl.Rule)
		}
		assert.Empty(t, tl.NotApplicable, tl.Rule)
	}
	assert.Equal(t, ruleIDs(), named)
	for _, id := range ruleIDs() {
		assert.NotEmpty(t, byRule(res.Diagnostics, id), "%s should fire on the flag fixture", id)
	}
}

func TestRunDeterministicAndSorted(t *testing.T) {
	p := load(t, flagAll)
	first := Run(p, nil, Options{})
	for range 5 {
		assert.Equal(t, first, Run(p, nil, Options{}))
	}
	sorted := slices.Clone(first.Diagnostics)
	diag.Sort(sorted)
	assert.Equal(t, sorted, first.Diagnostics)
	for _, d := range first.Diagnostics {
		assert.Equal(t, diag.DocsURL(d.Rule), d.Docs)
	}
}

func TestLoadDiagnosticsPassThroughAndCheckRuns(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{
  "x": { "$type": "number", "$value": "{nope}", ` + ext(`"tier": "semantic"`) + ` } }`})
	loadDiag := diag.Diagnostic{Rule: "schema-invalid", Severity: diag.Error, File: "z.tokens.json", Line: 9, Message: "bad", Docs: diag.DocsURL("schema-invalid")}
	res := Run(p, []diag.Diagnostic{loadDiag}, Options{})
	assert.Contains(t, res.Diagnostics, loadDiag)
	unresolved := byRule(res.Diagnostics, "alias-unresolved")
	require.Len(t, unresolved, 1, "Run calls Tokens.Check")
	assert.Len(t, without(res.Diagnostics, "not-formatted"), 2, "the load diagnostic and alias-unresolved")
	assert.Equal(t, 4, res.Errors, "those two plus not-formatted on airframe.yaml and a.tokens.json")
}

func TestSeverityOverrideOffAndStrict(t *testing.T) {
	cfg := testConfig + "lint:\n  rules: { token-unclassified: warn, literal-in-semantic: off, not-formatted: off }\n"
	files := map[string]string{"a.tokens.json": `{
  "plain": { "$type": "number", "$value": 1 },
  "ink": { "$type": "number", "$value": 2, ` + ext(`"tier": "semantic"`) + ` } }`}
	p := loadWith(t, cfg, files)

	res := Run(p, nil, Options{})
	require.Len(t, res.Diagnostics, 1)
	assert.Equal(t, "token-unclassified", res.Diagnostics[0].Rule)
	assert.Equal(t, diag.Warn, res.Diagnostics[0].Severity)
	assert.Equal(t, 0, res.Errors)
	assert.Equal(t, 1, res.Warnings)
	var off diag.Tally
	for _, tl := range res.Tallies {
		if tl.Rule == "literal-in-semantic" {
			off = tl
		}
	}
	assert.Equal(t, 1, off.Items, "an off rule is still tallied")

	res = Run(p, nil, Options{Strict: true})
	require.Len(t, res.Diagnostics, 1)
	assert.Equal(t, diag.Error, res.Diagnostics[0].Severity)
	assert.Equal(t, 1, res.Errors)
	assert.Equal(t, 0, res.Warnings)
}

func TestExemptionSuppressesAndIsRecorded(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{
  "plain": { "$type": "number", "$value": 1, ` + ext(`"exempt": { "token-unclassified": "legacy import, classified in #12" }`) + ` },
  "bare": { "$type": "number", "$value": 1 } }`})
	res := Run(p, nil, Options{})
	got := byRule(res.Diagnostics, "token-unclassified")
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, `"bare"`)
	assert.Equal(t, []Exemption{{
		Rule: "token-unclassified", File: "design-system/a.tokens.json", Line: 2,
		Marker: "exempt", Reason: "legacy import, classified in #12",
	}}, res.Exemptions)
	assert.Empty(t, byRule(res.Diagnostics, "exemption-without-reason"))
}

func TestExemptionWithoutReason(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{
  "plain": { "$type": "number", "$value": 1, ` + ext(`"tier": "semantic", "exempt": { "token-unclassified": "", "tier-violation": "  " }`) + ` },
  "bare": { "$type": "number", "$value": 1, ` + ext(`"exempt": { "token-unclassified": "" }`) + ` } }`})
	res := Run(p, nil, Options{})
	got := byRule(res.Diagnostics, "exemption-without-reason")
	assert.Equal(t, []string{
		`token "plain" is exempt from tier-violation with no reason; give the reason`,
		`token "plain" is exempt from token-unclassified with no reason; give the reason`,
		`token "bare" is exempt from token-unclassified with no reason; give the reason`,
	}, messages(got))
	for _, d := range got {
		assert.Equal(t, diag.Error, d.Severity)
		assert.Equal(t, "design-system/a.tokens.json", d.File)
	}
	assert.Equal(t, 2, got[0].Line)
	assert.Equal(t, "plain", got[0].Path)
	// A reasonless exemption does not suppress the finding.
	unclassified := byRule(res.Diagnostics, "token-unclassified")
	require.Len(t, unclassified, 1)
	assert.True(t, strings.Contains(unclassified[0].Message, `"bare"`))
	assert.Empty(t, res.Exemptions)
}

func TestRunWithEmptyTokenSet(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{}`})
	res := Run(p, nil, Options{})
	assert.Empty(t, without(res.Diagnostics, "not-formatted"), "the fixture's YAML is hand-aligned")
	assert.NotNil(t, res.Diagnostics, "an empty run reports [] not null")
	assert.Len(t, res.Tallies, len(Rules()))
}
