// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package rename plans a token rename across a project's sources: the
// token files, airframe.yaml, the component files and invariants.yaml. It
// computes every edit in memory and checks the result by loading it from
// an Overlay; it writes nothing.
package rename

import (
	"bytes"
	"cmp"
	"fmt"
	"path"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/invariants"
	"github.com/c65llc/airframe/internal/ojson"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// Edit is one textual replacement: Old, starting at 1-based Line and byte
// column Col of File as it was, becomes New. For a source file (one in
// EditPlan.Files) it reports a change whose result Files already holds.
type Edit struct {
	File      string
	Line, Col int
	Old, New  string
}

// Move is a token node moved from DTCG path From to To in File (dotted
// paths); Line is the node's line in File as it was.
type Move struct {
	File     string
	Line     int
	From, To string
}

// Manual is a reference the planner cannot rewrite and leaves to the user.
type Manual struct {
	File    string
	Line    int
	Message string
}

// EditPlan is a planned rename. Files holds the final, formatted bytes of
// every source file the rename changes, keyed by repo-relative path;
// generated outputs are not in it. After is the renamed project, loaded
// from Overlay(p.FS, Files).
type EditPlan struct {
	Old, New         string
	OldPath, NewPath []string
	Edits            []Edit
	Moves            []Move
	Manual           []Manual
	Files            map[string][]byte
	After            *project.Project
	// Consumers holds the final bytes of every consumer file Codemod
	// edits, and ConsumerSources the bytes those edits were spliced into;
	// both are nil from Plan, which leaves consumer files alone.
	Consumers, ConsumerSources map[string][]byte
}

// Refusal is a rename the planner will not make: a precondition failed, or
// the renamed project does not load (Diagnostics says why).
type Refusal struct {
	Msg         string
	Diagnostics []diag.Diagnostic
}

func (r *Refusal) Error() string { return r.Msg }

func refuse(format string, a ...any) error { return &Refusal{Msg: fmt.Sprintf(format, a...)} }

// Plan plans renaming token old to new. path is new's dotted DTCG path;
// when it is "" and old's path is old split on "-", new's is new split on
// "-", and otherwise path is required. A failed precondition is a
// *Refusal; any other error is I/O.
func Plan(p *project.Project, old, new, dotted string) (*EditPlan, error) {
	before, ds, err := project.Load(p.FS)
	if err != nil {
		return nil, err
	}
	if ds = errorsOnly(ds); before == nil || len(ds) > 0 {
		return nil, &Refusal{Msg: "rename: the project does not load cleanly; fix it first", Diagnostics: ds}
	}
	tok := p.Tokens.Tokens[old]
	switch {
	case tok == nil:
		return nil, refuse("rename: there is no token %s", old)
	case old == new:
		return nil, refuse("rename: %s and %s are the same name", old, new)
	case !tokens.ValidName(new):
		return nil, refuse("rename: %s is not a valid token name (%s)", new, tokens.NamePattern())
	case p.Tokens.Tokens[new] != nil:
		return nil, refuse("rename: token %s already exists", new)
	}
	newPath, err := pathFor(tok, old, new, dotted)
	if err != nil {
		return nil, err
	}

	pl := &EditPlan{Old: old, New: new, OldPath: tok.Path, NewPath: newPath, Files: map[string][]byte{}}
	r := &renamer{p: p, pl: pl, tok: tok}
	if err := r.tokenFiles(); err != nil {
		return nil, err
	}
	for _, step := range []func() error{r.config, r.components, r.invariants} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	sortPlan(pl)

	after, ds, err := project.Load(Overlay(p.FS, pl.Files))
	if err != nil {
		return nil, err
	}
	if ds = errorsOnly(ds); after == nil || len(ds) > 0 {
		return nil, &Refusal{Msg: fmt.Sprintf("rename: the renamed project does not load (%d problem(s))", len(ds)), Diagnostics: ds}
	}
	pl.After = after
	return pl, nil
}

func errorsOnly(ds []diag.Diagnostic) []diag.Diagnostic {
	return slices.DeleteFunc(slices.Clone(ds), func(d diag.Diagnostic) bool { return d.Severity != diag.Error })
}

// pathFor is new's DTCG path.
func pathFor(tok *tokens.Token, old, new, dotted string) ([]string, error) {
	if dotted != "" {
		segs := strings.Split(dotted, ".")
		if slices.Contains(segs, "") {
			return nil, refuse("rename: --path %s has an empty segment", dotted)
		}
		if got := strings.Join(segs, "-"); got != new {
			return nil, refuse("rename: --path %s names token %s, not %s", dotted, got, new)
		}
		return segs, nil
	}
	if slices.Equal(tok.Path, strings.Split(old, "-")) {
		return strings.Split(new, "-"), nil
	}
	return nil, refuse("rename: %s is at path %s, not %s; give the new path with --path",
		old, strings.Join(tok.Path, "."), strings.ReplaceAll(old, "-", "."))
}

func sortPlan(pl *EditPlan) {
	slices.SortStableFunc(pl.Edits, func(a, b Edit) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col))
	})
	slices.SortStableFunc(pl.Moves, func(a, b Move) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
	slices.SortStableFunc(pl.Manual, func(a, b Manual) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), strings.Compare(a.Message, b.Message))
	})
}

type renamer struct {
	p   *project.Project
	pl  *EditPlan
	tok *tokens.Token
}

// put stores file's new bytes, formatted, when they differ from the
// source.
func (r *renamer) put(file string, b []byte) error {
	out, err := format.File(file, b)
	if err != nil {
		return fmt.Errorf("format %s: %w", file, err)
	}
	if !bytes.Equal(out, r.p.Sources[file]) {
		r.pl.Files[file] = out
	}
	return nil
}

// columns finds where each edited string starts in a file's source lines,
// searching each line left to right as the edits are reported in source
// order.
type columns struct {
	lines [][]byte
	from  map[int]int // line → byte offset the next search starts at
}

func newColumns(src []byte) *columns {
	return &columns{lines: bytes.Split(src, []byte("\n")), from: map[int]int{}}
}

// at is the 1-based column of s on line, or 0 when it is not found.
func (c *columns) at(line int, s string) int {
	if line < 1 || line > len(c.lines) {
		return 0
	}
	l, from := c.lines[line-1], c.from[line]
	i := bytes.Index(l[from:], []byte(s))
	if i < 0 {
		return 0
	}
	c.from[line] = from + i + len(s)
	return from + i + 1
}

// ---- token files -------------------------------------------------------

var aliasRE = regexp.MustCompile(`^\{([^{}]+)\}$`)

func (r *renamer) tokenFiles() error {
	newAlias := "{" + strings.Join(r.pl.NewPath, ".") + "}"
	type doc struct {
		file string
		root *ojson.Node
	}
	var docs []doc
	for _, f := range r.p.TokenFiles {
		root, err := ojson.Parse(bytes.TrimPrefix(r.p.Sources[f], []byte{0xEF, 0xBB, 0xBF}))
		if err != nil {
			return fmt.Errorf("parse %s: %w", f, err) // it loaded, so this cannot happen
		}
		docs = append(docs, doc{f, root})
	}
	// Collisions and names first: nothing is changed before every file
	// passes.
	for _, d := range docs {
		if err := r.collision(d.file, d.root); err != nil {
			return err
		}
		if err := r.names(d.file, d.root, nil); err != nil {
			return err
		}
	}
	for _, d := range docs {
		changed := false
		cols := newColumns(r.p.Sources[d.file])
		walkTokens(d.root, nil, func(_ []string, n *ojson.Node) {
			for _, s := range aliasStrings(n) {
				m := aliasRE.FindStringSubmatch(s.Str)
				if m == nil || strings.ReplaceAll(m[1], ".", "-") != r.pl.Old {
					continue
				}
				col := cols.at(s.Line, `"`+s.Str+`"`)
				if col > 0 {
					col++ // past the quote
				}
				r.pl.Edits = append(r.pl.Edits, Edit{File: d.file, Line: s.Line, Col: col, Old: s.Str, New: newAlias})
				s.Str = newAlias
				changed = true
			}
		})
		var found [][]string
		walkTokens(d.root, nil, func(p []string, _ *ojson.Node) {
			if strings.Join(p, "-") == r.pl.Old {
				found = append(found, slices.Clone(p))
			}
		})
		if len(found) > 0 {
			if err := r.names(d.file, d.root, cols); err != nil {
				return err
			}
		}
		for _, from := range found {
			if err := r.move(d.file, d.root, from); err != nil {
				return err
			}
			changed = true
		}
		if changed {
			if err := r.put(d.file, ojson.Marshal(d.root, "  ")); err != nil {
				return err
			}
		}
	}
	return nil
}

// names rewrites each fixed-naming format's names entry of old's nodes in
// root: such an entry must be the format's default spelling of old, which
// becomes the default spelling of new. Any other spelling there is
// refused, since the renamed token could not keep it. A native format's
// names entry is a custom spelling and stays. With cols nil it only
// checks.
func (r *renamer) names(file string, root *ojson.Node, cols *columns) error {
	var err error
	walkTokens(root, nil, func(p []string, n *ojson.Node) {
		if err != nil || strings.Join(p, "-") != r.pl.Old {
			return
		}
		ext := n.Fields["$extensions"]
		if ext == nil || ext.Kind != ojson.Object {
			return
		}
		x := ext.Fields[tokens.Namespace]
		if x == nil || x.Kind != ojson.Object {
			return
		}
		names := x.Fields["names"]
		if names == nil || names.Kind != ojson.Object {
			return
		}
		for _, f := range names.Keys {
			v := names.Fields[f]
			if !slices.Contains(config.FixedNaming, f) || v.Kind != ojson.String {
				continue
			}
			want := config.DefaultName(f, r.pl.Old)
			if v.Str != want {
				err = refuse("rename: %s:%d: %s's names.%s is %q, not %q, its default %s spelling, which the rename would rewrite; fix it first",
					file, v.Line, r.pl.Old, f, v.Str, want, f)
				return
			}
			if cols == nil {
				continue
			}
			col := cols.at(v.Line, `"`+v.Str+`"`)
			if col > 0 {
				col++ // past the quote
			}
			nv := config.DefaultName(f, r.pl.New)
			r.pl.Edits = append(r.pl.Edits, Edit{File: file, Line: v.Line, Col: col, Old: v.Str, New: nv})
			v.Str = nv
		}
	})
	return err
}

// walkTokens calls fn on every token node under group n, in source order.
func walkTokens(n *ojson.Node, at []string, fn func([]string, *ojson.Node)) {
	for _, k := range n.Keys {
		c := n.Fields[k]
		if strings.HasPrefix(k, "$") || c.Kind != ojson.Object {
			continue
		}
		p := append(slices.Clone(at), k)
		if _, ok := c.Fields["$value"]; ok {
			fn(p, c)
			continue
		}
		walkTokens(c, p, fn)
	}
}

// aliasStrings are the string nodes of a token's values, where an alias
// may stand: its $value and its extension's modes and density entries,
// composite values included, in source order.
func aliasStrings(tok *ojson.Node) []*ojson.Node {
	var out []*ojson.Node
	var walk func(*ojson.Node)
	walk = func(n *ojson.Node) {
		switch n.Kind {
		case ojson.String:
			out = append(out, n)
		case ojson.Object:
			for _, k := range n.Keys {
				walk(n.Fields[k])
			}
		case ojson.Array:
			for _, it := range n.Items {
				walk(it)
			}
		}
	}
	// Source order: walk the token's keys as written.
	for _, k := range tok.Keys {
		switch k {
		case "$value":
			walk(tok.Fields[k])
		case "$extensions":
			ext := tok.Fields[k]
			if ext.Kind != ojson.Object {
				continue
			}
			x := ext.Fields[tokens.Namespace]
			if x == nil || x.Kind != ojson.Object {
				continue
			}
			for _, xk := range x.Keys {
				if xk == "modes" || xk == "density" {
					walk(x.Fields[xk])
				}
			}
		}
	}
	return out
}

// nodeAt is the node at path under root, or nil.
func nodeAt(root *ojson.Node, p []string) *ojson.Node {
	n := root
	for _, s := range p {
		if n.Kind != ojson.Object {
			return nil
		}
		if n = n.Fields[s]; n == nil {
			return nil
		}
	}
	return n
}

func isToken(n *ojson.Node) bool {
	_, ok := n.Fields["$value"]
	return n.Kind == ojson.Object && ok
}

// collision refuses a new path that file already uses: a token or group
// there (other than a group holding only old), or a token on the way to it
// (other than old, which moves away).
func (r *renamer) collision(file string, root *ojson.Node) error {
	np := r.pl.NewPath
	dotted := strings.Join(np, ".")
	for i := 1; i < len(np); i++ {
		n := nodeAt(root, np[:i])
		if n == nil {
			break
		}
		if n.Kind != ojson.Object {
			return refuse("rename: %s in %s is not a group, so %s cannot be", strings.Join(np[:i], "."), file, dotted)
		}
		if isToken(n) && strings.Join(np[:i], "-") != r.pl.Old {
			return refuse("rename: %s in %s is a token, so %s cannot be a token under it", strings.Join(np[:i], "."), file, dotted)
		}
		if isToken(n) {
			break // old's own node, which moves away
		}
	}
	n := nodeAt(root, np)
	if n == nil {
		return nil
	}
	if isToken(n) || n.Kind != ojson.Object {
		return refuse("rename: %s is already a token in %s", dotted, file)
	}
	var others []string
	walkTokens(n, np, func(p []string, _ *ojson.Node) {
		if name := strings.Join(p, "-"); name != r.pl.Old {
			others = append(others, name)
		}
	})
	if len(others) > 0 {
		return refuse("rename: %s is a group in %s (holding %s), so it cannot be a token", dotted, file, strings.Join(others, ", "))
	}
	if !slices.Equal(r.pl.OldPath[:min(len(np), len(r.pl.OldPath))], np) {
		return refuse("rename: %s is a group in %s, so it cannot be a token", dotted, file)
	}
	return nil
}

// typeAt is the $type a node at p would inherit from its groups in root.
func typeAt(root *ojson.Node, p []string) string {
	typ := ""
	n := root
	for i := 0; ; i++ {
		if t := n.Fields["$type"]; t != nil && t.Kind == ojson.String {
			typ = t.Str
		}
		if i == len(p)-1 {
			return typ
		}
		if n = n.Fields[p[i]]; n == nil || n.Kind != ojson.Object {
			return typ
		}
	}
}

// move moves the token node at from to the new path in root. A move within
// one group renames the key in place; otherwise the node leaves its group,
// groups it empties are deleted, and it is appended to its new group,
// created as needed. A node without its own $type whose new place would
// give it another one carries the token's type.
func (r *renamer) move(file string, root *ojson.Node, from []string) error {
	to := r.pl.NewPath
	node := nodeAt(root, from)
	oldType := typeAt(root, from)
	r.pl.Moves = append(r.pl.Moves, Move{File: file, Line: node.Line, From: strings.Join(from, "."), To: strings.Join(to, ".")})

	parent := nodeAt(root, from[:len(from)-1])
	if slices.Equal(from[:len(from)-1], to[:len(to)-1]) {
		i := slices.Index(parent.Keys, from[len(from)-1])
		parent.Keys[i] = to[len(to)-1]
		delete(parent.Fields, from[len(from)-1])
		parent.Fields[to[len(to)-1]] = node
		return nil
	}
	removeKey(parent, from[len(from)-1])
	for i := len(from) - 1; i > 0; i-- {
		g := nodeAt(root, from[:i])
		if slices.ContainsFunc(g.Keys, func(k string) bool { return !strings.HasPrefix(k, "$") }) {
			break
		}
		removeKey(nodeAt(root, from[:i-1]), from[i-1])
	}
	g := root
	for _, s := range to[:len(to)-1] {
		c := g.Fields[s]
		if c == nil {
			c = &ojson.Node{Kind: ojson.Object, Fields: map[string]*ojson.Node{}}
			g.Keys = append(g.Keys, s)
			g.Fields[s] = c
		}
		g = c
	}
	if g.Fields[to[len(to)-1]] != nil {
		return refuse("rename: %s declares %s at two paths, which both move to %s", file, r.pl.Old, strings.Join(to, "."))
	}
	g.Keys = append(g.Keys, to[len(to)-1])
	g.Fields[to[len(to)-1]] = node

	if _, own := node.Fields["$type"]; !own && typeAt(root, to) != oldType {
		node.Keys = append([]string{"$type"}, node.Keys...)
		node.Fields["$type"] = &ojson.Node{Kind: ojson.String, Str: r.tok.Type}
	}
	return nil
}

func removeKey(n *ojson.Node, k string) {
	n.Keys = slices.DeleteFunc(n.Keys, func(s string) bool { return s == k })
	delete(n.Fields, k)
}

// ---- YAML files --------------------------------------------------------

// yamlDoc is a parsed YAML source file being edited.
type yamlDoc struct {
	file    string
	doc     yaml.Node
	cols    *columns
	changed bool
}

func (r *renamer) yamlDoc(file string) (*yamlDoc, error) {
	src, ok := r.p.Sources[file]
	if !ok {
		return nil, nil
	}
	d := &yamlDoc{file: file, cols: newColumns(src)}
	if err := yaml.Unmarshal(src, &d.doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err) // it loaded, so this cannot happen
	}
	return d, nil
}

func (d *yamlDoc) root() *yaml.Node {
	if len(d.doc.Content) == 0 {
		return nil
	}
	return d.doc.Content[0]
}

// set rewrites scalar n to new when it is old.
func (r *renamer) set(d *yamlDoc, n *yaml.Node) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Value != r.pl.Old {
		return
	}
	col := n.Column
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		col++ // past the quote
	}
	r.pl.Edits = append(r.pl.Edits, Edit{File: d.file, Line: n.Line, Col: col, Old: n.Value, New: r.pl.New})
	n.Value = r.pl.New
	d.changed = true
}

// setAll rewrites every item of sequence (or scalar) n that is old.
func (r *renamer) setAll(d *yamlDoc, n *yaml.Node) {
	if n == nil {
		return
	}
	if n.Kind == yaml.ScalarNode {
		r.set(d, n)
		return
	}
	for _, it := range n.Content {
		r.set(d, it)
	}
}

func (r *renamer) save(d *yamlDoc) error {
	if !d.changed {
		return nil
	}
	b, err := yaml.Marshal(&d.doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", d.file, err)
	}
	return r.put(d.file, b)
}

// field is mapping n's value under key, or nil.
func field(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// pairs are mapping n's (key, value) nodes.
func pairs(n *yaml.Node) [][2]*yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

// config rewrites airframe.yaml's token references: accentColor, the
// material3Scheme role values, the highContrast keys and wrapper map
// values. A wrapper pattern that can resolve a member to old is a manual
// item: its template is not edited.
func (r *renamer) config() error {
	d, err := r.yamlDoc(config.File)
	if err != nil || d == nil {
		return err
	}
	for _, pp := range pairs(field(d.root(), "platforms")) {
		name, pn := pp[0].Value, pp[1]
		opts := field(pn, "options")
		r.set(d, field(opts, "accentColor"))
		for _, kv := range pairs(field(opts, "material3Scheme")) {
			r.set(d, kv[1])
		}
		for _, kv := range pairs(field(opts, "highContrast")) {
			r.set(d, kv[0])
		}
		ws := field(pn, "wrappers")
		if ws == nil || ws.Kind != yaml.SequenceNode {
			continue
		}
		for _, w := range ws.Content {
			for _, kv := range pairs(field(w, "map")) {
				r.set(d, kv[1])
			}
			root := field(w, "root")
			if root == nil {
				continue
			}
			pats := field(w, "patterns")
			if pats == nil || pats.Kind != yaml.SequenceNode {
				continue
			}
			for _, pe := range pats.Content {
				m, t := field(pe, "match"), field(pe, "token")
				if m == nil || t == nil || !canMake(m.Value, t.Value, r.pl.Old) {
					continue
				}
				r.pl.Manual = append(r.pl.Manual, Manual{File: d.file, Line: pe.Line, Message: fmt.Sprintf(
					"platform %s: wrapper %s pattern %s → %s can resolve a member to %s, which would now name no token; check its members by hand",
					name, root.Value, m.Value, t.Value, r.pl.Old)})
			}
		}
	}
	return r.save(d)
}

var groupRE = regexp.MustCompile(`\{(\d)\}`)

// canMake reports whether a wrapper pattern's token template, its {n}
// standing for what match's group n can capture, can produce name.
func canMake(match, template, name string) bool {
	re, err := syntax.Parse(match, syntax.Perl)
	if err != nil {
		return false
	}
	groups := map[int]string{}
	var walk func(*syntax.Regexp)
	walk = func(n *syntax.Regexp) {
		if n.Op == syntax.OpCapture {
			groups[n.Cap] = n.Sub[0].String()
		}
		for _, s := range n.Sub {
			walk(s)
		}
	}
	walk(re)
	var b strings.Builder
	b.WriteString("^")
	last := 0
	for _, m := range groupRE.FindAllStringSubmatchIndex(template, -1) {
		b.WriteString(regexp.QuoteMeta(template[last:m[0]]))
		g := groups[int(template[m[2]]-'0')]
		if g == "" {
			g = ".*"
		}
		b.WriteString("(?:" + g + ")")
		last = m[1]
	}
	b.WriteString(regexp.QuoteMeta(template[last:]) + "$")
	ok, err := regexp.MatchString(b.String(), name)
	return err == nil && ok
}

// components rewrites each component file's tokens entries.
func (r *renamer) components() error {
	for _, f := range r.p.ComponentFiles {
		d, err := r.yamlDoc(f)
		if err != nil || d == nil {
			return err
		}
		r.setAll(d, field(d.root(), "tokens"))
		if err := r.save(d); err != nil {
			return err
		}
	}
	return nil
}

// invariants rewrites the exact token names of invariants.yaml (tokens,
// fg, bg, and only and match entries that are old itself). A glob in only
// or match that matches old but not new, or new but not old, is a manual
// item: what the invariant covers would change.
func (r *renamer) invariants() error {
	d, err := r.yamlDoc(path.Join(r.p.Config.Content, invariants.File))
	if err != nil || d == nil {
		return err
	}
	root := d.root()
	if root == nil || root.Kind != yaml.SequenceNode {
		return nil
	}
	old, new := r.pl.Old, r.pl.New
	glob := func(id, key string, n *yaml.Node) {
		if n == nil || n.Kind != yaml.ScalarNode {
			return
		}
		if n.Value == old {
			r.set(d, n)
			return
		}
		mo, err1 := path.Match(n.Value, old)
		mn, err2 := path.Match(n.Value, new)
		if err1 != nil || err2 != nil || mo == mn {
			return
		}
		what := fmt.Sprintf("matched %s and does not match %s", old, new)
		if mn {
			what = fmt.Sprintf("did not match %s and matches %s", old, new)
		}
		r.pl.Manual = append(r.pl.Manual, Manual{File: d.file, Line: n.Line, Message: fmt.Sprintf(
			"invariant %s: %s %q %s, so what it covers changes; adjust it by hand", id, key, n.Value, what)})
	}
	for _, inv := range root.Content {
		id := ""
		if n := field(inv, "id"); n != nil {
			id = n.Value
		}
		r.setAll(d, field(inv, "tokens"))
		r.set(d, field(inv, "fg"))
		r.set(d, field(inv, "bg"))
		if only := field(inv, "only"); only != nil {
			items := only.Content
			if only.Kind == yaml.ScalarNode {
				items = []*yaml.Node{only}
			}
			for _, it := range items {
				glob(id, "only", it)
			}
		}
		glob(id, "match", field(inv, "match"))
	}
	return r.save(d)
}
