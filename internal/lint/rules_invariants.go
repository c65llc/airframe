// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"fmt"
	"maps"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/invariants"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// checksNoun is the Tally noun of the invariant rules: one check is one
// invariant evaluated in one context (for one token, where it lists many).
const checksNoun = "checks"

// The invariant kinds, as invariants.yaml spells them.
const (
	kindContrast    = "contrast"
	kindHueReserved = "hue-reserved"
	kindSameHue     = "same-hue-across-appearances"
	kindMinDim      = "min-dimension"
)

// accentGroup is the forEach shorthand for every loaded `group: accent` axis.
const accentGroup = "accentGroup"

// appearanceAxis is the axis accent-flip compares across.
const appearanceAxis = "appearance"

// flipLimit is the most, in degrees, a token's hue may move between two
// appearances under same-hue-across-appearances.
const flipLimit = 1.0

// pxPerRem converts rem dimensions to px for min-dimension.
const pxPerRem = 16.0

// resolveKey memoises one Resolve: a token in a full context, the context
// rendered by fullKey.
type resolveKey struct{ name, ctx string }

// resolve is p.Tokens.Resolve memoised in c for the run; nil when the token
// does not resolve (Tokens.Check reports why). key is fullKey(ctx).
func (c *Context) resolve(p *project.Project, name string, ctx config.Context, key string) *tokens.Value {
	k := resolveKey{name, key}
	if v, ok := c.resolved[k]; ok {
		return v
	}
	if c.resolved == nil {
		c.resolved = map[resolveKey]*tokens.Value{}
	}
	v, err := p.Tokens.Resolve(name, ctx)
	if err != nil {
		v = nil
	}
	c.resolved[k] = v
	return v
}

// varies is the set of axes the named token's value can depend on: every
// axis a mode key mentions on it or on any token it aliases in any mode.
// It is memoised in c.
func (c *Context) varies(p *project.Project, name string) map[string]bool {
	if out, ok := c.axesOf[name]; ok {
		return out
	}
	out := map[string]bool{}
	seen := map[string]bool{name: true}
	queue := []string{name}
	for i := 0; i < len(queue); i++ {
		t := p.Tokens.Tokens[queue[i]]
		if t == nil {
			continue
		}
		for _, kv := range valuesOf(t) {
			for _, pr := range pairs(kv.key) {
				axis, _, _ := strings.Cut(pr, ":")
				out[axis] = true
			}
			if a := kv.v.Alias; a != "" && !seen[a] {
				seen[a] = true
				queue = append(queue, a)
			}
		}
	}
	if c.axesOf == nil {
		c.axesOf = map[string]map[string]bool{}
	}
	c.axesOf[name] = out
	return out
}

// fullKey renders a context as a canonical mode key over all its axes.
func fullKey(ctx config.Context) string {
	ps := make([]string, 0, len(ctx))
	for _, axis := range slices.Sorted(maps.Keys(ctx)) {
		ps = append(ps, axis+":"+ctx[axis])
	}
	return strings.Join(ps, "+")
}

// shownKey is the part of ctx a finding names: the iterated axes the
// tokens involved actually vary on, as a canonical mode key.
func shownKey(ctx config.Context, iterated []string, vary ...map[string]bool) config.ModeKey {
	shown := config.Context{}
	for _, axis := range iterated {
		for _, v := range vary {
			if v[axis] {
				shown[axis] = ctx[axis]
			}
		}
	}
	return config.ModeKey(fullKey(shown))
}

// at is " at <key>", or "" for the empty key.
func at(k config.ModeKey) string {
	if k == "" {
		return ""
	}
	return " at " + string(k)
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func deg(d float64) string { return fmt.Sprintf("%.1f°", d) }

// checks tallies an invariant rule's evaluations and the files they read.
type checks struct {
	files map[string]bool
	n     int
}

func (k *checks) add(files ...string) {
	if k.files == nil {
		k.files = map[string]bool{}
	}
	for _, f := range files {
		k.files[f] = true
	}
	k.n++
}

func (k *checks) tally() diag.Tally {
	return diag.Tally{Files: len(k.files), Items: k.n, Noun: checksNoun}
}

// ofKind lists p's invariants of kind, in file order.
func ofKind(p *project.Project, kind string) []invariants.Invariant {
	var out []invariants.Invariant
	for _, inv := range p.Invariants {
		if inv.Kind == kind {
			out = append(out, inv)
		}
	}
	return out
}

func notApplicable(kind string) diag.Tally {
	return diag.Tally{Noun: checksNoun, NotApplicable: "no " + kind + " invariants declared"}
}

// invPath is where p's invariants.yaml lives.
func invPath(p *project.Project) string { return path.Join(p.Config.Content, invariants.File) }

// badInvariant is a schema-invalid finding at inv's entry in
// invariants.yaml: an invariant the rules cannot compute.
func badInvariant(p *project.Project, inv invariants.Invariant, format string, a ...any) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: "schema-invalid", Severity: diag.Error, File: invPath(p), Path: inv.ID, Line: inv.Line,
		Message: fmt.Sprintf("invariant %q", inv.ID) + fmt.Sprintf(format, a...),
	}
}

// finding is an invariant rule's finding on token t. Invariant findings are
// not token-exemptible: an invariant is the design system's own absolute.
func finding(rule string, t *tokens.Token, msg string) diag.Diagnostic {
	return diag.Diagnostic{Rule: rule, Severity: diag.Error, File: t.File, Path: jsonPath(t), Line: t.Line, Message: msg}
}

// expandForEach replaces accentGroup with every loaded accent axis and
// drops repeats; err names an axis that is unknown or not loaded.
func expandForEach(cfg *config.Config, names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		if n == accentGroup {
			for _, a := range accentAxes(cfg, false) {
				if !slices.Contains(out, a.Name) {
					out = append(out, a.Name)
				}
			}
			continue
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out, cfg.CheckAxes(out)
}

// loadedAxes names every axis whose values are loaded, in config order.
func loadedAxes(cfg *config.Config) []string {
	var out []string
	for _, a := range cfg.Axes {
		if !a.Deferred() {
			out = append(out, a.Name)
		}
	}
	return out
}

// colorToken returns the named colour token, or a reason it cannot be used
// as role (fg, bg, tokens).
func colorToken(p *project.Project, role, name string) (*tokens.Token, string) {
	t := p.Tokens.Tokens[name]
	switch {
	case t == nil:
		return nil, fmt.Sprintf(": %s names unknown token %q", role, name)
	case t.Type != "color":
		return nil, fmt.Sprintf(": %s %q is a %s token, not a color", role, name, t.Type)
	}
	return t, ""
}

// badGlobs is the first malformed pattern among globs, or "".
func badGlobs(globs ...string) string {
	for _, g := range globs {
		if _, err := path.Match(g, ""); err != nil {
			return g
		}
	}
	return ""
}

func matchesAny(name string, globs []string) bool {
	for _, g := range globs {
		if ok, _ := path.Match(g, name); ok {
			return true
		}
	}
	return false
}

func asColor(v *tokens.Value) (color.OKLCH, bool) {
	if v == nil {
		return color.OKLCH{}, false
	}
	c, ok := v.Lit.(color.OKLCH)
	return c, ok
}

// valuePair dedupes findings that the same resolved values produce in many
// contexts.
type valuePair struct {
	name string
	a, b *tokens.Value
}

// ruleContrastBelow checks each contrast invariant in every context over
// its forEach axes: fg over bg must reach min.
func ruleContrastBelow(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "contrast-below"
	invs := ofKind(p, kindContrast)
	if len(invs) == 0 {
		return nil, notApplicable(kindContrast)
	}
	var out []diag.Diagnostic
	var k checks
	for _, inv := range invs {
		if inv.FG == "" || inv.BG == "" || inv.Min <= 0 {
			out = append(out, badInvariant(p, inv, " (%s) needs fg, bg and a min above 0", inv.Kind))
			continue
		}
		fg, why := colorToken(p, "fg", inv.FG)
		if why == "" {
			_, why = colorToken(p, "bg", inv.BG)
		}
		if why != "" {
			out = append(out, badInvariant(p, inv, "%s", why))
			continue
		}
		bg := p.Tokens.Tokens[inv.BG]
		axes, err := expandForEach(p.Config, inv.ForEach)
		if err != nil {
			out = append(out, badInvariant(p, inv, ": forEach: %v", err))
			continue
		}
		vary := []map[string]bool{c.varies(p, fg.Name), c.varies(p, bg.Name)}
		seen := map[valuePair]bool{}
		for _, ctx := range p.Config.Contexts(axes) {
			key := fullKey(ctx)
			fv, bv := c.resolve(p, fg.Name, ctx, key), c.resolve(p, bg.Name, ctx, key)
			fc, ok1 := asColor(fv)
			bc, ok2 := asColor(bv)
			if !ok1 || !ok2 {
				continue
			}
			k.add(invPath(p), fg.File, bg.File)
			ratio := color.Contrast(fc, bc)
			if ratio >= inv.Min || seen[valuePair{"", fv, bv}] {
				continue
			}
			seen[valuePair{"", fv, bv}] = true
			out = append(out, finding(id, fg, fmt.Sprintf("%s: %s on %s is %.2f:1%s (min %s)",
				inv.ID, fg.Name, bg.Name, ratio, at(shownKey(ctx, axes, vary...)), num(inv.Min))))
		}
	}
	return out, k.tally()
}

// ruleHueReserved checks each hue-reserved invariant in every context: a
// chromatic colour within tolerance of the reserved hue must belong to a
// token whose name matches an `only` glob.
func ruleHueReserved(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "hue-reserved"
	invs := ofKind(p, kindHueReserved)
	if len(invs) == 0 {
		return nil, notApplicable(kindHueReserved)
	}
	var out []diag.Diagnostic
	var k checks
	axes := loadedAxes(p.Config)
	contexts := p.Config.Contexts(axes)
	keys := make([]string, len(contexts))
	for i, ctx := range contexts {
		keys[i] = fullKey(ctx)
	}
	for _, inv := range invs {
		if !inv.Has["hue"] || len(inv.Only) == 0 || inv.Tolerance <= 0 {
			out = append(out, badInvariant(p, inv, " (%s) needs hue, only and a tolerance above 0", inv.Kind))
			continue
		}
		if g := badGlobs(inv.Only...); g != "" {
			out = append(out, badInvariant(p, inv, ": only: bad pattern %q", g))
			continue
		}
		only := strings.Join(inv.Only, ", ")
		eachToken(p, func(t *tokens.Token) {
			if t.Type != "color" {
				return
			}
			allowed := matchesAny(t.Name, inv.Only)
			seen := map[*tokens.Value]bool{}
			for i, ctx := range contexts {
				v := c.resolve(p, t.Name, ctx, keys[i])
				if v == nil {
					continue
				}
				k.add(invPath(p), t.File)
				lit, ok := asColor(v)
				if allowed || !ok || color.Achromatic(lit) || seen[v] ||
					color.HueDistance(lit.H, inv.Hue) > inv.Tolerance {
					continue
				}
				seen[v] = true
				out = append(out, finding(id, t, fmt.Sprintf("%s: %s has hue %s%s, within %s of reserved hue %s (only %s)",
					inv.ID, t.Name, deg(lit.H), at(shownKey(ctx, axes, c.varies(p, t.Name))),
					deg(inv.Tolerance), deg(inv.Hue), only)))
			}
		})
	}
	return out, k.tally()
}

// ruleAccentFlip checks each same-hue-across-appearances invariant: for
// every context over its forEach axes, each listed token's hue may move at
// most flipLimit between any two appearances. Achromatic values are skipped.
func ruleAccentFlip(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "accent-flip"
	invs := ofKind(p, kindSameHue)
	if len(invs) == 0 {
		return nil, notApplicable(kindSameHue)
	}
	var appearance []string
	for _, a := range p.Config.Axes {
		if a.Name == appearanceAxis && !a.Deferred() {
			appearance = a.Values
		}
	}
	var out []diag.Diagnostic
	var k checks
	for _, inv := range invs {
		if len(inv.Tokens) == 0 {
			out = append(out, badInvariant(p, inv, " (%s) needs tokens", inv.Kind))
			continue
		}
		if len(appearance) < 2 {
			out = append(out, badInvariant(p, inv, ": %s needs an %s axis with values", inv.Kind, appearanceAxis))
			continue
		}
		var ts []*tokens.Token
		why := ""
		for _, name := range inv.Tokens {
			t, w := colorToken(p, "tokens", name)
			if w != "" {
				why = w
				break
			}
			ts = append(ts, t)
		}
		if why != "" {
			out = append(out, badInvariant(p, inv, "%s", why))
			continue
		}
		axes, err := expandForEach(p.Config, inv.ForEach)
		if err != nil {
			out = append(out, badInvariant(p, inv, ": forEach: %v", err))
			continue
		}
		axes = slices.DeleteFunc(axes, func(a string) bool { return a == appearanceAxis })
		seen := map[valuePair]bool{}
		for _, base := range p.Config.Contexts(axes) {
			per := make([]config.Context, len(appearance))
			keys := make([]string, len(appearance))
			for i, v := range appearance {
				per[i] = maps.Clone(base)
				per[i][appearanceAxis] = v
				keys[i] = fullKey(per[i])
			}
			for _, t := range ts {
				for i := range appearance {
					for j := i + 1; j < len(appearance); j++ {
						va := c.resolve(p, t.Name, per[i], keys[i])
						vb := c.resolve(p, t.Name, per[j], keys[j])
						ca, ok1 := asColor(va)
						cb, ok2 := asColor(vb)
						if !ok1 || !ok2 {
							continue
						}
						k.add(invPath(p), t.File)
						d := color.HueDistance(ca.H, cb.H)
						if color.Achromatic(ca) || color.Achromatic(cb) || d <= flipLimit || seen[valuePair{t.Name, va, vb}] {
							continue
						}
						seen[valuePair{t.Name, va, vb}] = true
						out = append(out, finding(id, t, fmt.Sprintf("%s: %s is %s in %s:%s and %s in %s:%s%s, %s apart (max %s)",
							inv.ID, t.Name, deg(ca.H), appearanceAxis, appearance[i], deg(cb.H), appearanceAxis, appearance[j],
							at(shownKey(base, axes, c.varies(p, t.Name))), deg(d), deg(flipLimit))))
					}
				}
			}
		}
	}
	return out, k.tally()
}

// ruleDimensionBelow checks each min-dimension invariant in every context:
// every dimension token whose name matches the glob must resolve to at
// least min px (rem counted at 16px).
func ruleDimensionBelow(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "dimension-below"
	invs := ofKind(p, kindMinDim)
	if len(invs) == 0 {
		return nil, notApplicable(kindMinDim)
	}
	var out []diag.Diagnostic
	var k checks
	axes := loadedAxes(p.Config)
	contexts := p.Config.Contexts(axes)
	keys := make([]string, len(contexts))
	for i, ctx := range contexts {
		keys[i] = fullKey(ctx)
	}
	for _, inv := range invs {
		if inv.Match == "" || inv.Min <= 0 {
			out = append(out, badInvariant(p, inv, " (%s) needs match and a min above 0", inv.Kind))
			continue
		}
		if g := badGlobs(inv.Match); g != "" {
			out = append(out, badInvariant(p, inv, ": match: bad pattern %q", g))
			continue
		}
		eachToken(p, func(t *tokens.Token) {
			if t.Type != "dimension" || !matchesAny(t.Name, []string{inv.Match}) {
				return
			}
			seen := map[*tokens.Value]bool{}
			for i, ctx := range contexts {
				v := c.resolve(p, t.Name, ctx, keys[i])
				if v == nil {
					continue
				}
				dim, ok := v.Lit.(tokens.Dim)
				if !ok {
					continue
				}
				k.add(invPath(p), t.File)
				size, shown := dim.V, num(dim.V)+"px"
				if dim.Unit == "rem" {
					size = dim.V * pxPerRem
					shown = num(math.Round(size*100)/100) + "px (" + num(dim.V) + "rem)"
				}
				if size >= inv.Min || seen[v] {
					continue
				}
				seen[v] = true
				out = append(out, finding(id, t, fmt.Sprintf("%s: %s is %s%s (min %spx)",
					inv.ID, t.Name, shown, at(shownKey(ctx, axes, c.varies(p, t.Name))), num(inv.Min))))
			}
		})
	}
	return out, k.tally()
}
