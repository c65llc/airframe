// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package consumer

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/css"
	"github.com/c65llc/airframe/internal/cssvalue"
	"github.com/c65llc/airframe/internal/emit"
)

// adapter finds the direct references in one kind of consumer file.
type adapter struct {
	format string // the emit format whose names it reads
	scan   func(r *platformRun, f *file) error
}

var (
	cssAdapter    = &adapter{format: "css", scan: scanCSS}
	swiftAdapter  = &adapter{format: "swift", scan: scanSwift}
	kotlinAdapter = &adapter{format: "kotlin", scan: scanKotlin}
	xamlAdapter   = &adapter{format: "xaml", scan: func(r *platformRun, f *file) error { return scanXAML(r, f, xamlRefPat) }}
	csAdapter     = &adapter{format: "xaml", scan: func(r *platformRun, f *file) error { return scanXAML(r, f, csRefPat) }}
)

// adapters maps a consumer file extension to its adapter.
var adapters = map[string]*adapter{
	".swift": swiftAdapter,
	".kt":    kotlinAdapter, ".kts": kotlinAdapter,
	".xaml": xamlAdapter,
	".cs":   csAdapter,
}

func init() {
	for _, ext := range strings.Fields(".css .scss .sass .less .html .htm .vue .svelte .js .jsx .mjs .cjs .ts .tsx") {
		adapters[ext] = cssAdapter
	}
}

// adapterFor is the adapter for consumer file f on pl, or nil and why not:
// no adapter reads its extension, or pl does not emit the adapter's format.
func adapterFor(pl config.Platform, f string) (*adapter, string) {
	ext := strings.ToLower(path.Ext(f))
	a := adapters[ext]
	switch {
	case a == nil && ext == "":
		return nil, "no adapter reads a file with no extension"
	case a == nil:
		return nil, "no adapter reads " + ext
	case !slices.Contains(pl.Emit, a.format):
		return nil, fmt.Sprintf("%s is read through %s names, which platform %s does not emit", ext, a.format, pl.Name)
	}
	return a, ""
}

// Format is the emit format whose names consumer file f is read through
// on pl ("css", "swift", "kotlin" or "xaml"), or "" when no adapter reads
// it there.
func Format(pl config.Platform, f string) string {
	if a, _ := adapterFor(pl, f); a != nil {
		return a.format
	}
	return ""
}

// ours reports whether name fits the naming template's literal prefix and
// suffix, so a name the index does not hold is a typo, not a foreign
// symbol. The name has the prefix, then a non-empty placeholder part, and
// then, when the suffix is not empty, the suffix followed by nothing or by
// a Pascal tail: the emitters append runtime suffixes (Compact) and shadow
// part suffixes (Layer1OffsetX) after the template's suffix.
func ours(name, prefix, suffix string) bool {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || rest == "" {
		return false
	}
	if suffix == "" {
		return true
	}
	for i := 1; i+len(suffix) <= len(rest); i++ {
		if !strings.HasPrefix(rest[i:], suffix) {
			continue
		}
		if tail := rest[i+len(suffix):]; tail == "" || pascalTailRE.MatchString(tail) {
			return true
		}
	}
	return false
}

var pascalTailRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// native resolves an identifier a swift, kotlin or xaml adapter matched
// at off: a token name is a reference, a generated symbol that is no token
// is ignored, and any other name is undeclared when it is ours and ignored
// (a foreign symbol) when it is not.
func (r *platformRun) native(f *file, off int, raw, ident, format string) error {
	names, err := r.s.refNames(r.pl, format)
	if err != nil {
		return err
	}
	bare := strings.Trim(ident, "`")
	tok, ok := names[ident]
	if !ok {
		tok, ok = names[bare]
	}
	switch {
	case ok && tok != "":
		r.ref(f, off, raw, tok, Direct)
	case ok:
		r.claim(f, off)
	default:
		prefix, suffix := emit.NamingAffixes(r.s.p.Config, format)
		if !ours(bare, prefix, suffix) {
			return nil
		}
		r.ref(f, off, raw, "", Direct)
		r.problem(f, off, Undeclared, raw, fmt.Sprintf(
			"%s: %s is not a name the %s emitter writes for platform %s", raw, bare, format, r.pl.Name))
	}
	return nil
}

// typePart is the naming template's type part for swift or kotlin
// (DesignTokens.{camel} gives DesignTokens).
func (r *platformRun) typePart(format string) string {
	t := r.s.p.Config.NamingFor(format)
	if i := strings.LastIndex(t, "."); i >= 0 {
		return t[:i]
	}
	return t
}

// identRune is a whole identifier, non-ASCII letters included, so a name
// such as café is matched entire rather than cut at its first non-ASCII
// letter; identPat is one that may be backticked.
const (
	identRune = `[\p{L}_][\p{L}\p{M}\p{N}_]*`
	identPat  = "`?" + identRune + "`?"
	notIdent  = `(?m)(?:^|[^\p{L}\p{M}\p{N}_])`
)

// scanMatches runs pat over f's code; group 1 is the raw reference and
// group 2 the identifier the format's names are looked up by.
func scanMatches(r *platformRun, f *file, pat, format string) error {
	for _, m := range r.regex(pat).FindAllStringSubmatchIndex(f.code, -1) {
		if err := r.native(f, m[2], f.code[m[2]:m[3]], f.code[m[4]:m[5]], format); err != nil {
			return err
		}
	}
	return nil
}

func scanSwift(r *platformRun, f *file) error {
	return scanMatches(r, f, notIdent+`(`+regexp.QuoteMeta(r.typePart("swift"))+`\.(`+identPat+`))`, "swift")
}

func scanKotlin(r *platformRun, f *file) error {
	typ := r.typePart("kotlin")
	if err := scanMatches(r, f, notIdent+`(`+regexp.QuoteMeta(typ)+`\.(`+identPat+`))`, "kotlin"); err != nil {
		return err
	}
	return scanMatches(r, f, notIdent+`(`+regexp.QuoteMeta(emit.KotlinColorValuesType(typ))+`\.`+identPat+`\.(`+identPat+`))`, "kotlin")
}

const (
	xamlRefPat = `(\{(?:StaticResource|ThemeResource)\s+(` + identRune + `)\s*\})`
	csRefPat   = `(Resources\["(` + identRune + `)"\])`
)

func scanXAML(r *platformRun, f *file, pat string) error {
	return scanMatches(r, f, pat, "xaml")
}

// localPropRE is a custom property definition (--name: or, in a script
// object, '--name':), which makes the property app-local.
var localPropRE = regexp.MustCompile(`(?:^|[^A-Za-z0-9_\x{80}-\x{10FFFF}-])(--(?:[A-Za-z0-9_-]|[^\x00-\x7F])+)['"]?\s*:`)

// isIdentByte is a byte of a CSS identifier: ASCII letters, digits, "-"
// and "_", and every byte of a non-ASCII character.
func isIdentByte(c byte) bool {
	return c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// cssVar is one var( occurrence: the custom property and, when there is
// one, the fallback after its comma up to the matching ")".
type cssVar struct {
	nameOff  int
	name     string
	fbOff    int // -1 with no fallback
	fallback string
}

// cssVars finds every var(--name …) in src, nested ones included: each
// var( is its own reference. A fallback runs, parentheses balanced and
// quoted strings skipped, to the ")" closing the var(; with none it runs
// to the end of the line.
func cssVars(src string) []cssVar {
	var out []cssVar
	for i := 0; ; {
		j := strings.Index(src[i:], "var(")
		if j < 0 {
			return out
		}
		at := i + j
		i = at + 4
		if at > 0 && isIdentByte(src[at-1]) {
			continue
		}
		k := skipSpace(src, at+4)
		if !strings.HasPrefix(src[k:], "--") {
			continue
		}
		n := k + 2
		for n < len(src) && isIdentByte(src[n]) {
			n++
		}
		if n == k+2 {
			continue
		}
		v := cssVar{nameOff: k, name: src[k:n], fbOff: -1}
		if c := skipSpace(src, n); c < len(src) && src[c] == ',' {
			start := skipSpace(src, c+1)
			end := fallbackEnd(src, start)
			v.fbOff, v.fallback = start, strings.TrimSpace(src[start:end])
		}
		out = append(out, v)
	}
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '\f') {
		i++
	}
	return i
}

// maxFallback bounds how far a fallback is searched for its ")", so a
// file of unclosed var( stays linear.
const maxFallback = 4096

// fallbackEnd is the offset of the ")" that closes the var( whose fallback
// starts at start. With none (a depth-0 ";", "{", "}" or "!" comes first,
// or none within maxFallback bytes) the fallback is unclosed: see
// unclosedEnd.
func fallbackEnd(s string, start int) int {
	depth := 0
	limit := min(len(s), start+maxFallback)
	for i := start; i < limit; i++ {
		switch c := s[i]; c {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		case '"', '\'':
			if q := strings.IndexByte(s[i+1:lineEnd(s, i+1)], c); q >= 0 {
				i += q + 1
			}
		case ';', '{', '}', '!':
			if depth == 0 {
				return unclosedEnd(s, start)
			}
		}
	}
	return unclosedEnd(s, start)
}

// unclosedEnd ends an unclosed fallback at its first depth-0 ";", "{",
// "}" or "!", or at the end of its line, whichever comes first.
func unclosedEnd(s string, start int) int {
	end, depth := lineEnd(s, start), 0
	for i := start; i < end; i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ';', '{', '}', '!':
			if depth <= 0 {
				return i
			}
		}
	}
	return end
}

// lineEnd is the offset of the newline ending the line holding i, or len(s).
func lineEnd(s string, i int) int {
	if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
		return i + nl
	}
	return len(s)
}

// stylesheetExts are the extensions whose /* … */ comments are blanked
// before scanning; in every other file type a comment is read as code.
var stylesheetExts = map[string]bool{".css": true, ".scss": true, ".sass": true, ".less": true}

// blankComments replaces each /* … */ comment in a stylesheet (an
// unclosed one runs to the end) with spaces, keeping newlines and so every
// byte offset, line and col. Quoted strings are skipped, so "/*" in one
// opens no comment.
func blankComments(src string) string {
	b := []byte(src)
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '"', '\'':
			q := b[i]
			for i++; i < len(b) && b[i] != q && b[i] != '\n'; i++ {
				if b[i] == '\\' {
					i++
				}
			}
		case '/':
			if i+1 >= len(b) || b[i+1] != '*' {
				continue
			}
			end := strings.Index(src[i+2:], "*/")
			stop := len(b)
			if end >= 0 {
				stop = i + 2 + end + 2
			}
			for ; i < stop; i++ {
				if b[i] != '\n' && b[i] != '\r' {
					b[i] = ' '
				}
			}
			i--
		}
	}
	return string(b)
}

func scanCSS(r *platformRun, f *file) error {
	names, err := r.s.refNames(r.pl, "css")
	if err != nil {
		return err
	}
	for _, v := range cssVars(f.code) {
		tok := names[v.name]
		switch {
		case tok != "":
			r.ref(f, v.nameOff, v.name, tok, Direct)
		case r.local[v.name]:
			continue // app-local: defined in a consumer file
		default:
			r.ref(f, v.nameOff, v.name, "", Direct)
			r.problem(f, v.nameOff, Undeclared, v.name, fmt.Sprintf(
				"%s is not a token, and no %s consumer file defines it", v.name, r.pl.Name))
		}
		if v.fbOff < 0 || v.fallback == "" || strings.HasPrefix(v.fallback, "var(") {
			continue
		}
		detail := fmt.Sprintf("var(%s) has the literal fallback %s; a fallback hides a missing token, so read another var() or none", v.name, v.fallback)
		if tok != "" && r.equalsToken(tok, v.name, v.fallback) {
			detail += " (it equals the token's base value)"
		}
		r.problem(f, v.fbOff, FallbackLiteral, v.fallback, detail)
	}
	return nil
}

// equalsToken reports whether the literal fallback means token tok's
// base (no mode set) value.
func (r *platformRun) equalsToken(tok, prop, fallback string) bool {
	v, err := r.s.p.Tokens.Resolve(tok, config.Context{})
	if err != nil {
		return false
	}
	val := emit.CSSValue(v)
	return val != "" && cssvalue.Equal(prop, css.Computed{Value: fallback}, css.Computed{Value: val})
}
