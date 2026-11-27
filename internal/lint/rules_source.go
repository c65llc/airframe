// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// The tiers the source rules know.
const (
	tierPrimitive = "primitive"
	tierSemantic  = "semantic"
	tierComponent = "component"
)

// hueLimit is the widest hue span, in degrees, one colour prefix may cover.
const hueLimit = 8.0

// keyed is one of a token's values with the mode key that sets it ("" for
// $value).
type keyed struct {
	key config.ModeKey
	v   *tokens.Value
}

// valuesOf lists t's values: the default first, then the modes in key order.
func valuesOf(t *tokens.Token) []keyed {
	var out []keyed
	if t.Default != nil {
		out = append(out, keyed{"", t.Default})
	}
	for _, k := range slices.Sorted(maps.Keys(t.Modes)) {
		out = append(out, keyed{k, t.Modes[k]})
	}
	return out
}

// eachToken calls fn for every token in declaration order.
func eachToken(p *project.Project, fn func(t *tokens.Token)) {
	if p.Tokens == nil {
		return
	}
	for _, name := range p.Tokens.Order {
		fn(p.Tokens.Tokens[name])
	}
}

// inMode is " (mode <key>)" for an override and "" for $value.
func inMode(k config.ModeKey) string {
	if k == "" {
		return ""
	}
	return " (mode " + string(k) + ")"
}

// pairs splits a mode key into its axis:value pairs.
func pairs(k config.ModeKey) []string {
	if k == "" {
		return nil
	}
	return strings.Split(string(k), "+")
}

// accentAxes lists the `group: accent` axes, in config order. withDeferred
// includes those whose values are not loaded yet.
func accentAxes(cfg *config.Config, withDeferred bool) []config.Axis {
	var out []config.Axis
	for _, a := range cfg.Axes {
		if a.Group == "accent" && (withDeferred || !a.Deferred()) {
			out = append(out, a)
		}
	}
	return out
}

// onAxes reports whether k sets any of the axes.
func onAxes(k config.ModeKey, axes []config.Axis) bool {
	for _, p := range pairs(k) {
		name, _, _ := strings.Cut(p, ":")
		for _, a := range axes {
			if a.Name == name {
				return true
			}
		}
	}
	return false
}

func add(out []diag.Diagnostic, d *diag.Diagnostic) []diag.Diagnostic {
	if d == nil {
		return out
	}
	return append(out, *d)
}

func ruleTokenUnclassified(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "token-unclassified"
	var out []diag.Diagnostic
	var k counter
	eachToken(p, func(t *tokens.Token) {
		k.add(t)
		switch {
		case t.Tier == "":
			out = add(out, c.TokenFinding(id, t, "", 0, fmt.Sprintf("token %q has no tier", t.Name)))
		case t.Tier == tierComponent && t.Component == "":
			out = add(out, c.TokenFinding(id, t, "", 0, fmt.Sprintf("component token %q names no component", t.Name)))
		}
	})
	return out, k.tally()
}

// readable reports why t may not alias target, or "" when it may.
func readable(t, target *tokens.Token) string {
	switch t.Tier {
	case tierPrimitive:
		return "a primitive must be a literal"
	case tierSemantic:
		if target.Tier != tierPrimitive && target.Tier != tierSemantic {
			return "a semantic token may read primitive or semantic tokens"
		}
	case tierComponent:
		ok := target.Tier == tierSemantic || target.Shared ||
			(target.Tier == tierComponent && target.Component == t.Component)
		if !ok {
			return "a component token may read semantic tokens, its own component's tokens or shared tokens"
		}
	}
	return ""
}

// describe names a token's tier, and its component when it has one.
func describe(t *tokens.Token) string {
	if t.Tier == tierComponent && t.Component != "" {
		return "component " + t.Component
	}
	return t.Tier
}

func ruleTierViolation(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "tier-violation"
	var out []diag.Diagnostic
	var k counter
	eachToken(p, func(t *tokens.Token) {
		if t.Tier != tierPrimitive && t.Tier != tierSemantic && t.Tier != tierComponent {
			return // token-unclassified, or a tier these rules do not know
		}
		k.add(t)
		for _, kv := range valuesOf(t) {
			if kv.v.Alias == "" {
				continue
			}
			target := p.Tokens.Tokens[kv.v.Alias]
			if target == nil || (t.Tier != tierPrimitive && target.Tier == "") {
				continue // alias-unresolved or token-unclassified reports it
			}
			why := readable(t, target)
			if why == "" {
				continue
			}
			var msg string
			switch t.Tier {
			case tierPrimitive:
				msg = fmt.Sprintf("primitive token %q aliases %q%s; %s", t.Name, target.Name, inMode(kv.key), why)
			case tierComponent:
				msg = fmt.Sprintf("component token %q (%s) aliases %q (%s)%s; %s",
					t.Name, t.Component, target.Name, describe(target), inMode(kv.key), why)
			default:
				msg = fmt.Sprintf("%s token %q aliases %q (%s)%s; %s",
					t.Tier, t.Name, target.Name, describe(target), inMode(kv.key), why)
			}
			out = add(out, c.TokenFinding(id, t, p.Tokens.FileOf(kv.v), kv.v.Line, msg))
		}
	})
	return out, k.tally()
}

func ruleLiteralInSemantic(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "literal-in-semantic"
	var out []diag.Diagnostic
	var k counter
	eachToken(p, func(t *tokens.Token) {
		if t.Tier != tierSemantic {
			return
		}
		k.add(t)
		for _, kv := range valuesOf(t) {
			if kv.v.Alias != "" {
				continue
			}
			msg := fmt.Sprintf("semantic token %q is a literal%s; alias a primitive instead", t.Name, inMode(kv.key))
			out = add(out, c.TokenFinding(id, t, p.Tokens.FileOf(kv.v), kv.v.Line, msg))
		}
	})
	return out, k.tally()
}

func ruleDensityMissing(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "density-missing"
	var density *config.Axis
	for i, a := range p.Config.Axes {
		if a.Name == "density" && !a.Deferred() && len(a.Values) > 0 {
			density = &p.Config.Axes[i]
		}
	}
	if density == nil {
		return nil, diag.Tally{Noun: tokensNoun, NotApplicable: "no density axis configured"}
	}
	var out []diag.Diagnostic
	var k counter
	eachToken(p, func(t *tokens.Token) {
		if t.Tier != tierComponent || t.Type != "dimension" {
			return
		}
		k.add(t)
		if strings.TrimSpace(t.Chrome) != "" {
			return
		}
		var missing []string
		for _, v := range density.Values {
			if _, ok := t.Modes[config.ModeKey(density.Name+":"+v)]; !ok {
				missing = append(missing, v)
			}
		}
		if len(missing) == 0 {
			return
		}
		msg := fmt.Sprintf(`component dimension %q has no density value for %s; give every density a value or set chrome: "<reason>"`,
			t.Name, strings.Join(missing, ", "))
		out = add(out, c.TokenFinding(id, t, "", 0, msg))
	})
	return out, k.tally()
}

// ruleAccentGroupPartial checks, for each value of each loaded accent axis,
// that the required roles are set under it all together or not at all. Its
// finding is about a group, not one token, so a token's exemption does not
// apply; it is located at the first role that is set.
func ruleAccentGroupPartial(p *project.Project, _ *Context) ([]diag.Diagnostic, diag.Tally) {
	required := p.Config.AccentRequired
	if len(required) == 0 {
		return nil, diag.Tally{Noun: tokensNoun, NotApplicable: "no accentGroups.required configured"}
	}
	var k counter
	for _, name := range required {
		if t := p.Tokens.Tokens[name]; t != nil {
			k.add(t)
		}
	}
	var out []diag.Diagnostic
	for _, axis := range accentAxes(p.Config, false) {
		for _, val := range axis.Values {
			pair := axis.Name + ":" + val
			var missing []string
			var at *tokens.Token
			var atValue *tokens.Value
			for _, name := range required {
				v := setUnder(p.Tokens.Tokens[name], pair)
				if v == nil {
					missing = append(missing, name)
					continue
				}
				if at == nil {
					at, atValue = p.Tokens.Tokens[name], v
				}
			}
			if at == nil || len(missing) == 0 {
				continue
			}
			out = append(out, diag.Diagnostic{
				Rule: "accent-group-partial", Severity: diag.Error,
				File: p.Tokens.FileOf(atValue), Path: jsonPath(at), Line: atValue.Line,
				Message: fmt.Sprintf("%s sets %d of %d accent roles; missing %s",
					pair, len(required)-len(missing), len(required), strings.Join(missing, ", ")),
			})
		}
	}
	return out, k.tally()
}

// setUnder is t's first mode value, in key order, whose key contains pair;
// nil when t is nil or has none.
func setUnder(t *tokens.Token, pair string) *tokens.Value {
	if t == nil {
		return nil
	}
	for _, kv := range valuesOf(t) {
		if slices.Contains(pairs(kv.key), pair) {
			return kv.v
		}
	}
	return nil
}

// hue is one chromatic literal colour value of a token.
type hue struct {
	t   *tokens.Token
	kv  keyed
	deg float64
}

func (h hue) String() string {
	s := h.t.Name + " " + degrees(h.deg)
	if h.kv.key != "" {
		s += " in " + string(h.kv.key)
	}
	return s
}

func degrees(d float64) string {
	return strconv.FormatFloat(math.Round(d*10)/10, 'f', -1, 64) + "°"
}

// rulePrefixMultipleHues groups colour tokens by their first name segment
// and reports a group whose chromatic literal values span more than
// hueLimit. Accent mode values are left out: they retint on purpose. A
// token exempt from the rule is left out of its group's span.
func rulePrefixMultipleHues(p *project.Project, c *Context) ([]diag.Diagnostic, diag.Tally) {
	const id = "prefix-multiple-hues"
	accent := accentAxes(p.Config, true)
	var k counter
	groups := map[string][]hue{}
	var prefixes []string
	eachToken(p, func(t *tokens.Token) {
		if t.Type != "color" {
			return
		}
		k.add(t)
		prefix, _, _ := strings.Cut(t.Name, "-")
		if _, seen := groups[prefix]; !seen {
			prefixes = append(prefixes, prefix)
			groups[prefix] = nil
		}
		for _, kv := range valuesOf(t) {
			lit, ok := kv.v.Lit.(color.OKLCH)
			if kv.v.Alias != "" || !ok || color.Achromatic(lit) || onAxes(kv.key, accent) {
				continue
			}
			groups[prefix] = append(groups[prefix], hue{t, kv, lit.H})
		}
	})
	var out []diag.Diagnostic
	for _, prefix := range prefixes {
		hs := groups[prefix]
		if _, _, span := widest(hs); span <= hueLimit {
			continue
		}
		var kept []hue
		for _, h := range hs {
			if !c.exempt(id, h.t) {
				kept = append(kept, h)
			}
		}
		a, b, span := widest(kept)
		if span <= hueLimit {
			continue
		}
		msg := fmt.Sprintf("color tokens prefixed %q span %s of hue (%s, %s); the limit is %s",
			prefix, degrees(span), a, b, degrees(hueLimit))
		out = add(out, c.TokenFinding(id, b.t, p.Tokens.FileOf(b.kv.v), b.kv.v.Line, msg))
	}
	return out, k.tally()
}

// widest is the pair of hs farthest apart in hue, first-found on ties, and
// their circular distance; 0 when hs has fewer than two.
func widest(hs []hue) (a, b hue, span float64) {
	span = -1
	for i := range hs {
		for j := i + 1; j < len(hs); j++ {
			if d := color.HueDistance(hs[i].deg, hs[j].deg); d > span {
				a, b, span = hs[i], hs[j], d
			}
		}
	}
	return a, b, math.Max(span, 0)
}

func ruleExemptionWithoutReason(p *project.Project, _ *Context) ([]diag.Diagnostic, diag.Tally) {
	var out []diag.Diagnostic
	var k counter
	eachToken(p, func(t *tokens.Token) {
		k.add(t)
		for _, rule := range slices.Sorted(maps.Keys(t.Exempt)) {
			if strings.TrimSpace(t.Exempt[rule]) != "" {
				continue
			}
			out = append(out, diag.Diagnostic{
				Rule: "exemption-without-reason", Severity: diag.Error,
				File: t.File, Path: jsonPath(t), Line: t.Line,
				Message: fmt.Sprintf("token %q is exempt from %s with no reason; give the reason", t.Name, rule),
			})
		}
	})
	return out, k.tally()
}
