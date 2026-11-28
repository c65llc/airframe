// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package format canonicalises Airframe's source files: DTCG token JSON and
// the YAML config, invariants and component files. Formatting never
// reorders groups or tokens.
package format

import (
	"bytes"
	"errors"
	"io"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/invariants"
	"github.com/c65llc/airframe/internal/ojson"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/retired"
)

// reserved is the order a token's reserved keys take; every other key
// follows in source order.
var reserved = []string{"$type", "$value", "$description", "$extensions"}

// Tokens canonicalises a DTCG token file: 2-space indent, LF, one final
// newline, and each token's reserved keys first in a fixed order. A token is
// any object with a $value key. Groups keep their key order, including their
// own $type, $description and $extensions.
func Tokens(data []byte) ([]byte, error) {
	n, err := ojson.Parse(data)
	if err != nil {
		return nil, err
	}
	reorder(n)
	return ojson.Marshal(n, "  "), nil
}

func reorder(n *ojson.Node) {
	switch n.Kind {
	case ojson.Object:
		if _, ok := n.Fields["$value"]; ok {
			keys := make([]string, 0, len(n.Keys))
			for _, k := range reserved {
				if _, ok := n.Fields[k]; ok {
					keys = append(keys, k)
				}
			}
			for _, k := range n.Keys {
				if !slices.Contains(reserved, k) {
					keys = append(keys, k)
				}
			}
			n.Keys = keys
		}
		for _, k := range n.Keys {
			reorder(n.Fields[k])
		}
	case ojson.Array:
		for _, it := range n.Items {
			reorder(it)
		}
	}
}

// YAML canonicalises a YAML file through a yaml.v3 node round trip: 2-space
// indent, comments kept, LF, exactly one final newline.
func YAML(data []byte) ([]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) { // no document: empty or comments only
			return normalise(data), nil
		}
		return nil, err
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case err == nil:
		return nil, errors.New("multiple YAML documents are not supported")
	case !errors.Is(err, io.EOF):
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return normalise(buf.Bytes()), nil
}

// normalise is LF line endings and exactly one final newline; empty input
// stays empty.
func normalise(b []byte) []byte {
	out := bytes.TrimRight(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), "\n")
	if len(out) == 0 {
		return []byte{}
	}
	return append(out, '\n')
}

// Paths lists, sorted, the repo-relative files Project formats: airframe.yaml,
// every token file, <content>/invariants.yaml and <content>/retired.yaml when the project has them, and
// every component file.
func Paths(p *project.Project) []string {
	out := []string{config.File}
	out = append(out, p.TokenFiles...)
	out = append(out, p.ComponentFiles...)
	inv := path.Join(p.Config.Content, invariants.File)
	if _, ok := p.Sources[inv]; ok {
		out = append(out, inv)
	}
	ret := path.Join(p.Config.Content, retired.File)
	if _, ok := p.Sources[ret]; ok {
		out = append(out, ret)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// File formats one source file by its name: token JSON or YAML.
func File(name string, data []byte) ([]byte, error) {
	if strings.HasSuffix(name, ".json") {
		return Tokens(data)
	}
	return YAML(data)
}

// Project returns the formatted bytes of every source file whose content
// would change, keyed by repo-relative path. CRLF-only differences in the
// source do not count (as not-formatted treats them). A file that does not
// parse is left out; its parse diagnostic already reports it. Generated
// outputs are never touched.
func Project(p *project.Project) map[string][]byte {
	out := map[string][]byte{}
	for _, f := range Paths(p) {
		src := p.Sources[f]
		got, err := File(f, src)
		if err != nil || bytes.Equal(got, bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))) {
			continue
		}
		out[f] = got
	}
	return out
}
