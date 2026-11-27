// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package config loads airframe.yaml: the tiers, mode axes, platforms and
// lint severities every other part of Airframe reads.
package config

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
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

var yamlLine = regexp.MustCompile(`line (\d+)`)

func diagnostic(rule, msg, path string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: File, Path: path, Line: line,
		Message: msg, Docs: diag.DocsURL(rule),
	}
}

// Load parses and validates airframe.yaml. On any diagnostic the returned
// Config is nil.
func Load(data []byte) (*Config, []diag.Diagnostic) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		msg := strings.TrimPrefix(err.Error(), "yaml: ")
		line := 0
		if m := yamlLine.FindStringSubmatch(msg); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		return nil, []diag.Diagnostic{diagnostic("yaml-syntax", msg, "", line)}
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
			c.Axes = append(c.Axes, buildAxis(k.Value, vs[i]))
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

func buildAxis(name string, n *yaml.Node) Axis {
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
	if w := field(n, "web"); w != nil {
		a.Web = WebMode{Selectors: map[string][]string{}, Also: map[string]string{}}
		ks, vs := mapping(w)
		for i, k := range ks {
			switch k.Value {
			case "attr":
				a.Web.Attr = vs[i].Value
			case "also":
				aks, avs := mapping(vs[i])
				for j, ak := range aks {
					a.Web.Also[ak.Value] = avs[j].Value
				}
			default:
				a.Web.Selectors[k.Value] = stringList(vs[i])
			}
		}
	}
	return a
}

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

func buildPlatform(key, n *yaml.Node) (Platform, *buildError) {
	p := Platform{Name: key.Value, Emit: stringList(field(n, "emit")), Consumers: stringList(field(n, "consumers"))}
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
