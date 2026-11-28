// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cssvalue

import (
	"math"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/tokens"
)

const (
	colourEps = 1e-4
	numberEps = 5e-5 + 1e-9
)

// Equal reports whether two resolved values of the custom property name
// mean the same thing. Two invalid values are equal, one is not. Values
// that both parse compare by type: colours in OKLab, numbers, dimensions
// and durations by unit and value, lists element by element. Values that
// both fail to parse compare as whitespace-collapsed text with single
// quotes read as double; one parsing and one not is a mismatch.
func Equal(name string, a, b css.Computed) bool {
	if a.Invalid || b.Invalid {
		return a.Invalid && b.Invalid
	}
	ta, va, _, ea := Parse(name, a.Value)
	tb, vb, _, eb := Parse(name, b.Value)
	pa, pb := ea == nil && va.Alias == "", eb == nil && vb.Alias == ""
	switch {
	case pa && pb:
		return ta == tb && litEqual(va.Lit, vb.Lit)
	case pa || pb:
		return false
	}
	return normalise(a.Value) == normalise(b.Value)
}

func normalise(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "'", `"`)
}

func near(a, b float64) bool { return math.Abs(a-b) <= numberEps }

func litEqual(a, b any) bool {
	switch x := a.(type) {
	case color.OKLCH:
		y, ok := b.(color.OKLCH)
		return ok && colourEqual(x, y)
	case tokens.Dim:
		y, ok := b.(tokens.Dim)
		return ok && dimEqual(x, y)
	case tokens.Duration:
		y, ok := b.(tokens.Duration)
		return ok && x.Unit == y.Unit && near(x.V, y.V)
	case float64:
		y, ok := b.(float64)
		return ok && near(x, y)
	case []string:
		y, ok := b.([]string)
		return ok && slices.Equal(x, y)
	case [4]float64:
		y, ok := b.([4]float64)
		if !ok {
			return false
		}
		for i := range x {
			if !near(x[i], y[i]) {
				return false
			}
		}
		return true
	case []tokens.Shadow:
		y, ok := b.([]tokens.Shadow)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			p, q := x[i], y[i]
			if p.Inset != q.Inset || !colourEqual(p.Color, q.Color) || !dimEqual(p.OffsetX, q.OffsetX) ||
				!dimEqual(p.OffsetY, q.OffsetY) || !dimEqual(p.Blur, q.Blur) || !dimEqual(p.Spread, q.Spread) {
				return false
			}
		}
		return true
	}
	return false
}

// dimEqual compares dimensions by unit and value, except that any two
// zeros are equal: CSS writes a zero length without its unit.
func dimEqual(a, b tokens.Dim) bool {
	if near(a.V, 0) && near(b.V, 0) {
		return true
	}
	return a.Unit == b.Unit && near(a.V, b.V)
}

// colourEqual compares in OKLab; when both chromas are below the
// tolerance the hue is meaningless and only L, C and alpha are compared.
func colourEqual(a, b color.OKLCH) bool {
	lab := func(c color.OKLCH) (l, x, y float64) {
		h := c.H * math.Pi / 180
		return c.L, c.C * math.Cos(h), c.C * math.Sin(h)
	}
	if math.Abs(a.Alpha-b.Alpha) > colourEps {
		return false
	}
	if a.C < colourEps && b.C < colourEps {
		return math.Abs(a.L-b.L) <= colourEps && math.Abs(a.C-b.C) <= colourEps
	}
	l1, a1, b1 := lab(a)
	l2, a2, b2 := lab(b)
	return math.Sqrt((l1-l2)*(l1-l2)+(a1-a2)*(a1-a2)+(b1-b2)*(b1-b2)) <= colourEps
}
