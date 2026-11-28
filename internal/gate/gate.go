// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package gate writes a lint run in Gantry's gate protocol (Gantry spec
// §6.6), so `gantry sweep` can run Airframe as one gate and refuse to
// believe a clean result that read nothing or a failed self-test.
package gate

import (
	"encoding/json"
	"io"

	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/selftest"
)

// Name is the gate's name in the protocol.
const Name = "airframe"

type check struct {
	ID      string `json:"id"`
	Scanned int    `json:"scanned"`
}

type selftestJSON struct {
	Flagged  int      `json:"flagged"`
	Ignored  int      `json:"ignored"`
	OK       bool     `json:"ok"`
	Failures []string `json:"failures,omitempty"`
}

type scanned struct {
	Files int `json:"files"`
	Items int `json:"items"`
}

type finding struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
	Rule    string `json:"rule"`
}

type exemption struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Marker string `json:"marker"`
	Reason string `json:"reason"`
}

// notApplicable is Airframe's addition to the protocol: a rule that had
// nothing to scan, and why. Gantry ignores it as an unknown field.
type notApplicable struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type document struct {
	Gate          string          `json:"gate"`
	Checks        []check         `json:"checks"`
	Selftest      selftestJSON    `json:"selftest"`
	Scanned       scanned         `json:"scanned"`
	Findings      []finding       `json:"findings"`
	Exemptions    []exemption     `json:"exemptions"`
	NotApplicable []notApplicable `json:"notApplicable"`
}

// Write writes r as gate JSON with a two-space indent. Checks are the
// rules that are applicable (scanned something, or reported a finding), in
// r's tally order; every other rule is in notApplicable with its tally's
// reason, so each rule is in exactly one. scanned.files is files, the
// number of files the project loaded; scanned.items sums every rule's
// items. Findings are every reported diagnostic; exemptions come from r,
// which already leaves out those of rules set off. Empty lists are written
// as [], never null. st.Failures is written only when there are some.
func Write(w io.Writer, r lint.Result, st selftest.Result, files int) error {
	doc := document{
		Gate:          Name,
		Checks:        []check{},
		Selftest:      selftestJSON{Flagged: st.Flagged, Ignored: st.Ignored, OK: st.OK, Failures: st.Failures},
		Scanned:       scanned{Files: files},
		Findings:      []finding{},
		Exemptions:    []exemption{},
		NotApplicable: []notApplicable{},
	}
	for _, t := range r.Tallies {
		doc.Scanned.Items += t.Items
		if t.NotApplicable == "" { // lint.Run gives every rule that ran nothing, and found nothing, a reason
			doc.Checks = append(doc.Checks, check{ID: t.Rule, Scanned: t.Items})
		} else {
			doc.NotApplicable = append(doc.NotApplicable, notApplicable{ID: t.Rule, Reason: t.NotApplicable})
		}
	}
	for _, d := range r.Diagnostics {
		doc.Findings = append(doc.Findings, finding{File: d.File, Line: d.Line, Message: d.Message, Rule: d.Rule})
	}
	for _, e := range r.Exemptions {
		doc.Exemptions = append(doc.Exemptions, exemption{File: e.File, Line: e.Line, Marker: e.Marker, Reason: e.Reason})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}
