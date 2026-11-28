// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package schema validates Airframe's YAML and JSON documents against their
// embedded JSON Schemas and reports violations as schema-invalid findings.
package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	jskind "github.com/santhosh-tekuri/jsonschema/v6/kind"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/c65llc/airframe/internal/diag"
)

// RuleID is the rule every violation is reported under.
const RuleID = "schema-invalid"

//go:embed config.schema.json
var configSchema []byte

//go:embed tokens.schema.json
var tokensSchema []byte

//go:embed invariants.schema.json
var invariantsSchema []byte

//go:embed component.schema.json
var componentSchema []byte

//go:embed retired.schema.json
var retiredSchema []byte

// sources maps a document kind to its schema.
var sources = map[string][]byte{
	"config":     configSchema,
	"tokens":     tokensSchema,
	"invariants": invariantsSchema,
	"component":  componentSchema,
	"retired":    retiredSchema,
}

// Kinds are the document kinds with a schema, in the order usage lists them.
var Kinds = []string{"config", "tokens", "invariants", "component", "retired"}

// Source returns the JSON Schema of a document kind as embedded.
func Source(kind string) ([]byte, bool) {
	b, ok := sources[kind]
	return b, ok
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

// Validate checks doc against the schema for kind (one of Kinds). doc is a decoded value or a *yaml.Node; with
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
		if ds := checkKeys(root, nil); len(ds) > 0 {
			return ds
		}
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
		if ap, ok := e.ErrorKind.(*jskind.AdditionalProperties); ok {
			for _, k := range ap.Properties {
				add(&out, seen, fmt.Sprintf("unknown key %q", k), append(append([]string{}, loc...), k), root)
			}
			return
		}
		add(&out, seen, e.ErrorKind.LocalizedString(p), loc, root)
	}
	walk(ve)
	return out
}

// checkKeys walks a YAML node tree and reports mapping keys that cannot be
// represented in JSON (non-strings), merge keys and duplicate keys, each at
// its own line.
func checkKeys(n *yaml.Node, path []string) []diag.Diagnostic {
	if n == nil {
		return nil
	}
	var out []diag.Diagnostic
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for i, c := range n.Content {
			out = append(out, checkKeys(c, append(path, strconv.Itoa(i)))...)
		}
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			here := strings.Join(append(append([]string{}, path...), k.Value), ".")
			// A merge key splices keys past the schema (and past every
			// loader that walks the node tree), so it is refused.
			if k.Tag == "!!merge" {
				out = append(out, invalid("YAML merge keys (<<) are not supported", strings.Join(append(append([]string{}, path...), "<<"), "."), k.Line))
				continue
			}
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				out = append(out, invalid("mapping keys must be strings", here, k.Line))
				continue
			}
			if seen[k.Value] {
				d := invalid(fmt.Sprintf("duplicate key %q", k.Value), here, k.Line)
				d.Rule = "yaml-syntax"
				d.Docs = diag.DocsURL(d.Rule)
				out = append(out, d)
			}
			seen[k.Value] = true
			out = append(out, checkKeys(v, append(path, k.Value))...)
		}
	}
	return out
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

var yamlLine = regexp.MustCompile(`line (\d+)`)

// DecodeYAML parses data as exactly one YAML document. Empty input (or
// comments only) is a zero Node. A syntax error, or a second document,
// which a single decode would silently ignore, is a yaml-syntax
// diagnostic at its line with no File set.
func DecodeYAML(data []byte) (yaml.Node, *diag.Diagnostic) {
	syntax := func(msg string, line int) *diag.Diagnostic {
		return &diag.Diagnostic{Rule: "yaml-syntax", Severity: diag.Error, Line: line, Message: msg, Docs: diag.DocsURL("yaml-syntax")}
	}
	fromErr := func(err error) *diag.Diagnostic {
		msg := strings.TrimPrefix(err.Error(), "yaml: ")
		line := 0
		if m := yamlLine.FindStringSubmatch(msg); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		return syntax(msg, line)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil && !errors.Is(err, io.EOF) {
		return yaml.Node{}, fromErr(err)
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case err == nil:
		return yaml.Node{}, syntax("multiple YAML documents are not supported", extra.Line)
	case !errors.Is(err, io.EOF):
		return yaml.Node{}, fromErr(err) // what follows the first --- does not parse
	}
	return root, nil
}
