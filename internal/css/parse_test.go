// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package css

import (
	"slices"
	"strings"
	"testing"

	"github.com/c65llc/airframe/internal/diag"
)

func mustParse(t *testing.T, src string) *Sheet {
	t.Helper()
	s, ds := Parse("tokens.css", []byte(src))
	if len(ds) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", ds)
	}
	return s
}

func TestParseRootBlock(t *testing.T) {
	s := mustParse(t, ":root { --a: 1px; color: red }")
	if s.File != "tokens.css" {
		t.Errorf("File = %q", s.File)
	}
	if len(s.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(s.Rules))
	}
	r := s.Rules[0]
	if !slices.Equal(r.Selectors, []string{":root"}) {
		t.Errorf("Selectors = %q", r.Selectors)
	}
	if len(r.Decls) != 1 || r.Decls[0].Name != "--a" || r.Decls[0].Value != "1px" {
		t.Fatalf("Decls = %+v", r.Decls)
	}
	if s.Ignored != 1 {
		t.Errorf("Ignored = %d, want 1", s.Ignored)
	}
}

func TestParseMediaNesting(t *testing.T) {
	s := mustParse(t, "@media (prefers-color-scheme: dark) { :root:not([data-theme='light']) { --a: 2px } }")
	if len(s.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(s.Rules))
	}
	r := s.Rules[0]
	if !slices.Equal(r.Media, []string{"(prefers-color-scheme: dark)"}) {
		t.Errorf("Media = %q", r.Media)
	}
	if !slices.Equal(r.Selectors, []string{":root:not([data-theme='light'])"}) {
		t.Errorf("Selectors = %q", r.Selectors)
	}
	if len(r.Decls) != 1 || r.Decls[0].Value != "2px" {
		t.Errorf("Decls = %+v", r.Decls)
	}
}

func TestParseNestedMediaConcatenates(t *testing.T) {
	s := mustParse(t, `@media (prefers-color-scheme: dark) {
  @media (pointer: coarse) { :root { --a: 1 } }
  :root { --b: 2 }
}
:root { --c: 3 }`)
	if len(s.Rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(s.Rules))
	}
	want := [][]string{
		{"(prefers-color-scheme: dark)", "(pointer: coarse)"},
		{"(prefers-color-scheme: dark)"},
		nil,
	}
	for i, w := range want {
		if !slices.Equal(s.Rules[i].Media, w) {
			t.Errorf("rule %d Media = %q, want %q", i, s.Rules[i].Media, w)
		}
	}
}

func TestParseSelectorList(t *testing.T) {
	s := mustParse(t, `:root, :root[data-theme="dark"] { --a: 1 }`)
	if len(s.Rules) != 1 {
		t.Fatalf("rules = %d", len(s.Rules))
	}
	want := []string{":root", `:root[data-theme="dark"]`}
	if !slices.Equal(s.Rules[0].Selectors, want) {
		t.Errorf("Selectors = %q, want %q", s.Rules[0].Selectors, want)
	}
	// A comma inside a string or brackets does not split.
	s = mustParse(t, `:root[data-x="a,b"], html { --a: 1 }`)
	want = []string{`:root[data-x="a,b"]`, "html"}
	if !slices.Equal(s.Rules[0].Selectors, want) {
		t.Errorf("Selectors = %q, want %q", s.Rules[0].Selectors, want)
	}
}

func TestParseCommentsAndStrings(t *testing.T) {
	s := mustParse(t, `/* a } b; */ :root { /* the ink */ --font: "A; B", sans-serif; --x: var(--y, "}") }`)
	if len(s.Rules) != 1 {
		t.Fatalf("rules = %d", len(s.Rules))
	}
	ds := s.Rules[0].Decls
	if len(ds) != 2 {
		t.Fatalf("Decls = %+v", ds)
	}
	if ds[0].Name != "--font" || ds[0].Value != `"A; B", sans-serif` {
		t.Errorf("decl 0 = %+v", ds[0])
	}
	if !strings.Contains(ds[0].Comment, "the ink") {
		t.Errorf("decl 0 Comment = %q", ds[0].Comment)
	}
	if ds[1].Name != "--x" || ds[1].Value != `var(--y, "}")` {
		t.Errorf("decl 1 = %+v", ds[1])
	}
	if ds[1].Comment != "" {
		t.Errorf("decl 1 Comment = %q, want empty", ds[1].Comment)
	}
}

func TestParseCommentAttachment(t *testing.T) {
	s := mustParse(t, `/* between rules */
:root {
  /* not mine */ color: red;
  /* first */ /* second */
  --a: 1;
  --b: 2 /* inside value */;
  --c: 3; /* trailing, attaches to d */ --d: 4;
}`)
	ds := s.Rules[0].Decls
	got := map[string]string{}
	for _, d := range ds {
		got[d.Name] = d.Comment
	}
	if got["--a"] != "/* second */" {
		t.Errorf("--a Comment = %q", got["--a"])
	}
	if got["--b"] != "" || got["--c"] != "" {
		t.Errorf("--b/--c Comments = %q / %q", got["--b"], got["--c"])
	}
	if !strings.Contains(got["--d"], "trailing") {
		t.Errorf("--d Comment = %q", got["--d"])
	}
	if ds[1].Value != "2" {
		t.Errorf("--b Value = %q", ds[1].Value)
	}
}

func TestParseUnsupportedAtRule(t *testing.T) {
	s, ds := Parse("tokens.css", []byte("@supports (x) { :root { --a: 1 } }"))
	if len(ds) != 1 || ds[0].Rule != "import-unsupported-at-rule" || ds[0].Line != 1 {
		t.Fatalf("diagnostics = %+v", ds)
	}
	if ds[0].Severity != diag.Error || ds[0].File != "tokens.css" || ds[0].Docs != diag.DocsURL("import-unsupported-at-rule") {
		t.Errorf("diagnostic = %+v", ds[0])
	}
	if !strings.Contains(ds[0].Message, "@supports") {
		t.Errorf("Message = %q", ds[0].Message)
	}
	if len(s.Rules) != 0 {
		t.Errorf("rules = %+v, want none", s.Rules)
	}
}

func TestParseUnsupportedAtRuleContinues(t *testing.T) {
	s, ds := Parse("tokens.css", []byte(`@import "x.css";
@layer base { :root { --a: 1 } }
:root { --b: 2 }`))
	if len(ds) != 2 || ds[0].Line != 1 || ds[1].Line != 2 {
		t.Fatalf("diagnostics = %+v", ds)
	}
	for _, d := range ds {
		if d.Rule != "import-unsupported-at-rule" {
			t.Errorf("rule = %q", d.Rule)
		}
	}
	if len(s.Rules) != 1 || s.Rules[0].Decls[0].Name != "--b" || s.Rules[0].Line != 3 {
		t.Errorf("rules = %+v", s.Rules)
	}
}

func TestParseLinesAndOrder(t *testing.T) {
	s := mustParse(t, `:root { --a: 1;
  color: red; --b: 2 }
@media (pointer: coarse) { :root { --c: 3 } }`)
	if len(s.Rules) != 2 {
		t.Fatalf("rules = %d", len(s.Rules))
	}
	if s.Rules[0].Line != 1 || s.Rules[1].Line != 3 {
		t.Errorf("rule lines = %d, %d", s.Rules[0].Line, s.Rules[1].Line)
	}
	type lo struct {
		name        string
		line, order int
	}
	var got []lo
	for _, r := range s.Rules {
		for _, d := range r.Decls {
			got = append(got, lo{d.Name, d.Line, d.Order})
		}
	}
	want := []lo{{"--a", 1, 0}, {"--b", 2, 1}, {"--c", 3, 2}}
	if !slices.Equal(got, want) {
		t.Errorf("decls = %+v, want %+v", got, want)
	}
}

func TestParseSyntaxErrors(t *testing.T) {
	cases := []struct {
		name, src string
		line      int
		msg       string
		rules     int
	}{
		{"unclosed brace", ":root { --a: 1 }\n:root {\n --b: 2;", 2, "unclosed '{'", 2},
		{"unmatched brace", ":root { --a: 1 }\n}\n:root { --b: 2 }", 2, "unmatched '}'", 1},
		{"unterminated comment", ":root { --a: 1 }\n/* oops", 2, "unterminated comment", 1},
		{"unterminated string", ":root {\n --a: \"x;\n}", 2, "unterminated string", 1},
		{"unclosed paren", ":root {\n --a: var(--b;\n}", 2, "unclosed '('", 1},
		{"missing block", ":root { --a: 1 }\n:root", 2, "expected '{'", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, ds := Parse("tokens.css", []byte(c.src))
			if len(ds) != 1 || ds[0].Rule != "import-css-syntax" || ds[0].Severity != diag.Error {
				t.Fatalf("diagnostics = %+v", ds)
			}
			if ds[0].Line != c.line || !strings.Contains(ds[0].Message, c.msg) {
				t.Errorf("diagnostic = line %d %q, want line %d containing %q", ds[0].Line, ds[0].Message, c.line, c.msg)
			}
			if len(s.Rules) != c.rules {
				t.Errorf("rules = %d, want %d (what parsed before the error)", len(s.Rules), c.rules)
			}
		})
	}
}

func TestParseNestedRule(t *testing.T) {
	s, ds := Parse("tokens.css", []byte(":root {\n --a: 1;\n .x { --b: 2 }\n --c: 3;\n}"))
	if len(ds) != 1 || ds[0].Rule != "import-css-syntax" || ds[0].Line != 3 || !strings.Contains(ds[0].Message, "nested rules are not supported") {
		t.Fatalf("diagnostics = %+v", ds)
	}
	var names []string
	for _, d := range s.Rules[0].Decls {
		names = append(names, d.Name)
	}
	if !slices.Equal(names, []string{"--a", "--c"}) {
		t.Errorf("decls = %q", names)
	}
}

func TestParseEmpty(t *testing.T) {
	s := mustParse(t, "  /* only a comment */\n")
	if len(s.Rules) != 0 || s.Ignored != 0 {
		t.Errorf("sheet = %+v", s)
	}
}

func TestParseUnclosedBracketIgnoresStrings(t *testing.T) {
	_, ds := Parse("tokens.css", []byte(":root {\n --a: \"(\" x[1;\n}"))
	if len(ds) != 1 || ds[0].Line != 2 || !strings.Contains(ds[0].Message, "unclosed '['") {
		t.Fatalf("diagnostics = %+v", ds)
	}
}
