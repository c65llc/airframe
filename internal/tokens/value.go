// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokens

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/ojson"
)

// fontWeights maps the DTCG font-weight keywords to their numeric weights.
var fontWeights = map[string]float64{
	"thin": 100, "hairline": 100,
	"extra-light": 200, "ultra-light": 200,
	"light":  300,
	"normal": 400, "regular": 400, "book": 400,
	"medium":    500,
	"semi-bold": 600, "demi-bold": 600,
	"bold":       700,
	"extra-bold": 800, "ultra-bold": 800,
	"black": 900, "heavy": 900,
	"extra-black": 950, "ultra-black": 950,
}

// unitOf is the unit of a dimension or duration literal, or "".
func unitOf(v *Value) string {
	if v == nil {
		return ""
	}
	switch l := v.Lit.(type) {
	case Dim:
		return l.Unit
	case Duration:
		return l.Unit
	}
	return ""
}

// shapeErr is a value that does not have the shape its $type needs.
type shapeErr struct {
	path, msg string
	line      int
}

func bad(n *ojson.Node, path, format string, a ...any) *shapeErr {
	return &shapeErr{path: path, line: n.Line, msg: fmt.Sprintf(format, a...)}
}

// parseValue reads n as a value of type typ: an alias "{group.token}" or a
// literal. unit, when set, lets a bare number stand for a dimension or
// duration in that unit (the density shorthand).
func parseValue(n *ojson.Node, typ, unit, path string) (*Value, *shapeErr) {
	if n.Kind == ojson.String {
		if m := aliasRE.FindStringSubmatch(n.Str); m != nil {
			segs := strings.Split(m[1], ".")
			if slices.Contains(segs, "") {
				return nil, bad(n, path, "alias %q has an empty path segment", n.Str)
			}
			return &Value{Alias: strings.Join(segs, "-"), Line: n.Line}, nil
		}
	}
	var lit any
	var err *shapeErr
	switch typ {
	case "color":
		lit, err = parseColor(n, path)
	case "dimension":
		if n.Kind == ojson.Number && unit != "" {
			lit = Dim{n.Num, unit}
			break
		}
		var d Duration
		d, err = parseUnit(n, path, "dimension", "px", "rem")
		lit = Dim(d)
	case "duration":
		if n.Kind == ojson.Number && unit != "" {
			lit = Duration{n.Num, unit}
			break
		}
		lit, err = parseUnit(n, path, "duration", "ms", "s")
	case "number":
		if n.Kind != ojson.Number {
			return nil, bad(n, path, "a number token needs a number")
		}
		lit = n.Num
	case "fontFamily":
		lit, err = parseFamily(n, path)
	case "fontWeight":
		lit, err = parseWeight(n, path)
	case "cubicBezier":
		lit, err = parseBezier(n, path)
	case "shadow":
		lit, err = parseShadow(n, path)
	}
	if err != nil {
		return nil, err
	}
	return &Value{Lit: lit, Line: n.Line}, nil
}

func number(n *ojson.Node) (float64, bool) {
	if n == nil || n.Kind != ojson.Number || math.IsNaN(n.Num) || math.IsInf(n.Num, 0) {
		return 0, false
	}
	return n.Num, true
}

// keys reports the first key of object n that is not in allowed.
func keys(n *ojson.Node, path string, allowed ...string) *shapeErr {
	for _, k := range n.Keys {
		if !slices.Contains(allowed, k) {
			return bad(n.Fields[k], path+"."+k, "unknown key %q", k)
		}
	}
	return nil
}

func parseColor(n *ojson.Node, path string) (color.OKLCH, *shapeErr) {
	if n.Kind != ojson.Object {
		if n.Kind == ojson.String {
			return color.OKLCH{}, bad(n, path, `a color must be an alias "{group.token}" or an oklch color object`)
		}
		return color.OKLCH{}, bad(n, path, "a color must be an oklch color object")
	}
	if e := keys(n, path, "colorSpace", "components", "alpha", "hex"); e != nil {
		return color.OKLCH{}, e
	}
	if cs, ok := n.Fields["colorSpace"]; !ok {
		return color.OKLCH{}, bad(n, path, `colorSpace must be "oklch"`)
	} else if cs.Kind != ojson.String || cs.Str != "oklch" {
		return color.OKLCH{}, bad(cs, path+".colorSpace", `colorSpace must be "oklch"`)
	}
	comps, ok := n.Fields["components"]
	if !ok || comps.Kind != ojson.Array || len(comps.Items) != 3 {
		at := n
		if ok {
			at = comps
		}
		return color.OKLCH{}, bad(at, path+".components", "components must be [L, C, H]")
	}
	var lch [3]float64
	for i, it := range comps.Items {
		if lch[i], ok = number(it); !ok {
			return color.OKLCH{}, bad(it, path+".components", "components must be [L, C, H] numbers")
		}
	}
	c := color.OKLCH{L: lch[0], C: lch[1], H: lch[2], Alpha: 1}
	switch {
	case c.L < 0 || c.L > 1:
		return c, bad(comps.Items[0], path+".components", "lightness must be between 0 and 1")
	case c.C < 0:
		return c, bad(comps.Items[1], path+".components", "chroma must not be negative")
	case c.H < 0 || c.H > 360:
		return c, bad(comps.Items[2], path+".components", "hue must be between 0 and 360")
	}
	if a, has := n.Fields["alpha"]; has {
		v, ok := number(a)
		if !ok || v < 0 || v > 1 {
			return c, bad(a, path+".alpha", "alpha must be a number between 0 and 1")
		}
		c.Alpha = v
	}
	if h, has := n.Fields["hex"]; has && h.Kind != ojson.String {
		return c, bad(h, path+".hex", "hex must be a string")
	}
	return c, nil
}

// parseUnit reads a {value, unit} object; the result is converted to Dim by
// the dimension caller.
func parseUnit(n *ojson.Node, path, what string, units ...string) (Duration, *shapeErr) {
	if n.Kind != ojson.Object {
		return Duration{}, bad(n, path, "a %s must be {value, unit}", what)
	}
	if e := keys(n, path, "value", "unit"); e != nil {
		return Duration{}, e
	}
	v, ok := number(n.Fields["value"])
	if !ok {
		return Duration{}, bad(n, path+".value", "a %s needs a numeric value", what)
	}
	u := n.Fields["unit"]
	if u == nil || u.Kind != ojson.String || !slices.Contains(units, u.Str) {
		at := n
		if u != nil {
			at = u
		}
		return Duration{}, bad(at, path+".unit", "%s unit must be %s", what, strings.Join(quoteAll(units), " or "))
	}
	return Duration{v, u.Str}, nil
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

func parseFamily(n *ojson.Node, path string) ([]string, *shapeErr) {
	switch n.Kind {
	case ojson.String:
		if n.Str == "" {
			return nil, bad(n, path, "fontFamily must not be empty")
		}
		return []string{n.Str}, nil
	case ojson.Array:
		if len(n.Items) == 0 {
			return nil, bad(n, path, "fontFamily must not be empty")
		}
		out := make([]string, 0, len(n.Items))
		for _, it := range n.Items {
			if it.Kind != ojson.String || it.Str == "" {
				return nil, bad(it, path, "fontFamily must be a name or a list of names")
			}
			out = append(out, it.Str)
		}
		return out, nil
	}
	return nil, bad(n, path, "fontFamily must be a name or a list of names")
}

func parseWeight(n *ojson.Node, path string) (float64, *shapeErr) {
	if n.Kind == ojson.String {
		if w, ok := fontWeights[n.Str]; ok {
			return w, nil
		}
		return 0, bad(n, path, "unknown fontWeight keyword %q", n.Str)
	}
	w, ok := number(n)
	if !ok || w < 1 || w > 1000 {
		return 0, bad(n, path, "fontWeight must be a number from 1 to 1000 or a weight keyword")
	}
	return w, nil
}

func parseBezier(n *ojson.Node, path string) ([4]float64, *shapeErr) {
	var out [4]float64
	if n.Kind != ojson.Array || len(n.Items) != 4 {
		return out, bad(n, path, "cubicBezier must be [x1, y1, x2, y2]")
	}
	for i, it := range n.Items {
		v, ok := number(it)
		if !ok {
			return out, bad(it, path, "cubicBezier must be [x1, y1, x2, y2] numbers")
		}
		if (i == 0 || i == 2) && (v < 0 || v > 1) {
			return out, bad(it, path, "cubicBezier x values must be between 0 and 1")
		}
		out[i] = v
	}
	return out, nil
}

func parseShadow(n *ojson.Node, path string) ([]Shadow, *shapeErr) {
	layers := []*ojson.Node{n}
	if n.Kind == ojson.Array {
		if len(n.Items) == 0 {
			return nil, bad(n, path, "shadow must have at least one layer")
		}
		layers = n.Items
	}
	var out []Shadow
	for _, l := range layers {
		if l.Kind != ojson.Object {
			return nil, bad(l, path, "a shadow layer must be an object")
		}
		if e := keys(l, path, "color", "offsetX", "offsetY", "blur", "spread", "inset"); e != nil {
			return nil, e
		}
		var s Shadow
		for _, k := range []string{"color", "offsetX", "offsetY", "blur", "spread"} {
			f, ok := l.Fields[k]
			if !ok {
				return nil, bad(l, path, "a shadow layer needs %s", k)
			}
			if f.Kind == ojson.String && aliasRE.MatchString(f.Str) {
				return nil, bad(f, path+"."+k, "aliases inside a shadow are not supported; alias the whole shadow")
			}
			var err *shapeErr
			switch k {
			case "color":
				s.Color, err = parseColor(f, path+"."+k)
			default:
				var d Duration
				d, err = parseUnit(f, path+"."+k, "dimension", "px", "rem")
				dim := Dim(d)
				switch k {
				case "offsetX":
					s.OffsetX = dim
				case "offsetY":
					s.OffsetY = dim
				case "blur":
					s.Blur = dim
				case "spread":
					s.Spread = dim
				}
			}
			if err != nil {
				return nil, err
			}
		}
		if in, ok := l.Fields["inset"]; ok {
			if in.Kind != ojson.Bool {
				return nil, bad(in, path+".inset", "inset must be true or false")
			}
			s.Inset = in.Bool
		}
		out = append(out, s)
	}
	return out, nil
}
