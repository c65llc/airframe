// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package css

import (
	"fmt"
	"slices"
	"strings"
)

// Diagnostic IDs for constructs the importer cannot evaluate.
const (
	RuleUnsupportedSelector = "import-unsupported-selector"
	RuleUnsupportedMedia    = "import-unsupported-media"
)

// Cond is one attribute condition on the root element. Present with an empty
// Value means [attr]; Negated wraps the condition in :not(...).
type Cond struct {
	Attr, Value string
	Present     bool
	Negated     bool
}

func (c Cond) key() string {
	s := "[" + c.Attr
	if c.Value != "" {
		s += "='" + c.Value + "'"
	}
	s += "]"
	if c.Negated {
		s = ":not(" + s + ")"
	}
	return s
}

// Selector is a compound on :root. Spec counts the conditions plus one for
// :root itself.
type Selector struct {
	Conds []Cond
	Spec  int
}

// ParseSelector accepts :root (not html) followed by attribute conditions and
// :not() of one attribute condition, in any order. Attribute names are
// lower-cased; values keep their case.
func ParseSelector(s string) (Selector, error) {
	bad := func(why string) (Selector, error) {
		return Selector{}, fmt.Errorf("unsupported selector %q: %s", s, why)
	}
	rest := strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(rest, ":root"):
		rest = rest[len(":root"):]
	default:
		return bad("only :root compounds are supported")
	}
	sel := Selector{Spec: 1}
	for rest != "" {
		switch {
		case rest[0] == '[':
			c, n, err := parseAttr(rest)
			if err != nil {
				return bad(err.Error())
			}
			sel.Conds = append(sel.Conds, c)
			rest = rest[n:]
		case strings.HasPrefix(rest, ":not("):
			inner := strings.TrimLeft(rest[len(":not("):], " \t\r\n\f")
			if inner == "" || inner[0] != '[' {
				return bad(":not() must hold one attribute selector")
			}
			c, n, err := parseAttr(inner)
			if err != nil {
				return bad(err.Error())
			}
			after := strings.TrimLeft(inner[n:], " \t\r\n\f")
			if after == "" || after[0] != ')' {
				return bad(":not() must hold one attribute selector")
			}
			c.Negated = true
			sel.Conds = append(sel.Conds, c)
			rest = after[1:]
		default:
			return bad("only attribute conditions may follow :root")
		}
		sel.Spec++
	}
	return sel, nil
}

// parseAttr parses "[name]" or "[name = 'v']" at the start of s and returns
// the condition and the bytes consumed.
func parseAttr(s string) (Cond, int, error) {
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return Cond{}, 0, fmt.Errorf("unterminated attribute selector")
	}
	body := s[1:end]
	name, val, hasVal := strings.Cut(body, "=")
	if hasVal && strings.ContainsAny(name[max(0, len(name)-1):], "~|^$*") {
		return Cond{}, 0, fmt.Errorf("only [attr] and [attr=value] are supported")
	}
	name = strings.TrimSpace(name)
	if !isIdent(name) {
		return Cond{}, 0, fmt.Errorf("bad attribute name")
	}
	c := Cond{Attr: strings.ToLower(name), Present: true}
	if !hasVal {
		return c, end + 1, nil
	}
	val = strings.TrimSpace(val)
	if len(val) >= 2 && (val[0] == '\'' || val[0] == '"') && val[len(val)-1] == val[0] {
		val = val[1 : len(val)-1]
		if strings.ContainsAny(val, `'"\`) {
			return Cond{}, 0, fmt.Errorf("unsupported attribute value")
		}
	} else if !isIdent(val) {
		return Cond{}, 0, fmt.Errorf("attribute value must be a quoted string or identifier")
	}
	if val == "" {
		return Cond{}, 0, fmt.Errorf("empty attribute value")
	}
	c.Value = val
	return c, end + 1, nil
}

// isIdent reports whether s is a CSS identifier (without escapes): it
// starts with a name-start character (a letter, '_' or non-ASCII), or with
// '-' followed by a name-start character or another '-', and continues
// with name characters (those, digits and '-'). A digit, a lone '-' and
// '-' followed by a digit cannot start one: a browser drops a selector
// that uses them unquoted.
func isIdent(s string) bool {
	nameStart := func(r rune) bool {
		return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127
	}
	rs := []rune(s)
	switch {
	case len(rs) == 0:
		return false
	case rs[0] == '-':
		if len(rs) < 2 || !(nameStart(rs[1]) || rs[1] == '-') {
			return false
		}
	case !nameStart(rs[0]):
		return false
	}
	for _, r := range rs {
		if !(nameStart(r) || r == '-' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// Key is the canonical text of the selector: :root plus the sorted conditions.
func (s Selector) Key() string {
	keys := make([]string, len(s.Conds))
	for i, c := range s.Conds {
		keys[i] = c.key()
	}
	slices.Sort(keys)
	return ":root" + strings.Join(keys, "")
}

// Matches reports whether the selector matches the root element of e.
func (s Selector) Matches(e Env) bool {
	for _, c := range s.Conds {
		v, set := e.Attrs[c.Attr]
		ok := set && (c.Value == "" || v == c.Value)
		if ok == c.Negated {
			return false
		}
	}
	return true
}

// Atom is one (feature: value) media condition. Both are lower-cased.
type Atom struct{ Feature, Value string }

// discreteFeatures is the allow-list of media features whose values are
// discrete keywords. Range features (width, min-width, ...) are unsupported.
var discreteFeatures = map[string]bool{
	"prefers-color-scheme": true, "prefers-contrast": true, "prefers-reduced-motion": true,
	"prefers-reduced-transparency": true, "forced-colors": true, "inverted-colors": true,
	"pointer": true, "any-pointer": true, "hover": true, "any-hover": true,
	"color-gamut": true, "dynamic-range": true, "display-mode": true,
}

// ParseMedia parses the raw conditions of enclosing @media blocks, each a
// conjunction of (feature: value) atoms, into one flat atom list.
func ParseMedia(conds []string) ([]Atom, error) {
	var atoms []Atom
	for _, raw := range conds {
		got, err := parseConj(raw)
		if err != nil {
			return nil, fmt.Errorf("unsupported media condition %q: %s", raw, err)
		}
		atoms = append(atoms, got...)
	}
	return atoms, nil
}

func parseConj(raw string) ([]Atom, error) {
	var atoms []Atom
	rest := strings.TrimSpace(raw)
	for {
		if rest == "" || rest[0] != '(' {
			return nil, fmt.Errorf("only (feature: value) atoms joined by and are supported")
		}
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return nil, fmt.Errorf("unterminated parenthesis")
		}
		f, v, ok := strings.Cut(rest[1:end], ":")
		f, v = strings.ToLower(strings.TrimSpace(f)), strings.ToLower(strings.TrimSpace(v))
		if !ok || f == "" || v == "" || strings.ContainsAny(f+v, "():<>=") || strings.ContainsAny(f, " \t\r\n") {
			return nil, fmt.Errorf("an atom must be (feature: value)")
		}
		if !discreteFeatures[f] {
			return nil, fmt.Errorf("media feature %q is not a supported discrete feature", f)
		}
		atoms = append(atoms, Atom{f, v})
		rest = rest[end+1:]
		trimmed := strings.TrimLeft(rest, " \t\r\n\f")
		if trimmed == "" {
			return atoms, nil
		}
		if len(trimmed) == len(rest) || len(trimmed) < 4 || !strings.EqualFold(trimmed[:3], "and") || !strings.ContainsAny(trimmed[3:4], " \t\r\n\f") {
			return nil, fmt.Errorf("only and may join atoms")
		}
		rest = strings.TrimLeft(trimmed[3:], " \t\r\n\f")
	}
}

// MediaKey is the canonical text of an atom list: sorted, duplicates removed.
func MediaKey(atoms []Atom) string {
	keys := make([]string, len(atoms))
	for i, a := range atoms {
		keys[i] = "(" + a.Feature + ": " + a.Value + ")"
	}
	slices.Sort(keys)
	return strings.Join(slices.Compact(keys), " and ")
}

// Env is one browser environment: root attributes and media feature values.
// A missing key means unset.
type Env struct {
	Attrs map[string]string
	Media map[string]string
}

// MediaMatches reports whether every atom holds in e.
func MediaMatches(atoms []Atom, e Env) bool {
	for _, a := range atoms {
		if v, ok := e.Media[a.Feature]; !ok || v != a.Value {
			return false
		}
	}
	return true
}
