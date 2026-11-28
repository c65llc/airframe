// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokens"
)

// ownerGlob is the first of c's owners globs that matches file, and
// whether one does.
func ownerGlob(c *component.Component, file string) (string, bool) {
	for _, g := range c.Owners {
		if ok, _ := doublestar.Match(g, file); ok {
			return g, true
		}
	}
	return "", false
}

// CanRead reports whether the consumer file (a slash path relative to the
// project root) may read token t under the consumer rules: a component
// token only from a file its component's owners match (or any file when
// the component is shared); any other token only when its tier's
// readableBy lists consumers. why says so either way, and notes when file
// matches no platform's consumers globs (lint would not scan it, but a
// token readable by consumers is still allowed).
//
// It is conservative where lint is silent, refusing two reads that
// token-owner-violation does not report: a component-tier token that names
// no component, and a token whose components/<id>.yaml failed to load
// (lint leaves that file to schema-invalid).
func CanRead(p *project.Project, file string, t *tokens.Token) (bool, string) {
	switch {
	case t.Tier == "":
		return false, "no tier: consumers may not read it"
	case t.Tier == tierComponent && t.Component == "":
		return false, "component-tier token with no component: no owners to read it"
	case t.Tier == tierComponent:
		comp := p.Components[t.Component]
		switch {
		case comp == nil:
			return false, fmt.Sprintf("component %s: no component file declares its owners", t.Component)
		case comp.Shared:
			return true, fmt.Sprintf("component %s: shared", t.Component)
		}
		if g, ok := ownerGlob(comp, file); ok {
			return true, fmt.Sprintf("component %s: owners include %s", t.Component, g)
		}
		return false, fmt.Sprintf("component %s: owners (%s) do not match %s", t.Component, strings.Join(comp.Owners, ", "), file)
	}
	if !slices.Contains(p.Config.Tiers[t.Tier].ReadableBy, "consumers") {
		return false, t.Tier + ": consumers may not read"
	}
	if !isConsumerFile(p, file) {
		return true, t.Tier + ": consumers may read (not a consumer file of any platform)"
	}
	return true, t.Tier + ": consumers may read"
}

// isConsumerFile reports whether some platform's consumers globs match file.
func isConsumerFile(p *project.Project, file string) bool {
	for _, pl := range p.Config.Platforms {
		for _, g := range pl.Consumers {
			if ok, _ := doublestar.Match(g, file); ok {
				return true
			}
		}
	}
	return false
}
