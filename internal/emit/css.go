// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/color"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// Scope is where one CSS block applies: a selector, inside zero or more
// nested @media conditions.
type Scope struct {
	// Media holds the nested @media conditions, outermost first.
	Media []string
	// Selector is the block's selector, starting from :root.
	Selector string
}

// option is one way an axis value is spelled: an optional media condition
// and a compound appended to the selector.
type option struct {
	media    []string
	compound string
}

// CSS writes every token as a custom property: the default values on
// :root, then one block per scope of every mode key some token uses.
// Blocks go in (pair count, key) order and declarations in name order. An
// alias is written as var(--target), so overrides cascade.
func CSS(p *project.Project, pl config.Platform) (map[string][]byte, error) {
	toks := emitted(p, "css")
	inCSS := map[string]bool{}
	for _, t := range toks {
		inCSS[t.Name] = true
	}
	keys := map[config.ModeKey]bool{"": true}
	for _, t := range toks {
		for k := range t.Modes {
			keys[k] = true
		}
	}
	var b strings.Builder
	b.WriteString("/* " + header(p) + " */\n")
	for _, key := range slices.SortedFunc(maps.Keys(keys), compareKeys) {
		var decls []string
		for _, t := range toks {
			v := t.Default
			if key != "" {
				v = t.Modes[key]
			}
			if v == nil {
				continue
			}
			if v.Alias != "" && !inCSS[v.Alias] {
				return nil, fmt.Errorf("--%s aliases %s, which is not emitted to css%s", t.Name, v.Alias, inMode(key))
			}
			val := CSSValue(v)
			if val == "" {
				return nil, fmt.Errorf("--%s: cannot write its value as CSS%s", t.Name, inMode(key))
			}
			decls = append(decls, "--"+t.Name+": "+val+";")
		}
		if len(decls) == 0 {
			continue
		}
		scopes, err := Scopes(p.Config, key)
		if err != nil {
			return nil, err
		}
		for _, sc := range scopes {
			b.WriteByte('\n')
			writeBlock(&b, sc, decls)
		}
	}
	return map[string][]byte{outPath(p, pl, "css"): []byte(b.String())}, nil
}

// inMode is " (mode <key>)" for an override and "" for $value.
func inMode(k config.ModeKey) string {
	if k == "" {
		return ""
	}
	return " (mode " + string(k) + ")"
}

func writeBlock(b *strings.Builder, sc Scope, decls []string) {
	ind := ""
	for _, m := range sc.Media {
		b.WriteString(ind + m + " {\n")
		ind += "  "
	}
	b.WriteString(ind + sc.Selector + " {\n")
	for _, d := range decls {
		b.WriteString(ind + "  " + d + "\n")
	}
	b.WriteString(ind + "}\n")
	for range sc.Media {
		ind = ind[2:]
		b.WriteString(ind + "}\n")
	}
}

// Scopes expands a mode key into its CSS scopes, the mapping the emitter
// writes and the importer inverts. Starting from :root, each of the key's
// pairs, in the key's own order, multiplies the scopes by that value's
// spellings: media lists concatenate (outermost first), compounds append.
// The scopes are returned in emit order, the order their blocks are written.
func Scopes(cfg *config.Config, key config.ModeKey) ([]Scope, error) {
	out := []Scope{{Selector: ":root"}}
	for _, pr := range pairsOf(key) {
		ax, _ := axisNamed(cfg, pr[0])
		opts, err := options(ax, pr[1])
		if err != nil {
			return nil, err
		}
		next := make([]Scope, 0, len(out)*len(opts))
		for _, s := range out {
			for _, o := range opts {
				next = append(next, Scope{
					Media:    slices.Concat(s.Media, o.media),
					Selector: s.Selector + o.compound,
				})
			}
		}
		out = next
	}
	return out, nil
}

func axisNamed(cfg *config.Config, name string) (config.Axis, bool) {
	for _, a := range cfg.Axes {
		if a.Name == name {
			return a, true
		}
	}
	return config.Axis{}, false
}

// options lists how ax=val is spelled in CSS: its explicit selectors, else
// [attr='val']; plus, when Also is set, its media condition on
// :root:not([attr]), a fallback only while the attribute is unset.
func options(ax config.Axis, val string) ([]option, error) {
	pair := ax.Name + ":" + val
	// The also media condition is a fallback for while the attribute is
	// unset, so an explicit attribute value always wins; config.Load
	// guarantees the axis has the attr it needs.
	also := strings.TrimSpace(ax.Web.Also[val])
	var out []option
	if sels := ax.Web.Selectors[val]; len(sels) > 0 {
		for _, s := range sels {
			o, ok := parseSelector(s)
			if !ok {
				return nil, &configError{
					path: "modes." + ax.Name + ".web." + val,
					msg: fmt.Sprintf("%s: web selector %q must be :root followed by a compound selector, "+
						"optionally after @media (…)", pair, s),
				}
			}
			out = append(out, o)
		}
	} else {
		if ax.Web.Attr == "" {
			return nil, &configError{
				path: "modes." + ax.Name + ".web",
				msg:  fmt.Sprintf("%s: the axis has no web attr and no selector for %s, so CSS cannot scope it", pair, val),
			}
		}
		out = append(out, option{compound: "[" + ax.Web.Attr + "='" + val + "']"})
	}
	if also != "" {
		if !strings.HasPrefix(also, "@media ") || len(also) == len("@media ") {
			return nil, &configError{
				path: "modes." + ax.Name + ".web.also." + val,
				msg:  fmt.Sprintf("%s: web also %q must be an @media condition", pair, also),
			}
		}
		out = append(out, option{media: []string{also}, compound: ":not([" + ax.Web.Attr + "])"})
	}
	return out, nil
}

// parseSelector splits "[@media (…) ]:root<compound>" into its media
// condition and compound. ok is false when the selector does not start
// with :root (after any media condition) or the remainder is not one
// compound selector.
func parseSelector(s string) (option, bool) {
	s = strings.TrimSpace(s)
	var o option
	if strings.HasPrefix(s, "@media") {
		depth, at := 0, -1
		for i := len("@media"); i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 && strings.HasPrefix(s[i:], ":root") && isSpace(s[i-1]) {
				at = i
				break
			}
		}
		media := strings.TrimSpace(s[:max(at, 0)])
		if at < 0 || media == "@media" {
			return o, false
		}
		o.media, s = []string{media}, s[at:]
	}
	rest, ok := strings.CutPrefix(s, ":root")
	if !ok || !oneCompound(rest) {
		return o, false
	}
	o.compound = rest
	return o, true
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

// oneCompound reports whether s continues a compound selector: it does
// not extend the :root name, and has no combinator or list comma outside
// brackets, parentheses and quotes.
func oneCompound(s string) bool {
	if s == "" {
		return true
	}
	if c := s[0]; c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
		return false
	}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth == 0 && strings.IndexByte(" \t\n,>+~", c) >= 0:
			return false
		}
	}
	return depth == 0 && quote == 0
}

// genericFamilies are the CSS generic font families, written unquoted.
var genericFamilies = map[string]bool{
	"serif": true, "sans-serif": true, "monospace": true, "cursive": true, "fantasy": true,
	"system-ui": true, "ui-serif": true, "ui-sans-serif": true, "ui-monospace": true,
	"ui-rounded": true, "math": true, "emoji": true, "fangsong": true,
}

// CSSValue is v as a CSS value: var(--target) for an alias, else the
// literal. It is "" when the value cannot be written (CSS reports that).
func CSSValue(v *tokens.Value) string {
	if v.Alias != "" {
		return "var(--" + v.Alias + ")"
	}
	switch l := v.Lit.(type) {
	case color.OKLCH:
		return oklch(l)
	case tokens.Dim:
		return num(l.V) + l.Unit
	case tokens.Duration:
		return num(l.V) + l.Unit
	case float64:
		return num(l)
	case []string:
		names := make([]string, len(l))
		for i, n := range l {
			if genericFamilies[n] {
				names[i] = n
			} else {
				names[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(n) + `"`
			}
		}
		return strings.Join(names, ", ")
	case [4]float64:
		return fmt.Sprintf("cubic-bezier(%s, %s, %s, %s)", num(l[0]), num(l[1]), num(l[2]), num(l[3]))
	case []tokens.Shadow:
		layers := make([]string, len(l))
		for i, s := range l {
			parts := []string{length(s.OffsetX), length(s.OffsetY), length(s.Blur), length(s.Spread), oklch(s.Color)}
			if s.Inset {
				parts = slices.Insert(parts, 0, "inset")
			}
			layers[i] = strings.Join(parts, " ")
		}
		return strings.Join(layers, ", ")
	}
	return ""
}

// oklch is c as oklch(L% C H), with " / A" when alpha is not 1.
func oklch(c color.OKLCH) string {
	s := "oklch(" + num(c.L*100) + "% " + num(c.C) + " " + num(c.H)
	if a := num(c.Alpha); a != "1" {
		s += " / " + a
	}
	return s + ")"
}

// length is a shadow length: a bare 0 when zero, else value and unit.
func length(d tokens.Dim) string {
	if n := num(d.V); n != "0" {
		return n + d.Unit
	}
	return "0"
}
