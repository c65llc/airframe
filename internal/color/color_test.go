// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package color

import (
	"math"
	"testing"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v ±%v", name, got, want, tol)
	}
}

func TestWhiteBlack(t *testing.T) {
	white := OKLCH{1, 0, 0, 1}
	black := OKLCH{0, 0, 0, 1}
	r, g, b := white.ToSRGB(CSSColor4)
	near(t, "r", r, 1, 1e-4)
	near(t, "g", g, 1, 1e-4)
	near(t, "b", b, 1, 1e-4)
	near(t, "contrast", Contrast(white, black), 21, 0.01)
}

func TestKnownRed(t *testing.T) {
	r, g, b := OKLCH{0.62796, 0.25768, 29.2339, 1}.ToSRGB(CSSColor4)
	near(t, "r", r, 1, 1e-3)
	near(t, "g", g, 0, 1e-3)
	near(t, "b", b, 0, 1e-3)
}

func hueOf(r, g, b float64, p3 bool) float64 {
	m := srgbToXYZ
	if p3 {
		m = p3ToXYZ
	}
	_, a, bb := rgbToOKLab(m, r, g, b)
	h := math.Atan2(bb, a) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return h
}

func TestOutOfGamutMapsInside(t *testing.T) {
	c := OKLCH{0.7, 0.4, 145, 1}
	for _, p3 := range []bool{false, true} {
		conv := c.ToSRGB
		if p3 {
			conv = c.ToP3
		}
		r, g, b := conv(CSSColor4)
		for _, v := range []float64{r, g, b} {
			if v < 0 || v > 1 {
				t.Errorf("p3=%v channel %v out of [0,1]", p3, v)
			}
		}
		// The CSS Color 4 algorithm returns a clipped colour within JND (0.02)
		// of the candidate, so hue can drift a few degrees (2.5° for sRGB here);
		// the brief's 1° bound is not achievable with the mandated algorithm.
		if d := HueDistance(hueOf(r, g, b, p3), 145); d > 3 {
			t.Errorf("p3=%v hue drifted %v°", p3, d)
		}
	}
	r, g, b := c.ToSRGB(Clip)
	for _, v := range []float64{r, g, b} {
		if v < 0 || v > 1 {
			t.Errorf("clip channel %v out of [0,1]", v)
		}
	}
	if r != 0 {
		t.Errorf("clip red = %v, want clamped 0", r)
	}
}

func TestHueDistanceWraps(t *testing.T) {
	near(t, "355/5", HueDistance(355, 5), 10, 1e-12)
	near(t, "5/355", HueDistance(5, 355), 10, 1e-12)
	near(t, "0/180", HueDistance(0, 180), 180, 1e-12)
}

func TestAchromatic(t *testing.T) {
	if !Achromatic(OKLCH{0.5, 0.019, 10, 1}) || Achromatic(OKLCH{0.5, 0.02, 10, 1}) {
		t.Error("Achromatic threshold must be C < 0.02")
	}
}

func TestContrastAlphaAndSymmetry(t *testing.T) {
	white := OKLCH{1, 0, 0, 1}
	black := OKLCH{0, 0, 0, 1}
	near(t, "symmetric", Contrast(black, white), 21, 0.01)
	// Fully transparent fg shows bg: ratio 1.
	near(t, "transparent", Contrast(OKLCH{0, 0, 0, 0}, white), 1, 1e-9)
	// 50% black over white is mid-grey in gamma space: 0.5 -> lum ~0.214.
	near(t, "half", Contrast(OKLCH{0, 0, 0, 0.5}, white), 1.05/(0.2140411+0.05), 1e-3)
}

// Every XYZ<->RGB pair must be inverse, and D65 white must map to (1,1,1).
func TestMatricesConsistent(t *testing.T) {
	for name, p := range map[string][2]mat3{"srgb": {srgbToXYZ, xyzToSRGB}, "p3": {p3ToXYZ, xyzToP3}} {
		x, y, z := p[0].mul(1, 1, 1)
		near(t, name+" Y", y, 1, 1e-9)
		r, g, b := p[1].mul(x, y, z)
		near(t, name+" r", r, 1, 1e-9)
		near(t, name+" g", g, 1, 1e-9)
		near(t, name+" b", b, 1, 1e-9)
	}
	// Grey stays grey at mid lightness.
	r, g, b := OKLCH{0.7, 0, 0, 1}.ToSRGB(CSSColor4)
	near(t, "grey g", g, r, 1e-6)
	near(t, "grey b", b, r, 1e-6)
}

func TestFromSRGBRed(t *testing.T) {
	got := FromSRGB(1, 0, 0, 1)
	near(t, "L", got.L, 0.62796, 1e-4)
	near(t, "C", got.C, 0.25768, 1e-4)
	near(t, "H", got.H, 29.2339, 1e-4)
	near(t, "Alpha", got.Alpha, 1, 1e-4)
}

func TestFromSRGBWhiteAchromatic(t *testing.T) {
	got := FromSRGB(1, 1, 1, 1)
	near(t, "L", got.L, 1, 1e-4)
	if got.C >= 1e-6 {
		t.Errorf("C = %v, want < 1e-6", got.C)
	}
	if got.H != 0 {
		t.Errorf("H = %v, want 0", got.H)
	}
}

func TestFromSRGBRoundTrip(t *testing.T) {
	cases := []OKLCH{
		{0.3, 0.05, 10, 1}, {0.4, 0.08, 45, 1}, {0.5, 0.1, 80, 1},
		{0.55, 0.12, 120, 1}, {0.6, 0.1, 150, 1}, {0.65, 0.09, 180, 1},
		{0.7, 0.1, 200, 1}, {0.45, 0.07, 230, 1}, {0.5, 0.15, 260, 1},
		{0.4, 0.15, 290, 1}, {0.55, 0.15, 320, 1}, {0.6, 0.12, 350, 1},
		{0.2, 0.02, 0, 1}, {0.8, 0.05, 90, 1}, {0.9, 0.03, 200, 1},
		{0.5, 0, 0, 1}, {0.75, 0.08, 359, 1}, {0.35, 0.06, 135, 1},
		{0.85, 0.1, 100, 1}, {0.6, 0.2, 30, 0.5},
	}
	for _, c := range cases {
		r, g, b := c.ToSRGB(Clip)
		got := FromSRGB(r, g, b, c.Alpha)
		l1, a1, b1 := polar(c)
		l2, a2, b2 := polar(got)
		if d := deltaEOK(l1, a1, b1, l2, a2, b2); d > 1e-6 {
			t.Errorf("%+v round-trips to %+v (dE %v)", c, got, d)
		}
		near(t, "alpha", got.Alpha, c.Alpha, 1e-12)
	}
}
