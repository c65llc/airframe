// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package importer turns hand-written CSS custom properties into DTCG token
// files for an existing Airframe project. Each CSS block is mapped back to
// a mode key by inverting the emitter's own scope expansion, so the config's
// axes and web spellings are the only mapping: the importer never invents
// an axis.
package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/cssvalue"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/tokens"
)

// Result is what an import produced. Files maps repo-relative paths to
// canonical token-file bytes; it is empty when any diagnostic is an error.
// Tokens counts the distinct custom properties read, Declarations the
// custom-property declarations, and Ignored the declarations that are not
// custom properties.
type Result struct {
	Files                         map[string][]byte
	Tokens, Declarations, Ignored int
	Diagnostics                   []diag.Diagnostic
}

// Source is one stylesheet to import, by name.
type Source struct {
	File string
	Data []byte
}

// The token files an import writes, under <content>/.
const (
	primitiveFile = "tokens/primitive.tokens.json"
	semanticFile  = "tokens/semantic.tokens.json"
)

// ImportSources parses each source with css.Parse, in order (later sources
// win the cascade), and imports the sheets. The parse diagnostics join the
// import's own in Result.Diagnostics, sorted, and any error among them
// leaves Files empty.
func ImportSources(cfg *config.Config, srcs []Source) Result {
	var sheets []*css.Sheet
	var parsed []diag.Diagnostic
	for _, s := range srcs {
		sh, ds := css.Parse(s.File, s.Data)
		sheets = append(sheets, sh)
		parsed = append(parsed, ds...)
	}
	r := Import(cfg, sheets)
	r.Diagnostics = append(parsed, r.Diagnostics...)
	diag.Sort(r.Diagnostics)
	if hasError(r.Diagnostics) {
		r.Files = map[string][]byte{}
	}
	return r
}

// Import maps every rule of sheets to a mode key and builds the token
// files. Problems are diagnostics; with any error, Files is empty.
func Import(cfg *config.Config, sheets []*css.Sheet) Result {
	im := &importer{cfg: cfg, toks: map[string]*token{}, declared: map[string]bool{}, failed: map[string]bool{}}
	r := im.run(sheets)
	r.Diagnostics = im.diags
	diag.Sort(r.Diagnostics)
	if r.Files == nil || hasError(r.Diagnostics) {
		r.Files = map[string][]byte{}
	}
	return r
}

func hasError(ds []diag.Diagnostic) bool {
	return slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Severity == diag.Error })
}

// entry is one token value under one mode key.
type entry struct {
	typ     string // "" for an alias until resolved
	val     *tokens.Value
	enc     string // the value's DTCG JSON, to compare spellings
	file    string
	line    int
	scope   string // the scope key it was declared in
	comment string // the cleaned $description, for a base value
}

func (e *entry) at() string { return fmt.Sprintf("%s:%d", e.file, e.line) }

// token is one custom property, with its values by mode key.
type token struct {
	name      string
	vals      map[config.ModeKey]*entry
	file      string // where it first appears
	line      int
	typ       string // resolved $type
	typed     bool   // typ has been resolved (or failed)
	resolving bool
}

func (t *token) keys() []config.ModeKey {
	return slices.SortedFunc(maps.Keys(t.vals), compareKeys)
}

type importer struct {
	cfg      *config.Config
	toks     map[string]*token
	order    []string        // token names in first-appearance order
	declared map[string]bool // every custom property named in the CSS
	failed   map[string]bool // tokens already reported, kept out of later checks
	diags    []diag.Diagnostic
}

func (im *importer) report(sev diag.Severity, rule, file string, line int, format string, a ...any) {
	im.diags = append(im.diags, diag.Diagnostic{
		Rule: rule, Severity: sev, File: file, Line: line,
		Message: fmt.Sprintf(format, a...), Docs: diag.DocsURL(rule),
	})
}

func (im *importer) errorf(rule, file string, line int, format string, a ...any) {
	im.report(diag.Error, rule, file, line, format, a...)
}

func (im *importer) run(sheets []*css.Sheet) Result {
	var r Result
	idx, err := buildIndex(im.cfg)
	if err != nil {
		im.errorf(RuleUnmappedScope, config.File, 0, "%v", err)
		return r
	}
	for _, sh := range sheets {
		r.Ignored += sh.Ignored
		for _, rule := range sh.Rules {
			r.Declarations += len(rule.Decls)
			for _, d := range rule.Decls {
				im.declared[strings.TrimPrefix(d.Name, "--")] = true
			}
			im.rule(sh.File, rule, idx)
		}
	}
	r.Tokens = len(im.order)
	if r.Declarations == 0 {
		im.nothing(sheets)
		return r
	}
	im.checkTargets()
	im.checkBase()
	im.checkCycles()
	for _, name := range im.order {
		im.typeOf(name)
	}
	if hasError(im.diags) {
		return r
	}
	r.Files = im.files()
	return r
}

func (im *importer) nothing(sheets []*css.Sheet) {
	file := ""
	var names []string
	for _, sh := range sheets {
		names = append(names, sh.File)
	}
	if len(names) > 0 {
		file = names[0]
	}
	what := strings.Join(names, ", ")
	if what == "" {
		what = "no stylesheets"
	}
	im.errorf(RuleNothing, file, 0, "no custom properties to import in %s", what)
}

// rule maps each selector of rule to a mode key and records its
// declarations under every key it maps to.
func (im *importer) rule(file string, rule css.Rule, idx map[string]config.ModeKey) {
	atoms, err := css.ParseMedia(rule.Media)
	if err != nil {
		im.errorf(css.RuleUnsupportedMedia, file, rule.Line, "%v", err)
		return
	}
	type target struct {
		key   config.ModeKey
		scope string
	}
	var targets []target
	for _, raw := range rule.Selectors {
		sel, err := css.ParseSelector(raw)
		if err != nil {
			im.errorf(css.RuleUnsupportedSelector, file, rule.Line, "%v", err)
			continue
		}
		sk := scopeKey(atoms, sel)
		key, ok := idx[sk]
		if !ok {
			im.unmapped(file, rule.Line, atoms, raw, sel)
			continue
		}
		targets = append(targets, target{key, sk})
	}
	if len(targets) == 0 {
		return
	}
	for _, d := range rule.Decls {
		name := strings.TrimPrefix(d.Name, "--")
		if !tokens.ValidName(name) {
			im.errorf(RuleNameInvalid, file, d.Line,
				"%s: a token name must match %s (lowercase words joined by -)", d.Name, tokens.NamePattern())
			continue
		}
		typ, val, warn, err := cssvalue.Parse(d.Name, d.Value)
		if err != nil {
			im.errorf(css.RuleUnsupportedValue, file, d.Line, "%s: %v", d.Name, err)
			continue
		}
		if warn != "" {
			im.report(diag.Warn, RuleFallbackDropped, file, d.Line, "%s: %s", d.Name, warn)
		}
		val.Line = d.Line
		enc, err := encode(val)
		if err != nil {
			im.errorf(css.RuleUnsupportedValue, file, d.Line, "%s: %v", d.Name, err)
			continue
		}
		for _, tg := range targets {
			e := &entry{typ: typ, val: val, enc: enc, file: file, line: d.Line, scope: tg.scope}
			if tg.key == "" {
				e.comment = cleanComment(d.Comment)
			}
			im.add(name, tg.key, e)
		}
	}
}

// unmapped reports a supported scope that no mode key produces, saying
// which web spelling would.
func (im *importer) unmapped(file string, line int, atoms []css.Atom, raw string, sel css.Selector) {
	hint := ""
	if len(atoms) == 0 && len(sel.Conds) == 1 && !sel.Conds[0].Negated && sel.Conds[0].Value != "" {
		c := sel.Conds[0]
		for _, a := range im.cfg.Axes {
			if a.Web.Attr == c.Attr {
				hint = fmt.Sprintf("; axis %s (web attr %s) has no value %s", a.Name, c.Attr, c.Value)
				break
			}
		}
	}
	block := strings.TrimSpace(raw)
	if len(atoms) > 0 {
		block = "@media " + css.MediaKey(atoms) + " { " + block + " }"
	}
	im.errorf(RuleUnmappedScope, file, line,
		"%s matches no mode in %s: an axis value whose web spelling is %s would produce it%s",
		block, config.File, spelling(atoms, raw), hint)
}

// add records e as name's value under key. A second value in the same
// scope, or a different value under another spelling of the same key, is
// a duplicate: the later one wins.
func (im *importer) add(name string, key config.ModeKey, e *entry) {
	t := im.toks[name]
	if t == nil {
		t = &token{name: name, vals: map[config.ModeKey]*entry{}, file: e.file, line: e.line}
		im.toks[name] = t
		im.order = append(im.order, name)
	}
	if prev := t.vals[key]; prev != nil {
		switch {
		case prev.scope == e.scope:
			im.report(diag.Warn, RuleDuplicate, e.file, e.line,
				"--%s is declared twice for %s (%s and %s); the later one wins", name, keyName(key), prev.at(), e.at())
		case prev.enc != e.enc:
			im.report(diag.Warn, RuleDuplicate, e.file, e.line,
				"--%s has different values under two spellings of %s (%s and %s); the later one wins", name, keyName(key), prev.at(), e.at())
		}
		if e.comment == "" && key == "" {
			e.comment = prev.comment
		}
	}
	t.vals[key] = e
}

// checkTargets reports an alias whose target the CSS never declares.
func (im *importer) checkTargets() {
	for _, name := range im.order {
		t := im.toks[name]
		for _, k := range t.keys() {
			e := t.vals[k]
			if e.val.Alias == "" || im.declared[e.val.Alias] {
				continue
			}
			im.errorf(css.RuleUnsupportedValue, e.file, e.line,
				"var(--%s) refers to a property the imported CSS does not declare", e.val.Alias)
			im.failed[name] = true
		}
	}
}

// checkBase reports a token with no :root value.
func (im *importer) checkBase() {
	for _, name := range im.order {
		t := im.toks[name]
		if _, ok := t.vals[""]; ok {
			continue
		}
		keys := make([]string, 0, len(t.vals))
		for _, k := range t.keys() {
			keys = append(keys, string(k))
		}
		im.errorf(RuleNoBase, t.file, t.line,
			"--%s is set only under %s and never on bare :root, so it has no $value", name, strings.Join(keys, ", "))
		im.failed[name] = true
	}
}

// targets lists the distinct alias targets of t's values, in key order.
func (im *importer) targets(t *token) []string {
	var out []string
	for _, k := range t.keys() {
		if a := t.vals[k].val.Alias; a != "" && im.toks[a] != nil && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

// checkCycles reports each alias cycle (through values in any modes) that
// a depth-first walk in token order finds, once, as a type conflict: a
// cycle's tokens take their type from each other. Its tokens are failed.
func (im *importer) checkCycles() {
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var stack []string
	var visit func(string)
	visit = func(n string) {
		color[n] = grey
		stack = append(stack, n)
		for _, m := range im.targets(im.toks[n]) {
			switch color[m] {
			case white:
				visit(m)
			case grey:
				at := slices.Index(stack, m)
				cycle := slices.Clone(stack[at:])
				for _, c := range cycle {
					im.failed[c] = true
				}
				// Locate the cycle at its first token's alias to the next.
				next := m
				if len(cycle) > 1 {
					next = cycle[1]
				}
				first := im.toks[cycle[0]]
				file, line := first.file, first.line
				for _, k := range first.keys() {
					if e := first.vals[k]; e.val.Alias == next {
						file, line = e.file, e.line
						break
					}
				}
				names := make([]string, 0, len(cycle)+1)
				for _, c := range append(cycle, m) {
					names = append(names, "--"+c)
				}
				im.errorf(RuleTypeConflict, file, line,
					"alias cycle %s: its tokens take their type from each other, so none has one", strings.Join(names, " → "))
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for _, n := range im.order {
		if color[n] == white {
			visit(n)
		}
	}
}

// typeOf resolves name's $type: the type of each literal value, and the
// target's type for each alias. One token has one type; a conflict is
// reported. ok is false when the token cannot be typed.
func (im *importer) typeOf(name string) (string, bool) {
	t := im.toks[name]
	if t == nil || im.failed[name] {
		return "", false
	}
	if t.typed {
		return t.typ, t.typ != ""
	}
	if t.resolving { // unreachable once cycles are failed; fail closed
		return "", false
	}
	t.resolving = true
	defer func() { t.resolving, t.typed = false, true }()
	var first *entry
	firstTyp := ""
	for _, k := range t.keys() {
		e := t.vals[k]
		typ := e.typ
		if e.val.Alias != "" {
			at, ok := im.typeOf(e.val.Alias)
			if !ok {
				im.failed[name] = true
				return "", false
			}
			typ = at
		}
		if first == nil {
			first, firstTyp = e, typ
			continue
		}
		if typ != firstTyp {
			im.errorf(RuleTypeConflict, e.file, e.line,
				"--%s is a %s %s and a %s %s; one token has one type",
				name, firstTyp, describe(first), typ, describe(e))
			im.failed[name] = true
			return "", false
		}
	}
	t.typ = firstTyp
	return t.typ, true
}

// describe locates a value for a type-conflict message.
func describe(e *entry) string {
	if e.val.Alias != "" {
		return fmt.Sprintf("(through var(--%s), %s)", e.val.Alias, e.at())
	}
	return "(" + e.at() + ")"
}

// encode is a value's DTCG JSON: "{name}" for an alias, else the
// emitter's own literal encoding.
func encode(v *tokens.Value) (string, error) {
	var x any = "{" + v.Alias + "}"
	if v.Alias == "" {
		x = emit.DTCGValue(v.Lit)
	}
	b, err := marshal(x)
	return string(b), err
}

func marshal(x any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(x); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// cleanComment turns a raw comment into a $description: the delimiters
// and a leading "*" on each line stripped, trimmed, whitespace runs
// collapsed to one space.
func cleanComment(raw string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(raw, "/*"), "*/")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		lines[i] = strings.TrimPrefix(l, "*")
	}
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

// files writes the token files: primitive (all-literal tokens) and
// semantic (any alias), flat keys in first-appearance order, each made
// canonical by format.File. A file with no tokens is not written.
func (im *importer) files() map[string][]byte {
	docs := map[string]*object{primitiveFile: {}, semanticFile: {}}
	for _, name := range im.order {
		t := im.toks[name]
		tier, file := "primitive", primitiveFile
		for _, e := range t.vals {
			if e.val.Alias != "" {
				tier, file = "semantic", semanticFile
				break
			}
		}
		base := t.vals[""]
		tok := &object{}
		tok.set("$type", t.typ)
		tok.set("$value", json.RawMessage(base.enc))
		if base.comment != "" {
			tok.set("$description", base.comment)
		}
		af := &object{}
		af.set("tier", tier)
		modes := &object{}
		for _, k := range t.keys() {
			if k != "" {
				modes.set(string(k), json.RawMessage(t.vals[k].enc))
			}
		}
		if len(modes.keys) > 0 {
			af.set("modes", modes)
		}
		ext := &object{}
		ext.set(tokens.Namespace, af)
		tok.set("$extensions", ext)
		docs[file].set(name, tok)
	}
	out := map[string][]byte{}
	for _, f := range []string{primitiveFile, semanticFile} {
		if len(docs[f].keys) == 0 {
			continue
		}
		p := path.Join(im.cfg.Content, f)
		raw, err := marshal(docs[f])
		if err == nil {
			raw, err = format.File(p, raw)
		}
		if err != nil {
			// The importer built this JSON itself; failing to format it
			// is a bug, not a problem with the input.
			panic(fmt.Sprintf("importer: formatting %s: %v", p, err))
		}
		out[p] = raw
	}
	return out
}

// object is a JSON object that keeps its keys in insertion order.
type object struct {
	keys []string
	vals []any
}

func (o *object) set(k string, v any) {
	o.keys = append(o.keys, k)
	o.vals = append(o.vals, v)
}

func (o *object) MarshalJSON() ([]byte, error) {
	out := []byte{'{'}
	for i, k := range o.keys {
		kb, err := marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := marshal(o.vals[i])
		if err != nil {
			return nil, err
		}
		if i > 0 {
			out = append(out, ',')
		}
		out = append(append(append(out, kb...), ':'), vb...)
	}
	return append(out, '}'), nil
}
