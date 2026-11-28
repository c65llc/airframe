// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package find ranks a project's tokens against a free-text query by the
// words of their names, descriptions and component nouns.
package find

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// DefaultLimit is how many hits Rank returns when Options.Limit is 0.
const DefaultLimit = 10

// The weights a word earns for each kind of match.
const (
	wholeSegment = 3 // the word is a whole name segment
	segmentStart = 2 // the word starts a name segment
	wholeDesc    = 2 // the word is a word of the description
	descStart    = 1 // the word starts a description word
	nounWord     = 1 // the word is a word of the component noun
)

var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "for": true, "of": true,
	"on": true, "in": true, "to": true, "with": true,
}

// Options are Rank's filters.
type Options struct {
	Limit   int    // at most this many hits; 0 means DefaultLimit
	Type    string // keep only tokens of this DTCG type; empty keeps all
	File    string // keep only tokens this consumer file may read; empty keeps all
	Retired bool   // include retired tokens
}

// Hit is one ranked token.
type Hit struct {
	Name        string `json:"name"`
	Tier        string `json:"tier"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Score       int    `json:"score"`
	Why         string `json:"why,omitempty"`
}

// Words are the query's words: lowercase runs of letters, without the
// stopwords.
func Words(query string) []string {
	var out []string
	for _, w := range split(query) {
		if !stopwords[w] {
			out = append(out, w)
		}
	}
	return out
}

// split lowercases s and splits it on every non-letter.
func split(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) })
}

// text is a token's searchable words.
type text struct {
	segments, desc, noun []string
}

func textOf(p *project.Project, t *tokens.Token) text {
	x := text{desc: split(t.Description)}
	for _, seg := range t.Path {
		x.segments = append(x.segments, split(seg)...)
	}
	if len(x.segments) == 0 {
		x.segments = split(t.Name)
	}
	if c := p.Components[t.Component]; c != nil && c.Noun != "" {
		x.noun = split(c.Noun)
	} else {
		x.noun = split(t.Component)
	}
	return x
}

// score is what word w earns against x, less penalty for each match.
func (x text) score(w string, penalty int) int {
	total := 0
	add := func(weight int) {
		if weight -= penalty; weight > 0 {
			total += weight
		}
	}
	add(best(w, x.segments, wholeSegment, segmentStart))
	add(best(w, x.desc, wholeDesc, descStart))
	if slices.Contains(x.noun, w) {
		add(nounWord)
	}
	return total
}

// best is whole when w is one of words, start when it begins one, else 0.
func best(w string, words []string, whole, start int) int {
	if slices.Contains(words, w) {
		return whole
	}
	for _, s := range words {
		if strings.HasPrefix(s, w) {
			return start
		}
	}
	return 0
}

// wordScore is w's score: its direct score, or a synonym's, one lower,
// whichever is more.
func (x text) wordScore(w string) int {
	s := x.score(w, 0)
	for _, syn := range synonyms[w] {
		s = max(s, x.score(syn, 1))
	}
	return s
}

// tierRank orders ties: semantic, component, primitive, then any other.
func tierRank(tier string) int {
	switch tier {
	case "semantic":
		return 0
	case "component":
		return 1
	case "primitive":
		return 2
	}
	return 3
}

// Rank returns the tokens of p that match query, best first: by score,
// then tier, then name. Tokens that score 0 are dropped, as are retired
// ones unless opts.Retired. A token that opts.File may not read is dropped
// too, and each remaining hit says why it may be read.
func Rank(p *project.Project, query string, opts Options) []Hit {
	words := Words(query)
	if p == nil || p.Tokens == nil || len(words) == 0 {
		return nil
	}
	var hits []Hit
	for _, name := range p.Tokens.Order {
		t := p.Tokens.Tokens[name]
		if t == nil || (t.Retired && !opts.Retired) {
			continue
		}
		if opts.Type != "" && !strings.EqualFold(t.Type, opts.Type) {
			continue
		}
		h := Hit{Name: t.Name, Tier: t.Tier, Type: t.Type, Description: t.Description}
		if opts.File != "" {
			ok, why := lint.CanRead(p, opts.File, t)
			if !ok {
				continue
			}
			h.Why = why
		}
		x := textOf(p, t)
		for _, w := range words {
			h.Score += x.wordScore(w)
		}
		if h.Score == 0 {
			continue
		}
		if t.Default != nil {
			h.Value = emit.CSSValue(t.Default)
		}
		hits = append(hits, h)
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			cmp.Compare(tierRank(a.Tier), tierRank(b.Tier)),
			cmp.Compare(a.Name, b.Name),
		)
	})
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}
