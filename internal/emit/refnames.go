// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package emit

import (
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/project"
)

// ReferenceNames maps every name format's emitter writes for platform pl
// that a consumer can write as the first segment after <Type>. (or after a
// declared wrapper root) to the token it stands for, or to "" for a
// generated symbol that is no token (a nested type, a parameter property,
// a ColorValues val, a theme dictionary key). Nested members, such as
// Shadow's fields or an enum's cases, are not first segments and are left
// out, so DesignTokens.x with no token x stays unmatched. It runs the emitter itself and reads the names it
// wrote, so the two cannot drift, and it fails with the emitter's own error
// when the emitter would fail. It is ReferenceVariants projected onto each
// Variant's Token.
//
//   - css: each custom property, as --<name>.
//   - swift: the <Type> struct's members, static and instance (a backticked
//     member also unescaped); the type, Bezier, Shadow, each runtime
//     parameter's enum and property, and init map to "".
//   - kotlin: the <Type> class's members and the <Type>Colors slots (a
//     member wins over a slot of the same name); <Type>, <Type>Colors,
//     <Type>ColorValues, Companion, Shadow, each parameter's enum class and
//     property, colors, and the ColorValues and scheme vals map to "".
//   - xaml: every resource key (base, runtime-suffixed, shadow brush and
//     part keys); the theme dictionary keys map to "".
//
// Any other format is an error.
func ReferenceNames(p *project.Project, pl config.Platform, format string) (map[string]string, error) {
	vs, err := ReferenceVariants(p, pl, format)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(vs))
	for n, v := range vs {
		out[n] = v.Token
	}
	return out, nil
}

// NamingAffixes are the literal parts of format's naming template around
// its placeholder ({name}, {camel} or {Pascal}): for swift and kotlin, of
// the member part after the last ".". Design{Pascal} gives ("Design", "");
// DesignTokens.{camel} gives ("", ""). A template with no placeholder is
// all prefix.
//
// The emitters append runtime suffixes (DesignGapCompact) and shadow
// Layer<N> suffixes (DesignCardShadowLayer1OffsetX) after the template's
// literal suffix, so a generated key matches prefix … suffix [tail], not
// prefix … suffix alone; the scanner's "ours" test must allow the tail.
func NamingAffixes(cfg *config.Config, format string) (prefix, suffix string) {
	tmpl := cfg.NamingFor(format)
	if i := strings.LastIndex(tmpl, "."); i >= 0 {
		tmpl = tmpl[i+1:]
	}
	for _, ph := range []string{"{name}", "{camel}", "{Pascal}"} {
		if before, after, ok := strings.Cut(tmpl, ph); ok {
			return before, after
		}
	}
	return tmpl, ""
}
