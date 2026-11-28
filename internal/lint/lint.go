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

	"github.com/c65llc/airframe/internal/consumer"
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
	// resolved memoises Resolve per (token, full context) for the invariant
	// rules; a nil value is a token that does not resolve there.
	resolved map[resolveKey]*tokens.Value
	// axesOf memoises the axes each token's value can vary on.
	axesOf map[string]map[string]bool
	// scan is the run's one consumer scan, read by every consumer rule;
	// scanNA says why it is nil. See consumerScan.
	scan    *consumer.Result
	scanNA  string
	scanned bool
	// err is the first I/O failure a rule hit; Run returns it.
	err error
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
// diagnostics are errors and warnings. Err is an I/O failure a rule hit
// (reading a consumer file): the run's findings are then incomplete, and
// the caller fails the run (exit 2) instead of reporting them.
type Result struct {
	Diagnostics []diag.Diagnostic
	Tallies     []diag.Tally
	Exemptions  []Exemption
	Errors      int
	Warnings    int
	Err         error
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
	{ID: "contrast-below", Default: diag.Error, Run: ruleContrastBelow,
		Summary: "A contrast invariant's fg over bg falls below its minimum ratio in some context."},
	{ID: "hue-reserved", Default: diag.Error, Run: ruleHueReserved,
		Summary: "A colour within a reserved hue's tolerance belongs to a token the invariant does not allow."},
	{ID: "accent-flip", Default: diag.Error, Run: ruleAccentFlip,
		Summary: "A token's hue moves more than 1° between appearances, which same-hue-across-appearances forbids."},
	{ID: "dimension-below", Default: diag.Error, Run: ruleDimensionBelow,
		Summary: "A dimension a min-dimension invariant matches resolves below its minimum px in some context."},
	{ID: "emitted-stale", Default: diag.Error, Run: ruleEmittedStale,
		Summary: "A generated output differs from what airframe build would write, or is missing."},
	{ID: "not-formatted", Default: diag.Error, Run: ruleNotFormatted,
		Summary: "A token, config or invariants file differs from what airframe fmt would write."},
	{ID: "exemption-without-reason", Default: diag.Error, Run: ruleExemptionWithoutReason,
		Summary: "A token is exempt from a rule but gives no reason."},
	{ID: "reference-undeclared", Default: diag.Error, Run: ruleReferenceUndeclared,
		Summary: "A consumer file reads a token name that does not exist, or a platform's consumers glob matches no file."},
	{ID: "primitive-read-by-consumer", Default: diag.Error, Run: rulePrimitiveRead,
		Summary: "A consumer file reads a token whose tier consumers may not read, such as a primitive."},
	{ID: "token-owner-violation", Default: diag.Error, Run: ruleOwnerViolation,
		Summary: "A consumer file reads a component token but matches none of the component's owners globs."},
	{ID: "fallback-literal", Default: diag.Warn, Run: ruleFallbackLiteral,
		Summary: "A consumer var() has a literal fallback, which hides a missing token."},
	{ID: "wrapper-unmapped", Default: diag.Error, Run: ruleWrapperUnmapped,
		Summary: "A read through a declared wrapper root maps to no token."},
}

// loadIDs are the rule IDs reported while loading, parsing, resolving or
// building a project rather than by a registered rule. Config cannot change
// their severity; they are always errors.
var loadIDs = []string{
	"alias-cycle", "alias-type-mismatch", "alias-unresolved", "config-missing", "emit-failed",
	"json-syntax", "mode-ambiguous", "mode-base-value", "mode-override-conflict", "mode-unknown",
	"no-tokens", "schema-invalid", "token-name-invalid", "type-unsupported", "yaml-syntax",
}

// warnIDs are reported while building rather than by a registered rule,
// always as warnings: emit-axis-fixed names a build-bound axis a native
// platform renders at its base value because it does not fan out over it.
// (emit-unsupported, retired in v0.5.0 when every accepted format was
// built, is no longer reported; its docs page stays.)
var warnIDs = []string{"emit-axis-fixed"}

// AllIDs returns every rule ID Airframe can report, sorted: each registered
// rule's, each load-time ID and each build-time warning ID. It is the one list the self-test and the
// rule docs are checked against.
func AllIDs() []string {
	out := slices.Concat(loadIDs, warnIDs)
	for _, r := range allRules {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// BuildWarningIDs returns the IDs reported while building, always as
// warnings.
func BuildWarningIDs() []string { return slices.Clone(warnIDs) }

// DefaultSeverity is id's severity when the config says nothing: a
// registered rule's Default, warn for a build warning, else error.
func DefaultSeverity(id string) diag.Severity {
	for _, r := range allRules {
		if r.ID == id {
			return r.Default
		}
	}
	if slices.Contains(warnIDs, id) {
		return diag.Warn
	}
	return diag.Error
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
// A rule's severity is config lint.rules over its Default, applied to the
// findings under the rule's own ID; an off rule's own findings are dropped,
// and so are the exemptions it recorded, since they suppressed nothing; it
// is still tallied. Findings a rule reports under another ID (such as
// schema-invalid) keep their severity whatever the rule's setting. A rule that scanned nothing and
// gave no reason is marked not applicable ("no <noun> in scope"), so an
// empty scan never reads as a clean check. Strict then turns warnings
// into errors. Every diagnostic is linked to its rule's page and sorted.
func Run(p *project.Project, loadDiags []diag.Diagnostic, opts Options) Result {
	all := slices.Clone(loadDiags)
	if p.Tokens != nil {
		all = append(all, p.Tokens.Check()...)
	}
	ctx := &Context{Generated: opts.Generated}
	res := Result{Diagnostics: []diag.Diagnostic{}}
	off := map[string]bool{}
	for _, r := range Rules() {
		ds, tally := r.Run(p, ctx)
		tally.Rule = r.ID
		if tally.Items == 0 && tally.NotApplicable == "" {
			noun := tally.Noun
			if noun == "" {
				noun = "items"
			}
			tally.NotApplicable = "no " + noun + " in scope"
		}
		res.Tallies = append(res.Tallies, tally)
		sev := r.Default
		if s, ok := p.Config.Lint.Rules[r.ID]; ok {
			sev = s
		}
		if sev == diag.Off {
			off[r.ID] = true
		}
		// The configured severity is the rule's own: a finding it passes
		// along under another ID (schema-invalid for an invariant it
		// cannot compute, emit-failed for a build that broke) keeps its
		// severity and is never dropped by turning the rule off.
		for _, d := range ds {
			if d.Rule == r.ID {
				if sev == diag.Off {
					continue
				}
				d.Severity = sev
			}
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
	res.Err = ctx.err
	res.Exemptions = sortedExemptions(slices.DeleteFunc(ctx.exemptions, func(e Exemption) bool { return off[e.Rule] }))
	return res
}

// sortedExemptions returns es sorted and deduplicated, never nil.
func sortedExemptions(es []Exemption) []Exemption {
	out := append([]Exemption{}, es...)
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
