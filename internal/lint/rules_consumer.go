// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/schema"
	"github.com/c65llc/airframe/internal/tokens"
)

// referencesNoun is the Tally noun of every consumer rule.
const referencesNoun = "references"

// consumerScan is the run's one consumer.Scan, memoised. It is nil, with
// a reason, when the rules have nothing to read: no platform declares
// consumers, or no tokens loaded. An I/O failure is the run's error (Run
// returns it as Result.Err, which the CLI exits 2 on) and also nil here.
func (c *Context) consumerScan(p *project.Project) (*consumer.Result, string) {
	if !c.scanned {
		c.scanned = true
		switch {
		case !slices.ContainsFunc(p.Config.Platforms, func(pl config.Platform) bool { return len(pl.Consumers) > 0 }):
			c.scanNA = "no platform declares consumers"
		case p.Tokens == nil:
			c.scanNA = "no tokens loaded"
		default:
			res, err := consumer.Scan(p)
			if err != nil {
				c.scanNA = "the consumer scan failed: " + err.Error()
				c.fail(err)
			} else {
				c.scan = &res
			}
		}
	}
	return c.scan, c.scanNA
}

// fail records the run's first fatal error.
func (c *Context) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// consumerFiles is how many consumer files the scan read, every platform's:
// a file two platforms read counts once for each.
func consumerFiles(s *consumer.Result) int {
	n := 0
	for _, k := range s.Files {
		n += k
	}
	return n
}

// consumerFinding is rule's finding on a consumer file at line:col. An
// airframe-exempt marker for the rule there with a reason suppresses it
// (nil) and records the exemption; one without a reason suppresses
// nothing and says so at the end of the message.
func (c *Context) consumerFinding(s *consumer.Result, rule, platform, file string, line, col int, msg string) *diag.Diagnostic {
	if file != config.File {
		var bare []int
		for _, e := range s.Exempt {
			if e.Rule != rule || e.Platform != platform || e.File != file || e.Line != line {
				continue
			}
			if strings.TrimSpace(e.Reason) == "" {
				bare = append(bare, e.MarkerLine)
				continue
			}
			c.exemptions = append(c.exemptions, Exemption{
				Rule: rule, File: file, Line: line, Marker: "airframe-exempt", Reason: e.Reason,
			})
			return nil
		}
		for _, ml := range slices.Compact(bare) {
			msg += fmt.Sprintf(" (airframe-exempt on line %d has no reason)", ml)
		}
	}
	return &diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: file, Line: line, Col: col,
		Message: msg, Docs: diag.DocsURL(rule),
	}
}

// problemFinding reports scan problem pr under rule. A problem about a
// platform's config (an empty scan, unavailable names) is located at
// airframe.yaml under platforms.<p>.consumers, or .emit for names.
func (c *Context) problemFinding(s *consumer.Result, rule string, pr consumer.Problem) *diag.Diagnostic {
	if pr.File == config.File {
		at := "platforms." + pr.Platform + ".consumers"
		if pr.Kind == consumer.NamesUnavailable {
			at = "platforms." + pr.Platform + ".emit"
		}
		return &diag.Diagnostic{
			Rule: rule, Severity: diag.Error, File: pr.File, Path: at,
			Message: pr.Detail, Docs: diag.DocsURL(rule),
		}
	}
	return c.consumerFinding(s, rule, pr.Platform, pr.File, pr.Line, pr.Col, pr.Detail)
}

// problemRule reports every scan problem of the given kinds under rule,
// tallying refs that keep.
func problemRule(p *project.Project, c *Context, rule string, kinds []string, keep func(consumer.Reference) bool) ([]diag.Diagnostic, diag.Tally) {
	tally := diag.Tally{Noun: referencesNoun}
	s, na := c.consumerScan(p)
	if s == nil {
		tally.NotApplicable = na
		return nil, tally
	}
	tally.Files = consumerFiles(s)
	for _, r := range s.Refs {
		if keep(r) {
			tally.Items++
		}
	}
	var out []diag.Diagnostic
	for _, pr := range s.Problems {
		if slices.Contains(kinds, pr.Kind) {
			out = add(out, c.problemFinding(s, rule, pr))
		}
	}
	return out, tally
}

func anyRef(consumer.Reference) bool { return true }

// ruleReferenceUndeclared reports the references to names that are not
// tokens, each consumers glob that matched no file, and each platform
// whose names could not be rendered. It examines every reference and
// every consumers glob, so a run whose globs are all dead still counts
// what it checked. It also passes along the scan's schema-invalid
// problems (a file no adapter reads, a bad glob, a symlink), once: no
// other consumer rule does.
func ruleReferenceUndeclared(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "reference-undeclared"
	out, tally := problemRule(p, c, id,
		[]string{consumer.Undeclared, consumer.EmptyScan, consumer.NamesUnavailable}, anyRef)
	if s, _ := c.consumerScan(p); s != nil {
		tally.Noun = "references and globs"
		for _, pl := range p.Config.Platforms {
			tally.Items += len(pl.Consumers)
		}
		for _, pr := range s.Problems {
			if pr.Kind == consumer.SchemaInvalid {
				out = add(out, c.problemFinding(s, schema.RuleID, pr))
			}
		}
	}
	return out, tally
}

// ruleFallbackLiteral reports every var() whose fallback is a literal. It
// examines the css references: those in files read through css names.
func ruleFallbackLiteral(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "fallback-literal"
	s, _ := c.consumerScan(p)
	if s != nil {
		css := cssFiles(p, s)
		if len(css) == 0 {
			return nil, diag.Tally{Noun: referencesNoun, NotApplicable: "no css consumer files"}
		}
		out, tally := problemRule(p, c, id, []string{consumer.FallbackLiteral}, func(r consumer.Reference) bool {
			return r.Via == consumer.Direct && css[r.Platform+"\x00"+r.File]
		})
		tally.Files = len(css) // once per platform, as consumerFiles counts
		return out, tally
	}
	return problemRule(p, c, id, nil, anyRef)
}

// cssFiles are the scanned consumer files read through css names, keyed
// platform NUL file.
func cssFiles(p *project.Project, s *consumer.Result) map[string]bool {
	out := map[string]bool{}
	for _, pl := range p.Config.Platforms {
		for _, f := range s.Paths[pl.Name] {
			if consumer.Format(pl, f) == "css" {
				out[pl.Name+"\x00"+f] = true
			}
		}
	}
	return out
}

// ruleWrapperUnmapped reports every wrapper read that stands for no
// token, and every wrapper map entry whose token does not exist, whether
// or not anything reads it (a pattern's token depends on its captures, so
// it is checked only when read). It examines the wrapper reads and the
// map entries. A read through a dangling map entry is still reported where
// it is, pointing at the entry.
func ruleWrapperUnmapped(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "wrapper-unmapped"
	hasWrappers := slices.ContainsFunc(p.Config.Platforms, func(pl config.Platform) bool { return len(pl.Wrappers) > 0 })
	s, na := c.consumerScan(p)
	switch {
	case !hasWrappers && s == nil:
		return nil, diag.Tally{Noun: referencesNoun, NotApplicable: na}
	case !hasWrappers:
		return nil, diag.Tally{Noun: referencesNoun, NotApplicable: "no platform declares wrappers"}
	case p.Tokens == nil:
		return nil, diag.Tally{Noun: referencesNoun, NotApplicable: "no tokens loaded"}
	}
	tally := diag.Tally{Noun: "references and map entries"}
	var out []diag.Diagnostic
	dangling := map[string]string{} // platform NUL root.member → its path
	for _, pl := range p.Config.Platforms {
		for i, w := range pl.Wrappers {
			for _, member := range slices.Sorted(maps.Keys(w.Map)) {
				tally.Items++
				tok := w.Map[member]
				if _, ok := p.Tokens.Tokens[tok]; ok {
					continue
				}
				at := fmt.Sprintf("platforms.%s.wrappers.%d.map.%s", pl.Name, i, member)
				dangling[pl.Name+"\x00"+w.Root+"."+member] = at
				out = append(out, diag.Diagnostic{
					Rule: id, Severity: diag.Error, File: config.File, Path: at, Docs: diag.DocsURL(id),
					Message: fmt.Sprintf("platform %s: wrapper %s maps %s to %s, which is not a token; reads are always declared, and this one declares a token that does not exist",
						pl.Name, w.Root, member, tok),
				})
			}
		}
	}
	if s == nil {
		return out, tally
	}
	tally.Files = consumerFiles(s)
	for _, r := range s.Refs {
		if strings.HasPrefix(r.Via, "wrapper:") {
			tally.Items++
		}
	}
	for _, pr := range s.Problems {
		if pr.Kind != consumer.WrapperUnmapped {
			continue
		}
		if at, ok := dangling[pr.Platform+"\x00"+pr.Raw]; ok {
			pr.Detail = fmt.Sprintf("%s reads a wrapper map entry whose token does not exist; see %s", pr.Raw, at)
		}
		out = add(out, c.problemFinding(s, id, pr))
	}
	return out, tally
}

// resolvedRule runs check on every resolved reference, tallying them.
func resolvedRule(p *project.Project, c *Context, check func(s *consumer.Result, r consumer.Reference, t *tokens.Token) *diag.Diagnostic) ([]diag.Diagnostic, diag.Tally) {
	tally := diag.Tally{Noun: referencesNoun}
	s, na := c.consumerScan(p)
	if s == nil {
		tally.NotApplicable = na
		return nil, tally
	}
	tally.Files = consumerFiles(s)
	var out []diag.Diagnostic
	for _, r := range s.Refs {
		t := p.Tokens.Tokens[r.Token]
		if r.Token == "" || t == nil {
			continue
		}
		tally.Items++
		out = add(out, check(s, r, t))
	}
	return out, tally
}

// refText is how a message names reference r: its text, and the wrapper
// it reads through.
func refText(r consumer.Reference) string {
	if root, ok := strings.CutPrefix(r.Via, "wrapper:"); ok {
		return fmt.Sprintf("%s (through wrapper %s)", r.Raw, root)
	}
	return r.Raw
}

// rulePrimitiveRead reports a consumer reference to a token whose tier's
// readableBy lacks consumers. A component token is the owner rule's to
// judge, and a token with no tier is token-unclassified's.
func rulePrimitiveRead(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "primitive-read-by-consumer"
	return resolvedRule(p, c, func(s *consumer.Result, r consumer.Reference, t *tokens.Token) *diag.Diagnostic {
		if t.Tier == "" || t.Tier == tierComponent {
			return nil
		}
		readers := p.Config.Tiers[t.Tier].ReadableBy
		if slices.Contains(readers, "consumers") {
			return nil
		}
		by := "nothing"
		if len(readers) > 0 {
			by = strings.Join(readers, ", ")
		}
		return c.consumerFinding(s, id, r.Platform, r.File, r.Line, r.Col, fmt.Sprintf(
			"platform %s: %s is token %s, of tier %s, which consumers may not read (it is readable by %s); %s",
			r.Platform, refText(r), t.Name, t.Tier, by, consumerTiersHint(p)))
	})
}

// consumerTiersHint names the tiers consumers may read, from the config.
func consumerTiersHint(p *project.Project) string {
	var ok []string
	for _, name := range slices.Sorted(maps.Keys(p.Config.Tiers)) {
		if slices.Contains(p.Config.Tiers[name].ReadableBy, "consumers") {
			ok = append(ok, name)
		}
	}
	switch len(ok) {
	case 0:
		return "no tier's readableBy lists consumers, so consumers may read no token outside their components"
	case 1:
		return "read a token of tier " + ok[0] + " instead"
	}
	return "read a token of tier " + strings.Join(ok, " or ") + " instead"
}

// ruleOwnerViolation reports a consumer reference to a component token
// from a file none of its component's owners globs match, unless the
// component is shared. A component with no components/<id>.yaml declares
// no owners, so every read of its tokens is a violation; one whose file
// failed to load is already a schema-invalid finding and is not reported
// again.
func ruleOwnerViolation(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "token-owner-violation"
	return resolvedRule(p, c, func(s *consumer.Result, r consumer.Reference, t *tokens.Token) *diag.Diagnostic {
		if t.Tier != tierComponent || t.Component == "" {
			return nil
		}
		find := func(why string) *diag.Diagnostic {
			return c.consumerFinding(s, id, r.Platform, r.File, r.Line, r.Col, fmt.Sprintf(
				"platform %s: %s is token %s of component %s, but %s", r.Platform, refText(r), t.Name, t.Component, why))
		}
		comp := p.Components[t.Component]
		if comp == nil {
			file := path.Join(p.Config.Content, component.Dir, t.Component+".yaml")
			if slices.Contains(p.ComponentFiles, file) {
				return nil // it did not load: schema-invalid says why
			}
			return find(fmt.Sprintf("component %s declares no owners (no %s/%s.yaml)", t.Component, component.Dir, t.Component))
		}
		if comp.Shared {
			return nil
		}
		for _, g := range comp.Owners {
			if ok, _ := doublestar.Match(g, r.File); ok {
				return nil
			}
		}
		return find(fmt.Sprintf("%s matches none of its owners (%s)", r.File, strings.Join(comp.Owners, ", ")))
	})
}
