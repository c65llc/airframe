// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package css reads the subset of CSS that Airframe imports: custom
// properties in top-level rules and in rules inside (possibly nested)
// @media blocks. Parsing is structural only; selectors and media
// conditions are returned raw for the importer to validate.
package css

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
)

// Sheet is one parsed stylesheet. Ignored counts declarations that are not
// custom properties; they are not stored. Failed is set when Parse reported
// an error for it.
type Sheet struct {
	File    string
	Rules   []Rule
	Ignored int
	Failed  bool
}

// Rule is one style rule. Media holds the raw conditions of the enclosing
// @media blocks, outermost first. Selectors is the raw selector list split
// on top-level commas and trimmed. Line is the line the selector starts on.
type Rule struct {
	Media     []string
	Selectors []string
	Decls     []Decl
	Line      int
}

// Decl is one custom-property declaration. Name includes the leading "--"
// and Value is trimmed, with comments removed. Comment is the raw text,
// delimiters included, of the last comment in the same block that is
// followed only by whitespace before Name; failing that, of a trailing
// comment: one that starts on the line of the declaration's terminating
// ';' with only whitespace between. Otherwise it is "". A trailing comment
// never describes the next declaration, and one after a declaration that
// already has a comment is discarded. Order is the 0-based index of the
// declaration among all stored declarations in the sheet.
type Decl struct {
	Name, Value, Comment string
	Line, Order          int
}

// The diagnostic IDs Parse reports.
const (
	RuleUnsupportedAtRule = "import-unsupported-at-rule"
	RuleSyntax            = "import-css-syntax"
	RuleUnsupportedValue  = "import-unsupported-value"
)

// maxMediaDepth caps @media nesting so deep input cannot blow up parsing.
const maxMediaDepth = 64

// bom is the UTF-8 byte order mark, stripped from the start of the input.
const bom = "\xEF\xBB\xBF"

// Parse reads src. An at-rule other than @media is reported as
// import-unsupported-at-rule at its line and skipped. Malformed CSS (an
// unbalanced brace or parenthesis, an unterminated string or comment) is
// reported as import-css-syntax at the line where the problem starts, and
// Parse returns what it parsed before it. A rule nested inside a rule is
// reported as import-css-syntax and skipped, as is @media nested more than
// 64 levels deep. A custom property whose value ends in !important is
// reported as import-unsupported-value and not stored. A leading UTF-8 byte
// order mark is ignored.
func Parse(file string, src []byte) (*Sheet, []diag.Diagnostic) {
	src = bytes.TrimPrefix(src, []byte(bom))
	p := &parser{src: src, line: 1, sheet: &Sheet{File: file}}
	p.parseList(nil, 0)
	return p.sheet, p.diags
}

type parser struct {
	src   []byte
	pos   int
	line  int
	sheet *Sheet
	diags []diag.Diagnostic
	order int
	dead  bool // a fatal syntax error stopped parsing
}

func (p *parser) report(rule string, line int, format string, args ...any) {
	p.sheet.Failed = true
	p.diags = append(p.diags, diag.Diagnostic{
		Rule:     rule,
		Severity: diag.Error,
		File:     p.sheet.File,
		Line:     line,
		Message:  fmt.Sprintf(format, args...),
		Docs:     diag.DocsURL(rule),
	})
}

// fail reports a syntax error that stops parsing.
func (p *parser) fail(line int, format string, args ...any) {
	p.report(RuleSyntax, line, format, args...)
	p.dead = true
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peek(off int) byte {
	if p.pos+off < len(p.src) {
		return p.src[p.pos+off]
	}
	return 0
}

// advance consumes one byte, counting lines.
func (p *parser) advance() byte {
	c := p.src[p.pos]
	p.pos++
	if c == '\n' {
		p.line++
	}
	return c
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// comment consumes a comment starting at "/*" and returns its raw text.
func (p *parser) comment() (string, bool) {
	start, line := p.pos, p.line
	p.advance()
	p.advance()
	for !p.eof() {
		if p.peek(0) == '*' && p.peek(1) == '/' {
			p.advance()
			p.advance()
			return string(p.src[start:p.pos]), true
		}
		p.advance()
	}
	p.fail(line, "unterminated comment")
	return "", false
}

// str consumes a quoted string and returns its raw text, quotes included.
// An unescaped newline or the end of input leaves it unterminated.
func (p *parser) str() (string, bool) {
	start, line := p.pos, p.line
	quote := p.advance()
	for !p.eof() {
		c := p.peek(0)
		switch {
		case c == quote:
			p.advance()
			return string(p.src[start:p.pos]), true
		case c == '\n':
			p.fail(line, "unterminated string")
			return "", false
		case c == '\\' && p.pos+1 < len(p.src):
			p.advance()
		}
		p.advance()
	}
	p.fail(line, "unterminated string")
	return "", false
}

// skipSpace skips whitespace and comments and returns the raw text of the
// last comment, provided only whitespace follows it.
func (p *parser) skipSpace() string {
	_, last := p.skipSpaceAfter(0)
	return last
}

// skipSpaceAfter is skipSpace after a declaration's ';' on line term
// (0 for none). A comment that starts on line term, before any newline, is
// trailing: the first is returned as trailing and none is ever last.
func (p *parser) skipSpaceAfter(term int) (trailing, last string) {
	for !p.eof() && !p.dead {
		switch c := p.peek(0); {
		case isSpace(c):
			p.advance()
		case c == '/' && p.peek(1) == '*':
			onTerm := term > 0 && p.line == term
			text, _ := p.comment()
			switch {
			case !onTerm:
				last = text
			case trailing == "":
				trailing = text
			}
		default:
			return trailing, last
		}
	}
	return trailing, last
}

// scanUntil reads up to the first byte of stops that is outside strings,
// comments, parentheses and brackets, without consuming it. Comments are
// replaced by a space. It returns 0 as the stop byte at the end of input;
// an open parenthesis or bracket there is a fatal syntax error.
func (p *parser) scanUntil(stops string) (string, byte) {
	var b strings.Builder
	type opener struct {
		c    byte
		line int
	}
	var opens []opener // the open parentheses and brackets
	for !p.eof() {
		c := p.peek(0)
		switch {
		case c == '/' && p.peek(1) == '*':
			if _, ok := p.comment(); !ok {
				return b.String(), 0
			}
			b.WriteByte(' ')
			continue
		case c == '"' || c == '\'':
			s, ok := p.str()
			if !ok {
				return b.String(), 0
			}
			b.WriteString(s)
			continue
		case c == '\\' && p.pos+1 < len(p.src):
			b.WriteByte(p.advance())
		case len(opens) == 0 && strings.IndexByte(stops, c) >= 0:
			return b.String(), c
		case c == '(' || c == '[':
			opens = append(opens, opener{c, p.line})
		case (c == ')' || c == ']') && len(opens) > 0:
			opens = opens[:len(opens)-1]
		}
		b.WriteByte(p.advance())
	}
	if len(opens) > 0 {
		p.fail(opens[0].line, "unclosed '%c'", opens[0].c)
	}
	return b.String(), 0
}

// skipBlock consumes a block whose '{' (on line open) is already consumed,
// through its matching '}'.
func (p *parser) skipBlock(open int) {
	depth := 1
	for !p.eof() && !p.dead {
		switch c := p.peek(0); {
		case c == '/' && p.peek(1) == '*':
			p.comment()
		case c == '"' || c == '\'':
			p.str()
		case c == '\\' && p.pos+1 < len(p.src):
			p.advance()
			p.advance()
		case c == '{':
			depth++
			p.advance()
		case c == '}':
			p.advance()
			if depth--; depth == 0 {
				return
			}
		default:
			p.advance()
		}
	}
	if !p.dead {
		p.fail(open, "unclosed '{'")
	}
}

// parseList parses rules and at-rules until the end of input or, inside a
// block opened on line open (open > 0), its closing '}'.
func (p *parser) parseList(media []string, open int) {
	for !p.dead {
		p.skipSpace()
		if p.dead {
			return
		}
		if p.eof() {
			if open > 0 {
				p.fail(open, "unclosed '{'")
			}
			return
		}
		line := p.line
		switch p.peek(0) {
		case '}':
			if open == 0 {
				p.fail(line, "unmatched '}'")
				return
			}
			p.advance()
			return
		case '@':
			p.atRule(media, line)
		default:
			p.rule(media, line)
		}
	}
}

func (p *parser) atRule(media []string, line int) {
	prelude, stop := p.scanUntil(";{}")
	if p.dead {
		return
	}
	name := atName(prelude)
	if strings.EqualFold(name, "@media") && stop == '{' {
		p.advance()
		if len(media) >= maxMediaDepth {
			p.report(RuleSyntax, line, "nesting too deep (more than %d levels)", maxMediaDepth)
			p.skipBlock(line)
			return
		}
		cond := strings.TrimSpace(prelude[len(name):])
		p.parseList(append(slices.Clip(media), cond), line)
		return
	}
	p.unsupportedAtRule(name, stop, line)
}

// unsupportedAtRule reports the at-rule name on line and skips the rest of
// it: through its ';', or through its balanced block when stop is '{'.
func (p *parser) unsupportedAtRule(name string, stop byte, line int) {
	p.report(RuleUnsupportedAtRule, line, "the at-rule %s is not supported", name)
	switch stop {
	case '{':
		p.advance()
		p.skipBlock(line)
	case ';':
		p.advance()
	}
	// A '}' is left for the enclosing list; the end of input ends parsing.
}

// atName is the at-keyword that starts prelude, "@" included.
func atName(prelude string) string {
	i := 1
	for i < len(prelude) {
		c := prelude[i]
		if c != '-' && c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && c < 0x80 {
			break
		}
		i++
	}
	return prelude[:i]
}

func (p *parser) rule(media []string, line int) {
	prelude, stop := p.scanUntil(";{}")
	if p.dead {
		return
	}
	if stop != '{' {
		p.fail(line, "expected '{' after the selector %q", strings.TrimSpace(prelude))
		return
	}
	p.advance()
	r := Rule{Media: slices.Clone(media), Selectors: splitTop(prelude), Line: line}
	p.body(&r, line)
	p.sheet.Rules = append(p.sheet.Rules, r)
}

// body parses declarations through the closing '}' of a rule opened on
// line open.
func (p *parser) body(r *Rule, open int) {
	term, prev := 0, -1 // the line of the last declaration's ';', and its index in r.Decls
	for !p.dead {
		trailing, comment := p.skipSpaceAfter(term)
		if trailing != "" && prev >= 0 && r.Decls[prev].Comment == "" {
			r.Decls[prev].Comment = trailing
		}
		term, prev = 0, -1
		if p.dead {
			return
		}
		if p.eof() {
			p.fail(open, "unclosed '{'")
			return
		}
		switch p.peek(0) {
		case '}':
			p.advance()
			return
		case ';':
			p.advance()
			continue
		}
		line := p.line
		at := p.peek(0) == '@'
		text, stop := p.scanUntil(";{}")
		if p.dead {
			return
		}
		if at && stop != 0 {
			p.unsupportedAtRule(atName(text), stop, line)
			continue
		}
		switch stop {
		case '{':
			p.report(RuleSyntax, line, "nested rules are not supported")
			p.advance()
			p.skipBlock(line)
			continue
		case 0:
			p.fail(open, "unclosed '{'")
			return
		}
		stored := p.decl(r, text, comment, line)
		if stop == ';' {
			// The ';' ends the declaration; a comment after it on its
			// line is the declaration's own. A '}' is left for the loop.
			term = p.line
			p.advance()
			if stored {
				prev = len(r.Decls) - 1
			}
		}
	}
}

// decl parses one declaration's text and stores it in r if it is a valid
// custom property, reporting whether it did.
func (p *parser) decl(r *Rule, text, comment string, line int) bool {
	name, value, ok := strings.Cut(text, ":")
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, "--") {
		p.sheet.Ignored++
		return false
	}
	if !ok {
		p.report(RuleSyntax, line, "the declaration %s has no ':'", name)
		return false
	}
	value = strings.TrimSpace(value)
	if isImportant(value) {
		p.report(RuleUnsupportedValue, line, "!important is not supported: Airframe never emits it")
		return false
	}
	r.Decls = append(r.Decls, Decl{
		Name:    name,
		Value:   value,
		Comment: comment,
		Line:    line,
		Order:   p.order,
	})
	p.order++
	return true
}

// splitTop splits a selector list on commas outside strings, parentheses
// and brackets, trimming each part. Empty parts are kept, so "a,,b" has an
// empty selector for the importer to reject.
func splitTop(s string) []string {
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
		case c == '\\':
			i++
		case c == '(' || c == '[':
			depth++
		case (c == ')' || c == ']') && depth > 0:
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// isImportant reports whether the trimmed value ends in "!important",
// case-insensitively, with optional whitespace after the "!".
func isImportant(v string) bool {
	const kw = "important"
	if len(v) < len(kw) || !strings.EqualFold(v[len(v)-len(kw):], kw) {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(v[:len(v)-len(kw)], " \t\n\r\f"), "!")
}
