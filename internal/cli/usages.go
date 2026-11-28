// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// usageRef is one consumer read of a token. A read of a component-tier
// token carries exactly one of Owner and Shared: Shared (true) when the
// component is shared, so any file may read it; otherwise Owner, whether
// the file may read it as one of the component's owners (lint.CanRead).
type usageRef struct {
	Platform string `json:"platform"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Raw      string `json:"raw"`
	Via      string `json:"via"`
	Owner    *bool  `json:"owner,omitempty"`
	Shared   bool   `json:"shared,omitempty"`
}

// usageAlias is a token whose value (Key: "default" or a mode key) aliases
// the token directly.
type usageAlias struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// usages is everything usages reports about one token.
type usages struct {
	Name       string       `json:"name"`
	Tier       string       `json:"tier"`
	Component  string       `json:"component"`
	References []usageRef   `json:"references"`
	AliasedBy  []usageAlias `json:"aliasedBy"`
}

// describeUsages gathers the reads and direct aliases of t. platform, when
// not empty, keeps only that platform's consumer reads.
func describeUsages(p *project.Project, t *tokens.Token, platform string) (usages, error) {
	u := usages{Name: t.Name, Tier: t.Tier, Component: t.Component, References: []usageRef{}, AliasedBy: []usageAlias{}}
	res, err := consumer.Scan(p)
	if err != nil {
		return u, err
	}
	comp := p.Components[t.Component]
	for _, r := range res.Refs {
		if r.Token != t.Name || (platform != "" && r.Platform != platform) {
			continue
		}
		ur := usageRef{Platform: r.Platform, File: r.File, Line: r.Line, Col: r.Col, Raw: r.Raw, Via: r.Via}
		switch {
		case t.Tier != "component":
		case comp != nil && comp.Shared:
			ur.Shared = true
		default:
			ok, _ := lint.CanRead(p, r.File, t)
			ur.Owner = &ok
		}
		u.References = append(u.References, ur)
	}
	for _, name := range p.Tokens.Order {
		o := p.Tokens.Tokens[name]
		keys, vals := o.Values()
		for i, v := range vals {
			if v.Alias != t.Name {
				continue
			}
			key := "default"
			if keys[i] != "" {
				key = string(keys[i])
			}
			u.AliasedBy = append(u.AliasedBy, usageAlias{Name: o.Name, Key: key, File: p.Tokens.FileOf(v), Line: v.Line})
		}
	}
	slices.SortFunc(u.AliasedBy, func(a, b usageAlias) int {
		return strings.Compare(a.Name+"\x00"+a.Key, b.Name+"\x00"+b.Key)
	})
	return u, nil
}

// writeUsages writes u as usages' text output.
func writeUsages(w io.Writer, u usages) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\ntier: %s\ncomponent: %s\n", u.Name, orNone(u.Tier), orNone(u.Component))
	if len(u.References) == 0 && len(u.AliasedBy) == 0 {
		b.WriteString("no usages\n")
		_, _ = io.WriteString(w, b.String())
		return
	}
	if len(u.References) > 0 {
		b.WriteString("references:\n")
	}
	for _, r := range u.References {
		fmt.Fprintf(&b, "  %s  %s:%d:%d  %s", r.Platform, r.File, r.Line, r.Col, r.Raw)
		if r.Via != consumer.Direct {
			fmt.Fprintf(&b, "  (via %s)", r.Via)
		}
		switch {
		case r.Shared:
			b.WriteString("  [shared]")
		case r.Owner == nil:
		case *r.Owner:
			b.WriteString("  [owner]")
		default:
			b.WriteString("  [not owner]")
		}
		b.WriteString("\n")
	}
	if len(u.AliasedBy) > 0 {
		b.WriteString("aliased by:\n")
	}
	for _, a := range u.AliasedBy {
		fmt.Fprintf(&b, "  %s  %s  %s:%d\n", a.Name, a.Key, a.File, a.Line)
	}
	_, _ = io.WriteString(w, b.String())
}

func runUsages(inv *invocation) int {
	if len(inv.args) != 1 {
		return inv.usageError("usages takes one token name")
	}
	p, t, code := inv.loadToken(inv.args[0])
	if t == nil {
		return code
	}
	platform := inv.value("platform")
	if platform != "" && !slices.ContainsFunc(p.Config.Platforms, func(pl config.Platform) bool { return pl.Name == platform }) {
		return inv.usageError("unknown platform %s", platform)
	}
	u, err := describeUsages(p, t, platform)
	if err != nil {
		return inv.fail(fmt.Errorf("scan consumers: %w", err))
	}
	if *inv.bools["json"] {
		enc := json.NewEncoder(inv.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(u) // a write error is Main's to report
		return ExitOK
	}
	writeUsages(inv.stdout, u)
	return ExitOK
}
