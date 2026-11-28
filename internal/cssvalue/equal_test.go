// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cssvalue

import (
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
		{"x", c("150ms"), c("0.15s"), false},
	} {
		if got := Equal(tc.name, tc.a, tc.b); got != tc.want {
			t.Errorf("%s: %q vs %q = %v", tc.name, tc.a.Value, tc.b.Value, got)
		}
	}
}
