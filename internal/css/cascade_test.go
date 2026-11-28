// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package css

import (
	"fmt"
	"testing"
	"time"
)

func sheet(t *testing.T, src string) *Sheet {
	t.Helper()
	s, d := Parse("t.css", []byte(src))
	if len(d) != 0 {
		t.Fatalf("parse diagnostics: %v", d)
	}
	return s
}

func resolve(t *testing.T, e Env, srcs ...string) map[string]Computed {
	t.Helper()
	var ss []*Sheet
	for _, s := range srcs {
		ss = append(ss, sheet(t, s))
	}
	got, err := Resolve(ss, e)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func want(t *testing.T, got map[string]Computed, name string, c Computed) {
	t.Helper()
	if got[name] != c {
		t.Errorf("%s = %+v, want %+v", name, got[name], c)
	}
}

func TestResolveSpecificityBeatsOrder(t *testing.T) {
	e := Env{Attrs: map[string]string{"data-theme": "dark"}}
	got := resolve(t, e, `:root[data-theme='dark']{--a:2} :root{--a:1}`)
	want(t, got, "--a", Computed{Value: "2"})
}

func TestResolveOrderBreaksTies(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a:1} :root{--a:2}`)
	want(t, got, "--a", Computed{Value: "2"})
	got = resolve(t, Env{}, `:root{--a:1}`, `:root{--a:2}`)
	want(t, got, "--a", Computed{Value: "2"})
}

func TestResolveMediaGate(t *testing.T) {
	src := `:root{--a:1} @media (pointer: coarse){:root{--a:2}}`
	want(t, resolve(t, Env{Media: map[string]string{"pointer": "coarse"}}, src), "--a", Computed{Value: "2"})
	want(t, resolve(t, Env{Media: map[string]string{"pointer": "fine"}}, src), "--a", Computed{Value: "1"})
}

func TestResolveVarChain(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a: var(--b); --b: 3px}`)
	want(t, got, "--a", Computed{Value: "3px"})
}

func TestResolveFallback(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a: var(--missing, 4px); --b: var(--nope, var(--a)); --c: 0 1px var(--a, x) "var(--q)"}`)
	want(t, got, "--a", Computed{Value: "4px"})
	want(t, got, "--b", Computed{Value: "4px"})
	want(t, got, "--c", Computed{Value: `0 1px 4px "var(--q)"`})
}

func TestResolveNestedParensInFallback(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a: var(--m, calc(1px + (2px)))}`)
	want(t, got, "--a", Computed{Value: "calc(1px + (2px))"})
}

func TestResolveCycleIsInvalid(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a: var(--b); --b: var(--a); --c: var(--zz); --d: var(--a, 1); --e: 1px var(--c)}`)
	for _, n := range []string{"--a", "--b", "--c", "--e"} {
		want(t, got, n, Computed{Invalid: true})
	}
	want(t, got, "--d", Computed{Value: "1"})
}

func TestResolveEmptyValueIsValid(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a:; --b: var(--a)}`)
	want(t, got, "--a", Computed{})
	want(t, got, "--b", Computed{})
}

func TestResolveSelectorListMaxSpec(t *testing.T) {
	e := Env{Attrs: map[string]string{"data-theme": "dark"}}
	got := resolve(t, e, `:root, :root[data-theme='dark']{--a:2} :root[data-theme='light']{--a:9} :root{--a:1}`)
	want(t, got, "--a", Computed{Value: "2"})
}

func TestResolveBadRuleIsError(t *testing.T) {
	s := &Sheet{File: "t.css", Rules: []Rule{{Selectors: []string{"html"}, Decls: []Decl{{Name: "--a", Value: "1"}}}}}
	if _, err := Resolve([]*Sheet{s}, Env{}); err == nil {
		t.Error("bad selector: want error")
	}
	s = &Sheet{File: "t.css", Rules: []Rule{{Media: []string{"(width > 1px)"}, Selectors: []string{":root"}}}}
	if _, err := Resolve([]*Sheet{s}, Env{}); err == nil {
		t.Error("bad media: want error")
	}
}

func TestResolveFallbackOverInvalidTarget(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a: var(--b); --b: var(--a); --c: var(--a, 5px); --d: var(--missing, var(--a, 7px)); --z: var(--zz); --y: var(--z, 3px)}`)
	want(t, got, "--a", Computed{Invalid: true})
	want(t, got, "--b", Computed{Invalid: true})
	want(t, got, "--c", Computed{Value: "5px"})
	want(t, got, "--d", Computed{Value: "7px"})
	want(t, got, "--z", Computed{Invalid: true})
	want(t, got, "--y", Computed{Value: "3px"})
}

func TestResolveCycleWithFallbackIsOrderIndependent(t *testing.T) {
	for _, src := range []string{
		`:root{--a: var(--c, 1px); --c: var(--a)}`,
		`:root{--c: var(--a); --a: var(--c, 1px)}`,
	} {
		for range 20 {
			got := resolve(t, Env{}, src)
			want(t, got, "--a", Computed{Invalid: true})
			want(t, got, "--c", Computed{Invalid: true})
		}
	}
}

func TestResolveSelfReferenceIsInvalid(t *testing.T) {
	want(t, resolve(t, Env{}, `:root{--s: var(--s, 2px)}`), "--s", Computed{Invalid: true})
}

func TestResolveExpansionCap(t *testing.T) {
	src := ":root{--a0: 1234567890;"
	for i := 1; i <= 30; i++ {
		src += fmt.Sprintf("--a%d: var(--a%d) var(--a%d);", i, i-1, i-1)
	}
	src += "}"
	start := time.Now()
	got := resolve(t, Env{}, src)
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
	want(t, got, "--a30", Computed{Invalid: true})
	want(t, got, "--a29", Computed{Invalid: true})
	if got["--a1"].Invalid {
		t.Error("--a1 should be valid")
	}
}

func TestResolveSmallPins(t *testing.T) {
	got := resolve(t, Env{}, `:root{--a:1; --a:2; --X:1; --u: var(--x); --e: var(--x,); --w: var( --a ); --f: var( --nope , 9px )}`)
	want(t, got, "--a", Computed{Value: "2"})
	want(t, got, "--u", Computed{Invalid: true})
	want(t, got, "--e", Computed{})
	want(t, got, "--w", Computed{Value: "2"})
	want(t, got, "--f", Computed{Value: "9px"})
}
