// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokens

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
)

// valueFor picks t's value for ctx: the matching key with the most pairs,
// the default having none. Ties go to the first key in sort order; Check
// reports them as mode-ambiguous.
func (t *Token) valueFor(ctx config.Context) (*Value, config.ModeKey) {
	best, bestKey, bestN := t.Default, config.ModeKey(""), 0
	for _, k := range sortedKeys(t.Modes) {
		if n := k.Pairs(); k.Matches(ctx) && (n > bestN || best == nil) {
			best, bestKey, bestN = t.Modes[k], k, n
		}
	}
	return best, bestKey
}

// ValueFor is the named token's own value for ctx, one step (an alias is
// not followed), and the mode key that supplied it ("" for $value). It
// returns nil when the token is unknown or has no value for ctx.
func (s *Set) ValueFor(name string, ctx config.Context) (*Value, config.ModeKey) {
	t := s.Tokens[name]
	if t == nil {
		return nil, ""
	}
	return t.valueFor(ctx)
}

// Resolve follows the named token's aliases in ctx down to a literal.
func (s *Set) Resolve(name string, ctx config.Context) (*Value, error) {
	var chain []string
	for {
		if slices.Contains(chain, name) {
			return nil, fmt.Errorf("alias cycle %s", strings.Join(append(chain, name), " → "))
		}
		t := s.Tokens[name]
		if t == nil {
			if len(chain) == 0 {
				return nil, fmt.Errorf("unknown token %q", name)
			}
			return nil, fmt.Errorf("token %q aliases %q, which is not defined", chain[len(chain)-1], name)
		}
		chain = append(chain, name)
		v, _ := t.valueFor(ctx)
		if v == nil {
			return nil, fmt.Errorf("token %q has no value for this context", name)
		}
		if v.Alias == "" {
			return v, nil
		}
		name = v.Alias
	}
}

func finding(rule, file, path string, line int, format string, a ...any) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: file, Path: path, Line: line,
		Message: fmt.Sprintf(format, a...), Docs: diag.DocsURL(rule),
	}
}

// inMode is " (mode <key>)" for an override and "" for $value.
func inMode(k config.ModeKey) string {
	if k == "" {
		return ""
	}
	return " (mode " + string(k) + ")"
}

// Check reports what is wrong across tokens: aliases to unknown tokens
// (alias-unresolved) or to a token of another $type (alias-type-mismatch),
// alias cycles that some context reaches (alias-cycle), and mode keys that
// tie for a context with different values (mode-ambiguous). Findings come
// in token order.
func (s *Set) Check() []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, name := range s.Order {
		t := s.Tokens[name]
		keys, vals := t.values()
		for i, v := range vals {
			if v.Alias == "" {
				continue
			}
			target := s.Tokens[v.Alias]
			switch {
			case target == nil:
				out = append(out, finding("alias-unresolved", s.fileOf(v), name, v.Line,
					"token %q aliases %q, which is not defined%s", name, v.Alias, inMode(keys[i])))
			case target.Type != t.Type:
				out = append(out, finding("alias-type-mismatch", s.fileOf(v), name, v.Line,
					"token %q (%s) aliases %q (%s, %s:%d)%s", name, t.Type, target.Name, target.Type,
					target.File, target.Line, inMode(keys[i])))
			}
		}
	}
	out = append(out, s.cycles()...)
	for _, name := range s.Order {
		out = append(out, s.ambiguities(s.Tokens[name])...)
	}
	return out
}

// union merges mode keys; ok is false when two of them set one axis to
// different values, so no context matches them all.
func union(keys ...config.ModeKey) (config.Context, bool) {
	ctx := config.Context{}
	for _, k := range keys {
		if k == "" {
			continue
		}
		for _, p := range strings.Split(string(k), "+") {
			axis, val, _ := strings.Cut(p, ":")
			if prev, ok := ctx[axis]; ok && prev != val {
				return nil, false
			}
			ctx[axis] = val
		}
	}
	return ctx, true
}

// keyOf renders a partial context as a canonical mode key.
func keyOf(ctx config.Context) config.ModeKey {
	pairs := make([]string, 0, len(ctx))
	for _, axis := range sortedKeys(ctx) {
		pairs = append(pairs, axis+":"+ctx[axis])
	}
	return config.ModeKey(strings.Join(pairs, "+"))
}

// axesOf lists the axes the tokens' mode keys mention.
func axesOf(ts ...*Token) map[string]bool {
	out := map[string]bool{}
	for _, t := range ts {
		for k := range t.Modes {
			c, _ := union(k)
			for axis := range c {
				out[axis] = true
			}
		}
	}
	return out
}

// extend returns base extended by every combination of values of the
// named axes that base leaves open, in config axis order.
func (s *Set) extend(base config.Context, axes map[string]bool) []config.Context {
	out := []config.Context{base}
	for _, a := range s.axes {
		if _, set := base[a.Name]; set || !axes[a.Name] || len(a.Values) == 0 {
			continue
		}
		next := make([]config.Context, 0, len(out)*len(a.Values))
		for _, c := range out {
			for _, v := range a.Values {
				n := config.Context{a.Name: v}
				for k, x := range c {
					n[k] = x
				}
				next = append(next, n)
			}
		}
		out = next
	}
	return out
}

// reach lists the tokens reachable from name through any alias in any
// mode, name first, then in discovery order.
func (s *Set) reach(name string) []string {
	seen := map[string]bool{name: true}
	queue := []string{name}
	for i := 0; i < len(queue); i++ {
		t := s.Tokens[queue[i]]
		if t == nil {
			continue
		}
		_, vals := t.values()
		for _, v := range vals {
			if v.Alias != "" && !seen[v.Alias] {
				seen[v.Alias] = true
				queue = append(queue, v.Alias)
			}
		}
	}
	return queue
}

// onCycle reports whether some chain of aliases, in any modes, leads from
// name back to name.
func (s *Set) onCycle(name string) bool {
	for _, n := range s.reach(name) {
		if t := s.Tokens[n]; t != nil {
			_, vals := t.values()
			for _, v := range vals {
				if v.Alias == name {
					return true
				}
			}
		}
	}
	return false
}

// cycles reports each alias cycle some context actually follows, once.
// A static cycle (an alias path back to the start through any modes) is a
// candidate; it is reported only if a context over the axes its tokens
// vary on picks every alias on it at once.
func (s *Set) cycles() []diag.Diagnostic {
	index := map[string]int{}
	for i, n := range s.Order {
		index[n] = i
	}
	reported := map[string]bool{}
	var out []diag.Diagnostic
	for _, start := range s.Order {
		if !s.onCycle(start) {
			continue
		}
		var reach []*Token
		for _, n := range s.reach(start) {
			if t := s.Tokens[n]; t != nil {
				reach = append(reach, t)
			}
		}
		for _, ctx := range s.extend(config.Context{}, axesOf(reach...)) {
			var names []string
			var used []config.ModeKey
			var lines []*Value
			name := start
			for {
				t := s.Tokens[name]
				if t == nil {
					break
				}
				if at := slices.Index(names, name); at >= 0 {
					names, used, lines = names[at:], used[at:], lines[at:]
					// Rotate so the cycle starts at its first token in Order.
					first := 0
					for i, n := range names {
						if index[n] < index[names[first]] {
							first = i
						}
					}
					names = slices.Concat(names[first:], names[:first])
					used = slices.Concat(used[first:], used[:first])
					lines = slices.Concat(lines[first:], lines[:first])
					path := strings.Join(append(slices.Clone(names), names[0]), " → ")
					if reported[path] {
						break
					}
					reported[path] = true
					when := ""
					if c, _ := union(used...); len(c) > 0 {
						when = " when " + string(keyOf(c))
					}
					out = append(out, finding("alias-cycle", s.fileOf(lines[0]), names[0], lines[0].Line,
						"alias cycle %s%s", path, when))
					break
				}
				v, key := t.valueFor(ctx)
				if v == nil || v.Alias == "" {
					break
				}
				names, used, lines = append(names, name), append(used, key), append(lines, v)
				name = v.Alias
			}
		}
	}
	return out
}

func sameValue(a, b *Value) bool {
	return a.Alias == b.Alias && reflect.DeepEqual(a.Lit, b.Lit)
}

// ambiguities reports each pair of t's mode keys that tie with different
// values in some context. Only pairs that can match together are
// considered (their shared axes agree), and a pair is ambiguous only when
// some context matching both is not settled by a more specific key.
func (s *Set) ambiguities(t *Token) []diag.Diagnostic {
	keys := sortedKeys(t.Modes)
	axes := axesOf(t)
	var out []diag.Diagnostic
	for i, a := range keys {
		for _, b := range keys[i+1:] {
			if a.Pairs() != b.Pairs() || sameValue(t.Modes[a], t.Modes[b]) {
				continue
			}
			both, ok := union(a, b)
			if !ok {
				continue
			}
			for _, ctx := range s.extend(both, axes) {
				if _, won := t.valueFor(ctx); won.Pairs() != a.Pairs() {
					continue
				}
				v := t.Modes[b]
				out = append(out, finding("mode-ambiguous", s.fileOf(v), t.Name, v.Line,
					"token %q: mode keys %s and %s both match %s with different values; add a value for %s",
					t.Name, a, b, keyOf(both), keyOf(both)))
				break
			}
		}
	}
	return out
}
