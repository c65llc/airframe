// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/ojson"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// A record version is a content hash of one record's formatted source: a
// token's node, or a whole component file. The read tools return it, and
// the write tools take it back as baseVersion, so an edit made on a stale
// read is refused instead of overwriting someone else's change, as in
// Starboard's MCP server design. Formatting first means a change fmt would
// undo is not a change; a change to another token is not one either.

// hash is the version of text.
func hash(text []byte) string {
	sum := sha256.Sum256(text)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// tokenVersion is the version of t: its node in its own token file and in
// every mode file that gives it a value, each with only its own $ keys (a
// nested token is a record of its own), in file order. Each node also
// carries the group $type it inherits along its path, when it has none of
// its own, since that is the token's type too.
func tokenVersion(p *project.Project, t *tokens.Token) string {
	files := []string{t.File}
	_, vals := t.Values()
	for _, v := range vals {
		if f := p.Tokens.FileOf(v); f != "" {
			files = append(files, f)
		}
	}
	slices.Sort(files)
	var b bytes.Buffer
	for _, file := range slices.Compact(files) {
		n := tokenNode(p.Sources[file], t.Path)
		if n == nil {
			continue
		}
		b.WriteString(file + "\n")
		b.Write(ojson.Marshal(n, "  "))
		b.WriteString("\n")
	}
	return hash(b.Bytes())
}

// tokenNode is the node at path in the token file src, formatted and with
// only its $ keys, or nil when src does not parse or has no such node.
func tokenNode(src []byte, path []string) *ojson.Node {
	if formatted, err := format.Tokens(src); err == nil {
		src = formatted
	}
	n, err := ojson.Parse(src)
	if err != nil {
		return nil
	}
	var inherited *ojson.Node
	for _, key := range path {
		if n.Kind != ojson.Object || n.Fields[key] == nil {
			return nil
		}
		if typ := n.Fields["$type"]; typ != nil {
			inherited = typ
		}
		n = n.Fields[key]
	}
	if n.Kind != ojson.Object {
		return nil
	}
	own := &ojson.Node{Kind: ojson.Object, Fields: map[string]*ojson.Node{}}
	for _, k := range n.Keys {
		if strings.HasPrefix(k, "$") {
			own.Keys = append(own.Keys, k)
			own.Fields[k] = n.Fields[k]
		}
	}
	// A key no token file can hold, so it never collides with the node's
	// own: the group's $type, when the token inherits it.
	if inherited != nil && own.Fields["$type"] == nil {
		own.Keys = append(own.Keys, "inherited $type")
		own.Fields["inherited $type"] = inherited
	}
	return own
}

// componentVersion is the version of c: its whole file, formatted.
func componentVersion(p *project.Project, c *component.Component) string {
	src := p.Sources[c.File]
	if formatted, err := format.YAML(src); err == nil {
		src = formatted
	}
	return hash(src)
}
