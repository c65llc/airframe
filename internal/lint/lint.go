// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package lint runs Airframe's rules over a loaded project. Each rule is a
// small function registered by ID that returns its findings and a tally of
// what it scanned; Run merges them with the load diagnostics, applies the
// configured severities and exemptions, and sorts the result.
package lint

import (
	"cmp"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// Context is the per-run state every rule receives besides the project.
// Rules share it, so state worth computing once per run (such as a memo of
// resolved values) belongs here as a field.
type Context struct {
	// Generated is every file generate would write, as Options gives it.
	Generated map[string][]byte

	exemptions []Exemption
}

// Exemption is one finding a token's exempt marker suppressed. File and
// Line locate the token; Marker is how it was exempted ("exempt" for a
// token's $extensions exempt map).
type Exemption struct {
	Rule, File string
	Line       int
	Marker     string
	Reason     string
}

// Rule is one registered check. Default is its severity when the config
// says nothing. Summary is one sentence saying what it catches. Run returns
// the rule's findings and what it scanned; Run (the package function) sets
// the findings' severities and the tally's Rule, so a rule need not.
type Rule struct {
	ID      string
	Default diag.Severity
	Summary string
	Run     func(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally)
}

// Options adjust a lint run. Strict turns every warning into an error.
// Generated is the freshly generated output, for the rules that compare
// against it.
type Options struct {
	Strict    bool
	Generated map[string][]byte
}

// Result is a lint run: its sorted diagnostics, one tally per rule in
// Rules order, the exemptions that suppressed findings, and how many
// diagnostics are errors and warnings.
type Result struct {
	Diagnostics []diag.Diagnostic
	Tallies     []diag.Tally
	Exemptions  []Exemption
	Errors      int
	Warnings    int
}

// tokensNoun is the Tally noun of every rule that counts tokens.
const tokensNoun = "tokens"

// allRules is every rule, in no particular order; Rules sorts it.
var allRules = []Rule{
	{ID: "token-unclassified", Default: diag.Error, Run: ruleTokenUnclassified,
		Summary: "A token has no tier, or is a component token that names no component."},
	{ID: "tier-violation", Default: diag.Error, Run: ruleTierViolation,
		Summary: "A token aliases a token its tier may not read: a primitive aliases anything, a semantic token reads a component token, or a component token reads a primitive or another component's unshared token."},
	{ID: "literal-in-semantic", Default: diag.Error, Run: ruleLiteralInSemantic,
		Summary: "A semantic token, or one of its mode values, is a literal instead of an alias to a primitive."},
	{ID: "density-missing", Default: diag.Error, Run: ruleDensityMissing,
		Summary: "A component dimension lacks a value for every density and has no chrome reason."},
	{ID: "accent-group-partial", Default: diag.Error, Run: ruleAccentGroupPartial,
		Summary: "An accent mode value sets some of the roles accentGroups.required lists but not all."},
	{ID: "prefix-multiple-hues", Default: diag.Error, Run: rulePrefixMultipleHues,
		Summary: "Colour tokens sharing a first name segment span more than 8° of hue."},
	{ID: "exemption-without-reason", Default: diag.Error, Run: ruleExemptionWithoutReason,
		Summary: "A token is exempt from a rule but gives no reason."},
}

// Rules returns every rule, sorted by ID.
func Rules() []Rule {
	out := slices.Clone(allRules)
	slices.SortFunc(out, func(a, b Rule) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// Run lints p, which must not be nil (when loading stopped there is
// nothing to lint; report the load diagnostics alone). loadDiags pass
// through unchanged, joined by p.Tokens.Check's findings and every rule's.
// A rule's severity is config lint.rules over its Default; an off rule's
// findings are dropped but it is still tallied. Strict then turns warnings
// into errors. Every diagnostic is linked to its rule's page and sorted.
func Run(p *project.Project, loadDiags []diag.Diagnostic, opts Options) Result {
	all := slices.Clone(loadDiags)
	if p.Tokens != nil {
		all = append(all, p.Tokens.Check()...)
	}
	ctx := &Context{Generated: opts.Generated}
	res := Result{Diagnostics: []diag.Diagnostic{}}
	for _, r := range Rules() {
		ds, tally := r.Run(p, ctx)
		tally.Rule = r.ID
		res.Tallies = append(res.Tallies, tally)
		sev := r.Default
		if s, ok := p.Config.Lint.Rules[r.ID]; ok {
			sev = s
		}
		if sev == diag.Off {
			continue
		}
		for _, d := range ds {
			d.Severity = sev
			all = append(all, d)
		}
	}
	for _, d := range all {
		if opts.Strict && d.Severity == diag.Warn {
			d.Severity = diag.Error
		}
		switch d.Severity {
		case diag.Off:
			continue
		case diag.Error:
			res.Errors++
		case diag.Warn:
			res.Warnings++
		}
		d.Docs = diag.DocsURL(d.Rule)
		res.Diagnostics = append(res.Diagnostics, d)
	}
	diag.Sort(res.Diagnostics)
	res.Exemptions = sortedExemptions(ctx.exemptions)
	return res
}

func sortedExemptions(es []Exemption) []Exemption {
	out := slices.Clone(es)
	cmpEx := func(a, b Exemption) int {
		return cmp.Or(
			strings.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			strings.Compare(a.Rule, b.Rule),
			strings.Compare(a.Marker, b.Marker),
			strings.Compare(a.Reason, b.Reason),
		)
	}
	slices.SortFunc(out, cmpEx)
	return slices.Compact(out)
}

// jsonPath is t's path in its file, the groups joined with ".".
func jsonPath(t *tokens.Token) string { return strings.Join(t.Path, ".") }

// exempt reports whether t is exempt from rule with a reason, recording the
// exemption when it is. An exemption with an empty reason exempts nothing;
// exemption-without-reason reports it.
func (c *Context) exempt(rule string, t *tokens.Token) bool {
	reason := t.Exempt[rule]
	if strings.TrimSpace(reason) == "" {
		return false
	}
	c.exemptions = append(c.exemptions, Exemption{
		Rule: rule, File: t.File, Line: t.Line, Marker: "exempt", Reason: reason,
	})
	return true
}

// TokenFinding is a rule's finding on token t, located at file:line (0
// means the token's own declaration). It returns nil, and records the
// exemption, when t is exempt from rule with a reason. Every token-scoped
// finding goes through here so exemptions apply uniformly.
func (c *Context) TokenFinding(rule string, t *tokens.Token, file string, line int, msg string) *diag.Diagnostic {
	if c.exempt(rule, t) {
		return nil
	}
	if file == "" {
		file = t.File
	}
	if line == 0 {
		line = t.Line
	}
	return &diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: file, Path: jsonPath(t), Line: line,
		Message: msg, Docs: diag.DocsURL(rule),
	}
}

// counter tallies the tokens a rule examined and the files they came from.
type counter struct {
	files map[string]bool
	items int
}

func (k *counter) add(t *tokens.Token) {
	if k.files == nil {
		k.files = map[string]bool{}
	}
	k.files[t.File] = true
	k.items++
}

func (k *counter) tally() diag.Tally {
	return diag.Tally{Files: len(k.files), Items: k.items, Noun: tokensNoun}
}
