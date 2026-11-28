// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package css

import (
	"slices"
	"strings"
	"testing"
)

func TestParseSelectorForms(t *testing.T) {
	cases := []struct {
		in    string
		conds []Cond
		spec  int
	}{
		{":root", nil, 1},
		{"html", nil, 1},
		{":root[data-theme='dark']", []Cond{{Attr: "data-theme", Value: "dark", Present: true}}, 2},
		{`:root:not([data-theme="light"])[data-app='x']`, []Cond{
			{Attr: "data-theme", Value: "light", Present: true, Negated: true},
			{Attr: "data-app", Value: "x", Present: true},
		}, 3},
		{":root[data-density]", []Cond{{Attr: "data-density", Present: true}}, 2},
		{"html[ Data-Theme = 'Dark' ]", []Cond{{Attr: "data-theme", Value: "Dark", Present: true}}, 2},
		{":root:not([data-x])", []Cond{{Attr: "data-x", Present: true, Negated: true}}, 2},
	}
	for _, c := range cases {
		got, err := ParseSelector(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if !slices.Equal(got.Conds, c.conds) || got.Spec != c.spec {
			t.Errorf("%q: got %+v spec %d, want %+v spec %d", c.in, got.Conds, got.Spec, c.conds, c.spec)
		}
	}
}

func TestParseSelectorRejects(t *testing.T) {
	for _, in := range []string{".dark", "body", ":root .card", ":root:hover", "[data-x]", "",
		":rootx", ":root[a~='b']", ":root[a='']", ":root[a='b' i]", ":root:not(.x)", ":root:not([a][b])", ":root[a='b", ":root > body"} {
		_, err := ParseSelector(in)
		if err == nil {
			t.Errorf("%q: want error", in)
		} else if !strings.Contains(err.Error(), "\""+in+"\"") && in != "" {
			t.Errorf("%q: error %q does not name the selector", in, err)
		}
	}
}

func TestMatches(t *testing.T) {
	mk := func(s string) Selector {
		t.Helper()
		sel, err := ParseSelector(s)
		if err != nil {
			t.Fatal(err)
		}
		return sel
	}
	env := func(kv ...string) Env {
		e := Env{Attrs: map[string]string{}, Media: map[string]string{}}
		for i := 0; i < len(kv); i += 2 {
			e.Attrs[kv[i]] = kv[i+1]
		}
		return e
	}
	neg := mk(`:root:not([data-theme='light'])`)
	if !neg.Matches(env()) || !neg.Matches(env("data-theme", "dark")) || neg.Matches(env("data-theme", "light")) {
		t.Error("negated value condition wrong")
	}
	pres := mk(":root[data-x]")
	if pres.Matches(env()) || !pres.Matches(env("data-x", "anything")) {
		t.Error("presence condition wrong")
	}
	negp := mk(":root:not([data-x])")
	if !negp.Matches(env()) || negp.Matches(env("data-x", "v")) {
		t.Error("negated presence wrong")
	}
	if !mk("html").Matches(env("a", "b")) {
		t.Error("bare root must match")
	}
	if mk(":root[data-theme='dark']").Matches(env("data-theme", "Dark")) {
		t.Error("values are case-sensitive")
	}
}

func TestSelectorKey(t *testing.T) {
	a, _ := ParseSelector(`html[data-b='1'][data-a='2']`)
	b, _ := ParseSelector(`:root[data-a="2"][data-b="1"]`)
	if a.Key() != b.Key() {
		t.Errorf("keys differ: %q vs %q", a.Key(), b.Key())
	}
	c, _ := ParseSelector(`:root[data-a='2']`)
	if a.Key() == c.Key() {
		t.Error("distinct selectors share a key")
	}
}

func TestParseMediaForms(t *testing.T) {
	atoms, err := ParseMedia([]string{"(prefers-color-scheme: dark) and (pointer: coarse)"})
	if err != nil || len(atoms) != 2 {
		t.Fatalf("got %v %v", atoms, err)
	}
	if atoms[0] != (Atom{"prefers-color-scheme", "dark"}) || atoms[1] != (Atom{"pointer", "coarse"}) {
		t.Errorf("atoms %+v", atoms)
	}
	nested, err := ParseMedia([]string{"(Pointer:coarse)", "(prefers-color-scheme : DARK) AND (x: y)"})
	if err != nil || len(nested) != 3 || nested[0] != (Atom{"pointer", "coarse"}) || nested[1].Value != "dark" {
		t.Errorf("nested %+v %v", nested, err)
	}
	for _, in := range []string{"not screen", "(min-width: 40em) or (x: y)", "print", "screen and (x: y)",
		"(hover)", "(width >= 40px)", "(a: b) and", "(a: b)(c: d)", "(a: b", "only screen", "(: b)", "(a: )"} {
		if _, err := ParseMedia([]string{in}); err == nil {
			t.Errorf("%q: want error", in)
		} else if !strings.Contains(err.Error(), in) {
			t.Errorf("%q: error %q does not name the condition", in, err)
		}
	}
	if a, err := ParseMedia(nil); err != nil || len(a) != 0 {
		t.Errorf("empty: %v %v", a, err)
	}
}

func TestMediaMatchesAndKey(t *testing.T) {
	atoms, _ := ParseMedia([]string{"(pointer: coarse) and (prefers-color-scheme: dark)"})
	e := Env{Media: map[string]string{"pointer": "coarse", "prefers-color-scheme": "dark"}}
	if !MediaMatches(atoms, e) || MediaMatches(atoms, Env{Media: map[string]string{"pointer": "coarse"}}) {
		t.Error("media match wrong")
	}
	if !MediaMatches(nil, Env{}) {
		t.Error("no media always matches")
	}
	other, _ := ParseMedia([]string{"(prefers-color-scheme: dark)", "(pointer: coarse)"})
	if MediaKey(atoms) != MediaKey(other) {
		t.Errorf("%q vs %q", MediaKey(atoms), MediaKey(other))
	}
	conflict, _ := ParseMedia([]string{"(pointer: coarse)", "(pointer: fine)"})
	if MediaMatches(conflict, e) {
		t.Error("conflicting atoms cannot match")
	}
}
