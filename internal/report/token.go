// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package report

import (
	"maps"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/tokens"
)

// Value is one of a token's values as show reports it: its mode key
// ("default" for $value), the value in CSS spelling, the alias chain from
// the token to the literal in that key's context, and the literal. Problem
// says why the chain stops short.
type Value struct {
	Key      string   `json:"key"`
	Value    string   `json:"value"`
	Chain    []string `json:"chain"`
	Resolved string   `json:"resolved,omitempty"`
	Problem  string   `json:"problem,omitempty"`
}

// Token is a token as show reports it.
type Token struct {
	Name        string  `json:"name"`
	Tier        string  `json:"tier"`
	Component   string  `json:"component"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Values      []Value `json:"values"`
}

// DescribeToken gathers what show reports about t.
func DescribeToken(set *tokens.Set, t *tokens.Token) Token {
	s := Token{
		Name: t.Name, Tier: t.Tier, Component: t.Component, Type: t.Type,
		Description: t.Description, Values: []Value{},
	}
	keys := slices.Sorted(maps.Keys(t.Modes))
	if t.Default != nil {
		keys = slices.Insert(keys, 0, "")
	}
	for _, k := range keys {
		v := t.Default
		label := "default"
		if k != "" {
			v, label = t.Modes[k], string(k)
		}
		sv := Value{Key: label, Value: emit.CSSValue(v), Chain: []string{t.Name}}
		ctx := k.Context()
		for v != nil && v.Alias != "" {
			if slices.Contains(sv.Chain, v.Alias) {
				sv.Problem = "alias cycle at " + v.Alias
				v = nil
				break
			}
			sv.Chain = append(sv.Chain, v.Alias)
			next, _ := set.ValueFor(v.Alias, ctx)
			if next == nil {
				sv.Problem = v.Alias + " is not defined or has no value here"
			}
			v = next
		}
		if v != nil {
			sv.Resolved = emit.CSSValue(v)
		}
		s.Values = append(s.Values, sv)
	}
	return s
}

// LookupToken finds the token name names, written with "-" or with the
// DTCG path's "."; nil when there is none.
func LookupToken(set *tokens.Set, name string) *tokens.Token {
	if set == nil {
		return nil
	}
	if t := set.Tokens[name]; t != nil {
		return t
	}
	return set.Tokens[strings.ReplaceAll(name, ".", "-")]
}
