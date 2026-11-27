// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package invariants parses invariants.yaml: the absolute rules a design
// system holds across every mode, each an entry of one of a few generic kinds.
package invariants

import (
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
)

// File is the name diagnostics report invariants under, relative to the
// content directory.
const File = "invariants.yaml"

// Invariant is one entry of invariants.yaml. Fields a kind does not use are
// left at their zero value. Line is the line of the entry's list item. Has
// records which keys the entry sets, so a key whose value may legitimately
// be zero (hue: 0) can be told from an absent one.
type Invariant struct {
	ID, Kind            string
	Tokens              []string
	ForEach             []string
	Hue, Tolerance, Min float64
	Only                []string
	FG, BG, Match       string
	Line                int
	Has                 map[string]bool
}

var yamlLine = regexp.MustCompile(`line (\d+)`)

func diagnostic(rule, msg string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: File, Line: line,
		Message: msg, Docs: diag.DocsURL(rule),
	}
}

// Parse reads invariants.yaml. On any diagnostic it returns no invariants.
func Parse(data []byte) ([]Invariant, []diag.Diagnostic) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		msg := strings.TrimPrefix(err.Error(), "yaml: ")
		line := 0
		if m := yamlLine.FindStringSubmatch(msg); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		return nil, []diag.Diagnostic{diagnostic("yaml-syntax", msg, line)}
	}
	if root.Kind == 0 { // empty file: no invariants
		return nil, nil
	}
	if ds := schema.Validate("invariants", &root); len(ds) > 0 {
		for i := range ds {
			ds[i].File = File
		}
		return nil, ds
	}
	var out []Invariant
	for _, item := range root.Content[0].Content {
		out = append(out, build(item))
	}
	return out, nil
}

func build(n *yaml.Node) Invariant {
	inv := Invariant{Line: n.Line, Has: map[string]bool{}}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]
		inv.Has[k] = true
		switch k {
		case "id":
			inv.ID = v.Value
		case "kind":
			inv.Kind = v.Value
		case "tokens":
			inv.Tokens = list(v)
		case "forEach":
			inv.ForEach = list(v)
		case "only":
			inv.Only = list(v)
		case "hue":
			inv.Hue = num(v)
		case "tolerance":
			inv.Tolerance = num(v)
		case "min":
			inv.Min = num(v)
		case "fg":
			inv.FG = v.Value
		case "bg":
			inv.BG = v.Value
		case "match":
			inv.Match = v.Value
		}
	}
	return inv
}

// list returns a scalar as a one-element list, or a sequence's items.
func list(n *yaml.Node) []string {
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}

func num(n *yaml.Node) float64 {
	f, _ := strconv.ParseFloat(n.Value, 64)
	return f
}
