// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/rulingref"
	"github.com/c65llc/airframe/internal/tokens"
)

// rulingIndex is the run's one rulingref.Resolve, memoised: the Index (nil
// when there is no Gantry project, so resolution is syntax only) and the
// diagnostics resolving gave, which component-unruled reports once.
func (c *Context) rulingIndex(p *project.Project) (*rulingref.Index, []diag.Diagnostic) {
	if !c.rulingsDone {
		c.rulingsDone = true
		if p.FS != nil {
			c.rulings, c.rulingDiags = rulingref.Resolve(p.FS, p.Config)
		}
	}
	return c.rulings, c.rulingDiags
}

// sortedComponents are p's loaded components in id order.
func sortedComponents(p *project.Project) []*component.Component {
	out := make([]*component.Component, 0, len(p.Components))
	for _, id := range slices.Sorted(maps.Keys(p.Components)) {
		out = append(out, p.Components[id])
	}
	return out
}

// rulingProblem says what is wrong with ruling ref, or "" when it is
// ruled (or well formed, with no Gantry project to resolve it against).
// It is "-" when the ruling's record did not read: Resolve already
// reported that, at the record.
func rulingProblem(ix *rulingref.Index, ref string) string {
	m := rulingref.RefRE.FindStringSubmatch(ref)
	if m == nil {
		return fmt.Sprintf("ruling %q is not @ruling:<id> (the id is a-z, 0-9 and -, starting with a letter or digit)", ref)
	}
	if ix == nil {
		return ""
	}
	id := m[1]
	status, by, ok := ix.Lookup(id)
	switch {
	case !ok && ix.Unreadable(id):
		return "-"
	case !ok:
		return fmt.Sprintf("ruling %s does not exist: the Gantry project (%s) has no rulings/%s.md", id, ix.File(), id)
	case status == "ruled":
		return ""
	case status == "proposed":
		return fmt.Sprintf("ruling %s is proposed, not yet ruled; a component answers to a ruled ruling", id)
	case status == "superseded" && by != "":
		return fmt.Sprintf("ruling %s is superseded by %s; point ruling at @ruling:%s", id, by, by)
	case status == "superseded":
		return fmt.Sprintf("ruling %s is superseded, and names no supersededBy; point ruling at the ruling that replaced it", id)
	}
	return fmt.Sprintf("ruling %s is %s, not ruled", id, status)
}

// ruleComponentUnruled reports every component whose ruling is missing,
// malformed or, with a Gantry project, not a ruled ruling; and every
// retired treatment whose (optional) ruling is wrong in the same ways. It
// also passes along, once, what resolving the rulings reported: each
// unreadable ruling record (its own ID) and each gantry.yaml problem
// (schema-invalid or yaml-syntax), which keep their severity.
func ruleComponentUnruled(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "component-unruled"
	tally := diag.Tally{Noun: "components and retired treatments"}
	ix, rds := c.rulingIndex(p)
	out := slices.Clone(rds)
	files := map[string]bool{}
	for _, comp := range sortedComponents(p) {
		tally.Items++
		files[comp.File] = true
		msg := ""
		if comp.Ruling == "" {
			msg = fmt.Sprintf(`component %s has no ruling; a component with no ruling is not in the system: add ruling: "@ruling:<id>"`, comp.ID)
		} else if why := rulingProblem(ix, comp.Ruling); why != "" && why != "-" {
			msg = fmt.Sprintf("component %s: %s", comp.ID, why)
		}
		if msg != "" {
			out = append(out, diag.Diagnostic{
				Rule: id, Severity: diag.Error, File: comp.File, Path: "ruling", Line: comp.LineOf("ruling"),
				Message: msg, Docs: diag.DocsURL(id),
			})
		}
	}
	for i, t := range p.Retired {
		tally.Items++
		files[t.File] = true
		if t.Ruling == "" {
			continue
		}
		if why := rulingProblem(ix, t.Ruling); why != "" && why != "-" {
			out = append(out, diag.Diagnostic{
				Rule: id, Severity: diag.Error, File: t.File, Path: fmt.Sprintf("%d.ruling", i), Line: t.Line,
				Message: fmt.Sprintf("retired treatment %s: %s", t.ID, why), Docs: diag.DocsURL(id),
			})
		}
	}
	tally.Files = len(files)
	if tally.Items == 0 {
		tally.NotApplicable = "no components or retired treatments declared"
	}
	return out, tally
}

// ruleComponentIncomplete reports a component whose inventory is not
// complete and true: an implementations key that is not a configured
// platform, a platform that declares consumers with no implementations
// entry, a value that is neither an existing repo-relative file nor
// "n/a: <reason>", a hostedBy naming no component, and each hostedBy
// cycle, once, at its first member in id order.
func ruleComponentIncomplete(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "component-incomplete"
	tally := diag.Tally{Noun: "components"}
	comps := sortedComponents(p)
	if len(comps) == 0 {
		tally.NotApplicable = "no components declared"
		return nil, tally
	}
	var out []diag.Diagnostic
	find := func(comp *component.Component, at, format string, a ...any) {
		out = append(out, diag.Diagnostic{
			Rule: id, Severity: diag.Error, File: comp.File, Path: at, Line: comp.LineOf(at),
			Message: fmt.Sprintf("component %s: ", comp.ID) + fmt.Sprintf(format, a...), Docs: diag.DocsURL(id),
		})
	}
	platforms := map[string]config.Platform{}
	var names []string
	for _, pl := range p.Config.Platforms {
		platforms[pl.Name] = pl
		names = append(names, pl.Name)
	}
	slices.Sort(names)
	for _, comp := range comps {
		tally.Items++
		tally.Files++
		for _, k := range slices.Sorted(maps.Keys(comp.Implementations)) {
			at := "implementations." + k
			if _, ok := platforms[k]; !ok {
				known := "no platform is configured"
				if len(names) > 0 {
					known = "platforms: " + strings.Join(names, ", ")
				}
				find(comp, at, "implementations names %s, which is not a configured platform (%s)", k, known)
				continue
			}
			if why := implementationProblem(p.FS, comp.Implementations[k]); why != "" {
				find(comp, at, "implementations.%s: %s", k, why)
			}
		}
		for _, pl := range p.Config.Platforms {
			if len(pl.Consumers) == 0 {
				continue
			}
			if _, ok := comp.Implementations[pl.Name]; !ok {
				find(comp, "implementations", `platform %s declares consumers, but implementations has no %s entry; name the file that builds the component there, or "n/a: <reason>"`, pl.Name, pl.Name)
			}
		}
		if h := comp.HostedBy; h != "" && p.Components[h] == nil {
			file := path.Join(p.Config.Content, component.Dir, h+".yaml")
			if !slices.Contains(p.ComponentFiles, file) { // else it did not load: schema-invalid says why
				find(comp, "hostedBy", "hostedBy %s is not a component (no %s)", h, file)
			}
		}
	}
	for _, cycle := range hostCycles(p.Components) {
		first := p.Components[cycle[0]]
		find(first, "hostedBy", "hostedBy cycles: %s; a host chain must end at a component with no hostedBy",
			strings.Join(append(cycle, cycle[0]), " → "))
	}
	return out, tally
}

// implementationProblem says what is wrong with implementations value v,
// or "": it must be "n/a: <reason>" or a repo-relative file that exists.
func implementationProblem(fsys fs.FS, v string) string {
	if rest, ok := strings.CutPrefix(v, "n/a"); ok && (rest == "" || strings.HasPrefix(rest, ":")) {
		if strings.TrimSpace(strings.TrimPrefix(rest, ":")) == "" {
			return `n/a needs a reason: "n/a: <why this platform does not build it>"`
		}
		return ""
	}
	if strings.Contains(v, `\`) || !fs.ValidPath(v) || v == "." {
		return fmt.Sprintf(`%q is not a repo-relative file path (a clean slash path under the project root) nor "n/a: <reason>"`, v)
	}
	// Like the consumer scan and rulingref, a symlink is never followed:
	// not the file, nor any directory on its path. fs.Lstat is fs.Stat on
	// an FS that cannot read links.
	parts := strings.Split(v, "/")
	for i := 1; i < len(parts); i++ {
		at := strings.Join(parts[:i], "/")
		if info, err := fs.Lstat(fsys, at); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Sprintf("%s is under %s, which is a symlink; not followed", v, at)
		}
	}
	info, err := fs.Lstat(fsys, v)
	switch {
	case err == nil && info.Mode()&fs.ModeSymlink != 0:
		return fmt.Sprintf("%s is a symlink; not followed", v)
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("%s does not exist; implementations are checked, so name the file that builds the component", v)
	case err != nil:
		return fmt.Sprintf("%s cannot be read: %v", v, err)
	case info.IsDir():
		return fmt.Sprintf("%s is a directory, not a file", v)
	}
	return ""
}

// hostCycles are the hostedBy cycles among cs, each once: its members in
// chain order starting from its least id, the cycles sorted by that id.
// Each component has at most one host, so every walk ends at a component
// with none, an unknown one, or a cycle.
func hostCycles(cs map[string]*component.Component) [][]string {
	const (
		unseen = iota
		onPath
		done
	)
	state := map[string]int{}
	var out [][]string
	for _, start := range slices.Sorted(maps.Keys(cs)) {
		var walk []string
		at := start
		for cs[at] != nil && state[at] == unseen {
			state[at] = onPath
			walk = append(walk, at)
			at = cs[at].HostedBy
		}
		if cs[at] != nil && state[at] == onPath {
			cycle := slices.Clone(walk[slices.Index(walk, at):])
			least := slices.Index(cycle, slices.Min(cycle))
			out = append(out, append(cycle[least:], cycle[:least]...))
		}
		for _, w := range walk {
			state[w] = done
		}
	}
	slices.SortFunc(out, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	return out
}

// ruleRetiredUsed reports what retirement forbids: a consumer reference,
// direct or through a wrapper, to a retired token; a non-retired token
// that aliases a retired one directly (at the token, so its exempt map
// applies); and a retired.yaml spelling matched in a consumer file read by
// the spelling's adapter. A treatment's marker with a reason escapes its
// spellings, as airframe-exempt retired-used does. It examines every
// resolved reference, every token and every (file, treatment) spelling
// search.
func ruleRetiredUsed(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "retired-used"
	tally := diag.Tally{Noun: "references, tokens and spelling checks"}
	anyRetired := len(p.Retired) > 0
	if p.Tokens != nil {
		anyRetired = anyRetired || slices.ContainsFunc(p.Tokens.Order, func(n string) bool { return p.Tokens.Tokens[n].Retired })
	}
	if !anyRetired {
		tally.NotApplicable = "no retired tokens or treatments"
		return nil, tally
	}
	var out []diag.Diagnostic
	if p.Tokens != nil {
		files := map[string]bool{}
		for _, name := range p.Tokens.Order {
			t := p.Tokens.Tokens[name]
			tally.Items++
			files[t.File] = true
			if !t.Retired {
				out = append(out, retiredAliases(p, c, t)...)
			}
		}
		tally.Files += len(files)
	}
	s, _ := c.consumerScan(p)
	if s == nil {
		return out, tally
	}
	tally.Files += consumerFiles(s)
	tally.Items += s.SpellingChecks
	for _, r := range s.Refs {
		t := p.Tokens.Tokens[r.Token]
		if r.Token == "" || t == nil {
			continue
		}
		tally.Items++
		if t.Retired {
			out = add(out, c.consumerFinding(s, id, r.Platform, r.File, r.Line, r.Col, fmt.Sprintf(
				"platform %s: %s is token %s, which is retired; it is still emitted so code compiles while it migrates, but nothing may read it: read what replaced it",
				r.Platform, refText(r), t.Name)))
		}
	}
	markers := map[string]string{}
	for _, t := range p.Retired {
		markers[t.ID] = t.Marker
	}
	for _, pr := range s.Problems {
		if pr.Kind == consumer.RetiredSpelling {
			out = add(out, c.markedFinding(s, id, markers[pr.Treatment], pr.Platform, pr.File, pr.Line, pr.Col, pr.Detail))
		}
	}
	return out, tally
}

// retiredAliases reports each retired token t aliases directly, on the
// first hop, from its base value or any mode value: once per retired
// token, at the first such value (base, then modes in key order). A chain
// that passes through a non-retired token is that token's finding, and is
// fixed by fixing it.
func retiredAliases(p *project.Project, c *Context, t *tokens.Token) []diag.Diagnostic {
	const id = "retired-used"
	seen := map[string]bool{}
	var out []diag.Diagnostic
	for _, tv := range tokenValues(t) {
		x := p.Tokens.Tokens[tv.v.Alias]
		if tv.v.Alias == "" || x == nil || !x.Retired || seen[x.Name] {
			continue
		}
		seen[x.Name] = true
		where := ""
		if tv.key != "" {
			where = " (" + string(tv.key) + ")"
		}
		out = add(out, c.TokenFinding(id, t, p.Tokens.FileOf(tv.v), tv.v.Line, fmt.Sprintf(
			"token %s%s aliases retired token %s; a retired token is kept only while code migrates off it, so alias what replaced it",
			t.Name, where, x.Name)))
	}
	return out
}

type keyedValue struct {
	key config.ModeKey
	v   *tokens.Value
}

// tokenValues are t's values: its base value (key "") first, then its mode
// values in key order.
func tokenValues(t *tokens.Token) []keyedValue {
	keys, vals := t.Values()
	out := make([]keyedValue, len(keys))
	for i := range keys {
		out[i] = keyedValue{keys[i], vals[i]}
	}
	return out
}
