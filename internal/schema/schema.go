// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package schema validates Airframe's YAML and JSON documents against their
// embedded JSON Schemas and reports violations as schema-invalid findings.
package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/c65llc/airframe/internal/diag"
)

// RuleID is the rule every violation is reported under.
const RuleID = "schema-invalid"

//go:embed config.schema.json
var configSchema []byte

// sources maps a document kind to its schema. Tasks 6 and 7 add "tokens"
// and "invariants" here.
var sources = map[string][]byte{
	"config": configSchema,
}

var (
	mu       sync.Mutex
	compiled = map[string]*jsonschema.Schema{}
)

func compile(kind string) (*jsonschema.Schema, error) {
	mu.Lock()
	defer mu.Unlock()
	if s, ok := compiled[kind]; ok {
		return s, nil
	}
	src, ok := sources[kind]
	if !ok {
		return nil, fmt.Errorf("unknown schema kind %q", kind)
	}
	raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	url := "airframe://schema/" + kind + ".json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(url, raw); err != nil {
		return nil, err
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, err
	}
	compiled[kind] = s
	return s, nil
}

func invalid(msg, path string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: RuleID, Severity: diag.Error, Path: path, Line: line,
		Message: msg, Docs: diag.DocsURL(RuleID),
	}
}

// Validate checks doc against the schema for kind ("config", later
// "tokens" and "invariants"). doc is a decoded value or a *yaml.Node; with
// a node, each finding carries the line of the offending key. The File of
// each finding is left for the caller to fill in.
func Validate(kind string, doc any) []diag.Diagnostic {
	s, err := compile(kind)
	if err != nil {
		return []diag.Diagnostic{invalid(err.Error(), "", 0)}
	}
	var root *yaml.Node
	value := doc
	if n, ok := doc.(*yaml.Node); ok {
		root = n
		if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
			root = n.Content[0]
		}
		value = nil
		if root != nil && root.Kind != 0 {
			if err := root.Decode(&value); err != nil {
				return []diag.Diagnostic{invalid(err.Error(), "", root.Line)}
			}
		}
	}
	// Round-trip through JSON so the validator sees only JSON types.
	b, err := json.Marshal(value)
	if err != nil {
		return []diag.Diagnostic{invalid("document is not JSON-representable: "+err.Error(), "", 0)}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return []diag.Diagnostic{invalid(err.Error(), "", 0)}
	}
	verr := s.Validate(inst)
	if verr == nil {
		return nil
	}
	ve, ok := verr.(*jsonschema.ValidationError)
	if !ok {
		return []diag.Diagnostic{invalid(verr.Error(), "", 0)}
	}
	p := message.NewPrinter(language.English)
	var out []diag.Diagnostic
	seen := map[string]bool{}
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		loc := e.InstanceLocation
		// An unknown key is reported at the key itself, one finding per key.
		if ap, ok := e.ErrorKind.(interface{ KeywordPath() []string }); ok && len(ap.KeywordPath()) == 1 && ap.KeywordPath()[0] == "additionalProperties" {
			props := additionalProps(e)
			for _, k := range props {
				add(&out, seen, fmt.Sprintf("unknown key %q", k), append(append([]string{}, loc...), k), root)
			}
			if len(props) > 0 {
				return
			}
		}
		add(&out, seen, e.ErrorKind.LocalizedString(p), loc, root)
	}
	walk(ve)
	return out
}

// additionalProps extracts the offending keys from an additionalProperties
// error; its message has the form "additional properties 'a', 'b' not allowed".
func additionalProps(e *jsonschema.ValidationError) []string {
	msg := e.ErrorKind.LocalizedString(message.NewPrinter(language.English))
	const pre, post = "additional properties ", " not allowed"
	if !strings.HasPrefix(msg, pre) || !strings.HasSuffix(msg, post) {
		return nil
	}
	var keys []string
	for _, f := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(msg, pre), post), ", ") {
		keys = append(keys, strings.Trim(f, "'"))
	}
	return keys
}

func add(out *[]diag.Diagnostic, seen map[string]bool, msg string, loc []string, root *yaml.Node) {
	path := strings.Join(loc, ".")
	key := path + "\x00" + msg
	if seen[key] {
		return
	}
	seen[key] = true
	*out = append(*out, invalid(msg, path, lineOf(root, loc)))
}

// lineOf walks root along loc and returns the line of the last key (or the
// last sequence item). When the walk falls off the tree it returns the line
// of the deepest node reached; 0 when there is no node tree.
func lineOf(root *yaml.Node, loc []string) int {
	if root == nil || root.Kind == 0 {
		return 0
	}
	n, line := root, root.Line
	for _, seg := range loc {
		switch n.Kind {
		case yaml.MappingNode:
			found := false
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == seg {
					line, n, found = n.Content[i].Line, n.Content[i+1], true
					break
				}
			}
			if !found {
				return line
			}
		case yaml.SequenceNode:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(n.Content) {
				return line
			}
			n = n.Content[i]
			line = n.Line
		default:
			return line
		}
	}
	return line
}
