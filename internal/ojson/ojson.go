// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package ojson parses JSON while preserving object key order and source line
// numbers, and marshals it back deterministically.
package ojson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Kind is the JSON type of a Node.
type Kind int

// Node kinds.
const (
	Object Kind = iota
	Array
	String
	Number
	Bool
	Null
)

// Node is one parsed JSON value. Line is the 1-based source line of the
// value's first token.
type Node struct {
	Kind   Kind
	Keys   []string
	Fields map[string]*Node
	Items  []*Node
	Str    string
	Num    float64
	Bool   bool
	Line   int
}

// SyntaxError is a parse failure with the line it occurred on.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

type parser struct {
	data []byte
	dec  *json.Decoder
}

// line returns the line of the decoder's current position. Tokens never
// contain raw newlines, so this is also the line of the token just read.
func (p *parser) line() int {
	off := int(p.dec.InputOffset())
	if off > len(p.data) {
		off = len(p.data)
	}
	return 1 + bytes.Count(p.data[:off], []byte{'\n'})
}

func (p *parser) errf(format string, a ...any) error {
	return &SyntaxError{Line: p.line(), Msg: fmt.Sprintf(format, a...)}
}

// Parse decodes a single JSON document, preserving key order and line numbers.
// Duplicate object keys are an error.
func Parse(data []byte) (*Node, error) {
	p := &parser{data: data, dec: json.NewDecoder(bytes.NewReader(data))}
	tok, err := p.dec.Token()
	if err != nil {
		return nil, p.wrap(err)
	}
	n, err := p.value(tok)
	if err != nil {
		return nil, err
	}
	if _, err := p.dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, p.errf("unexpected data after top-level value")
		}
		return nil, p.wrap(err)
	}
	return n, nil
}

func (p *parser) wrap(err error) error {
	var se *SyntaxError
	if errors.As(err, &se) {
		return err
	}
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return p.errf("%s", err.Error())
}

func (p *parser) value(tok json.Token) (*Node, error) {
	n := &Node{Line: p.line()}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			n.Kind = Object
			n.Fields = map[string]*Node{}
			for p.dec.More() {
				kt, err := p.dec.Token()
				if err != nil {
					return nil, p.wrap(err)
				}
				key, ok := kt.(string)
				if !ok {
					return nil, p.errf("expected string key")
				}
				if _, dup := n.Fields[key]; dup {
					return nil, p.errf("duplicate key %q", key)
				}
				vt, err := p.dec.Token()
				if err != nil {
					return nil, p.wrap(err)
				}
				child, err := p.value(vt)
				if err != nil {
					return nil, err
				}
				n.Keys = append(n.Keys, key)
				n.Fields[key] = child
			}
			if _, err := p.dec.Token(); err != nil { // closing }
				return nil, p.wrap(err)
			}
		case '[':
			n.Kind = Array
			for p.dec.More() {
				vt, err := p.dec.Token()
				if err != nil {
					return nil, p.wrap(err)
				}
				child, err := p.value(vt)
				if err != nil {
					return nil, err
				}
				n.Items = append(n.Items, child)
			}
			if _, err := p.dec.Token(); err != nil { // closing ]
				return nil, p.wrap(err)
			}
		default:
			return nil, p.errf("unexpected %q", rune(v))
		}
	case string:
		n.Kind, n.Str = String, v
	case float64:
		n.Kind, n.Num = Number, v
	case bool:
		n.Kind, n.Bool = Bool, v
	case nil:
		n.Kind = Null
	}
	return n, nil
}

// Marshal renders n with keys in Keys order. An empty indent produces compact
// output. The result always ends in a single LF.
func Marshal(n *Node, indent string) []byte {
	var b bytes.Buffer
	write(&b, n, indent, 0)
	b.WriteByte('\n')
	return b.Bytes()
}

func newline(b *bytes.Buffer, indent string, depth int) {
	if indent == "" {
		return
	}
	b.WriteByte('\n')
	for i := 0; i < depth; i++ {
		b.WriteString(indent)
	}
}

func quote(b *bytes.Buffer, s string) {
	var q bytes.Buffer
	enc := json.NewEncoder(&q)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // encoding a string cannot fail
	b.Write(bytes.TrimSuffix(q.Bytes(), []byte{'\n'}))
}

func write(b *bytes.Buffer, n *Node, indent string, depth int) {
	switch n.Kind {
	case Object:
		if len(n.Keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, k := range n.Keys {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(b, indent, depth+1)
			quote(b, k)
			b.WriteByte(':')
			if indent != "" {
				b.WriteByte(' ')
			}
			write(b, n.Fields[k], indent, depth+1)
		}
		newline(b, indent, depth)
		b.WriteByte('}')
	case Array:
		if len(n.Items) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, it := range n.Items {
			if i > 0 {
				b.WriteByte(',')
			}
			newline(b, indent, depth+1)
			write(b, it, indent, depth+1)
		}
		newline(b, indent, depth)
		b.WriteByte(']')
	case String:
		quote(b, n.Str)
	case Number:
		b.WriteString(strconv.FormatFloat(n.Num, 'f', -1, 64))
	case Bool:
		b.WriteString(strconv.FormatBool(n.Bool))
	case Null:
		b.WriteString("null")
	}
}
