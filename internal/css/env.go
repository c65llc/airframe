// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package css

import (
	"fmt"
	"slices"
)

// RuleTooManyEnvironments is the diagnostic ID for an environment product
// past MaxEnvs.
const RuleTooManyEnvironments = "import-too-many-environments"

// MaxEnvs caps the number of environments Envs will enumerate.
const MaxEnvs = 200000

// anyValue stands for "set to a value no selector names"; every attribute
// gets it.
const anyValue = "*"

type dim struct {
	name   string
	isAttr bool
	values []string // values[0] is unset
}

// Envs returns the product of every attribute and media feature mentioned in
// the sheets. Each attribute is unset or one of its mentioned values or
// the synthetic "*" (set to a value no selector names); each media feature
// is unset or one of its values. Attributes come before features, each sorted
// by name, with unset first and then values sorted; the last dimension varies
// fastest. A rule with an unsupported selector or media condition is an
// error naming its file and line.
func Envs(sheets ...*Sheet) ([]Env, error) {
	attrs := map[string]map[string]bool{}
	feats := map[string]map[string]bool{}
	for _, sh := range sheets {
		for _, r := range sh.Rules {
			atoms, err := ParseMedia(r.Media)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %s: %w", sh.File, r.Line, RuleUnsupportedMedia, err)
			}
			for _, a := range atoms {
				add(feats, a.Feature, a.Value)
			}
			for _, raw := range r.Selectors {
				sel, err := ParseSelector(raw)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: %s: %w", sh.File, r.Line, RuleUnsupportedSelector, err)
				}
				for _, c := range sel.Conds {
					add(attrs, c.Attr, c.Value)
				}
			}
		}
	}
	var dims []dim
	for _, name := range sortedKeys(attrs) {
		set := attrs[name]
		set[anyValue] = true
		vals := sortedKeys(set)
		dims = append(dims, dim{name, true, append([]string{""}, vals...)})
	}
	for _, name := range sortedKeys(feats) {
		dims = append(dims, dim{name, false, append([]string{""}, sortedKeys(feats[name])...)})
	}
	total := 1
	for _, d := range dims {
		total *= len(d.values)
		if total > MaxEnvs {
			return nil, fmt.Errorf("%s: more than %d environments (%d attributes and media features in play)", RuleTooManyEnvironments, MaxEnvs, len(dims))
		}
	}
	envs := make([]Env, total)
	idx := make([]int, len(dims))
	for i := range envs {
		e := Env{Attrs: map[string]string{}, Media: map[string]string{}}
		for j, d := range dims {
			if idx[j] == 0 {
				continue
			}
			if d.isAttr {
				e.Attrs[d.name] = d.values[idx[j]]
			} else {
				e.Media[d.name] = d.values[idx[j]]
			}
		}
		envs[i] = e
		for j := len(dims) - 1; j >= 0; j-- {
			idx[j]++
			if idx[j] < len(dims[j].values) {
				break
			}
			idx[j] = 0
		}
	}
	return envs, nil
}

// add records name; a non-empty value is recorded under it.
func add(m map[string]map[string]bool, name, value string) {
	if m[name] == nil {
		m[name] = map[string]bool{}
	}
	if value != "" {
		m[name][value] = true
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
