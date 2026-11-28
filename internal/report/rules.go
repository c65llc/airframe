// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package report

import (
	"slices"
	"strings"

	"github.com/c65llc/airframe/docs"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/lint"
)

// RuleEntry is one rule ID in the rules --json report. Kind is "check" for
// a registered rule, "load" for one reported while loading and "build" for
// one reported while building.
type RuleEntry struct {
	ID      string        `json:"id"`
	Kind    string        `json:"kind"`
	Default diag.Severity `json:"default"`
	Summary string        `json:"summary"`
	Docs    string        `json:"docs"`
}

// RulesReport is the rules --json document.
type RulesReport struct {
	Version int         `json:"version"`
	Rules   []RuleEntry `json:"rules"`
}

// Rules is the rules --json document: every rule ID, sorted.
func Rules() RulesReport { return RulesReport{Version: 1, Rules: RuleEntries()} }

// RuleEntries lists every rule ID, sorted. A load-time ID is always an
// error and a build-time one a warning; their summary is the line under
// the docs page's title.
func RuleEntries() []RuleEntry {
	registered := map[string]lint.Rule{}
	for _, r := range lint.Rules() {
		registered[r.ID] = r
	}
	var out []RuleEntry
	for _, id := range lint.AllIDs() {
		e := RuleEntry{ID: id, Kind: "load", Default: lint.DefaultSeverity(id), Docs: diag.DocsURL(id)}
		if slices.Contains(lint.BuildWarningIDs(), id) {
			e.Kind = "build"
		}
		if r, ok := registered[id]; ok {
			e.Kind, e.Default, e.Summary = "check", r.Default, r.Summary
		} else if page, ok := docs.Rule(id); ok {
			e.Summary = DocSummary(page)
		}
		out = append(out, e)
	}
	return out
}

// PageBody is a docs page without its front matter.
func PageBody(page string) string {
	if rest, ok := strings.CutPrefix(page, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			page = strings.TrimLeft(body, "\n")
		}
	}
	return page
}

// DocSummary is the first paragraph line after a docs page's H1.
func DocSummary(page string) string {
	lines := strings.Split(PageBody(page), "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "# ") {
			continue
		}
		for _, next := range lines[i+1:] {
			if next != "" {
				return next
			}
		}
	}
	return ""
}
