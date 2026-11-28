// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package retired loads <content>/retired.yaml: treatments that have no
// token to delete, held instead by the spellings that must not come back
// into consumer code.
package retired

import (
	"fmt"
	"regexp"
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
)

// File is the name of the retired treatments file, relative to the content
// directory.
const File = "retired.yaml"

// Treatment is one entry of retired.yaml. Spellings maps an adapter name
// (config.AdapterNames) to its compiled RE2 patterns, in file order. Ruling
// and Marker are "" when unset. File is the repo-relative path of the file
// and Line the line of the entry's list item.
type Treatment struct {
	ID, Ruling, Marker, File string
	Line                     int
	Spellings                map[string][]*regexp.Regexp
}

var kebab = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func invalid(file, at string, line int, format string, a ...any) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: schema.RuleID, Severity: diag.Error, File: file, Path: at, Line: line,
		Message: fmt.Sprintf(format, a...), Docs: diag.DocsURL(schema.RuleID),
	}
}

// isString is a plain YAML string scalar, not a timestamp or other scalar
// that JSON validation would see as a string.
func isString(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" }

// findAlias returns the first alias node under n, in document order.
func findAlias(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.AliasNode {
		return n
	}
	for _, c := range n.Content {
		if a := findAlias(c); a != nil {
			return a
		}
	}
	return nil
}

// Parse reads the retired file at file, a repo-relative path. On any
// diagnostic it returns no treatments; every diagnostic carries file.
func Parse(file string, data []byte) ([]Treatment, []diag.Diagnostic) {
	root, bad := schema.DecodeYAML(data)
	if bad != nil {
		bad.File = file
		return nil, []diag.Diagnostic{*bad}
	}
	if a := findAlias(&root); a != nil {
		return nil, []diag.Diagnostic{invalid(file, "", a.Line, "YAML aliases are not supported (*%s)", a.Value)}
	}
	if ds := schema.Validate("retired", &root); len(ds) > 0 {
		for i := range ds {
			ds[i].File = file
		}
		return nil, ds
	}
	var out []Treatment
	seen := map[string]int{} // id -> line of its first entry
	for i, item := range root.Content[0].Content {
		at := strconv.Itoa(i)
		t := Treatment{File: file, Line: item.Line, Spellings: map[string][]*regexp.Regexp{}}
		var idNode *yaml.Node
		for j := 0; j+1 < len(item.Content); j += 2 {
			k, v := item.Content[j].Value, item.Content[j+1]
			var bad string
			switch k {
			case "id":
				idNode = v
				if !isString(v) {
					bad = "id"
					break
				}
				t.ID = v.Value
			case "ruling":
				if !isString(v) {
					bad = "ruling"
					break
				}
				t.Ruling = v.Value
			case "marker":
				if !isString(v) {
					bad = "marker"
					break
				}
				t.Marker = v.Value
			case "spellings":
				for m := 0; m+1 < len(v.Content); m += 2 {
					adapter, list := v.Content[m].Value, v.Content[m+1]
					for n, p := range list.Content {
						pat := at + ".spellings." + adapter + "." + strconv.Itoa(n)
						if !isString(p) {
							return nil, []diag.Diagnostic{invalid(file, pat, p.Line, "%s.spellings.%s.%d must be a string", at, adapter, n)}
						}
						re, err := regexp.Compile(p.Value)
						if err != nil {
							return nil, []diag.Diagnostic{invalid(file, pat, p.Line, "spelling %q is not a valid RE2 pattern: %v", p.Value, err)}
						}
						t.Spellings[adapter] = append(t.Spellings[adapter], re)
					}
				}
			}
			if bad != "" {
				return nil, []diag.Diagnostic{invalid(file, at+"."+bad, v.Line, "%s.%s must be a string", at, bad)}
			}
		}
		if !kebab.MatchString(t.ID) {
			return nil, []diag.Diagnostic{invalid(file, at+".id", idNode.Line, "id %q must be kebab-case (a-z, 0-9, single hyphens)", t.ID)}
		}
		if first, dup := seen[t.ID]; dup {
			return nil, []diag.Diagnostic{invalid(file, at+".id", idNode.Line, "duplicate id %q (first at line %d)", t.ID, first)}
		}
		seen[t.ID] = idNode.Line
		out = append(out, t)
	}
	return out, nil
}
