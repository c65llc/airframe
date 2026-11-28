// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package diag holds the findings every Airframe command reports, the
// per-rule tallies of what was scanned, and the text, JSON and GitHub
// reporters. The JSON shape mirrors Starboard's.
package diag

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Severity is how serious a finding is.
type Severity string

// The severities a rule can have. Off silences a rule.
const (
	Error Severity = "error"
	Warn  Severity = "warn"
	Off   Severity = "off"
)

// Edit replaces Old with New on one line of File.
type Edit struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// Fix describes a safe automatic fix for a finding.
type Fix struct {
	Description string `json:"description"`
	Edits       []Edit `json:"edits,omitempty"`
}

// Diagnostic is one finding. File is relative to the project root and uses
// "/". Path locates the value inside the file. Line is 1-based; 0 means
// unknown. Col is the 1-based byte column on Line, 0 when the finding has
// none (only a consumer-file reference has one). Docs is the rule's documentation page, DocsURL(Rule).
type Diagnostic struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	File     string   `json:"file"`
	Path     string   `json:"path"`
	Line     int      `json:"line,omitempty"`
	Col      int      `json:"col,omitempty"`
	Message  string   `json:"message"`
	Fix      *Fix     `json:"fix,omitempty"`
	Docs     string   `json:"docs,omitempty"`
}

// Tally records what one rule scanned, so a clean run never reads as "did
// nothing". NotApplicable, when non-empty, says why the rule had no input.
// Noun names the scanned items in text output and defaults to "items".
type Tally struct {
	Rule          string
	Files, Items  int
	Noun          string
	NotApplicable string
}

// docsBase is where the rule pages are published, one directory per rule.
const docsBase = "https://airframe.c65llc.com/rules/"

// DocsURL is the documentation page of the rule with the given ID.
func DocsURL(rule string) string { return docsBase + rule + "/" }

// Sort orders ds by File, Line, Col, Rule, then Message.
func Sort(ds []Diagnostic) {
	slices.SortStableFunc(ds, func(a, b Diagnostic) int {
		return cmp.Or(
			strings.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Col, b.Col),
			strings.Compare(a.Rule, b.Rule),
			strings.Compare(a.Message, b.Message),
		)
	})
}

// sorted returns a sorted copy of ds, leaving the caller's slice untouched.
func sorted(ds []Diagnostic) []Diagnostic {
	out := slices.Clone(ds)
	Sort(out)
	return out
}

// WriteText writes ds in sorted order, one per line, as
// "file:line: severity rule: message (path)" (file:line:col when the
// finding has a column), then one line per tally:
// "rule  N noun", or "rule  n/a (reason)" when the tally is not applicable.
func WriteText(w io.Writer, ds []Diagnostic, tallies []Tally) {
	var b strings.Builder
	for _, d := range sorted(ds) {
		b.WriteString(d.File)
		if d.Line > 0 {
			fmt.Fprintf(&b, ":%d", d.Line)
			if d.Col > 0 {
				fmt.Fprintf(&b, ":%d", d.Col)
			}
		}
		fmt.Fprintf(&b, ": %s %s: %s", d.Severity, d.Rule, d.Message)
		if d.Path != "" {
			fmt.Fprintf(&b, " (%s)", d.Path)
		}
		b.WriteByte('\n')
	}
	for _, t := range tallies {
		if t.NotApplicable != "" {
			fmt.Fprintf(&b, "%s  n/a (%s)\n", t.Rule, t.NotApplicable)
			continue
		}
		noun := t.Noun
		if noun == "" {
			noun = "items"
		}
		fmt.Fprintf(&b, "%s  %d %s\n", t.Rule, t.Items, noun)
	}
	_, _ = io.WriteString(w, b.String())
}

// report is the JSON document WriteJSON emits.
type report struct {
	Version  int          `json:"version"`
	Errors   []Diagnostic `json:"errors"`
	Warnings []Diagnostic `json:"warnings"`
}

// WriteJSON writes ds as {"version":1,"errors":[…],"warnings":[…]} with a
// two-space indent, each list in sorted order. Findings at Off are dropped.
func WriteJSON(w io.Writer, ds []Diagnostic) error {
	r := report{Version: 1, Errors: []Diagnostic{}, Warnings: []Diagnostic{}}
	for _, d := range sorted(ds) {
		switch d.Severity {
		case Error:
			r.Errors = append(r.Errors, d)
		case Warn:
			r.Warnings = append(r.Warnings, d)
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

var (
	dataEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	propEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

// WriteGitHub writes ds as GitHub Actions workflow commands, one per
// finding in sorted order: "::error file=F,line=N,title=RULE::message",
// with ",col=C" after the line when the finding has a column.
// Findings at Off are dropped.
func WriteGitHub(w io.Writer, ds []Diagnostic) {
	var b strings.Builder
	for _, d := range sorted(ds) {
		level := "error"
		switch d.Severity {
		case Error:
		case Warn:
			level = "warning"
		default:
			continue
		}
		fmt.Fprintf(&b, "::%s file=%s", level, propEscaper.Replace(d.File))
		if d.Line > 0 {
			fmt.Fprintf(&b, ",line=%d", d.Line)
			if d.Col > 0 {
				fmt.Fprintf(&b, ",col=%d", d.Col)
			}
		}
		fmt.Fprintf(&b, ",title=%s::%s\n", propEscaper.Replace(d.Rule), dataEscaper.Replace(d.Message))
	}
	_, _ = io.WriteString(w, b.String())
}
