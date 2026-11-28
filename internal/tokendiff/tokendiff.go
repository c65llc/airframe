// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package tokendiff compares the tokens of two loaded projects: which were
// added, removed or possibly renamed, and how each token both have changed,
// value by value in every mode context and fact by fact.
package tokendiff

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// Kind names what a Change is about.
type Kind string

// The kinds of change, in the order a token's changes are listed after
// its value changes.
const (
	KindValue       Kind = "value"
	KindType        Kind = "type"
	KindTier        Kind = "tier"
	KindComponent   Kind = "component"
	KindRetired     Kind = "retired"
	KindDescription Kind = "description"
)

// Value is a token's resolved value in one context. Value is the literal
// in CSS spelling (a colour as oklch(…)); Hex is a colour's sRGB hex,
// gamut-mapped as CSS Color 4 does, with an alpha byte when it is not
// opaque. Error says why the token has no value there; Value and Hex are
// then empty.
type Value struct {
	Value string `json:"value,omitempty"`
	Hex   string `json:"hex,omitempty"`
	Error string `json:"error,omitempty"`
}

// Token is an added or removed token with its base value.
type Token struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Tier      string `json:"tier,omitempty"`
	Component string `json:"component,omitempty"`
	Value     Value  `json:"value"`
}

// Rename pairs a removed token with an added one of the same type, tier
// and component and the same value in every context. It is a guess: both
// stay in Removed and Added.
type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Change is one difference in a token both projects have. A value change
// has the Context ("" for the base) and the Before and After values; any
// other kind has From and To ("true" or "false" for retired).
type Change struct {
	Kind          Kind
	Context       config.ModeKey
	Before, After Value
	From, To      string
}

// Changed is a token both projects have, with its changes: value changes
// first, by context (base first, then by pair count and key), then the
// other kinds in Kind order.
type Changed struct {
	Name    string   `json:"name"`
	Changes []Change `json:"changes"`
}

// Diff is how project b's tokens differ from project a's. Every list is
// sorted by name (Renamed by From).
type Diff struct {
	Added   []Token
	Removed []Token
	Renamed []Rename
	Changed []Changed
}

// Empty reports whether the projects' tokens are the same.
func (d Diff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

// Compare reports how b's tokens differ from a's.
func Compare(a, b *project.Project) Diff {
	as, bs := tokensOf(a), tokensOf(b)
	var d Diff
	for _, name := range slices.Sorted(maps.Keys(as.Tokens)) {
		ta := as.Tokens[name]
		tb := bs.Tokens[name]
		if tb == nil {
			d.Removed = append(d.Removed, summary(as, ta))
			continue
		}
		if cs := changes(as, ta, bs, tb); len(cs) > 0 {
			d.Changed = append(d.Changed, Changed{Name: name, Changes: cs})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(bs.Tokens)) {
		if as.Tokens[name] == nil {
			d.Added = append(d.Added, summary(bs, bs.Tokens[name]))
		}
	}
	d.Renamed = renames(as, bs, d.Removed, d.Added)
	return d
}

// tokensOf is p's token set, or an empty one.
func tokensOf(p *project.Project) *tokens.Set {
	if p == nil || p.Tokens == nil {
		return &tokens.Set{Tokens: map[string]*tokens.Token{}}
	}
	return p.Tokens
}

func summary(s *tokens.Set, t *tokens.Token) Token {
	return Token{Name: t.Name, Type: t.Type, Tier: t.Tier, Component: t.Component, Value: resolve(s, t.Name, "")}
}

// contexts lists every mode key that can change either token's value,
// closed under compatible unions, with the base first.
func contexts(as *tokens.Set, ta *tokens.Token, bs *tokens.Set, tb *tokens.Token) []config.ModeKey {
	keys := map[config.ModeKey]bool{"": true}
	for _, k := range emit.ModeKeys(as, ta) {
		keys[k] = true
	}
	for _, k := range emit.ModeKeys(bs, tb) {
		keys[k] = true
	}
	for grown := true; grown; {
		grown = false
		sorted := slices.SortedFunc(maps.Keys(keys), emit.CompareKeys)
		for i, x := range sorted {
			for _, y := range sorted[i+1:] {
				if u, ok := emit.UnionKey(x, y); ok && !keys[u] {
					keys[u], grown = true, true
				}
			}
		}
	}
	return slices.SortedFunc(maps.Keys(keys), emit.CompareKeys)
}

func changes(as *tokens.Set, ta *tokens.Token, bs *tokens.Set, tb *tokens.Token) []Change {
	var out []Change
	for _, k := range contexts(as, ta, bs, tb) {
		va, vb := resolve(as, ta.Name, k), resolve(bs, tb.Name, k)
		if va != vb {
			out = append(out, Change{Kind: KindValue, Context: k, Before: va, After: vb})
		}
	}
	facts := []struct {
		kind Kind
		a, b string
	}{
		{KindType, ta.Type, tb.Type},
		{KindTier, ta.Tier, tb.Tier},
		{KindComponent, ta.Component, tb.Component},
		{KindRetired, strconv.FormatBool(ta.Retired), strconv.FormatBool(tb.Retired)},
		{KindDescription, ta.Description, tb.Description},
	}
	for _, f := range facts {
		if f.a != f.b {
			out = append(out, Change{Kind: f.kind, From: f.a, To: f.b})
		}
	}
	return out
}

// renames pairs removed tokens with added ones. A pair needs the same
// type, tier and component, and the same value in every context. Among
// the candidates, pairs sharing more name segments go first, then by From
// and To name; each token pairs at most once. The result is sorted by From.
func renames(as, bs *tokens.Set, removed, added []Token) []Rename {
	type candidate struct {
		from, to string
		shared   int
	}
	var cands []candidate
	for _, r := range removed {
		ta := as.Tokens[r.Name]
		for _, n := range added {
			tb := bs.Tokens[n.Name]
			if ta.Type != tb.Type || ta.Tier != tb.Tier || ta.Component != tb.Component || !sameEverywhere(as, ta, bs, tb) {
				continue
			}
			cands = append(cands, candidate{r.Name, n.Name, sharedSegments(ta, tb)})
		}
	}
	slices.SortFunc(cands, func(x, y candidate) int {
		return cmp.Or(cmp.Compare(y.shared, x.shared), cmp.Compare(x.from, y.from), cmp.Compare(x.to, y.to))
	})
	var out []Rename
	taken := map[string]bool{} // "from\x00" or "to\x00" plus the name
	for _, c := range cands {
		if taken["from\x00"+c.from] || taken["to\x00"+c.to] {
			continue
		}
		taken["from\x00"+c.from], taken["to\x00"+c.to] = true, true
		out = append(out, Rename{From: c.from, To: c.to})
	}
	slices.SortFunc(out, func(x, y Rename) int { return cmp.Compare(x.From, y.From) })
	return out
}

// sharedSegments is how many distinct name segments a and b have in common.
func sharedSegments(a, b *tokens.Token) int {
	in := map[string]bool{}
	for _, s := range segments(a) {
		in[s] = true
	}
	n := 0
	for _, s := range segments(b) {
		if in[s] {
			n++
			in[s] = false
		}
	}
	return n
}

// segments are t's name segments: its DTCG path segments, each split on
// dashes ("gap-large" in a path is two), or its name split on dashes.
func segments(t *tokens.Token) []string {
	var out []string
	for _, p := range t.Path {
		out = append(out, strings.Split(p, "-")...)
	}
	if len(out) == 0 {
		out = strings.Split(t.Name, "-")
	}
	return out
}

func sameEverywhere(as *tokens.Set, ta *tokens.Token, bs *tokens.Set, tb *tokens.Token) bool {
	for _, k := range contexts(as, ta, bs, tb) {
		va, vb := resolve(as, ta.Name, k), resolve(bs, tb.Name, k)
		if va.Error != "" || vb.Error != "" || va != vb {
			return false
		}
	}
	return true
}

// resolve is the named token's literal in the context key sets.
func resolve(s *tokens.Set, name string, key config.ModeKey) Value {
	v, err := s.Resolve(name, key.Context())
	if err != nil {
		return Value{Error: err.Error()}
	}
	out := Value{Value: emit.CSSValue(v)}
	if c, ok := v.Lit.(color.OKLCH); ok {
		out.Hex = hex(c)
	}
	return out
}

// hex is c in sRGB as #rrggbb, or #rrggbbaa when it is not opaque.
func hex(c color.OKLCH) string {
	r, g, b := c.ToSRGB(color.CSSColor4)
	byteOf := func(v float64) int { return int(math.Round(math.Min(1, math.Max(0, v)) * 255)) }
	s := fmt.Sprintf("#%02x%02x%02x", byteOf(r), byteOf(g), byteOf(b))
	if c.Alpha < 1 {
		s += fmt.Sprintf("%02x", byteOf(c.Alpha))
	}
	return s
}
