// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/tokens"
)

// Camel spells a token name in lower camel case ("accent-ink" →
// "accentInk", "space-4" → "space4"); see config.Camel.
func Camel(name string) string { return config.Camel(name) }

// Pascal spells a token name in upper camel case ("accent-ink" →
// "AccentInk"); see config.Pascal.
func Pascal(name string) string { return config.Pascal(name) }

// Apply fills a naming template's placeholder ({name}, {camel} or
// {Pascal}) with name and splits the result at its last "." into the type
// and member parts: "DesignTokens.{camel}" gives ("DesignTokens",
// "accentInk"). A template with no "." has no type part.
func Apply(template, name string) (typ, member string) {
	s := config.FillNaming(template, name)
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// NameFor is t's member name in format: its own names entry for the format
// when it has one, else the member part of the config's naming template.
func NameFor(cfg *config.Config, t *tokens.Token, format string) string {
	if n, ok := t.Names[format]; ok {
		return n
	}
	_, member := Apply(cfg.NamingFor(format), t.Name)
	return member
}

// swiftKeywords are Swift's reserved words, which an identifier may only
// use in backticks.
var swiftKeywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`associatedtype class deinit enum extension fileprivate func import init
		inout internal let open operator private precedencegroup protocol public rethrows static struct
		subscript typealias var break case catch continue default defer do else fallthrough for guard if
		in repeat return throw switch where while Any as await false is nil self Self super throws true
		try Type`) {
		swiftKeywords[k] = true
	}
}

// SwiftIdent escapes what Swift reserves in an identifier: a reserved word
// is wrapped in backticks and a leading digit gets a "_" prefix. It does
// not make any string an identifier ("accent-ink" stays as it is); the
// Swift emitter rejects what is still not one.
func SwiftIdent(s string) string {
	switch {
	case swiftKeywords[s]:
		return "`" + s + "`"
	case s != "" && s[0] >= '0' && s[0] <= '9':
		return "_" + s
	}
	return s
}
