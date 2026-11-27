// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// JSON writes the neutral token contract:
//
//	{ "$comment": header, "version": 1, "modes": {axis: [values]},
//	  "tokens": {name: {type, tier, component, value, modes: {key: value}}} }
//
// Tokens are sorted by name, mode keys by (pair count, key). Every value is
// resolved to its DTCG literal. A token lists every mode key that can change
// its value: its own, those of the tokens it reaches through aliases, and
// every compatible union of them, so a reader applying the most-pairs rule
// to this file alone gets the same answer Airframe does.
func JSON(p *project.Project, pl config.Platform) (map[string][]byte, error) {
	modes := &object{}
	for _, a := range p.Config.Axes {
		if !a.Deferred() {
			modes.set(a.Name, a.Values)
		}
	}
	toks := &object{}
	for _, t := range emitted(p, "json") {
		entry := &object{}
		entry.set("type", t.Type)
		if t.Tier != "" {
			entry.set("tier", t.Tier)
		}
		if t.Component != "" {
			entry.set("component", t.Component)
		}
		v, err := resolved(p.Tokens, t.Name, "")
		if err != nil {
			return nil, err
		}
		entry.set("value", v)
		byKey := &object{}
		for _, k := range modeKeys(p.Tokens, t) {
			v, err := resolved(p.Tokens, t.Name, k)
			if err != nil {
				return nil, err
			}
			byKey.set(string(k), v)
		}
		if len(byKey.keys) > 0 {
			entry.set("modes", byKey)
		}
		toks.set(t.Name, entry)
	}
	doc := &object{}
	doc.set("$comment", header(p))
	doc.set("version", 1)
	doc.set("modes", modes)
	doc.set("tokens", toks)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return map[string][]byte{outPath(p, pl, "json"): buf.Bytes()}, nil
}

// resolved is the named token's literal in the context key sets, in DTCG form.
func resolved(s *tokens.Set, name string, key config.ModeKey) (any, error) {
	ctx := config.Context{}
	for _, pr := range pairsOf(key) {
		ctx[pr[0]] = pr[1]
	}
	v, err := s.Resolve(name, ctx)
	if err != nil {
		if key != "" {
			return nil, fmt.Errorf("%v (mode %s)", err, key)
		}
		return nil, err
	}
	return dtcg(v.Lit), nil
}

// modeKeys lists the keys that can change t's value: every mode key of a
// token reachable from t through aliases, closed under compatible unions,
// sorted by (pair count, key).
func modeKeys(s *tokens.Set, t *tokens.Token) []config.ModeKey {
	keys := map[config.ModeKey]bool{}
	seen := map[string]bool{t.Name: true}
	for queue := []string{t.Name}; len(queue) > 0; queue = queue[1:] {
		u := s.Tokens[queue[0]]
		if u == nil {
			continue
		}
		vals := []*tokens.Value{u.Default}
		for k, v := range u.Modes {
			keys[k] = true
			vals = append(vals, v)
		}
		for _, v := range vals {
			if v != nil && v.Alias != "" && !seen[v.Alias] {
				seen[v.Alias] = true
				queue = append(queue, v.Alias)
			}
		}
	}
	for grown := true; grown; {
		grown = false
		sorted := slices.SortedFunc(maps.Keys(keys), compareKeys)
		for i, a := range sorted {
			for _, b := range sorted[i+1:] {
				if u, ok := unionKey(a, b); ok && !keys[u] {
					keys[u], grown = true, true
				}
			}
		}
	}
	return slices.SortedFunc(maps.Keys(keys), compareKeys)
}

// unionKey joins two mode keys; ok is false when they set one axis to
// different values.
func unionKey(a, b config.ModeKey) (config.ModeKey, bool) {
	vals := map[string]string{}
	for _, pr := range slices.Concat(pairsOf(a), pairsOf(b)) {
		if prev, ok := vals[pr[0]]; ok && prev != pr[1] {
			return "", false
		}
		vals[pr[0]] = pr[1]
	}
	parts := make([]string, 0, len(vals))
	for _, axis := range slices.Sorted(maps.Keys(vals)) {
		parts = append(parts, axis+":"+vals[axis])
	}
	return config.ModeKey(strings.Join(parts, "+")), true
}

// dtcg is a literal in its DTCG JSON form.
func dtcg(lit any) any {
	switch l := lit.(type) {
	case color.OKLCH:
		c := &object{}
		c.set("colorSpace", "oklch")
		c.set("components", []float64{zero(l.L), zero(l.C), zero(l.H)})
		if l.Alpha != 1 {
			c.set("alpha", zero(l.Alpha))
		}
		return c
	case tokens.Dim:
		return unit(l.V, l.Unit)
	case tokens.Duration:
		return unit(l.V, l.Unit)
	case float64:
		return zero(l)
	case []string:
		return l
	case [4]float64:
		return []float64{zero(l[0]), zero(l[1]), zero(l[2]), zero(l[3])}
	case []tokens.Shadow:
		out := make([]any, len(l))
		for i, s := range l {
			o := &object{}
			o.set("color", dtcg(s.Color))
			o.set("offsetX", dtcg(s.OffsetX))
			o.set("offsetY", dtcg(s.OffsetY))
			o.set("blur", dtcg(s.Blur))
			o.set("spread", dtcg(s.Spread))
			if s.Inset {
				o.set("inset", true)
			}
			out[i] = o
		}
		return out
	}
	return nil
}

func unit(v float64, u string) *object {
	o := &object{}
	o.set("value", zero(v))
	o.set("unit", u)
	return o
}

// zero turns -0 into 0 so it never prints as "-0".
func zero(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}

// object is a JSON object that keeps its keys in insertion order.
type object struct {
	keys []string
	vals []any
}

func (o *object) set(k string, v any) {
	o.keys = append(o.keys, k)
	o.vals = append(o.vals, v)
}

// MarshalJSON writes the object compactly, keys in insertion order and
// without HTML escaping; the caller's encoder indents it.
func (o *object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	out := []byte{'{'}
	for i, k := range o.keys {
		buf.Reset()
		if err := enc.Encode(k); err != nil {
			return nil, err
		}
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, bytes.TrimSuffix(buf.Bytes(), []byte("\n"))...)
		out = append(out, ':')
		buf.Reset()
		if err := enc.Encode(o.vals[i]); err != nil {
			return nil, err
		}
		out = append(out, bytes.TrimSuffix(buf.Bytes(), []byte("\n"))...)
	}
	return append(out, '}'), nil
}
