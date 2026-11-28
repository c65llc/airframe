// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package tokendiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Side names one side of a diff: a ref as the user wrote it and the commit
// it resolved to, or the working tree when Commit is "".
type Side struct {
	Ref    string
	Commit string
}

// Label is the side as text and Markdown name it: the ref, or "working
// tree".
func (s Side) Label() string {
	if s.Commit == "" {
		return "working tree"
	}
	return s.Ref
}

// MarshalJSON writes {"ref", "commit"}, or {"workingTree": true}.
func (s Side) MarshalJSON() ([]byte, error) {
	if s.Commit == "" {
		return []byte(`{"workingTree":true}`), nil
	}
	return marshal(struct {
		Ref    string `json:"ref"`
		Commit string `json:"commit"`
	}{s.Ref, s.Commit})
}

// ContextLabel is a value change's context as printed: "base" or the mode
// key.
func ContextLabel(k string) string {
	if k == "" {
		return "base"
	}
	return k
}

// MarshalJSON writes a value change as {kind, context, before, after} with
// Value objects, retired with booleans, and any other kind with strings.
func (c Change) MarshalJSON() ([]byte, error) {
	switch c.Kind {
	case KindValue:
		return marshal(struct {
			Kind    Kind   `json:"kind"`
			Context string `json:"context"`
			Before  Value  `json:"before"`
			After   Value  `json:"after"`
		}{c.Kind, ContextLabel(string(c.Context)), c.Before, c.After})
	case KindRetired:
		return marshal(struct {
			Kind   Kind `json:"kind"`
			Before bool `json:"before"`
			After  bool `json:"after"`
		}{c.Kind, c.From == "true", c.To == "true"})
	}
	return marshal(struct {
		Kind   Kind   `json:"kind"`
		Before string `json:"before"`
		After  string `json:"after"`
	}{c.Kind, c.From, c.To})
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// counts is the one-line tally both text and Markdown lead with.
func (d Diff) counts() string {
	return fmt.Sprintf("%d added, %d removed, %d possibly renamed, %d changed",
		len(d.Added), len(d.Removed), len(d.Renamed), len(d.Changed))
}

// shown is a value as text prints it: a colour as "#rrggbb oklch(…)", a
// missing one as "? (why)".
func shown(v Value) string {
	switch {
	case v.Error != "":
		return "? (" + v.Error + ")"
	case v.Hex != "":
		return v.Hex + " " + v.Value
	}
	return v.Value
}

// orDash is s, or "-" when it is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// WriteText writes d grouped by kind: added, removed, possibly renamed and
// changed, each token's changes indented under it.
func WriteText(w io.Writer, d Diff, from, to Side) error {
	var b strings.Builder
	head := from.Label() + " → " + to.Label() + ": "
	if d.Empty() {
		b.WriteString(head + "no token changes\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	b.WriteString(head + d.counts() + "\n")
	group := func(title string, ts []Token) {
		if len(ts) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", title, len(ts))
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, t := range ts {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", t.Name, orDash(t.Type), orDash(t.Tier), shown(t.Value))
		}
		_ = tw.Flush()
	}
	group("added", d.Added)
	group("removed", d.Removed)
	if len(d.Renamed) > 0 {
		fmt.Fprintf(&b, "\npossibly renamed (%d):\n", len(d.Renamed))
		for _, r := range d.Renamed {
			fmt.Fprintf(&b, "  %s → %s\n", r.From, r.To)
		}
	}
	if len(d.Changed) > 0 {
		fmt.Fprintf(&b, "\nchanged (%d):\n", len(d.Changed))
		for _, c := range d.Changed {
			fmt.Fprintf(&b, "  %s\n", c.Name)
			for _, ch := range c.Changes {
				switch ch.Kind {
				case KindValue:
					fmt.Fprintf(&b, "    value %s: %s → %s\n", ContextLabel(string(ch.Context)), shown(ch.Before), shown(ch.After))
				case KindDescription:
					fmt.Fprintf(&b, "    description: %s → %s\n", strconv.Quote(ch.From), strconv.Quote(ch.To))
				default:
					fmt.Fprintf(&b, "    %s: %s → %s\n", ch.Kind, orDash(ch.From), orDash(ch.To))
				}
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// cell escapes s for a Markdown table cell: a pipe, even inside a code
// span, would end the cell, and a newline the row.
func cell(s string) string {
	return strings.NewReplacer("|", "\\|", "\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

// code is s as a Markdown code span, fenced with more backticks than any
// run inside it.
func code(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if longest > 0 || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// mdText is prose (a description, a fact) as a Markdown table cell, shown
// as written: HTML's <, > and & become entities, Markdown's emphasis and
// code marks and the backslash are escaped, and a pipe does not end the
// cell.
var mdText = func() func(string) string {
	r := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;",
		"\\", "\\\\", "*", "\\*", "_", "\\_", "`", "\\`",
	)
	return func(s string) string { return cell(r.Replace(s)) }
}()

// mdValue is a value as a Markdown cell: a colour as ■ `#rrggbb`
// `oklch(…)`, two code spans. GitHub strips inline styles and data-URI
// images from job summaries, so the swatch is a glyph and the hex.
func mdValue(v Value) string {
	switch {
	case v.Error != "":
		return "? (" + mdText(v.Error) + ")"
	case v.Hex != "":
		return "■ " + cell(code(v.Hex)) + " " + cell(code(v.Value))
	}
	return cell(code(v.Value))
}

// foldChangedOver is how many changed tokens Markdown lists in the open;
// past it, their tables fold into one <details>.
const foldChangedOver = 50

// WriteMarkdown writes d for a pull request's job summary: a heading per
// group and a context | before | after table per changed token. Past
// foldChangedOver changed tokens, those tables fold into a <details>, so a
// large diff stays under the summary's size limit; the other groups stay
// open.
func WriteMarkdown(w io.Writer, d Diff, from, to Side) error {
	var b strings.Builder
	label := func(s Side) string {
		if s.Commit == "" {
			return s.Label()
		}
		return code(s.Ref)
	}
	fmt.Fprintf(&b, "## Token changes: %s → %s\n\n", label(from), label(to))
	if d.Empty() {
		b.WriteString("No token changes.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	b.WriteString(d.counts() + ".\n")
	group := func(title string, ts []Token) {
		if len(ts) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n### %s (%d)\n\n| token | type | tier | value |\n| --- | --- | --- | --- |\n", title, len(ts))
		for _, t := range ts {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", code(t.Name), mdText(orDash(t.Type)), mdText(orDash(t.Tier)), mdValue(t.Value))
		}
	}
	group("Added", d.Added)
	group("Removed", d.Removed)
	if len(d.Renamed) > 0 {
		fmt.Fprintf(&b, "\n### Possibly renamed (%d)\n\n", len(d.Renamed))
		for _, r := range d.Renamed {
			fmt.Fprintf(&b, "- %s → %s\n", code(r.From), code(r.To))
		}
	}
	if len(d.Changed) > 0 {
		fmt.Fprintf(&b, "\n### Changed (%d)\n", len(d.Changed))
		fold := len(d.Changed) > foldChangedOver
		if fold {
			fmt.Fprintf(&b, "\n<details><summary>%d changed tokens</summary>\n", len(d.Changed))
		}
		for _, c := range d.Changed {
			fmt.Fprintf(&b, "\n#### %s\n\n| context | before | after |\n| --- | --- | --- |\n", code(c.Name))
			for _, ch := range c.Changes {
				if ch.Kind == KindValue {
					fmt.Fprintf(&b, "| %s | %s | %s |\n", cell(ContextLabel(string(ch.Context))), mdValue(ch.Before), mdValue(ch.After))
					continue
				}
				fmt.Fprintf(&b, "| *%s* | %s | %s |\n", ch.Kind, mdText(orDash(ch.From)), mdText(orDash(ch.To)))
			}
		}
		if fold {
			b.WriteString("\n</details>\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Report is the JSON document diff --format json writes.
type Report struct {
	Version int       `json:"version"`
	From    Side      `json:"from"`
	To      Side      `json:"to"`
	Added   []Token   `json:"added"`
	Removed []Token   `json:"removed"`
	Renamed []Rename  `json:"renamed"`
	Changed []Changed `json:"changed"`
}

// WriteJSON writes d as an indented Report; every list is present, empty
// when there is nothing in it.
func WriteJSON(w io.Writer, d Diff, from, to Side) error {
	r := Report{
		Version: 1, From: from, To: to,
		Added: orEmpty(d.Added), Removed: orEmpty(d.Removed),
		Renamed: orEmpty(d.Renamed), Changed: orEmpty(d.Changed),
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
