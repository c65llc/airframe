// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package importer

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/emit"
)

// scopeKey is the normalised text of a CSS block's scope: its sorted media
// atoms and its :root selector's sorted conditions. A source block and an
// emitted block are the same scope exactly when their keys are equal.
func scopeKey(atoms []css.Atom, sel css.Selector) string {
	return css.MediaKey(atoms) + "|" + sel.Key()
}

// modeKeys lists every canonical mode key over cfg's loaded axes: each axis
// absent or set to one of its non-base values. Deferred axes are skipped.
// The keys are sorted by (pair count, key).
func modeKeys(cfg *config.Config) []config.ModeKey {
	axes := slices.Clone(cfg.Axes)
	axes = slices.DeleteFunc(axes, func(a config.Axis) bool { return a.Deferred() || len(a.Values) == 0 })
	slices.SortFunc(axes, func(a, b config.Axis) int { return strings.Compare(a.Name, b.Name) })
	combos := [][]string{nil} // pairs in axis-name order, so already canonical
	for _, a := range axes {
		next := slices.Clone(combos)
		for _, c := range combos {
			for _, v := range a.Values {
				if !a.IsBase(v) {
					next = append(next, append(slices.Clone(c), a.Name+":"+v))
				}
			}
		}
		combos = next
	}
	keys := make([]config.ModeKey, len(combos))
	for i, c := range combos {
		keys[i] = config.ModeKey(strings.Join(c, "+"))
	}
	slices.SortFunc(keys, compareKeys)
	return keys
}

// compareKeys orders mode keys by pair count, then text, as emit does.
func compareKeys(a, b config.ModeKey) int {
	return cmp.Or(cmp.Compare(a.Pairs(), b.Pairs()), strings.Compare(string(a), string(b)))
}

// buildIndex maps the scope key of every scope emit writes for some mode
// key back to that key. A key whose scopes emit cannot write is skipped: it
// cannot appear in generated CSS. Two keys sharing a scope are an error,
// since a block in that scope could not be mapped to one of them.
func buildIndex(cfg *config.Config) (map[string]config.ModeKey, error) {
	idx := map[string]config.ModeKey{}
	for _, key := range modeKeys(cfg) {
		scopes, err := emit.Scopes(cfg, key)
		if err != nil {
			continue
		}
		for _, sc := range scopes {
			atoms, sel, ok := parseEmitted(sc)
			if !ok {
				continue // a spelling the importer's CSS subset cannot read
			}
			sk := scopeKey(atoms, sel)
			if prev, dup := idx[sk]; dup && prev != key {
				return nil, fmt.Errorf("modes %s and %s are both spelled %s in %s, so a CSS block in that scope cannot be mapped to one of them",
					keyName(prev), keyName(key), spelling(atoms, sc.Selector), config.File)
			}
			idx[sk] = key
		}
	}
	return idx, nil
}

// parseEmitted parses an emitted scope with the css package, so source and
// emitted scopes normalise identically. Emitted media conditions carry
// their "@media" keyword; source ones do not.
func parseEmitted(sc emit.Scope) ([]css.Atom, css.Selector, bool) {
	conds := make([]string, len(sc.Media))
	for i, m := range sc.Media {
		conds[i] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m), "@media"))
	}
	atoms, err := css.ParseMedia(conds)
	if err != nil {
		return nil, css.Selector{}, false
	}
	sel, err := css.ParseSelector(sc.Selector)
	if err != nil {
		return nil, css.Selector{}, false
	}
	return atoms, sel, true
}

// spelling is a scope as an airframe.yaml web selector writes it, quoted:
// "@media (a) and (b) :root[...]", or the bare selector without media.
func spelling(atoms []css.Atom, selector string) string {
	sel := strings.TrimSpace(selector)
	if len(atoms) == 0 {
		return fmt.Sprintf("%q", sel)
	}
	return fmt.Sprintf("%q", "@media "+css.MediaKey(atoms)+" "+sel)
}

// keyName names a mode key in a message.
func keyName(k config.ModeKey) string {
	if k == "" {
		return "bare :root"
	}
	return string(k)
}
