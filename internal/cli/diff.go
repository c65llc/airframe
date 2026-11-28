// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/report"
	"github.com/c65llc/airframe/internal/tokendiff"
)

// diffFormats are the values diff --format takes.
var diffFormats = []string{"text", "md", "json"}

// diffCommand is diff's entry in Commands.
var diffCommand = Command{
	Name:    "diff",
	Args:    "<ref-a> [<ref-b>]",
	Summary: "Show how the tokens changed from git ref A to ref B, or to the working tree; reads git, checks nothing out. A ref with no airframe.yaml counts as no tokens; symlinked files are skipped at a ref but followed in the working tree.",
	Flags: []Flag{
		{Name: "format", Arg: "F", Summary: "print as F: text, md (a pull request job summary) or json"},
		{Name: "exit-code", Summary: "exit 1 when the tokens changed, as git diff --exit-code does"},
	},
}

// runDiff compares the project's tokens at ref A with those at ref B, or in
// the working tree. Each side loads with its own airframe.yaml. A ref with
// no airframe.yaml at the project's directory (from before the project was
// adopted) is an empty side, with a note on stderr; when both refs are
// missing, the root is likely wrong and diff exits 2. Git missing, a
// directory outside a work tree or a bad ref exits 2; a side that does not
// load exits 1 with its diagnostics. The working tree loads before any
// note is printed.
//
// A ref side is read from git's objects, which store a symlink as a link:
// a symlinked token or config file is skipped there, though the working
// tree follows it.
func runDiff(inv *invocation) int {
	if len(inv.args) < 1 || len(inv.args) > 2 {
		return inv.usageError("diff takes one or two git refs")
	}
	format := inv.value("format")
	if format == "" {
		format = "text"
	}
	if !slices.Contains(diffFormats, format) {
		return inv.usageError("unknown format %s; use %s", format, strings.Join(diffFormats, ", "))
	}
	dir, err := inv.rootDir()
	if err != nil {
		return inv.fail(err)
	}
	c, err := report.Diff(dir, os.DirFS(dir), inv.args, func(note string) {
		_, _ = fmt.Fprintf(inv.stderr, "airframe diff: %s\n", note)
	})
	var notLoaded *report.NotLoadedError
	if errors.As(err, &notLoaded) {
		_, _ = fmt.Fprintf(inv.stderr, "airframe diff: the project does not load at %s:\n", notLoaded.Where)
		diag.WriteText(inv.stderr, notLoaded.Diagnostics, nil)
		return ExitFindings
	}
	if err != nil {
		return inv.fail(err)
	}
	switch format {
	case "md":
		_ = tokendiff.WriteMarkdown(inv.stdout, c.Diff, c.From, c.To)
	case "json":
		_ = tokendiff.WriteJSON(inv.stdout, c.Diff, c.From, c.To)
	default:
		_ = tokendiff.WriteText(inv.stdout, c.Diff, c.From, c.To)
	} // a write error is Main's to report
	if *inv.bools["exit-code"] && !c.Diff.Empty() {
		return ExitFindings
	}
	return ExitOK
}
