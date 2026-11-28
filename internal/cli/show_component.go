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
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/rulingref"
)

// Ruling statuses show reports besides rulingref's own four.
const (
	statusUnchecked  = "unchecked"  // no Gantry project to resolve against
	statusMissing    = "missing"    // the Gantry project has no such ruling
	statusUnreadable = "unreadable" // the ruling's record did not read
	statusMalformed  = "malformed"  // not @ruling:<id>
)

// shownRuling is a component's ruling as show reports it.
type shownRuling struct {
	Ref    string `json:"ref"`
	Status string `json:"status"`
	// SupersededBy is a superseded ruling's replacement, omitted when empty.
	SupersededBy string `json:"supersededBy,omitempty"`
}

// shownComponentToken is one token a component owns.
type shownComponentToken struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// shownConsumerFile is a consumer file that reads the component's tokens.
type shownConsumerFile struct {
	File       string `json:"file"`
	References int    `json:"references"`
}

// shownComponent is a component as show reports it. Every list is sorted
// and non-nil, so the JSON keys and shapes are stable.
type shownComponent struct {
	ID              string                         `json:"id"`
	Noun            string                         `json:"noun"`
	Ruling          shownRuling                    `json:"ruling"`
	Shared          bool                           `json:"shared"`
	HostedBy        string                         `json:"hostedBy"`
	Hosts           []string                       `json:"hosts"`
	Tokens          []shownComponentToken          `json:"tokens"`
	Owners          []string                       `json:"owners"`
	Implementations map[string]string              `json:"implementations"`
	Consumers       map[string][]shownConsumerFile `json:"consumers"`

	// unchecked says why the ruling was not resolved; text only.
	unchecked string
}

// describeComponent gathers what show reports about c. The error is the
// consumer scan's I/O failure.
func describeComponent(p *project.Project, c *component.Component) (shownComponent, error) {
	s := shownComponent{
		ID: c.ID, Noun: c.Noun, Shared: c.Shared, HostedBy: c.HostedBy,
		Ruling:          shownRuling{Ref: c.Ruling},
		Hosts:           []string{},
		Tokens:          []shownComponentToken{},
		Owners:          slices.Sorted(slices.Values(c.Owners)),
		Implementations: map[string]string{},
		Consumers:       map[string][]shownConsumerFile{},
	}
	if s.Owners == nil {
		s.Owners = []string{}
	}
	maps.Copy(s.Implementations, c.Implementations)
	for _, id := range slices.Sorted(maps.Keys(p.Components)) {
		if p.Components[id].HostedBy == c.ID {
			s.Hosts = append(s.Hosts, id)
		}
	}
	owned := map[string]bool{}
	for _, n := range slices.Sorted(slices.Values(c.Tokens)) {
		st := shownComponentToken{Name: n}
		if p.Tokens != nil && p.Tokens.Tokens[n] != nil {
			st.Tier = p.Tokens.Tokens[n].Tier
		}
		s.Tokens = append(s.Tokens, st)
		owned[n] = true
	}
	s.resolveRuling(p)
	if p.Tokens == nil || len(owned) == 0 {
		return s, nil
	}
	res, err := consumer.Scan(p)
	if err != nil {
		return s, err
	}
	counts := map[string]map[string]int{}
	for _, r := range res.Refs {
		if r.Token == "" || !owned[r.Token] {
			continue
		}
		if counts[r.Platform] == nil {
			counts[r.Platform] = map[string]int{}
		}
		counts[r.Platform][r.File]++
	}
	for pl, files := range counts {
		for _, f := range slices.Sorted(maps.Keys(files)) {
			s.Consumers[pl] = append(s.Consumers[pl], shownConsumerFile{File: f, References: files[f]})
		}
	}
	return s, nil
}

// resolveRuling sets the ruling's status from the Gantry project, when
// there is one.
func (s *shownComponent) resolveRuling(p *project.Project) {
	m := rulingref.RefRE.FindStringSubmatch(s.Ruling.Ref)
	if m == nil {
		s.Ruling.Status = statusMalformed
		return
	}
	id := m[1]
	var ix *rulingref.Index
	var ds []diag.Diagnostic
	if p.FS != nil {
		ix, ds = rulingref.Resolve(p.FS, p.Config)
	}
	if ix == nil {
		s.Ruling.Status = statusUnchecked
		s.unchecked = "no gantry.yaml"
		if len(ds) > 0 {
			s.unchecked = "gantry.yaml did not load"
		}
		return
	}
	status, by, found := ix.Lookup(id)
	switch {
	case found:
		s.Ruling.Status, s.Ruling.SupersededBy = status, by
	case ix.Unreadable(id):
		s.Ruling.Status = statusUnreadable
	default:
		s.Ruling.Status = statusMissing
	}
}

// orList is items joined with ", ", or "-" when there are none.
func orList(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

// writeShownComponent renders s as show's text output.
func writeShownComponent(w interface{ Write([]byte) (int, error) }, s shownComponent) {
	var b strings.Builder
	ruling := s.Ruling.Ref + " (" + s.Ruling.Status + ")"
	switch {
	case s.Ruling.Status == statusUnchecked:
		ruling = s.Ruling.Ref + " (not checked: " + s.unchecked + ")"
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
	s, err := describeComponent(p, c)
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
