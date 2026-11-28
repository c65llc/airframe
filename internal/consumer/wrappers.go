// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package consumer

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/config"
)

// nativeFormats are the formats whose names a generated wrapper's members
// are, tried in this order before css.
var nativeFormats = []string{"swift", "kotlin", "xaml"}

// rootRE matches a read of root.<member> that is not itself a member of
// something else: root is preceded by the start of a line or by a
// character that is neither a word character nor a ".".
func rootRE(root string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)(?:^|[^\w.])(` + regexp.QuoteMeta(root) + `)\.([A-Za-z_]\w*)`)
}

// wrappers finds the reads through each of the platform's wrapper roots in
// f, whatever its type. A longer root is read first and claims its
// position, so AppTheme.spacing.small is not also a read of a root
// AppTheme; nor is a position an adapter already read.
func (r *platformRun) wrappers(f *file) error {
	ws := slices.Clone(r.pl.Wrappers)
	slices.SortStableFunc(ws, func(a, b config.Wrapper) int {
		return cmp.Or(cmp.Compare(len(b.Root), len(a.Root)), cmp.Compare(a.Root, b.Root))
	})
	for _, w := range ws {
		for _, m := range rootRE(w.Root).FindAllStringSubmatchIndex(f.src, -1) {
			off, member := m[2], f.src[m[4]:m[5]]
			if r.claimed(f, off) {
				continue
			}
			raw, via := f.src[off:m[5]], "wrapper:"+w.Root
			tok, why, err := r.resolveMember(w, member)
			if err != nil {
				return err
			}
			if why != "" {
				r.ref(f, off, raw, "", via)
				r.problem(f, off, WrapperUnmapped, raw, fmt.Sprintf("%s: %s", raw, why))
				continue
			}
			r.ref(f, off, raw, tok, via)
		}
	}
	return nil
}

// resolveMember is the token member of wrapper w stands for, or why it
// stands for none. A map entry wins over the patterns, which are tried in
// order; a generated wrapper reads the platform's names, its native
// formats first and then css (as --<member>).
func (r *platformRun) resolveMember(w config.Wrapper, member string) (tok, why string, err error) {
	if w.Generated {
		for _, format := range append(slices.Clone(nativeFormats), "css") {
			if !slices.Contains(r.pl.Emit, format) {
				continue
			}
			names, err := r.s.refNames(r.pl, format)
			if err != nil {
				return "", "", err
			}
			key := member
			if format == "css" {
				key = "--" + member
			}
			if t, ok := names[key]; ok {
				if t == "" {
					return "", fmt.Sprintf("%s is a generated %s symbol of platform %s, not a token", member, format, r.pl.Name), nil
				}
				return t, "", nil
			}
		}
		return "", fmt.Sprintf("%s is not a name platform %s generates; wrapper %s is generated: true", member, r.pl.Name, w.Root), nil
	}
	tok, how := "", ""
	if t, ok := w.Map[member]; ok {
		tok, how = t, "map entry "+member
	} else {
		for i, pat := range w.Patterns {
			if g := pat.Match.FindStringSubmatch(member); g != nil {
				tok, how = fillTemplate(pat.Token, g), fmt.Sprintf("pattern %d (%s)", i+1, pat.Match)
				break
			}
		}
	}
	if how == "" {
		return "", fmt.Sprintf("wrapper %s maps no member %s: no map entry, no pattern matches; reads are always declared, never inferred", w.Root, member), nil
	}
	if _, ok := r.s.p.Tokens.Tokens[tok]; !ok {
		return "", fmt.Sprintf("wrapper %s's %s maps %s to %s, which is not a token", w.Root, how, member, tok), nil
	}
	return tok, "", nil
}

// fillTemplate replaces {1}…{9} in t with groups[1]…groups[9].
func fillTemplate(t string, groups []string) string {
	var b strings.Builder
	for i := 0; i < len(t); i++ {
		if t[i] == '{' && i+2 < len(t) && t[i+1] >= '1' && t[i+1] <= '9' && t[i+2] == '}' {
			if n, _ := strconv.Atoi(t[i+1 : i+2]); n < len(groups) {
				b.WriteString(groups[n])
			}
			i += 2
			continue
		}
		b.WriteByte(t[i])
	}
	return b.String()
}
