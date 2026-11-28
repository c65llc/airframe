// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package cssvalue turns a CSS custom property's value into a typed token
// value, and compares two resolved CSS values semantically.
package cssvalue

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/tokens"
)

// Parse reads raw, the value of the custom property name, as a token value.
// typ is the DTCG $type ("" for an alias, whose type comes from its
// target). warn is non-empty when a var() fallback was dropped. An
// unsupported value is an error naming it.
func Parse(name, raw string) (typ string, v *tokens.Value, warn string, err error) {
	s := strings.TrimSpace(raw)
	name = strings.TrimPrefix(strings.TrimSpace(name), "--")
	unsupported := func() (string, *tokens.Value, string, error) {
		return "", nil, "", fmt.Errorf("unsupported value %q", raw)
	}
	if s == "" {
		return unsupported()
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "var(") {
		if end := matchParen(s, 3); end == len(s)-1 {
			args := s[4:end]
			target, fallback, has := strings.Cut(args, ",")
			target = strings.TrimSpace(target)
			if !strings.HasPrefix(target, "--") || len(target) == 2 || strings.ContainsAny(target, " \t\n()") {
				return unsupported()
			}
			if has {
				warn = fmt.Sprintf("var() fallback %q dropped from %q", strings.TrimSpace(fallback), raw)
			}
			return "", &tokens.Value{Alias: target[2:]}, warn, nil
		}
	}
	if strings.Contains(low, "var(") {
		return unsupported()
	}
	ok := func(t string, lit any) (string, *tokens.Value, string, error) {
		return t, &tokens.Value{Lit: lit}, "", nil
	}
	if c, good := parseColor(s); good {
		return ok("color", c)
	}
	if n, unit, good := splitNum(s); good {
		switch strings.ToLower(unit) {
		case "px", "rem":
			return ok("dimension", tokens.Dim{V: n, Unit: strings.ToLower(unit)})
		case "ms", "s":
			return ok("duration", tokens.Duration{V: n, Unit: strings.ToLower(unit)})
		case "":
			if strings.Contains(name, "weight") && n >= 100 && n <= 900 {
				return ok("fontWeight", n)
			}
			return ok("number", n)
		}
		return unsupported()
	}
	if strings.HasPrefix(low, "cubic-bezier(") && matchParen(s, 12) == len(s)-1 {
		parts := strings.Split(s[13:len(s)-1], ",")
		var b [4]float64
		if len(parts) == 4 {
			good := true
			for i, p := range parts {
				n, unit, g := splitNum(strings.TrimSpace(p))
				good = good && g && unit == ""
				b[i] = n
			}
			if good {
				return ok("cubicBezier", b)
			}
		}
		return unsupported()
	}
	if strings.Contains(name, "weight") {
		switch low {
		case "normal":
			return ok("fontWeight", 400.0)
		case "bold":
			return ok("fontWeight", 700.0)
		}
	}
	if strings.HasSuffix(name, "-font") || strings.HasSuffix(name, "-family") || strings.HasSuffix(name, "font-family") {
		if fam, good := parseFamilies(s); good {
			return ok("fontFamily", fam)
		}
		return unsupported()
	}
	if sh, good := parseShadow(s); good {
		return ok("shadow", sh)
	}
	return unsupported()
}

// matchParen is the index of the ) closing the ( at open, or -1.
func matchParen(s string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitNum reads a leading CSS number from s and returns it with the rest
// as the unit (which may be "%" or letters). ok is false when s has no
// number or the unit is not made of letters or %.
func splitNum(s string) (n float64, unit string, ok bool) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, "", false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && s[j] >= '0' && s[j] <= '9' {
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			i = j
		}
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, "", false
	}
	unit = s[i:]
	for k := 0; k < len(unit); k++ {
		c := unit[k] | 0x20
		if !(c >= 'a' && c <= 'z') && unit != "%" {
			return 0, "", false
		}
	}
	return f, unit, true
}

// splitTop splits s on sep outside parentheses and quotes.
func splitTop(s string, sep func(byte) bool) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && sep(c):
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// fields splits s on whitespace outside parentheses and quotes.
func fields(s string) []string {
	var out []string
	for _, f := range splitTop(s, isWS) {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func parseFamilies(s string) ([]string, bool) {
	var out []string
	for _, p := range splitTop(s, func(c byte) bool { return c == ',' }) {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
			return nil, false
		case p[0] == '"' || p[0] == '\'':
			if len(p) < 2 || p[len(p)-1] != p[0] {
				return nil, false
			}
			name := strings.NewReplacer(`\"`, `"`, `\'`, `'`, `\\`, `\`).Replace(p[1 : len(p)-1])
			if emit.GenericFamily(name) {
				// A quoted generic keyword names a font, not the generic.
				name = emit.QuotedGeneric(name)
			}
			out = append(out, name)
		default:
			for _, id := range strings.Fields(p) {
				if !isIdent(id) {
					return nil, false
				}
			}
			out = append(out, strings.Join(strings.Fields(p), " "))
		}
	}
	return out, len(out) > 0
}

func isIdent(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20
		if !(c >= 'a' && c <= 'z') && !(s[i] >= '0' && s[i] <= '9') && s[i] != '-' && s[i] != '_' {
			return false
		}
	}
	return s != ""
}

func parseShadow(s string) ([]tokens.Shadow, bool) {
	var out []tokens.Shadow
	for _, layer := range splitTop(s, func(c byte) bool { return c == ',' }) {
		var sh tokens.Shadow
		var lens []tokens.Dim
		haveColor := false
		for _, f := range fields(layer) {
			if strings.EqualFold(f, "inset") && !sh.Inset {
				sh.Inset = true
				continue
			}
			if !haveColor {
				if c, ok := parseColor(f); ok {
					sh.Color, haveColor = c, true
					continue
				}
			}
			n, unit, ok := splitNum(f)
			unit = strings.ToLower(unit)
			switch {
			case !ok:
				return nil, false
			case unit == "px" || unit == "rem":
				lens = append(lens, tokens.Dim{V: n, Unit: unit})
			case unit == "" && n == 0:
				lens = append(lens, tokens.Dim{Unit: "px"})
			default:
				return nil, false
			}
		}
		if !haveColor || len(lens) < 2 || len(lens) > 4 {
			return nil, false
		}
		for len(lens) < 4 {
			lens = append(lens, tokens.Dim{Unit: "px"})
		}
		sh.OffsetX, sh.OffsetY, sh.Blur, sh.Spread = lens[0], lens[1], lens[2], lens[3]
		out = append(out, sh)
	}
	return out, len(out) > 0
}

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, v)) }

func parseColor(s string) (color.OKLCH, bool) {
	low := strings.ToLower(strings.TrimSpace(s))
	if low == "transparent" {
		return color.OKLCH{}, true
	}
	if strings.HasPrefix(low, "#") {
		return parseHex(low[1:])
	}
	open := strings.IndexByte(low, '(')
	if open < 0 || !strings.HasSuffix(low, ")") || matchParen(low, open) != len(low)-1 {
		return color.OKLCH{}, false
	}
	fn := low[:open]
	args := strings.Map(func(r rune) rune {
		if r == ',' || r == '/' {
			return ' '
		}
		return r
	}, low[open+1:len(low)-1])
	a := strings.Fields(args)
	if len(a) != 3 && len(a) != 4 {
		return color.OKLCH{}, false
	}
	alpha := 1.0
	if len(a) == 4 {
		v, ok := numOrPct(a[3], 1)
		if !ok {
			return color.OKLCH{}, false
		}
		alpha = clamp01(v)
	}
	switch fn {
	case "rgb", "rgba":
		var ch [3]float64
		for i := range ch {
			v, ok := numOrPct(a[i], 255)
			if !ok {
				return color.OKLCH{}, false
			}
			ch[i] = clamp01(v / 255)
		}
		return color.FromSRGB(ch[0], ch[1], ch[2], alpha), true
	case "hsl", "hsla":
		h, ok := parseHue(a[0])
		sat, ok2 := pct(a[1])
		lig, ok3 := pct(a[2])
		if !ok || !ok2 || !ok3 {
			return color.OKLCH{}, false
		}
		r, g, b := hslToSRGB(h, clamp01(sat), clamp01(lig))
		return color.FromSRGB(r, g, b, alpha), true
	case "oklch":
		l, ok := numOrPct(a[0], 1)
		cc, ok2 := numOrPct(a[1], 0.4)
		h, ok3 := parseHue(a[2])
		if !ok || !ok2 || !ok3 {
			return color.OKLCH{}, false
		}
		return color.OKLCH{L: l, C: cc, H: h, Alpha: alpha}, true
	}
	return color.OKLCH{}, false
}

// numOrPct reads a number, or a percentage scaled so 100% is full.
func numOrPct(s string, full float64) (float64, bool) {
	n, unit, ok := splitNum(s)
	switch {
	case ok && unit == "":
		return n, true
	case ok && unit == "%":
		return n / 100 * full, true
	}
	return 0, false
}

func pct(s string) (float64, bool) {
	n, unit, ok := splitNum(s)
	return n / 100, ok && unit == "%"
}

// parseHue reads an angle (deg by default, turn, rad, grad) in degrees.
func parseHue(s string) (float64, bool) {
	n, unit, ok := splitNum(s)
	if !ok {
		return 0, false
	}
	switch unit {
	case "", "deg":
	case "turn":
		n *= 360
	case "rad":
		n = n * 180 / math.Pi
	case "grad":
		n = n * 0.9
	default:
		return 0, false
	}
	n = math.Mod(n, 360)
	if n < 0 {
		n += 360
	}
	return n, true
}

func hslToSRGB(h, s, l float64) (r, g, b float64) {
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))
	}
	return f(0), f(8), f(4)
}

func parseHex(h string) (color.OKLCH, bool) {
	switch len(h) {
	case 3, 4:
		var e strings.Builder
		for i := 0; i < len(h); i++ {
			e.WriteByte(h[i])
			e.WriteByte(h[i])
		}
		h = e.String()
	case 6, 8:
	default:
		return color.OKLCH{}, false
	}
	var ch [4]float64
	ch[3] = 255
	for i := 0; i < len(h)/2; i++ {
		v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			return color.OKLCH{}, false
		}
		ch[i] = float64(v)
	}
	return color.FromSRGB(ch[0]/255, ch[1]/255, ch[2]/255, ch[3]/255), true
}
