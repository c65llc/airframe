// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cssvalue

import (
	"math"
	"strconv"
	"testing"

	"github.com/c65llc/airframe/internal/css"
)

func c(s string) css.Computed { return css.Computed{Value: s} }

func TestEqual(t *testing.T) {
	inv := css.Computed{Invalid: true}
	for _, tc := range []struct {
		name string
		a, b css.Computed
		want bool
	}{
		{"x", c("#ff0000"), c("oklch(62.7955% 0.2577 29.2339)"), true},
		{"x", c("#ff0000"), c("#fe0000"), false},
		{"x", c("12px"), c("0.75rem"), false},
		{"x", c("12px"), c("12px"), true},
		{"x", inv, inv, true},
		{"x", inv, c("1"), false},
		{"x", c("1"), inv, false},
		{"x", c("transparent"), c("rgb(0 0 0 / 0)"), true},
		{"x", c("oklch(50% 0 0)"), c("oklch(50% 0.00001 200)"), true},
		{"x", c("0 1px 2px #000"), c("0 1px 2px rgb(0 0 0)"), true},
		{"x", c("0 1px 2px #000"), c("0 1px 3px #000"), false},
		{"x-font", c(`"A", sans-serif`), c(`'A', sans-serif`), true},
		{"x", c("auto"), c("auto"), true},
		{"x", c("a   'b'"), c(`a "b"`), true},
		{"x", c("auto"), c("12px"), false},
		{"elev", c("0rem 0.25rem 0.5rem #000"), c("0 0.25rem 0.5rem oklch(0% 0 0)"), true},
		{"x", c("0px"), c("0rem"), true},
		{"x", c("1px"), c("1rem"), false},
		{"x", c("oklch(50% 0.00009 0)"), c("oklch(50% 0.00009 180)"), true},
		{"x", c("150ms"), c("0.15s"), false},
	} {
		if got := Equal(tc.name, tc.a, tc.b); got != tc.want {
			t.Errorf("%s: %q vs %q = %v", tc.name, tc.a.Value, tc.b.Value, got)
		}
	}
}

func TestEqualHalfStepRounding(t *testing.T) {
	for k := 0; k < 10000; k++ {
		src := float64(k)*1e-4 + 5e-5
		rounded := math.Round(src*1e4) / 1e4
		a := strconv.FormatFloat(src, 'f', -1, 64) + "rem"
		b := strconv.FormatFloat(rounded, 'f', -1, 64) + "rem"
		if !Equal("x", c(a), c(b)) {
			t.Fatalf("%s vs %s", a, b)
		}
	}
}
