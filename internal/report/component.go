// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package report

import (
	"maps"
	"slices"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/rulingref"
)

// Ruling statuses show reports besides rulingref's own four.
const (
	StatusUnchecked  = "unchecked"  // no Gantry project to resolve against
	StatusMissing    = "missing"    // the Gantry project has no such ruling
	StatusUnreadable = "unreadable" // the ruling's record did not read
	StatusMalformed  = "malformed"  // not @ruling:<id>
)

// Ruling is a component's ruling as show reports it.
type Ruling struct {
	Ref    string `json:"ref"`
	Status string `json:"status"`
	// SupersededBy is a superseded ruling's replacement, omitted when empty.
	SupersededBy string `json:"supersededBy,omitempty"`
}

// ComponentToken is one token a component owns.
type ComponentToken struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// ConsumerFile is a consumer file that reads the component's tokens.
type ConsumerFile struct {
	File       string `json:"file"`
	References int    `json:"references"`
}

// Component is a component as show reports it. Every list is sorted and
// non-nil, so the JSON keys and shapes are stable.
type Component struct {
	ID              string                    `json:"id"`
	Noun            string                    `json:"noun"`
	Ruling          Ruling                    `json:"ruling"`
	Shared          bool                      `json:"shared"`
	HostedBy        string                    `json:"hostedBy"`
	Hosts           []string                  `json:"hosts"`
	Tokens          []ComponentToken          `json:"tokens"`
	Owners          []string                  `json:"owners"`
	Implementations map[string]string         `json:"implementations"`
	Consumers       map[string][]ConsumerFile `json:"consumers"`

	// Unchecked says why the ruling was not resolved; text output only.
	Unchecked string `json:"-"`
}

// DescribeComponent gathers what show reports about c. The error is the
// consumer scan's I/O failure.
func DescribeComponent(p *project.Project, c *component.Component) (Component, error) {
	s := Component{
		ID: c.ID, Noun: c.Noun, Shared: c.Shared, HostedBy: c.HostedBy,
		Ruling:          Ruling{Ref: c.Ruling},
		Hosts:           []string{},
		Tokens:          []ComponentToken{},
		Owners:          slices.Sorted(slices.Values(c.Owners)),
		Implementations: map[string]string{},
		Consumers:       map[string][]ConsumerFile{},
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
		st := ComponentToken{Name: n}
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
			s.Consumers[pl] = append(s.Consumers[pl], ConsumerFile{File: f, References: files[f]})
		}
	}
	return s, nil
}

// resolveRuling sets the ruling's status from the Gantry project, when
// there is one.
func (s *Component) resolveRuling(p *project.Project) {
	m := rulingref.RefRE.FindStringSubmatch(s.Ruling.Ref)
	if m == nil {
		s.Ruling.Status = StatusMalformed
		return
	}
	id := m[1]
	var ix *rulingref.Index
	var ds []diag.Diagnostic
	if p.FS != nil {
		ix, ds = rulingref.Resolve(p.FS, p.Config)
	}
	if ix == nil {
		s.Ruling.Status = StatusUnchecked
		s.Unchecked = "no gantry.yaml"
		if len(ds) > 0 {
			s.Unchecked = "gantry.yaml did not load"
		}
		return
	}
	status, by, found := ix.Lookup(id)
	switch {
	case found:
		s.Ruling.Status, s.Ruling.SupersededBy = status, by
	case ix.Unreadable(id):
		s.Ruling.Status = StatusUnreadable
	default:
		s.Ruling.Status = StatusMissing
	}
}
