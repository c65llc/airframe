// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/testfix"
)

// invConfig has an accent axis whose values are loaded (app) and one that
// is deferred (swatch), so accentGroup expands to app alone.
const invConfig = `version: 1
modes:
  appearance: { values: [light, dark] }
  density:    { values: [compact, standard, relaxed] }
  app:        { values: [slides, tasks], group: accent }
  swatch:     { values: from brands/swatches.tokens.json, group: accent }
`

// lch is an oklch colour literal.
func lch(l, c, h float64) string {
	return fmt.Sprintf(`{ "colorSpace": "oklch", "components": [%g, %g, %g] }`, l, c, h)
}

// loadInv loads tokens and invariants.yaml under invConfig.
func loadInv(t *testing.T, tokens, invs string) *project.Project {
	t.Helper()
	return loadWith(t, invConfig, map[string]string{"a.tokens.json": tokens, "invariants.yaml": invs})
}

func TestContrastBelow(t *testing.T) {
	// A measured accent pair: accent-ink
	// on accent-soft is 4.99:1, so it passes 4.5 and fails 7.
	tokens := `{
  "accent-ink": { "$type": "color", "$value": ` + lch(0.52, 0.12, 75) + `,
    ` + ext(`"modes": { "app:slides": `+lch(0.25, 0.05, 75)+` }`) + ` },
  "accent-soft": { "$type": "color", "$value": ` + lch(0.96, 0.03, 75) + `,
    ` + ext(`"modes": { "appearance:dark": `+lch(0.96, 0.03, 75)+` }`) + ` }
}`
	inv := func(min string) string {
		return `- id: selection-text-contrast
  kind: contrast
  fg: accent-ink
  bg: accent-soft
  min: ` + min + `
  forEach: [accentGroup, appearance]
`
	}
	ds, tally := ruleFindings(t, loadInv(t, tokens, inv("7")), "contrast-below")
	assert.Equal(t, []string{
		"selection-text-contrast: accent-ink on accent-soft is 4.99:1 at app:tasks+appearance:light (min 7)",
		"selection-text-contrast: accent-ink on accent-soft is 4.99:1 at app:tasks+appearance:dark (min 7)",
	}, messages(ds))
	require.Len(t, ds, 2)
	assert.Equal(t, "contrast-below", ds[0].Rule)
	assert.Equal(t, "design-system/a.tokens.json", ds[0].File)
	assert.Equal(t, 2, ds[0].Line, "the fg token's line")
	assert.Equal(t, "accent-ink", ds[0].Path)
	assert.Equal(t, diag.Tally{Files: 2, Items: 4, Noun: "checks"}, tally, "2 apps x 2 appearances")

	ds, _ = ruleFindings(t, loadInv(t, tokens, inv("4.5")), "contrast-below")
	assert.Empty(t, ds)
}

func TestContrastIgnoresTokenExemption(t *testing.T) {
	tokens := `{
  "fg": { "$type": "color", "$value": ` + lch(0.6, 0, 0) + `, ` + ext(`"exempt": { "contrast-below": "legacy" }`) + ` },
  "bg": { "$type": "color", "$value": ` + lch(0.62, 0, 0) + ` }
}`
	ctx := &Context{}
	ds, _ := ruleContrastBelow(loadInv(t, tokens, "- { id: c, kind: contrast, fg: fg, bg: bg, min: 3 }\n"), ctx)
	assert.Equal(t, []string{"c: fg on bg is 1.08:1 (min 3)"}, messages(ds))
	assert.Empty(t, ctx.exemptions)
}

func TestHueReservedDanger(t *testing.T) {
	tokens := `{
  "danger": { "$type": "color", "$value": ` + lch(0.6, 0.15, 25) + ` },
  "danger-strong": { "$type": "color", "$value": ` + lch(0.5, 0.15, 25) + ` },
  "warn-1": { "$type": "color", "$value": ` + lch(0.6, 0.15, 28) + ` },
  "warn-2": { "$type": "color", "$value": ` + lch(0.6, 0.15, 40) + `,
    ` + ext(`"modes": { "appearance:dark": `+lch(0.7, 0.15, 30)+` }`) + ` },
  "warn-alias": { "$type": "color", "$value": "{warn-1}" },
  "slate": { "$type": "color", "$value": ` + lch(0.6, 0.01, 25) + ` },
  "gap": { "$type": "dimension", "$value": { "value": 4, "unit": "px" } }
}`
	invs := `- id: danger-hue-reserved
  kind: hue-reserved
  hue: 25
  tolerance: 8
  only: ["danger", "danger-*"]
`
	ds, tally := ruleFindings(t, loadInv(t, tokens, invs), "hue-reserved")
	assert.Equal(t, []string{
		"danger-hue-reserved: warn-1 has hue 28.0°, within 8.0° of reserved hue 25.0° (only danger, danger-*)",
		"danger-hue-reserved: warn-2 has hue 30.0° at appearance:dark, within 8.0° of reserved hue 25.0° (only danger, danger-*)",
		"danger-hue-reserved: warn-alias has hue 28.0°, within 8.0° of reserved hue 25.0° (only danger, danger-*)",
	}, messages(ds))
	assert.Equal(t, 4, ds[0].Line)
	assert.Equal(t, "warn-1", ds[0].Path)
	// 6 colour tokens x 12 contexts (2 appearances x 3 densities x 2 apps).
	assert.Equal(t, diag.Tally{Files: 2, Items: 72, Noun: "checks"}, tally)
}

func TestHueReservedAtZero(t *testing.T) {
	tokens := `{ "pink": { "$type": "color", "$value": ` + lch(0.6, 0.15, 358) + ` } }`
	ds, _ := ruleFindings(t, loadInv(t, tokens, "- { id: red, kind: hue-reserved, hue: 0, tolerance: 4, only: [red] }\n"), "hue-reserved")
	assert.Equal(t, []string{"red: pink has hue 358.0°, within 4.0° of reserved hue 0.0° (only red)"}, messages(ds))
}

func TestAccentFlip(t *testing.T) {
	tokens := `{
  "accent": { "$type": "color", "$value": ` + lch(0.6, 0.12, 230) + `,
    ` + ext(`"modes": { "appearance:dark": `+lch(0.7, 0.12, 232)+`, "app:tasks+appearance:dark": `+lch(0.7, 0.12, 230)+` }`) + ` },
  "accent-ink": { "$type": "color", "$value": ` + lch(0.5, 0.12, 230) + `,
    ` + ext(`"modes": { "appearance:dark": `+lch(0.8, 0.12, 230.5)+` }`) + ` },
  "accent-gray": { "$type": "color", "$value": ` + lch(0.5, 0.12, 230) + `,
    ` + ext(`"modes": { "appearance:dark": `+lch(0.8, 0.01, 90)+` }`) + ` }
}`
	invs := `- id: accent-hue-stable
  kind: same-hue-across-appearances
  tokens: ["accent", "accent-ink", "accent-gray"]
  forEach: accentGroup
`
	ds, tally := ruleFindings(t, loadInv(t, tokens, invs), "accent-flip")
	assert.Equal(t, []string{
		"accent-hue-stable: accent is 230.0° in appearance:light and 232.0° in appearance:dark at app:slides, 2.0° apart (max 1.0°)",
	}, messages(ds))
	require.Len(t, ds, 1)
	assert.Equal(t, "accent", ds[0].Path)
	assert.Equal(t, 2, ds[0].Line)
	// 2 apps x 3 tokens x 1 appearance pair.
	assert.Equal(t, diag.Tally{Files: 2, Items: 6, Noun: "checks"}, tally)
}

func TestDimensionBelow(t *testing.T) {
	tokens := `{
  "row-target": { "$type": "dimension", "$value": { "value": 48, "unit": "px" },
    ` + ext(`"density": { "compact": 40, "standard": 48, "relaxed": 52 }`) + ` },
  "chip-target": { "$type": "dimension", "$value": { "value": 2.5, "unit": "rem" } },
  "icon-target": { "$type": "dimension", "$value": { "value": 3, "unit": "rem" } },
  "gap": { "$type": "dimension", "$value": { "value": 2, "unit": "px" } }
}`
	invs := `- id: touch-floor
  kind: min-dimension
  match: "*-target*"
  min: 44
  densities: all
`
	ds, tally := ruleFindings(t, loadInv(t, tokens, invs), "dimension-below")
	assert.Equal(t, []string{
		"touch-floor: row-target is 40px at density:compact (min 44px)",
		"touch-floor: chip-target is 40px (2.5rem) (min 44px)",
	}, messages(ds))
	assert.Equal(t, 2, ds[0].Line)
	// 3 matching tokens x 12 contexts.
	assert.Equal(t, diag.Tally{Files: 2, Items: 36, Noun: "checks"}, tally)
}

func TestNotApplicableTally(t *testing.T) {
	p := load(t, map[string]string{"a.tokens.json": `{ "x": { "$type": "number", "$value": 1 } }`})
	for id, kind := range map[string]string{
		"contrast-below": "contrast", "hue-reserved": "hue-reserved",
		"accent-flip": "same-hue-across-appearances", "dimension-below": "min-dimension",
	} {
		ds, tally := ruleFindings(t, p, id)
		assert.Empty(t, ds, id)
		assert.Equal(t, diag.Tally{Noun: "checks", NotApplicable: "no " + kind + " invariants declared"}, tally, id)
	}
}

func TestInvariantSchemaInvalid(t *testing.T) {
	tokens := `{
  "fg": { "$type": "color", "$value": ` + lch(0.2, 0, 0) + ` },
  "bg": { "$type": "color", "$value": ` + lch(0.9, 0, 0) + ` },
  "n": { "$type": "number", "$value": 1 }
}`
	cases := []struct {
		rule, invs, want string
	}{
		{"contrast-below", "- { id: c, kind: contrast, fg: fg, min: 3 }",
			`invariant "c" (contrast) needs fg, bg and a min above 0`},
		{"contrast-below", "- { id: c, kind: contrast, fg: fg, bg: bg, min: 0 }",
			`invariant "c" (contrast) needs fg, bg and a min above 0`},
		{"contrast-below", "- { id: c, kind: contrast, fg: fg, bg: nope, min: 3 }",
			`invariant "c": bg names unknown token "nope"`},
		{"contrast-below", "- { id: c, kind: contrast, fg: n, bg: bg, min: 3 }",
			`invariant "c": fg "n" is a number token, not a color`},
		{"contrast-below", "- { id: c, kind: contrast, fg: fg, bg: bg, min: 3, forEach: [apperance] }",
			`invariant "c": forEach: unknown axis "apperance"`},
		{"contrast-below", "- { id: c, kind: contrast, fg: fg, bg: bg, min: 3, forEach: [swatch] }",
			`invariant "c": forEach: axis swatch has no values loaded`},
		{"hue-reserved", "- { id: h, kind: hue-reserved, tolerance: 4, only: [x] }",
			`invariant "h" (hue-reserved) needs hue, only and a tolerance above 0`},
		{"hue-reserved", "- { id: h, kind: hue-reserved, hue: 0, tolerance: 4, only: [\"[\"] }",
			`invariant "h": only: bad pattern "["`},
		{"accent-flip", "- { id: a, kind: same-hue-across-appearances, forEach: accentGroup }",
			`invariant "a" (same-hue-across-appearances) needs tokens`},
		{"dimension-below", "- { id: d, kind: min-dimension, min: 44 }",
			`invariant "d" (min-dimension) needs match and a min above 0`},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			ds, tally := ruleFindings(t, loadInv(t, tokens, tc.invs+"\n"), tc.rule)
			require.Len(t, ds, 1)
			assert.Equal(t, tc.want, ds[0].Message)
			assert.Equal(t, "schema-invalid", ds[0].Rule)
			assert.Equal(t, "design-system/invariants.yaml", ds[0].File)
			assert.Equal(t, 1, ds[0].Line)
			assert.Zero(t, tally.Items, "nothing is computed on a bad invariant")
		})
	}
}

func TestAccentFlipNeedsAppearance(t *testing.T) {
	cfg := "version: 1\nmodes:\n  app: { values: [slides, tasks], group: accent }\n"
	p := loadWith(t, cfg, map[string]string{
		"a.tokens.json":   `{ "accent": { "$type": "color", "$value": ` + lch(0.6, 0.1, 200) + ` } }`,
		"invariants.yaml": "- { id: a, kind: same-hue-across-appearances, tokens: [accent], forEach: accentGroup }\n",
	})
	ds, _ := ruleFindings(t, p, "accent-flip")
	assert.Equal(t, []string{`invariant "a": same-hue-across-appearances needs an appearance axis with values`}, messages(ds))
}

func TestInvariantsSkipUnresolvable(t *testing.T) {
	tokens := `{
  "fg": { "$type": "color", "$value": "{nope}" },
  "bg": { "$type": "color", "$value": ` + lch(0.9, 0, 0) + ` }
}`
	ds, tally := ruleFindings(t, loadInv(t, tokens, "- { id: c, kind: contrast, fg: fg, bg: bg, min: 3 }\n"), "contrast-below")
	assert.Empty(t, ds, "alias-unresolved reports it")
	assert.Zero(t, tally.Items)
}

func runBig576(t testing.TB) Result {
	p, ds, err := project.Load(testfix.Big576())
	require.NoError(t, err)
	require.Empty(t, ds)
	return Run(p, nil, Options{})
}

func TestInvariants576Under2s(t *testing.T) {
	start := time.Now()
	res := runBig576(t)
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 2*time.Second)
	assert.Empty(t, res.Diagnostics, "Big576 is clean")
	items := map[string]int{}
	for _, tl := range res.Tallies {
		items[tl.Rule] = tl.Items
	}
	assert.Equal(t, map[string]int{
		"contrast-below":  96,        // 6 apps x 8 swatches x 2 appearances
		"accent-flip":     96,        // 48 accent contexts x 2 tokens x 1 pair
		"hue-reserved":    576 * 132, // every context x every colour token
		"dimension-below": 576 * 12,  // every context x the 12 *-target tokens
	}, map[string]int{
		"contrast-below": items["contrast-below"], "accent-flip": items["accent-flip"],
		"hue-reserved": items["hue-reserved"], "dimension-below": items["dimension-below"],
	})
}

func BenchmarkInvariants576(b *testing.B) {
	p, ds, err := project.Load(testfix.Big576())
	require.NoError(b, err)
	require.Empty(b, ds)
	b.ResetTimer()
	for range b.N {
		Run(p, nil, Options{})
	}
}
