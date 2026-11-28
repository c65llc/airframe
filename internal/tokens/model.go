// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package tokens parses DTCG token files with Airframe's extension fields
// into a Set, and resolves token values in a mode context.
package tokens

import (
	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
)

// Namespace is the $extensions key that carries Airframe's token facts.
const Namespace = "com.c65llc.airframe"

// Value is one token value as written: either an alias to another token
// (Alias holds the target's joined name) or a literal. Lit is a
// color.OKLCH, Dim, float64 (number, fontWeight), []string (fontFamily),
// Duration, [4]float64 (cubicBezier) or []Shadow. Line is where the value
// starts in its file.
type Value struct {
	Alias string
	Lit   any
	Line  int
}

// Dim is a dimension literal; Unit is "px" or "rem".
type Dim struct {
	V    float64
	Unit string
}

// Duration is a duration literal; Unit is "ms" or "s".
type Duration struct {
	V    float64
	Unit string
}

// Shadow is one layer of a shadow literal.
type Shadow struct {
	Color                          color.OKLCH
	OffsetX, OffsetY, Blur, Spread Dim
	Inset                          bool
}

// Token is one design token. Name is the DTCG path joined with "-". Default
// is the plain $value (nil when the token is only ever given in mode files,
// or when its $value was invalid); Modes holds the overrides by canonical
// mode key. Exempt maps a rule ID to its reason as written; an empty
// reason exempts nothing and lint reports it. File and Line
// locate the token's base declaration.
type Token struct {
	Name                                       string
	Path                                       []string
	Type, Tier, Component, Description, Chrome string
	Shared, Retired                            bool
	Default                                    *Value
	Modes                                      map[config.ModeKey]*Value
	Exempt                                     map[string]string
	Emit                                       []string
	// Names maps an emit format to this token's spelling in it, which wins
	// over the config's naming template.
	Names map[string]string
	File  string
	Line  int
}

// Set is every token parsed from a project's token files. Order lists the
// token names by file (sorted by path), then declaration order.
type Set struct {
	Tokens map[string]*Token
	Order  []string

	axes   []config.Axis     // the config's axes, for enumerating contexts
	origin map[*Value]string // the file each value was declared in
}

// FileOf is the file v was declared in: a mode value may come from a
// different file than its token's base declaration.
func (s *Set) FileOf(v *Value) string { return s.origin[v] }

// Values lists t's values with their keys: the default first (key ""),
// then the modes in key order. Callers walk it to find aliases.
func (t *Token) Values() ([]config.ModeKey, []*Value) { return t.values() }

// values lists t's values with their keys: the default first (key ""),
// then the modes in key order.
func (t *Token) values() ([]config.ModeKey, []*Value) {
	var keys []config.ModeKey
	var vals []*Value
	if t.Default != nil {
		keys, vals = append(keys, ""), append(vals, t.Default)
	}
	for _, k := range sortedKeys(t.Modes) {
		keys, vals = append(keys, k), append(vals, t.Modes[k])
	}
	return keys, vals
}
