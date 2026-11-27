// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package diag

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDocsURL(t *testing.T) {
	got := DocsURL("tier-violation")
	want := "https://airframe.c65llc.com/rules/tier-violation/"
	if got != want {
		t.Fatalf("DocsURL = %q, want %q", got, want)
	}
}

func TestSortOrder(t *testing.T) {
	ds := []Diagnostic{
		{File: "b", Line: 1, Rule: "x", Message: "m"},
		{File: "a", Line: 2, Rule: "x", Message: "m"},
		{File: "a", Line: 1, Rule: "z", Message: "m"},
		{File: "a", Line: 1, Rule: "y", Message: "n"},
		{File: "a", Line: 1, Rule: "y", Message: "m"},
	}
	Sort(ds)
	var got []string
	for _, d := range ds {
		got = append(got, d.File+d.Rule+d.Message)
	}
	want := []string{"aym", "ayn", "azm", "axm", "bxm"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestWriteJSONShape(t *testing.T) {
	ds := []Diagnostic{
		{Rule: "tier-violation", Severity: Error, File: "a.tokens.json", Path: "x", Line: 3, Message: "e", Docs: DocsURL("tier-violation")},
		{Rule: "soft", Severity: Warn, File: "a.tokens.json", Path: "y", Message: "w"},
		{Rule: "quiet", Severity: Off, File: "a.tokens.json", Message: "dropped"},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, ds); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version  int          `json:"version"`
		Errors   []Diagnostic `json:"errors"`
		Warnings []Diagnostic `json:"warnings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Errors) != 1 || len(got.Warnings) != 1 {
		t.Fatalf("got %+v", got)
	}
	if !strings.HasSuffix(buf.String(), "}\n") {
		t.Fatalf("missing final newline: %q", buf.String())
	}
}

func TestWriteJSONEmptyListsAreArrays(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, `"errors": []`) || !strings.Contains(s, `"warnings": []`) {
		t.Fatalf("want empty arrays, got %s", s)
	}
	if strings.Contains(s, "null") {
		t.Fatalf("null in output: %s", s)
	}
}

func TestWriteJSONFixAndOmitted(t *testing.T) {
	ds := []Diagnostic{{Rule: "r", Severity: Error, File: "f", Path: "p", Message: "m",
		Fix: &Fix{Description: "d", Edits: []Edit{{File: "f", Line: 2, Old: "a", New: "b"}}}}}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, ds); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if strings.Contains(s, `"line": 0`) && !strings.Contains(s, `"old"`) {
		t.Fatalf("unexpected: %s", s)
	}
	for _, want := range []string{`"fix"`, `"edits"`, `"old": "a"`, `"new": "b"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	// Diagnostic-level line is omitted when unknown.
	var top struct {
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(buf.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top.Errors[0]["line"]; ok {
		t.Fatalf("line should be omitted when 0")
	}
}

func TestWriteGitHub(t *testing.T) {
	ds := []Diagnostic{
		{Rule: "tier-violation", Severity: Error, File: "a.tokens.json", Line: 3, Message: "msg"},
		{Rule: "soft", Severity: Warn, File: "b.tokens.json", Message: "careful"},
		{Rule: "quiet", Severity: Off, File: "c", Message: "x"},
	}
	var buf bytes.Buffer
	WriteGitHub(&buf, ds)
	want := "::error file=a.tokens.json,line=3,title=tier-violation::msg\n" +
		"::warning file=b.tokens.json,title=soft::careful\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestWriteGitHubEscapes(t *testing.T) {
	var buf bytes.Buffer
	WriteGitHub(&buf, []Diagnostic{{Rule: "r", Severity: Error, File: "a,b.json", Message: "100%\nnext"}})
	want := "::error file=a%2Cb.json,title=r::100%25%0Anext\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestWriteTextLines(t *testing.T) {
	ds := []Diagnostic{
		{Rule: "r", Severity: Error, File: "b.json", Line: 2, Path: "x.y", Message: "m"},
		{Rule: "r", Severity: Warn, File: "a.json", Message: "w"},
	}
	var buf bytes.Buffer
	WriteText(&buf, ds, nil)
	want := "a.json: warn r: w\nb.json:2: error r: m (x.y)\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestWriteTextTallyByName(t *testing.T) {
	ds := []Diagnostic{{Rule: "tier-violation", Severity: Error, File: "a.json", Message: "m"}}
	tallies := []Tally{
		{Rule: "tier-violation", Files: 2, Items: 12, Noun: "tokens"},
		{Rule: "orphan", Files: 1, Items: 1},
		{Rule: "contrast-below", NotApplicable: "no contrast invariants declared"},
	}
	var buf bytes.Buffer
	WriteText(&buf, ds, tallies)
	want := "a.json: error tier-violation: m\n" +
		"tier-violation  12 tokens\n" +
		"orphan  1 items\n" +
		"contrast-below  n/a (no contrast invariants declared)\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
	if !strings.HasSuffix(buf.String(), "(no contrast invariants declared)\n") {
		t.Fatal("output must end with the tally lines")
	}
}

func TestWriteTextDoesNotMutateInput(t *testing.T) {
	ds := []Diagnostic{{File: "b", Rule: "r"}, {File: "a", Rule: "r"}}
	WriteText(&bytes.Buffer{}, ds, nil)
	if ds[0].File != "b" {
		t.Fatal("input was reordered")
	}
}
