// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package specimen

import (
	"bytes"
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// edgeToks are the shapes that overflowed fixed columns: a translucent
// colour whose OKLCH label is long, a name past the name column's cap, a
// long font list and a font size at the cap and past it.
var edgeToks = `{
  "tint": {"$type": "color", "$value": {"colorSpace": "oklch", "components": [0.62704, 0.25768, 29.23389], "alpha": 0.5},
    "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "` + strings.Repeat("very-long-name-", 6) + `color": {"$type": "color", "$value": {"colorSpace": "oklch", "components": [0.5, 0.1, 200]},
    "$description": "a long one",
    "$extensions": {"com.c65llc.airframe": {"tier": "semantic"}}},
  "font-stack": {"$type": "fontFamily", "$value": ["Inter Display", "Helvetica Neue", "Segoe UI Variable", "Noto Sans"],
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "font-size-display": {"$type": "dimension", "$value": {"value": 72, "unit": "px"},
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "font-size-huge": {"$type": "dimension", "$value": {"value": 9, "unit": "rem"},
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}},
  "` + strings.Repeat("spacing-name-", 6) + `x": {"$type": "dimension", "$value": {"value": 12, "unit": "px"},
    "$extensions": {"com.c65llc.airframe": {"tier": "primitive"}}}
}`

type svgText struct {
	x, y, size float64
	mono       bool
	s, title   string
}

type svgBox struct{ x, y float64 }

// parseSVG reads the sheet's width, its texts and its rects.
func parseSVG(t *testing.T, b []byte) (width float64, texts []svgText, boxes []svgBox) {
	t.Helper()
	f := func(se xml.StartElement, k string) float64 {
		for _, a := range se.Attr {
			if a.Name.Local == k {
				v, err := strconv.ParseFloat(a.Value, 64)
				require.NoError(t, err)
				return v
			}
		}
		return 0
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	var cur *svgText
	inTitle := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		switch e := tok.(type) {
		case xml.StartElement:
			switch e.Name.Local {
			case "svg":
				width = f(e, "width")
			case "rect":
				if f(e, "x") > 0 {
					boxes = append(boxes, svgBox{f(e, "x"), f(e, "y")})
				}
			case "text":
				cur = &svgText{x: f(e, "x"), y: f(e, "y"), size: f(e, "font-size")}
				for _, a := range e.Attr {
					if a.Name.Local == "font-family" {
						cur.mono = strings.Contains(a.Value, "monospace")
					}
				}
			case "title":
				inTitle = cur != nil
			}
		case xml.CharData:
			if cur != nil && inTitle {
				cur.title += string(e)
			} else if cur != nil {
				cur.s += string(e)
			}
		case xml.EndElement:
			switch e.Name.Local {
			case "title":
				inTitle = false
			case "text":
				texts = append(texts, *cur)
				cur = nil
			}
		}
	}
	return width, texts, boxes
}

// end estimates where a text ends: monospace at 0.6em a character, the
// sans sample at 0.55em, as the layout does.
func (s svgText) end() float64 {
	em := 0.55
	if s.mono {
		em = 0.6
	}
	return s.x + em*s.size*float64(utf8.RuneCountInString(s.s))
}

// assertFits checks that no text runs into the element to its right on
// its row (any text or swatch or bar within 30px of its baseline), or off
// the sheet.
func assertFits(t *testing.T, name string, b []byte) {
	t.Helper()
	width, texts, boxes := parseSVG(t, b)
	for _, s := range texts {
		limit := width
		for _, o := range texts {
			if o.x > s.x && math.Abs(o.y-s.y) <= 30 {
				limit = math.Min(limit, o.x)
			}
		}
		for _, o := range boxes {
			if o.x > s.x && math.Abs(o.y-s.y) <= 30 {
				limit = math.Min(limit, o.x)
			}
		}
		assert.LessOrEqual(t, s.end(), limit-4, "%s: %q at x=%v y=%v runs into x=%v", name, s.s, s.x, s.y, limit)
	}
}

func TestSpecimenColumnsFit(t *testing.T) {
	for _, cfg := range []string{shapesCfg, noAppearanceCfg} {
		for _, toks := range []string{shapesToks, edgeToks} {
			sheets, err := Render(memProject(t, cfg, toks))
			require.NoError(t, err)
			for name, b := range sheets {
				assertFits(t, name, b)
			}
		}
	}
	sheets, err := Render(loadApple(t))
	require.NoError(t, err)
	for name, b := range sheets {
		assertFits(t, name, b)
	}
}

func TestSpecimenLongNameTruncated(t *testing.T) {
	sheets, err := Render(memProject(t, shapesCfg, edgeToks))
	require.NoError(t, err)
	long := strings.Repeat("very-long-name-", 6) + "color"
	_, texts, _ := parseSVG(t, sheets["colors.svg"])
	found := false
	for _, s := range texts {
		if strings.HasPrefix(long, strings.TrimSuffix(s.s, "…")) && strings.HasSuffix(s.s, "…") {
			found = true
			assert.Contains(t, s.title, long, "the full name is in the tooltip")
			assert.Contains(t, s.title, "a long one", "with the description")
			assert.LessOrEqual(t, s.end(), float64(margin+nameCap))
		}
	}
	assert.True(t, found, "the long name is truncated with an ellipsis")
}

// The type sample column fits the largest sample set, which is capped at
// typeMaxPx: a 9rem font size is drawn at 72px.
func TestSpecimenTypeSampleFits(t *testing.T) {
	sheets, err := Render(memProject(t, shapesCfg, edgeToks))
	require.NoError(t, err)
	width, texts, _ := parseSVG(t, sheets["type.svg"])
	for _, s := range texts {
		if s.s == sample {
			assert.LessOrEqual(t, s.size, float64(typeMaxPx))
			assert.LessOrEqual(t, s.end(), width-margin, "size %v", s.size)
		}
	}
}
