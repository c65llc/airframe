// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/report"
)

// orList is items joined with ", ", or "-" when there are none.
func orList(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

// writeShownComponent renders s as show's text output.
func writeShownComponent(w interface{ Write([]byte) (int, error) }, s report.Component) {
	var b strings.Builder
	ruling := s.Ruling.Ref + " (" + s.Ruling.Status + ")"
	switch {
	case s.Ruling.Status == report.StatusUnchecked:
		ruling = s.Ruling.Ref + " (not checked: " + s.Unchecked + ")"
	case s.Ruling.SupersededBy != "":
		ruling = s.Ruling.Ref + " (superseded by " + s.Ruling.SupersededBy + ")"
	}
	fmt.Fprintf(&b, "%s\nnoun: %s\nruling: %s\nshared: %t\nhostedBy: %s\nhosts: %s\n",
		s.ID, orNone(s.Noun), ruling, s.Shared, orNone(s.HostedBy), orList(s.Hosts))
	b.WriteString("tokens:")
	if len(s.Tokens) == 0 {
		b.WriteString(" -")
	}
	b.WriteString("\n")
	for _, t := range s.Tokens {
		fmt.Fprintf(&b, "  %s: %s\n", t.Name, orNone(t.Tier))
	}
	b.WriteString("owners:")
	if len(s.Owners) == 0 {
		b.WriteString(" -")
	}
	b.WriteString("\n")
	for _, o := range s.Owners {
		fmt.Fprintf(&b, "  %s\n", o)
	}
	b.WriteString("implementations:")
	if len(s.Implementations) == 0 {
		b.WriteString(" -")
	}
	b.WriteString("\n")
	for _, pl := range slices.Sorted(maps.Keys(s.Implementations)) {
		fmt.Fprintf(&b, "  %s: %s\n", pl, s.Implementations[pl])
	}
	b.WriteString("consumers:")
	if len(s.Consumers) == 0 {
		b.WriteString(" -")
	}
	b.WriteString("\n")
	for _, pl := range slices.Sorted(maps.Keys(s.Consumers)) {
		fmt.Fprintf(&b, "  %s:\n", pl)
		for _, f := range s.Consumers[pl] {
			noun := "references"
			if f.References == 1 {
				noun = "reference"
			}
			fmt.Fprintf(&b, "    %s: %d %s\n", f.File, f.References, noun)
		}
	}
	_, _ = w.Write([]byte(b.String()))
}

// runShowComponent is show's component mode for c.
func (inv *invocation) runShowComponent(p *project.Project, c *component.Component) int {
	s, err := report.DescribeComponent(p, c)
	if err != nil {
		return inv.fail(fmt.Errorf("scan consumers: %w", err))
	}
	if *inv.bools["json"] {
		enc := json.NewEncoder(inv.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(s) // a write error is Main's to report
		return ExitOK
	}
	writeShownComponent(inv.stdout, s)
	return ExitOK
}
