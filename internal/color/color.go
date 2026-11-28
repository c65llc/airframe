// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package color converts OKLCH to sRGB / Display-P3 with a stated gamut
// mapping and computes WCAG 2.x contrast. Results are unrounded float64.
//
// Matrices and transfer functions are the CSS Color Module Level 4 sample
// code, https://www.w3.org/TR/css-color-4/#color-conversion-code
// ("Sample code for color conversions").
package color

import "math"

// OKLCH is an OKLab colour in polar form. L is 0-1, H is degrees [0,360).
type OKLCH struct{ L, C, H, Alpha float64 }

// Gamut selects how out-of-gamut colours are brought into [0,1].
type Gamut int

const (
	// CSSColor4 is the CSS Color 4 section 13.2 "Binary Search Gamut
	// Mapping with Local MINDE" (https://www.w3.org/TR/css-color-4/#binsearch):
	// chroma is reduced in OKLCH, keeping hue and lightness, until the
	// clipped colour is within JND of the candidate.
	CSSColor4 Gamut = iota
	// Clip clamps each channel independently.
	Clip
)

const (
	jnd     = 0.02
	epsilon = 0.0001
)

type mat3 [3][3]float64

func (m mat3) mul(x, y, z float64) (float64, float64, float64) {
	return m[0][0]*x + m[0][1]*y + m[0][2]*z,
		m[1][0]*x + m[1][1]*y + m[1][2]*z,
		m[2][0]*x + m[2][1]*y + m[2][2]*z
}

var (
	xyzToLMS = mat3{
		{0.8190224379967030, 0.3619062600528904, -0.1288737815209879},
		{0.0329836539323885, 0.9292868615863434, 0.0361446663506424},
		{0.0481771893596242, 0.2642395317527308, 0.6335478284694309},
	}
	lmsToXYZ = mat3{
		{1.2268798758459243, -0.5578149944602171, 0.2813910456659647},
		{-0.0405757452148008, 1.1122868032803170, -0.0717110580655164},
		{-0.0763729366746601, -0.4214933324022432, 1.5869240198367816},
	}
	lmsToOKLab = mat3{
		{0.2104542683093140, 0.7936177747023054, -0.0040720430116193},
		{1.9779985324311684, -2.4285922420485799, 0.4505937096174110},
		{0.0259040424655478, 0.7827717124575296, -0.8086757549230774},
	}
	oklabToLMS = mat3{
		{1, 0.3963377773761749, 0.2158037573099136},
		{1, -0.1055613458156586, -0.0638541728258133},
		{1, -0.0894841775298119, -1.2914855480194092},
	}
	xyzToSRGB = mat3{
		{12831.0 / 3959, -329.0 / 214, -1974.0 / 3959},
		{-851781.0 / 878810, 1648619.0 / 878810, 36519.0 / 878810},
		{705.0 / 12673, -2585.0 / 12673, 705.0 / 667},
	}
	srgbToXYZ = mat3{
		{506752.0 / 1228815, 87881.0 / 245763, 12673.0 / 70218},
		{87098.0 / 409605, 175762.0 / 245763, 12673.0 / 175545},
		{7918.0 / 409605, 87881.0 / 737289, 1001167.0 / 1053270},
	}
	xyzToP3 = mat3{
		{446124.0 / 178915, -333277.0 / 357830, -72051.0 / 178915},
		{-14852.0 / 17905, 63121.0 / 35810, 423.0 / 17905},
		{11844.0 / 330415, -50337.0 / 660830, 316169.0 / 330415},
	}
	p3ToXYZ = mat3{
		{608311.0 / 1250200, 189793.0 / 714400, 198249.0 / 1000160},
		{35783.0 / 156275, 247089.0 / 357200, 198249.0 / 2500400},
		{0, 32229.0 / 714400, 5220557.0 / 5000800},
	}
)

// sRGB and Display-P3 share one transfer function.
func encode(v float64) float64 {
	a := math.Abs(v)
	if a > 0.0031308 {
		return math.Copysign(1.055*math.Pow(a, 1/2.4)-0.055, v)
	}
	return 12.92 * v
}

func decode(v float64) float64 {
	a := math.Abs(v)
	if a <= 0.04045 {
		return v / 12.92
	}
	return math.Copysign(math.Pow((a+0.055)/1.055, 2.4), v)
}

// oklabToLinear converts OKLab to linear RGB using the given XYZ->RGB matrix.
func oklabToLinear(xyzToRGB mat3, l, a, b float64) (float64, float64, float64) {
	lp, mp, sp := oklabToLMS.mul(l, a, b)
	x, y, z := lmsToXYZ.mul(lp*lp*lp, mp*mp*mp, sp*sp*sp)
	return xyzToRGB.mul(x, y, z)
}

// rgbToOKLab converts gamma-encoded RGB to OKLab using the given RGB->XYZ matrix.
func rgbToOKLab(rgbToXYZ mat3, r, g, b float64) (float64, float64, float64) {
	x, y, z := rgbToXYZ.mul(decode(r), decode(g), decode(b))
	l, m, s := xyzToLMS.mul(x, y, z)
	return lmsToOKLab.mul(math.Cbrt(l), math.Cbrt(m), math.Cbrt(s))
}

func polar(c OKLCH) (l, a, b float64) {
	h := c.H * math.Pi / 180
	return c.L, c.C * math.Cos(h), c.C * math.Sin(h)
}

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, v)) }

// toGamma returns the gamma-encoded, unclamped channels of c.
func toGamma(xyzToRGB mat3, c OKLCH) (float64, float64, float64) {
	l, a, b := polar(c)
	r, g, bl := oklabToLinear(xyzToRGB, l, a, b)
	return encode(r), encode(g), encode(bl)
}

func inGamut(xyzToRGB mat3, c OKLCH) bool {
	const e = 1e-9
	r, g, b := toGamma(xyzToRGB, c)
	in := func(v float64) bool { return v >= -e && v <= 1+e }
	return in(r) && in(g) && in(b)
}

func clipped(xyzToRGB mat3, c OKLCH) (float64, float64, float64) {
	r, g, b := toGamma(xyzToRGB, c)
	return clamp01(r), clamp01(g), clamp01(b)
}

func deltaEOK(l1, a1, b1, l2, a2, b2 float64) float64 {
	return math.Sqrt((l1-l2)*(l1-l2) + (a1-a2)*(a1-a2) + (b1-b2)*(b1-b2))
}

// convert implements the section 13.2 algorithm for one target space.
func convert(c OKLCH, xyzToRGB, rgbToXYZ mat3, g Gamut) (float64, float64, float64) {
	if g == Clip {
		return clipped(xyzToRGB, c)
	}
	if c.L >= 1 {
		return 1, 1, 1
	}
	if c.L <= 0 {
		return 0, 0, 0
	}
	if inGamut(xyzToRGB, c) {
		return clipped(xyzToRGB, c)
	}
	// dE between the clipped colour and the (possibly out-of-gamut) candidate.
	dE := func(cand OKLCH) (r, gg, b, e float64) {
		r, gg, b = clipped(xyzToRGB, cand)
		l1, a1, b1 := rgbToOKLab(rgbToXYZ, r, gg, b)
		l2, a2, b2 := polar(cand)
		return r, gg, b, deltaEOK(l1, a1, b1, l2, a2, b2)
	}
	r, gg, b, e := dE(c)
	if e < jnd {
		return r, gg, b
	}
	lo, hi := 0.0, c.C
	minInGamut := true
	cur := c
	for hi-lo > epsilon {
		cur.C = (lo + hi) / 2
		if minInGamut && inGamut(xyzToRGB, cur) {
			lo = cur.C
			continue
		}
		r, gg, b, e = dE(cur)
		if e < jnd {
			if jnd-e < epsilon {
				return r, gg, b
			}
			minInGamut = false
			lo = cur.C
		} else {
			hi = cur.C
		}
	}
	// The spec returns its last computed clip, which is stale when the final
	// iterations stayed in gamut. Clip at the converged chroma instead.
	cur.C = lo
	return clipped(xyzToRGB, cur)
}

// ToSRGB returns gamma-encoded sRGB channels in [0,1].
func (c OKLCH) ToSRGB(gamut Gamut) (r, g, b float64) {
	return convert(c, xyzToSRGB, srgbToXYZ, gamut)
}

// ToP3 returns gamma-encoded Display-P3 channels in [0,1].
func (c OKLCH) ToP3(gamut Gamut) (r, g, b float64) {
	return convert(c, xyzToP3, p3ToXYZ, gamut)
}

// FromSRGB converts gamma-encoded sRGB channels (0-1) to OKLCH. Hue is
// normalised to [0,360) and is 0 when chroma is below 1e-9.
func FromSRGB(r, g, b, alpha float64) OKLCH {
	l, a, bb := rgbToOKLab(srgbToXYZ, r, g, b)
	c := math.Hypot(a, bb)
	h := 0.0
	if c >= 1e-9 {
		h = math.Atan2(bb, a) * 180 / math.Pi
		if h < 0 {
			h += 360
		}
		if h >= 360 {
			h -= 360
		}
	}
	return OKLCH{L: l, C: c, H: h, Alpha: alpha}
}

func luminance(r, g, b float64) float64 {
	return 0.2126*decode(r) + 0.7152*decode(g) + 0.0722*decode(b)
}

// Contrast is the WCAG 2.x contrast ratio (>= 1) of fg over bg. Both are
// mapped into sRGB with CSSColor4; fg is composited over bg by fg.Alpha in
// gamma-encoded sRGB.
func Contrast(fg, bg OKLCH) float64 {
	fr, fgn, fb := fg.ToSRGB(CSSColor4)
	br, bgn, bb := bg.ToSRGB(CSSColor4)
	a := fg.Alpha
	lf := luminance(a*fr+(1-a)*br, a*fgn+(1-a)*bgn, a*fb+(1-a)*bb)
	lb := luminance(br, bgn, bb)
	if lf < lb {
		lf, lb = lb, lf
	}
	return (lf + 0.05) / (lb + 0.05)
}

// HueDistance is the circular distance between two hues in degrees, 0-180.
func HueDistance(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	if d > 180 {
		d = 360 - d
	}
	return d
}

// Achromatic reports whether c is too low-chroma for its hue to matter.
func Achromatic(c OKLCH) bool { return c.C < 0.02 }
