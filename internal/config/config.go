// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package config loads airframe.yaml: the tiers, mode axes, platforms and
// lint severities every other part of Airframe reads.
package config

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
)

// File is the name diagnostics report the config under.
const File = "airframe.yaml"

// Config is a loaded airframe.yaml.
type Config struct {
	Version        int
	Content        string
	Tiers          map[string]Tier
	Axes           []Axis // in file order
	AccentRequired []string
	Platforms      []Platform // in file order
	Lint           LintConfig
}

// Tier is one layer of the token hierarchy and who may read it.
type Tier struct {
	Name       string
	ReadableBy []string
}

// Axis is one dimension a token value may vary on. A deferred axis takes its
// values from Source, a file resolved by a later stage; until then Values is
// empty.
type Axis struct {
	Name    string
	Values  []string
	Base    string // the value $value is, rendered when no attribute is set
	Source  string // path after "from ", for a deferred axis
	Binding string // "runtime" or "build"
	Group   string
	Web     WebMode
}

// Deferred reports whether the axis takes its values from a file not yet loaded.
func (a Axis) Deferred() bool { return a.Source != "" }

// WebMode says how an axis is spelled in CSS. Selectors holds the per-value
// selectors (a value maps to one or several), Attr the data attribute, and
// Also extra conditions per value such as a media query.
type WebMode struct {
	Attr      string
	Selectors map[string][]string
	Also      map[string]string
}

// Platform is one output target. Emit lists its formats; Out maps each
// format to its output path. Consumers are the globs of files that read it.
type Platform struct {
	Name      string
	Emit      []string
	Out       map[string]string
	Consumers []string
}

// LintConfig holds per-rule severity overrides.
type LintConfig struct {
	Rules map[string]diag.Severity
}

var defaultTiers = map[string]Tier{
	"primitive": {Name: "primitive", ReadableBy: []string{"semantic"}},
	"semantic":  {Name: "semantic", ReadableBy: []string{"component", "consumers", "emitters"}},
	"component": {Name: "component", ReadableBy: []string{"owner"}},
}

func diagnostic(rule, msg, path string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: File, Path: path, Line: line,
		Message: msg, Docs: diag.DocsURL(rule),
	}
}

// Load parses and validates airframe.yaml. On any diagnostic the returned
// Config is nil.
func Load(data []byte) (*Config, []diag.Diagnostic) {
	root, bad := schema.DecodeYAML(data)
	if bad != nil {
		return nil, []diag.Diagnostic{diagnostic("yaml-syntax", bad.Message, "", bad.Line)}
	}
	if ds := schema.Validate("config", &root); len(ds) > 0 {
		for i := range ds {
			ds[i].File = File
		}
		return nil, ds
	}
	c, err := build(root.Content[0])
	if err != nil {
		return nil, []diag.Diagnostic{diagnostic("schema-invalid", err.msg, err.path, err.line)}
	}
	return c, nil
}

type buildError struct {
	msg, path string
	line      int
}

// mapping returns the entries of a mapping node in file order.
func mapping(n *yaml.Node) (keys, vals []*yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i])
		vals = append(vals, n.Content[i+1])
	}
	return keys, vals
}

func field(n *yaml.Node, name string) *yaml.Node {
	ks, vs := mapping(n)
	for i, k := range ks {
		if k.Value == name {
			return vs[i]
		}
	}
	return nil
}

// stringList returns a scalar as a one-element list, or a sequence's items.
func stringList(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	var out []string
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}

func build(root *yaml.Node) (*Config, *buildError) {
	c := &Config{Content: "design-system", Tiers: map[string]Tier{}, Lint: LintConfig{Rules: map[string]diag.Severity{}}}
	c.Version = 1
	if n := field(root, "content"); n != nil {
		// content is a directory under the project root, compared and
		// joined in its clean form.
		clean, err := underRoot("content", n.Value, "content", n.Line)
		if err != nil {
			return nil, err
		}
		c.Content = strings.TrimSuffix(clean, "/")
	}

	if tn := field(root, "tiers"); tn != nil {
		ks, vs := mapping(tn)
		for i, k := range ks {
			c.Tiers[k.Value] = Tier{Name: k.Value, ReadableBy: stringList(field(vs[i], "readableBy"))}
		}
	} else {
		for k, v := range defaultTiers {
			c.Tiers[k] = v
		}
	}

	if mn := field(root, "modes"); mn != nil {
		ks, vs := mapping(mn)
		for i, k := range ks {
			a, err := buildAxis(k, vs[i])
			if err != nil {
				return nil, err
			}
			c.Axes = append(c.Axes, a)
		}
	}

	c.AccentRequired = stringList(field(field(root, "accentGroups"), "required"))

	if pn := field(root, "platforms"); pn != nil {
		ks, vs := mapping(pn)
		for i, k := range ks {
			p, err := buildPlatform(k, vs[i])
			if err != nil {
				return nil, err
			}
			c.Platforms = append(c.Platforms, p)
		}
	}

	ks, vs := mapping(field(field(root, "lint"), "rules"))
	for i, k := range ks {
		c.Lint.Rules[k.Value] = diag.Severity(vs[i].Value)
	}
	return c, nil
}

func buildAxis(key, n *yaml.Node) (Axis, *buildError) {
	name := key.Value
	at := "modes." + name
	a := Axis{Name: name, Binding: "runtime"}
	if v := field(n, "values"); v != nil {
		if v.Kind == yaml.ScalarNode {
			a.Source = strings.TrimPrefix(v.Value, "from ")
		} else {
			a.Values = stringList(v)
		}
	}
	if b := field(n, "binding"); b != nil {
		a.Binding = b.Value
	}
	if g := field(n, "group"); g != nil {
		a.Group = g.Value
	}
	var alsoNode *yaml.Node
	selNodes := map[string]*yaml.Node{}
	if w := field(n, "web"); w != nil {
		a.Web = WebMode{Selectors: map[string][]string{}, Also: map[string]string{}}
		ks, vs := mapping(w)
		for i, k := range ks {
			switch k.Value {
			case "attr":
				a.Web.Attr = vs[i].Value
			case "also":
				alsoNode = vs[i]
				aks, avs := mapping(vs[i])
				for j, ak := range aks {
					a.Web.Also[ak.Value] = avs[j].Value
				}
			default:
				a.Web.Selectors[k.Value] = stringList(vs[i])
				selNodes[k.Value] = vs[i]
			}
		}
	}

	// The bare-:root value, if any, is the base: CSS cannot tell it from
	// "attribute unset". It must be that value's only selector.
	rootValue := ""
	for _, v := range slices.Sorted(maps.Keys(a.Web.Selectors)) {
		sels := a.Web.Selectors[v]
		if !slices.ContainsFunc(sels, isBareRoot) {
			continue
		}
		if len(sels) > 1 {
			return a, &buildError{
				msg:  fmt.Sprintf("modes.%s.web.%s: a bare :root must be the value's only selector (it is the base, the render when no attribute is set)", name, v),
				path: at + ".web." + v, line: selNodes[v].Line,
			}
		}
		if rootValue != "" {
			return a, &buildError{
				msg:  fmt.Sprintf("modes.%s.web: %s and %s are both bare :root; an axis has one base", name, rootValue, v),
				path: at + ".web." + v, line: selNodes[v].Line,
			}
		}
		rootValue = v
	}
	a.Base = rootValue
	if b := field(n, "base"); b != nil {
		switch {
		case !a.Deferred() && !slices.Contains(a.Values, b.Value):
			return a, &buildError{
				msg:  fmt.Sprintf("axis %s: base %s is not one of its values", name, b.Value),
				path: at + ".base", line: b.Line,
			}
		case rootValue != "" && rootValue != b.Value:
			return a, &buildError{
				msg:  fmt.Sprintf("axis %s: base %s disagrees with %s, whose only web selector is bare :root", name, b.Value, rootValue),
				path: at + ".base", line: b.Line,
			}
		}
		a.Base = b.Value
	}
	if a.Base == "" && !a.Deferred() {
		return a, &buildError{
			msg:  fmt.Sprintf("axis %s needs a base: value — the value rendered when no attribute is set", name),
			path: at, line: key.Line,
		}
	}

	if alsoNode != nil {
		// The also media condition is a fallback for while the attribute
		// is unset, so it needs the attribute, and every non-base value
		// must be spelled by it: an explicit selector would bypass the
		// :not([attr]) the fallback depends on.
		if a.Web.Attr == "" {
			return a, &buildError{
				msg:  fmt.Sprintf("axis %s: web also needs the axis's web attr, so the fallback can apply only while it is unset", name),
				path: at + ".web.also", line: alsoNode.Line,
			}
		}
		for _, v := range slices.Sorted(maps.Keys(a.Web.Selectors)) {
			if v != rootValue {
				return a, &buildError{
					msg:  fmt.Sprintf("axis %s: with web also, %s must be spelled by the attr (%s), not an explicit selector", name, v, a.Web.Attr),
					path: at + ".web." + v, line: selNodes[v].Line,
				}
			}
		}
	}
	return a, nil
}

func isBareRoot(s string) bool { return strings.TrimSpace(s) == ":root" }

var driveLetter = regexp.MustCompile(`^[A-Za-z]:`)

// underRoot cleans v, a project-relative slash path the config names under
// key (keeping a trailing slash, which marks a directory output), and
// rejects one that could leave the project root: absolute, with a
// ".." escaping it, with a backslash, or with a drive-letter prefix.
func underRoot(key, v, at string, line int) (string, *buildError) {
	clean := path.Clean(v)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") ||
		strings.Contains(v, `\`) || driveLetter.MatchString(v) {
		return "", &buildError{
			msg:  fmt.Sprintf("%s %s must be a slash path under the project root", key, v),
			path: at, line: line,
		}
	}
	if strings.HasSuffix(v, "/") && clean != "." {
		clean += "/"
	}
	return clean, nil
}

// Formats are the emit formats airframe.yaml may name: css and json are
// built now; swift, colorsets, kotlin and xaml are accepted so a config can
// declare them, and are reported as not yet built until their emitters land.
var Formats = []string{"css", "json", "swift", "colorsets", "kotlin", "xaml"}

func buildPlatform(key, n *yaml.Node) (Platform, *buildError) {
	p := Platform{Name: key.Value, Emit: stringList(field(n, "emit")), Consumers: stringList(field(n, "consumers"))}
	if e := field(n, "emit"); e != nil {
		items := []*yaml.Node{e}
		if e.Kind == yaml.SequenceNode {
			items = e.Content
		}
		for _, it := range items {
			if !slices.Contains(Formats, it.Value) {
				return p, &buildError{
					msg: fmt.Sprintf("platform %s: unknown emit format %s; known: %s",
						p.Name, it.Value, strings.Join(Formats, ", ")),
					path: "platforms." + p.Name + ".emit", line: it.Line,
				}
			}
		}
	}
	if o := field(n, "out"); o != nil {
		p.Out = map[string]string{}
		if o.Kind == yaml.ScalarNode {
			if len(p.Emit) != 1 {
				return p, &buildError{
					msg:  fmt.Sprintf("platform %s: a scalar out needs exactly one emit format, found %d", p.Name, len(p.Emit)),
					path: "platforms." + p.Name + ".out", line: o.Line,
				}
			}
			clean, err := underRoot("out", o.Value, "platforms."+p.Name+".out", o.Line)
			if err != nil {
				return p, err
			}
			p.Out[p.Emit[0]] = clean
		} else {
			ks, vs := mapping(o)
			for i, k := range ks {
				if !slices.Contains(p.Emit, k.Value) {
					return p, &buildError{
						msg: fmt.Sprintf("platform %s: out names %s, which is not one of its emit formats (%s)",
							p.Name, k.Value, strings.Join(p.Emit, ", ")),
						path: "platforms." + p.Name + ".out." + k.Value, line: k.Line,
					}
				}
				clean, err := underRoot("out", vs[i].Value, "platforms."+p.Name+".out."+k.Value, vs[i].Line)
				if err != nil {
					return p, err
				}
				p.Out[k.Value] = clean
			}
		}
	}
	return p, nil
}
