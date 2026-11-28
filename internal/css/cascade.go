// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package css

import (
	"fmt"
	"strings"
)

// Computed is a custom property's final value with every var() substituted.
// Invalid is CSS's guaranteed-invalid value: a var() that cannot resolve.
type Computed struct {
	Value   string
	Invalid bool
}

// Compiled is a set of sheets with every selector and media condition
// parsed once, so Resolve can run once per environment cheaply.
type Compiled struct {
	rules []compiledRule
}

type compiledRule struct {
	sheet int
	sels  []Selector
	media []Atom
	decls []Decl
}

// Compile parses every rule's selectors and media. A rule that fails to
// parse is an error: no rule is ever skipped silently.
func Compile(sheets []*Sheet) (*Compiled, error) {
	c := &Compiled{}
	for si, sh := range sheets {
		for _, r := range sh.Rules {
			cr := compiledRule{sheet: si, decls: r.Decls}
			for _, s := range r.Selectors {
				sel, err := ParseSelector(s)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: %w", sh.File, r.Line, err)
				}
				cr.sels = append(cr.sels, sel)
			}
			atoms, err := ParseMedia(r.Media)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", sh.File, r.Line, err)
			}
			cr.media = atoms
			c.rules = append(c.rules, cr)
		}
	}
	return c, nil
}

// Resolve compiles the sheets and resolves them in one environment.
func Resolve(sheets []*Sheet, e Env) (map[string]Computed, error) {
	c, err := Compile(sheets)
	if err != nil {
		return nil, err
	}
	return c.Resolve(e), nil
}

type winner struct {
	value string
	spec  int
	sheet int
	order int
}

// Resolve computes every custom property in one environment.
func (c *Compiled) Resolve(e Env) map[string]Computed {
	wins := map[string]winner{}
	for _, r := range c.rules {
		if !MediaMatches(r.media, e) {
			continue
		}
		spec, ok := 0, false
		for _, s := range r.sels {
			if s.Matches(e) {
				ok = true
				spec = max(spec, s.Spec)
			}
		}
		if !ok {
			continue
		}
		for _, d := range r.decls {
			w, seen := wins[d.Name]
			if seen && (spec < w.spec ||
				(spec == w.spec && (r.sheet < w.sheet || (r.sheet == w.sheet && d.Order < w.order)))) {
				continue
			}
			wins[d.Name] = winner{d.Value, spec, r.sheet, d.Order}
		}
	}
	res := &resolver{wins: wins, out: map[string]Computed{}}
	names := sortedKeys(wins)
	res.markCycles(names)
	for _, name := range names {
		res.resolve(name)
	}
	return res.out
}

// maxValueLen caps an expanded value (CSS Variables L1 permits a UA cap);
// a value over it is Invalid.
const maxValueLen = 1 << 20

type resolver struct {
	wins map[string]winner
	out  map[string]Computed
}

// markCycles marks every property in a dependency cycle (an SCC of more
// than one node, or a self-reference) Invalid, whatever its fallbacks.
func (r *resolver) markCycles(names []string) {
	index := map[string]int{}
	low := map[string]int{}
	on := map[string]bool{}
	var stack []string
	next := 0
	var visit func(n string)
	visit = func(n string) {
		index[n], low[n] = next, next
		next++
		stack = append(stack, n)
		on[n] = true
		self := false
		for _, m := range collectVars(r.wins[n].value) {
			if _, ok := r.wins[m]; !ok {
				continue
			}
			if m == n {
				self = true
			}
			if _, seen := index[m]; !seen {
				visit(m)
				low[n] = min(low[n], low[m])
			} else if on[m] {
				low[n] = min(low[n], index[m])
			}
		}
		if low[n] != index[n] {
			return
		}
		var comp []string
		for {
			m := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			on[m] = false
			comp = append(comp, m)
			if m == n {
				break
			}
		}
		if len(comp) > 1 || self {
			for _, m := range comp {
				r.out[m] = Computed{Invalid: true}
			}
		}
	}
	for _, n := range names {
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}
}

// collectVars lists every var() target in s, including those inside fallbacks.
func collectVars(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		c := s[i]
		if c == '"' || c == '\'' {
			i = skipString(s, i)
			continue
		}
		if isVarAt(s, i) {
			end := matchParen(s, i+3)
			if end < 0 {
				return out
			}
			name, fallback, has := splitVarArgs(s[i+4 : end])
			out = append(out, name)
			if has {
				out = append(out, collectVars(fallback)...)
			}
			i = end + 1
			continue
		}
		i++
	}
	return out
}

func (r *resolver) resolve(name string) (Computed, bool) {
	if c, ok := r.out[name]; ok {
		return c, true
	}
	w, ok := r.wins[name]
	if !ok {
		return Computed{}, false
	}
	v, ok := r.subst(w.value)
	c := Computed{Value: v}
	if !ok {
		c = Computed{Invalid: true}
	}
	r.out[name] = c
	return c, true
}

// subst replaces every var() in s textually; false means some var() was
// unresolvable.
func (r *resolver) subst(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c == '"' || c == '\'' {
			j := skipString(s, i)
			b.WriteString(s[i:j])
			i = j
			continue
		}
		if isVarAt(s, i) {
			end := matchParen(s, i+3)
			if end < 0 {
				return "", false
			}
			name, fallback, hasFallback := splitVarArgs(s[i+4 : end])
			v, ok := r.lookup(name, fallback, hasFallback)
			if !ok {
				return "", false
			}
			b.WriteString(v)
			if b.Len() > maxValueLen {
				return "", false
			}
			i = end + 1
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), b.Len() <= maxValueLen
}

func (r *resolver) lookup(name, fallback string, hasFallback bool) (string, bool) {
	if !strings.HasPrefix(name, "--") {
		return "", false
	}
	if c, found := r.resolve(name); found && !c.Invalid {
		return c.Value, true
	}
	if !hasFallback {
		return "", false
	}
	return r.subst(fallback)
}

func isVarAt(s string, i int) bool {
	if i+4 > len(s) || !strings.EqualFold(s[i:i+4], "var(") {
		return false
	}
	if i > 0 {
		p := s[i-1]
		if p == '-' || p == '_' || p == '\\' || p >= 0x80 ||
			(p >= 'a' && p <= 'z') || (p >= 'A' && p <= 'Z') || (p >= '0' && p <= '9') {
			return false
		}
	}
	return true
}

// skipString returns the index just past the string starting at s[i].
func skipString(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(s)
}

// matchParen returns the index of the ')' closing the '(' at s[open], or -1.
func matchParen(s string, open int) int {
	depth := 0
	for j := open; j < len(s); j++ {
		switch s[j] {
		case '"', '\'':
			j = skipString(s, j) - 1
		case '\\':
			j++
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// splitVarArgs splits "--x, fallback" at the first top-level comma.
func splitVarArgs(args string) (name, fallback string, has bool) {
	depth := 0
	for j := 0; j < len(args); j++ {
		switch args[j] {
		case '"', '\'':
			j = skipString(args, j) - 1
		case '\\':
			j++
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(args[:j]), strings.TrimSpace(args[j+1:]), true
			}
		}
	}
	return strings.TrimSpace(args), "", false
}
