// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cssvalue

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/tokens"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 2e-3 }

func TestParseColourSpellings(t *testing.T) {
	red := color.OKLCH{L: 0.628, C: 0.2577, H: 29.23, Alpha: 1}
	for _, tc := range []struct {
		in    string
		alpha float64
	}{
		{"#f00", 1}, {"#FF0000", 1}, {"#ff000080", 0.502}, {"rgb(255 0 0 / 50%)", 0.5},
		{"rgba(255,0,0,.5)", 0.5}, {"hsl(0deg 100% 50%)", 1}, {"hsl(0, 100%, 50%)", 1},
		{"hsl(0turn 100% 50% / .5)", 0.5}, {"rgb(100% 0% 0%)", 1},
		{"oklch(62.8% 0.2577 29.23)", 1}, {"#f00f", 1},
	} {
		typ, v, _, err := Parse("c", tc.in)
		if err != nil || typ != "color" {
			t.Fatalf("%s: typ %q err %v", tc.in, typ, err)
		}
		c := v.Lit.(color.OKLCH)
		if !approx(c.L, red.L) || !approx(c.C, red.C) || math.Abs(c.H-red.H) > 0.3 || !approx(c.Alpha, tc.alpha) {
			t.Errorf("%s: got %+v", tc.in, c)
		}
	}
	// Non-zero hues, with OKLCH computed independently of the converter.
	for in, want := range map[string]color.OKLCH{
		"hsl(120deg 100% 25%)":     {L: 0.518287, C: 0.176360, H: 142.4953, Alpha: 1},
		"hsl(240, 100%, 50%)":      {L: 0.452014, C: 0.313214, H: 264.0520, Alpha: 1},
		"hsl(0.6667turn 100% 50%)": {L: 0.452014, C: 0.313214, H: 264.0520, Alpha: 1},
	} {
		typ, v, _, err := Parse("c", in)
		if err != nil || typ != "color" {
			t.Fatalf("%s: typ %q err %v", in, typ, err)
		}
		c := v.Lit.(color.OKLCH)
		if math.Abs(c.L-want.L) > 1e-3 || math.Abs(c.C-want.C) > 1e-3 || math.Abs(c.H-want.H) > 0.1 || c.Alpha != want.Alpha {
			t.Errorf("%s: got %+v, want %+v", in, c, want)
		}
	}
	_, v, _, err := Parse("c", "transparent")
	if err != nil || v.Lit.(color.OKLCH) != (color.OKLCH{}) {
		t.Errorf("transparent: %+v %v", v, err)
	}
	_, v, _, _ = Parse("c", "oklch(50% 0.1 120 / 0.25)")
	if v.Lit.(color.OKLCH).Alpha != 0.25 {
		t.Errorf("oklch alpha: %+v", v.Lit)
	}
}

func TestParseUnits(t *testing.T) {
	cases := []struct {
		in, typ string
		lit     any
	}{
		{"12px", "dimension", tokens.Dim{V: 12, Unit: "px"}},
		{"1.25rem", "dimension", tokens.Dim{V: 1.25, Unit: "rem"}},
		{"150ms", "duration", tokens.Duration{V: 150, Unit: "ms"}},
		{".2s", "duration", tokens.Duration{V: 0.2, Unit: "s"}},
		{"cubic-bezier(.2, 0, 0, 1)", "cubicBezier", [4]float64{0.2, 0, 0, 1}},
		{"0.5", "number", 0.5},
	}
	for _, tc := range cases {
		typ, v, _, err := Parse("x", tc.in)
		if err != nil || typ != tc.typ || v.Lit != tc.lit {
			t.Errorf("%s: %q %v %v", tc.in, typ, v, err)
		}
	}
}

// A quoted family name that spells a generic keyword names a font, not the
// generic: it keeps its quotes, so it stays distinct from the generic.
func TestParseFontQuotedGeneric(t *testing.T) {
	_, v, _, err := Parse("--body-font", `"sans-serif", 'serif', monospace, "Sans-Serif", "system-ui"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`"sans-serif"`, `"serif"`, "monospace", "Sans-Serif", `"system-ui"`}
	if got, _ := v.Lit.([]string); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseFontByName(t *testing.T) {
	typ, v, _, err := Parse("--body-font", `"IBM Plex Sans", system-ui`)
	got, _ := v.Lit.([]string)
	if err != nil || typ != "fontFamily" || len(got) != 2 || got[0] != "IBM Plex Sans" || got[1] != "system-ui" {
		t.Fatalf("%q %v %v", typ, v, err)
	}
	if _, _, _, err := Parse("x", `"IBM Plex Sans", system-ui`); err == nil {
		t.Error("font list under --x should be unsupported")
	}
	typ, v, _, err = Parse("heading-weight", "bold")
	if err != nil || typ != "fontWeight" || v.Lit != 700.0 {
		t.Errorf("bold: %q %v %v", typ, v, err)
	}
	typ, v, _, _ = Parse("heading-weight", "600")
	if typ != "fontWeight" || v.Lit != 600.0 {
		t.Errorf("600: %q %v", typ, v)
	}
}

func TestParseShadow(t *testing.T) {
	typ, v, _, err := Parse("elev", "0 1px 2px 0 rgb(0 0 0 / 20%), inset 0 0 0 1px #000")
	if err != nil || typ != "shadow" {
		t.Fatalf("%q %v", typ, err)
	}
	l := v.Lit.([]tokens.Shadow)
	if len(l) != 2 || l[0].Inset || !l[1].Inset || l[0].Blur != (tokens.Dim{V: 2, Unit: "px"}) ||
		l[1].Spread != (tokens.Dim{V: 1, Unit: "px"}) || !approx(l[0].Color.Alpha, 0.2) {
		t.Errorf("%+v", l)
	}
	_, v, _, err = Parse("elev", "#000 0 1px")
	if err != nil || len(v.Lit.([]tokens.Shadow)) != 1 {
		t.Errorf("colour first: %v %v", v, err)
	}
}

func TestParseAlias(t *testing.T) {
	typ, v, warn, err := Parse("x", " var( --accent ) ")
	if err != nil || typ != "" || v.Alias != "accent" || warn != "" {
		t.Errorf("%q %+v %q %v", typ, v, warn, err)
	}
	_, v, warn, err = Parse("x", "var(--a, 4px)")
	if err != nil || v.Alias != "a" || !strings.Contains(warn, "4px") {
		t.Errorf("%+v %q %v", v, warn, err)
	}
	if _, _, _, err := Parse("x", "0 1px var(--c)"); err == nil {
		t.Error("embedded var should error")
	}
}

func TestParseUnsupported(t *testing.T) {
	for _, in := range []string{"calc(1px + 2px)", "1em", "50%", "auto", "red"} {
		_, _, _, err := Parse("x", in)
		if err == nil || !strings.Contains(err.Error(), in) {
			t.Errorf("%s: %v", in, err)
		}
	}
}
