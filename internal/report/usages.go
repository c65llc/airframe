// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package report

import (
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// UsageRef is one consumer read of a token. A read of a component-tier
// token carries exactly one of Owner and Shared: Shared (true) when the
// component is shared, so any file may read it; otherwise Owner, whether
// the file may read it as one of the component's owners (lint.CanRead).
type UsageRef struct {
	Platform string `json:"platform"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Raw      string `json:"raw"`
	Via      string `json:"via"`
	Owner    *bool  `json:"owner,omitempty"`
	Shared   bool   `json:"shared,omitempty"`
}

// UsageAlias is a token whose value (Key: "default" or a mode key) aliases
// the token directly.
type UsageAlias struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// Usages is everything usages reports about one token.
type Usages struct {
	Name       string       `json:"name"`
	Tier       string       `json:"tier"`
	Component  string       `json:"component"`
	References []UsageRef   `json:"references"`
	AliasedBy  []UsageAlias `json:"aliasedBy"`
}

// DescribeUsages gathers the reads and direct aliases of t. platform, when
// not empty, keeps only that platform's consumer reads. The error is the
// consumer scan's I/O failure.
func DescribeUsages(p *project.Project, t *tokens.Token, platform string) (Usages, error) {
	u := Usages{Name: t.Name, Tier: t.Tier, Component: t.Component, References: []UsageRef{}, AliasedBy: []UsageAlias{}}
	res, err := consumer.Scan(p)
	if err != nil {
		return u, err
	}
	comp := p.Components[t.Component]
	for _, r := range res.Refs {
		if r.Token != t.Name || (platform != "" && r.Platform != platform) {
			continue
		}
		ur := UsageRef{Platform: r.Platform, File: r.File, Line: r.Line, Col: r.Col, Raw: r.Raw, Via: r.Via}
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
			u.AliasedBy = append(u.AliasedBy, UsageAlias{Name: o.Name, Key: key, File: p.Tokens.FileOf(v), Line: v.Line})
		}
	}
	slices.SortFunc(u.AliasedBy, func(a, b UsageAlias) int {
		return strings.Compare(a.Name+"\x00"+a.Key, b.Name+"\x00"+b.Key)
	})
	return u, nil
}
