// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package rulingref resolves @ruling:<id> references against a Gantry
// project: the rulings/<id>.md records under its gantry.yaml's content
// root. It reads files only; Gantry is not a dependency. Only the front
// matter fields Airframe needs (id, status, supersededBy) are checked, and
// a ruling that does not read is a diagnostic, never a crash.
package rulingref

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
)

// DefaultFile is the gantry.yaml looked for next to airframe.yaml when
// airframe.yaml has no gantry key.
const DefaultFile = "gantry.yaml"

// defaultContent is Gantry's content root when gantry.yaml sets none.
const defaultContent = "docs/design"

// RulingRule is the rule a ruling record that does not read is reported
// under: component-unruled is the rule that needs the record.
const RulingRule = "component-unruled"

// Statuses are a Gantry ruling's statuses, in Gantry's order.
var Statuses = []string{"proposed", "ruled", "superseded", "retired"}

type ruling struct{ status, supersededBy string }

// Index is the rulings of one Gantry project. A nil *Index is no Gantry
// project: every Lookup misses and resolution is syntax only.
type Index struct {
	file       string
	rulings    map[string]ruling
	unreadable map[string]bool
}

// Lookup returns ruling id's status and, for a superseded ruling, its
// supersededBy ("" when unset). ok is false when there is no readable
// rulings/<id>.md.
func (ix *Index) Lookup(id string) (status, supersededBy string, ok bool) {
	if ix == nil {
		return "", "", false
	}
	r, ok := ix.rulings[id]
	return r.status, r.supersededBy, ok
}

// Unreadable reports whether rulings/<id>.md exists but did not read: its
// diagnostic is already among Resolve's, so a reference to it need not
// also be reported as a missing ruling.
func (ix *Index) Unreadable(id string) bool { return ix != nil && ix.unreadable[id] }

// File is the repo-relative path of the project's gantry.yaml.
func (ix *Index) File() string {
	if ix == nil {
		return ""
	}
	return ix.file
}

func problem(rule, file, at string, line int, format string, a ...any) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: file, Path: at, Line: line,
		Message: fmt.Sprintf(format, a...), Docs: diag.DocsURL(rule),
	}
}

// Resolve finds cfg's Gantry project in fsys (the repository root, where
// airframe.yaml is) and reads its rulings. The Index is nil when there is
// no Gantry project, or when its gantry.yaml does not load.
//
// A problem with gantry.yaml itself (the configured file is missing, its
// content root escapes the project) is schema-invalid, or yaml-syntax when
// it does not parse. A ruling record that does not read is a RulingRule
// diagnostic at the record; it is left out of the Index and marked
// Unreadable. Diagnostics come in file order.
func Resolve(fsys fs.FS, cfg *config.Config) (*Index, []diag.Diagnostic) {
	file := cfg.Gantry
	data, err := fs.ReadFile(fsys, cmpOr(file, DefaultFile))
	switch {
	case err == nil:
	case file == "" && errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, []diag.Diagnostic{problem(schema.RuleID, config.File, "gantry", 0,
			"gantry %s does not exist", file)}
	default:
		return nil, []diag.Diagnostic{problem(schema.RuleID, cmpOr(file, DefaultFile), "", 0,
			"gantry.yaml cannot be read: %v", err)}
	}
	file = cmpOr(file, DefaultFile)

	content, bad := contentRoot(file, data)
	if bad != nil {
		return nil, []diag.Diagnostic{*bad}
	}
	ix := &Index{file: file, rulings: map[string]ruling{}, unreadable: map[string]bool{}}
	dir := path.Join(content, "rulings")
	entries, err := fs.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return ix, nil
	}
	if err != nil {
		return nil, []diag.Diagnostic{problem(schema.RuleID, dir, "", 0, "rulings directory cannot be read: %v", err)}
	}
	var ds []diag.Diagnostic
	for _, e := range entries { // ReadDir sorts by name
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		id, rf := strings.TrimSuffix(name, ".md"), path.Join(dir, name)
		b, err := fs.ReadFile(fsys, rf)
		if err != nil {
			ix.unreadable[id] = true
			ds = append(ds, problem(RulingRule, rf, "", 0, "ruling cannot be read: %v", err))
			continue
		}
		r, d := parseRuling(rf, id, b)
		if d != nil {
			ix.unreadable[id] = true
			ds = append(ds, *d)
			continue
		}
		ix.rulings[id] = r
	}
	return ix, ds
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var driveLetter = regexp.MustCompile(`^[A-Za-z]:`)

// contentRoot reads gantry.yaml at file and returns its content root as a
// repo-relative path. Like Gantry, it refuses a content root outside the
// Gantry project (the directory of gantry.yaml), which also keeps it in the
// repository.
func contentRoot(file string, data []byte) (string, *diag.Diagnostic) {
	root, bad := schema.DecodeYAML(data)
	if bad != nil {
		bad.File = file
		return "", bad
	}
	if len(root.Content) == 0 {
		return path.Join(path.Dir(file), defaultContent), nil
	}
	if n := findAnchor(&root); n != nil {
		d := problem(schema.RuleID, file, "", n.Line, "YAML anchors, aliases and merge keys are not allowed in gantry.yaml")
		return "", &d
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		d := problem(schema.RuleID, file, "", doc.Line, "gantry.yaml must be a mapping")
		return "", &d
	}
	content := defaultContent
	if n := field(doc, "content"); n != nil {
		if !isString(n) {
			d := problem(schema.RuleID, file, "content", n.Line, "content must be a string")
			return "", &d
		}
		clean := path.Clean(n.Value)
		if n.Value == "" || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") ||
			strings.Contains(n.Value, `\`) || driveLetter.MatchString(n.Value) {
			d := problem(schema.RuleID, file, "content", n.Line,
				"content %s must be a slash path under the Gantry project root (%s)", n.Value, cmpOr(path.Dir(file), "."))
			return "", &d
		}
		content = clean
	}
	return path.Join(path.Dir(file), content), nil
}

// parseRuling reads one ruling record's front matter. id is the record's
// file name without .md, which its id field must match.
func parseRuling(file, id string, data []byte) (ruling, *diag.Diagnostic) {
	fail := func(line int, format string, a ...any) (ruling, *diag.Diagnostic) {
		d := problem(RulingRule, file, "", line, "ruling "+format, a...)
		return ruling{}, &d
	}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if strings.TrimRight(lines[0], " \t") != "---" {
		return fail(1, "front matter does not start with --- on the first line")
	}
	end := slices.IndexFunc(lines[1:], func(l string) bool { return strings.TrimRight(l, " \t") == "---" })
	if end < 0 {
		return fail(1, "front matter has no closing ---")
	}
	end++ // index into lines
	// The fence is line 1, so a front matter line n is file line n+1.
	root, bad := schema.DecodeYAML([]byte(strings.Join(lines[1:end], "\n")))
	if bad != nil {
		line := 1
		if bad.Line > 0 {
			line = bad.Line + 1
		}
		return fail(line, "front matter is not valid YAML: %s", bad.Message)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		line := 1
		if len(root.Content) > 0 {
			line = root.Content[0].Line + 1
		}
		return fail(line, "front matter must be a mapping")
	}
	if n := findAnchor(&root); n != nil {
		return fail(n.Line+1, "front matter: YAML anchors, aliases and merge keys are not allowed")
	}
	doc := root.Content[0]
	str := func(key string) (*yaml.Node, *diag.Diagnostic) {
		n := field(doc, key)
		if n != nil && !isString(n) {
			_, d := fail(n.Line+1, "%s must be a string", key)
			return nil, d
		}
		return n, nil
	}
	idNode, d := str("id")
	if d != nil {
		return ruling{}, d
	}
	if idNode == nil {
		return fail(1, "id is required")
	}
	if idNode.Value != id {
		return fail(idNode.Line+1, "id %q does not match the file name (%s)", idNode.Value, id)
	}
	statusNode, d := str("status")
	if d != nil {
		return ruling{}, d
	}
	if statusNode == nil {
		return fail(1, "status is required")
	}
	if !slices.Contains(Statuses, statusNode.Value) {
		return fail(statusNode.Line+1, "status %q is not one of %s", statusNode.Value, strings.Join(Statuses, ", "))
	}
	r := ruling{status: statusNode.Value}
	byNode, d := str("supersededBy")
	if d != nil {
		return ruling{}, d
	}
	if byNode != nil {
		r.supersededBy = byNode.Value
	}
	return r, nil
}

func field(n *yaml.Node, name string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == name {
			return n.Content[i+1]
		}
	}
	return nil
}

// isString is a plain YAML string scalar, not a number, timestamp or null.
func isString(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" }

// findAnchor returns the first anchor, alias or merge key under n, in
// document order: Gantry forbids all three so a record reads the same to
// every parser.
func findAnchor(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.AliasNode || n.Anchor != "" || (n.Kind == yaml.ScalarNode && n.ShortTag() == "!!merge") {
		return n
	}
	for _, c := range n.Content {
		if a := findAnchor(c); a != nil {
			return a
		}
	}
	return nil
}
