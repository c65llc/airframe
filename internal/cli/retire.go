// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/ojson"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/rename"
	"github.com/c65llc/airframe/internal/retired"
	"github.com/c65llc/airframe/internal/tokens"
)

// retireCommand is the retire command; Commands lists it.
var retireCommand = Command{
	Name:    "retire",
	Args:    "[<token>]",
	Short:   "[--dry-run] [<token>] [--treatment ID --spelling A=RE…]",
	Summary: "Mark a token retired and list what lint will then report; or, with --treatment, record a retired treatment and its spellings in retired.yaml.",
	Flags: []Flag{
		{Name: "dry-run", Summary: "print what would change and write nothing"},
		{Name: "treatment", Arg: "ID", Summary: "retire the treatment ID (kebab-case) instead of a token"},
		{Name: "spelling", Arg: "ADAPTER=RE2", Repeat: true, Summary: "with --treatment: a spelling to forbid in ADAPTER's consumer files (css, swift, kotlin, xaml, cs)"},
		{Name: "ruling", Arg: "@ruling:x", Summary: "with --treatment: the ruling that retired it"},
		{Name: "marker", Arg: "M", Summary: "with --treatment: the marker that exempts a use, as <marker>: <reason>"},
	},
}

// reloadWith loads the project as it would be with files replaced.
func reloadWith(p *project.Project, files map[string][]byte) (*project.Project, []diag.Diagnostic, error) {
	return project.Load(rename.Overlay(p.FS, files))
}

func runRetire(inv *invocation) int {
	treatment := inv.value("treatment")
	extra := len(*inv.lists["spelling"]) > 0 || inv.value("ruling") != "" || inv.value("marker") != ""
	if treatment == "" {
		if len(inv.args) != 1 {
			return inv.usageError("retire takes one token name, or --treatment")
		}
		if extra {
			return inv.usageError("--spelling, --ruling and --marker need --treatment")
		}
		return retireToken(inv, inv.args[0])
	}
	if len(inv.args) != 0 {
		return inv.usageError("retire takes a token name or --treatment, not both")
	}
	if len(*inv.lists["spelling"]) == 0 {
		return inv.usageError("--treatment needs at least one --spelling ADAPTER=RE2")
	}
	return retireTreatment(inv, treatment)
}

// loadForRetire loads the project and refuses (exit 1) one that has errors.
func (inv *invocation) loadForRetire() (string, *project.Project, int) {
	dir, p, ds, err := inv.load()
	if err != nil {
		return "", nil, inv.fail(err)
	}
	if p == nil || hasErrors(ds) {
		diag.WriteText(inv.stderr, ds, nil)
		return "", nil, ExitFindings
	}
	return dir, p, ExitOK
}

// write writes files through os.Root at dir, in name order.
func writeAll(inv *invocation, dir string, files map[string][]byte) int {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		if err := writeFile(root, n, files[n]); err != nil {
			return inv.fail(err)
		}
	}
	return ExitOK
}

// tokenNode finds t's node in the token file tree root.
func tokenNode(root *ojson.Node, t *tokens.Token) *ojson.Node {
	n := root
	for _, seg := range t.Path {
		if n == nil || n.Kind != ojson.Object {
			return nil
		}
		n = n.Fields[seg]
	}
	if n == nil || n.Kind != ojson.Object {
		return nil
	}
	return n
}

func retireToken(inv *invocation, name string) int {
	dir, p, code := inv.loadForRetire()
	if p == nil {
		return code
	}
	t, suggestion := lookupToken(p.Tokens, name)
	if t == nil {
		msg := "unknown token " + name
		if suggestion != "" {
			msg += "; did you mean " + suggestion + "?"
		}
		_, _ = fmt.Fprintf(inv.stderr, "airframe retire: %s\n", msg)
		return ExitFindings
	}
	if t.Retired {
		_, _ = fmt.Fprintf(inv.stderr, "airframe retire: %s is already retired\n", t.Name)
		return ExitFindings
	}
	u, err := describeUsages(p, t, "")
	if err != nil {
		return inv.fail(fmt.Errorf("scan consumers: %w", err))
	}
	root, err := ojson.Parse(bytes.TrimPrefix(p.Sources[t.File], []byte{0xEF, 0xBB, 0xBF}))
	if err != nil {
		return inv.fail(fmt.Errorf("%s: %w", t.File, err))
	}
	node := tokenNode(root, t)
	if node == nil {
		return inv.fail(fmt.Errorf("%s: cannot find token %s at %s", t.File, t.Name, strings.Join(t.Path, ".")))
	}
	ext := node.Fields["$extensions"]
	if ext == nil {
		ext = &ojson.Node{Kind: ojson.Object, Fields: map[string]*ojson.Node{}}
		node.Keys = append(node.Keys, "$extensions")
		node.Fields["$extensions"] = ext
	}
	af := ext.Fields[tokens.Namespace]
	if af == nil {
		af = &ojson.Node{Kind: ojson.Object, Fields: map[string]*ojson.Node{}}
		ext.Keys = append(ext.Keys, tokens.Namespace)
		ext.Fields[tokens.Namespace] = af
	}
	if _, has := af.Fields["retired"]; !has {
		af.Keys = append(af.Keys, "retired")
	}
	af.Fields["retired"] = &ojson.Node{Kind: ojson.Bool, Bool: true}
	out, err := format.Tokens(ojson.Marshal(root, "  "))
	if err != nil {
		return inv.fail(err)
	}
	files := map[string][]byte{t.File: out}
	np, nds, err := reloadWith(p, files)
	if err != nil {
		return inv.fail(err)
	}
	if np == nil || hasErrors(nds) {
		diag.WriteText(inv.stderr, nds, nil)
		return ExitFindings
	}
	if nt := np.Tokens.Tokens[t.Name]; nt == nil || !nt.Retired {
		return inv.fail(fmt.Errorf("%s: retired did not take effect", t.File))
	}
	if c := inv.refuseLinks(dir, files); c != ExitOK {
		return c
	}
	verb := "retired"
	if *inv.bools["dry-run"] {
		verb = "would retire"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s in %s\n", verb, t.Name, t.File)
	writeRetireConsequences(&b, u)
	if !*inv.bools["dry-run"] {
		if c := writeAll(inv, dir, files); c != ExitOK {
			return c
		}
	}
	_, _ = io.WriteString(inv.stdout, b.String())
	return ExitOK
}

// writeRetireConsequences lists what retired-used reports once the token
// is retired: the reads, and the tokens that alias it.
func writeRetireConsequences(b *strings.Builder, u usages) {
	if len(u.References) == 0 && len(u.AliasedBy) == 0 {
		b.WriteString("no reads or aliases: lint will report nothing\n")
		return
	}
	b.WriteString("retired-used will report:\n")
	for _, r := range u.References {
		fmt.Fprintf(b, "  %s  %s:%d:%d  %s", r.Platform, r.File, r.Line, r.Col, r.Raw)
		if r.Via != consumer.Direct {
			fmt.Fprintf(b, "  (via %s)", r.Via)
		}
		b.WriteString("\n")
	}
	if len(u.AliasedBy) > 0 {
		b.WriteString("aliased by:\n")
	}
	for _, a := range u.AliasedBy {
		fmt.Fprintf(b, "  %s  %s  %s:%d\n", a.Name, a.Key, a.File, a.Line)
	}
}

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }

func retireTreatment(inv *invocation, id string) int {
	refuse := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(inv.stderr, "airframe retire: "+format+"\n", a...)
		return ExitFindings
	}
	var adapters []string
	spellings := map[string][]string{}
	for _, s := range *inv.lists["spelling"] {
		adapter, re, ok := strings.Cut(s, "=")
		if !ok || re == "" {
			return refuse("spelling %q is not ADAPTER=RE2", s)
		}
		if !slices.Contains(config.AdapterNames, adapter) {
			return refuse("unknown adapter %q in spelling %q; the adapters are %s", adapter, s, strings.Join(config.AdapterNames, ", "))
		}
		if _, err := regexp.Compile(re); err != nil {
			return refuse("spelling %q is not a valid RE2 pattern: %v", re, err)
		}
		if _, seen := spellings[adapter]; !seen {
			adapters = append(adapters, adapter)
		}
		spellings[adapter] = append(spellings[adapter], re)
	}
	dir, p, code := inv.loadForRetire()
	if p == nil {
		return code
	}
	file := path.Join(p.Config.Content, retired.File)
	for _, t := range p.Retired {
		if t.ID == id {
			return refuse("treatment %s is already retired (%s:%d)", id, t.File, t.Line)
		}
	}
	// Append the entry to the file's list, keeping what is there.
	var doc yaml.Node
	if src, ok := p.Sources[file]; ok {
		if err := yaml.Unmarshal(src, &doc); err != nil {
			return inv.fail(fmt.Errorf("%s: %w", file, err))
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.SequenceNode, Tag: "!!seq"}}}
	}
	seq := doc.Content[0]
	if seq.Kind != yaml.SequenceNode {
		return refuse("%s is not a list of treatments", file)
	}
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(k string, v *yaml.Node) { entry.Content = append(entry.Content, scalar(k), v) }
	add("id", scalar(id))
	if v := inv.value("ruling"); v != "" {
		add("ruling", scalar(v))
	}
	if v := inv.value("marker"); v != "" {
		add("marker", scalar(v))
	}
	sp := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, a := range adapters {
		list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, re := range spellings[a] {
			list.Content = append(list.Content, scalar(re))
		}
		sp.Content = append(sp.Content, scalar(a), list)
	}
	add("spellings", sp)
	seq.Content = append(seq.Content, entry)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return inv.fail(err)
	}
	if err := enc.Close(); err != nil {
		return inv.fail(err)
	}
	out, err := format.YAML(buf.Bytes())
	if err != nil {
		return inv.fail(err)
	}
	files := map[string][]byte{file: out}
	np, nds, err := reloadWith(p, files)
	if err != nil {
		return inv.fail(err)
	}
	if np == nil || hasErrors(nds) {
		diag.WriteText(inv.stderr, nds, nil)
		return ExitFindings
	}
	if !slices.ContainsFunc(np.Retired, func(t retired.Treatment) bool { return t.ID == id }) {
		return inv.fail(errors.New(file + ": the new treatment did not load"))
	}
	res, err := consumer.Scan(np)
	if err != nil {
		return inv.fail(fmt.Errorf("scan consumers: %w", err))
	}
	if c := inv.refuseLinks(dir, files); c != ExitOK {
		return c
	}
	verb := "retired"
	if *inv.bools["dry-run"] {
		verb = "would retire"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s treatment %s in %s\n", verb, id, file)
	n := 0
	for _, pr := range res.Problems {
		if pr.Kind != consumer.RetiredSpelling || pr.Treatment != id {
			continue
		}
		if n == 0 {
			b.WriteString("retired-used will report:\n")
		}
		n++
		fmt.Fprintf(&b, "  %s  %s:%d:%d  %s\n", pr.Platform, pr.File, pr.Line, pr.Col, pr.Raw)
	}
	if n == 0 {
		b.WriteString("no consumer code spells it: lint will report nothing\n")
	}
	if !*inv.bools["dry-run"] {
		if c := writeAll(inv, dir, files); c != ExitOK {
			return c
		}
	}
	_, _ = io.WriteString(inv.stdout, b.String())
	return ExitOK
}
