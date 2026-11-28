// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package component loads the component records under <content>/components/:
// one <id>.yaml per component, naming the component-tier tokens it owns and
// the files allowed to read them.
package component

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
	"github.com/c65llc/airframe/internal/tokens"
)

// Dir is the directory, under the content root, that holds component files.
const Dir = "components"

// Tier is the token tier a component owns.
const Tier = "component"

// Component is one components/<id>.yaml. Owners are repo-relative globs of
// the files allowed to read Tokens; a Shared component's tokens are readable
// by any consumer. Noun, Ruling, HostedBy and Implementations are accepted
// and type-checked, and given rules later. HostedBy is "" when null or
// unset. Line is the line of the record's top-level mapping.
type Component struct {
	ID, File        string
	Line            int
	Tokens, Owners  []string
	Shared          bool
	Noun, Ruling    string
	HostedBy        string
	Implementations map[string]string

	tokensLine int   // line of the tokens key; 0 when unset
	tokenLines []int // line of each Tokens item
}

func invalid(file, at string, line int, format string, a ...any) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: schema.RuleID, Severity: diag.Error, File: file, Path: at, Line: line,
		Message: fmt.Sprintf(format, a...), Docs: diag.DocsURL(schema.RuleID),
	}
}

// isString is a plain YAML string scalar, not a timestamp or other scalar
// that JSON validation would see as a string.
func isString(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" }

var driveLetter = regexp.MustCompile(`^[A-Za-z]:`)

// Parse reads the component file at file, a repo-relative path whose
// basename (minus .yaml) the id must equal. On any diagnostic it returns
// nil; every diagnostic carries file.
func Parse(file string, data []byte) (*Component, []diag.Diagnostic) {
	root, bad := schema.DecodeYAML(data)
	if bad != nil {
		bad.File = file
		return nil, []diag.Diagnostic{*bad}
	}
	if ds := schema.Validate("component", &root); len(ds) > 0 {
		for i := range ds {
			ds[i].File = file
		}
		return nil, ds
	}
	n := root.Content[0]
	c := &Component{File: file, Line: n.Line}
	stringAt := func(at string, v *yaml.Node) (string, *diag.Diagnostic) {
		if !isString(v) {
			d := invalid(file, at, v.Line, "%s must be a string", at)
			return "", &d
		}
		return v.Value, nil
	}
	var idNode, ownersNode *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]
		var err *diag.Diagnostic
		switch k {
		case "id":
			idNode = v
			c.ID, err = stringAt(k, v)
		case "noun":
			c.Noun, err = stringAt(k, v)
		case "ruling":
			c.Ruling, err = stringAt(k, v)
		case "hostedBy":
			if v.ShortTag() != "!!null" {
				c.HostedBy, err = stringAt(k, v)
			}
		case "shared":
			c.Shared = v.Value == "true" // the schema checked it is a boolean
		case "tokens":
			c.tokensLine = n.Content[i].Line
			for j, it := range v.Content {
				var s string
				if s, err = stringAt(k+"."+strconv.Itoa(j), it); err != nil {
					break
				}
				c.Tokens = append(c.Tokens, s)
				c.tokenLines = append(c.tokenLines, it.Line)
			}
		case "owners":
			ownersNode = n.Content[i]
			for j, it := range v.Content {
				at := k + "." + strconv.Itoa(j)
				var s string
				if s, err = stringAt(at, it); err != nil {
					break
				}
				if e := ownerProblem(s); e != "" {
					d := invalid(file, at, it.Line, "owner %q %s", s, e)
					err = &d
					break
				}
				c.Owners = append(c.Owners, s)
			}
		case "implementations":
			c.Implementations = map[string]string{}
			for j := 0; j+1 < len(v.Content); j += 2 {
				var s string
				if s, err = stringAt(k+"."+v.Content[j].Value, v.Content[j+1]); err != nil {
					break
				}
				c.Implementations[v.Content[j].Value] = s
			}
		}
		if err != nil {
			return nil, []diag.Diagnostic{*err}
		}
	}
	if want := strings.TrimSuffix(path.Base(file), ".yaml"); c.ID != want {
		return nil, []diag.Diagnostic{invalid(file, "id", idNode.Line,
			"id %s must equal the file's basename (%s)", c.ID, want)}
	}
	if !c.Shared && len(c.Owners) == 0 {
		line := c.Line
		if ownersNode != nil {
			line = ownersNode.Line
		}
		return nil, []diag.Diagnostic{invalid(file, "owners", line,
			"component %s: owners is required and non-empty unless shared: true; owners are declared, never inferred", c.ID)}
	}
	return c, nil
}

// ownerProblem says what is wrong with owner glob g, or "" when it is a
// valid repo-relative glob.
func ownerProblem(g string) string {
	clean := path.Clean(g)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") ||
		strings.Contains(g, `\`) || driveLetter.MatchString(g) {
		return "must be a repo-relative glob: a slash path under the project root"
	}
	if !doublestar.ValidatePattern(g) {
		return "is not a valid glob"
	}
	return ""
}

// Check runs the consistency checks between the loaded components and the
// token set, each finding schema-invalid at the component file:
//   - every listed token exists, has tier component and names the component;
//   - every component-tier token naming component X is in X's tokens list.
//
// A token whose component is not in cs is skipped: either its file did not
// load (already reported) or it has none, which is not a load error (a
// consumer read of it is token-owner-violation). Findings are in
// component-id order, then token order.
func Check(cs map[string]*Component, set *tokens.Set) []diag.Diagnostic {
	if set == nil {
		return nil
	}
	var out []diag.Diagnostic
	ids := make([]string, 0, len(cs))
	for id := range cs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		c := cs[id]
		for i, name := range c.Tokens {
			at, line := "tokens."+strconv.Itoa(i), c.tokenLines[i]
			t, ok := set.Tokens[name]
			switch {
			case !ok:
				out = append(out, invalid(c.File, at, line, "component %s: listed token %s does not exist", id, name))
			case t.Tier != Tier:
				out = append(out, invalid(c.File, at, line, "component %s: listed token %s is tier %s, not %s (%s:%d)",
					id, name, tierName(t.Tier), Tier, t.File, t.Line))
			case t.Component != id:
				owner := "no component"
				if t.Component != "" {
					owner = "component " + t.Component
				}
				out = append(out, invalid(c.File, at, line, "component %s: listed token %s belongs to %s, not %s (%s:%d)",
					id, name, owner, id, t.File, t.Line))
			}
		}
	}
	for _, name := range set.Order {
		t := set.Tokens[name]
		if t.Tier != Tier || t.Component == "" {
			continue
		}
		c, ok := cs[t.Component]
		if !ok || slices.Contains(c.Tokens, name) {
			continue
		}
		line := c.tokensLine
		if line == 0 {
			line = c.Line
		}
		out = append(out, invalid(c.File, "tokens", line,
			"component %s: %s (%s:%d) is a component-tier token of %s, but %s's tokens does not list it",
			c.ID, name, t.File, t.Line, c.ID, c.ID))
	}
	return out
}

func tierName(t string) string {
	if t == "" {
		return "unset"
	}
	return t
}
