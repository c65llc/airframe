// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package find

import "slices"

// synonymGroups are the role words that stand for one another. A query
// word matches a name segment of any word in its group, one point below a
// direct match.
var synonymGroups = [][]string{
	{"muted", "subtle", "secondary"},
	{"bg", "background", "surface"},
	{"fg", "text", "ink", "foreground"},
	{"border", "stroke", "outline", "divider"},
	{"accent", "brand", "primary"},
	{"danger", "error", "destructive"},
	{"gap", "space", "spacing"},
	{"radius", "corner", "rounding"},
}

// synonyms maps a word to the other words of its groups, sorted.
var synonyms = func() map[string][]string {
	m := map[string][]string{}
	for _, g := range synonymGroups {
		for _, w := range g {
			for _, o := range g {
				if o != w {
					m[w] = append(m[w], o)
				}
			}
		}
	}
	for w := range m {
		slices.Sort(m[w])
		m[w] = slices.Compact(m[w])
	}
	return m
}()
