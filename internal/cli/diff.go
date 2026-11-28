// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/gitfs"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokendiff"
)

// diffFormats are the values diff --format takes.
var diffFormats = []string{"text", "md", "json"}

// diffCommand is diff's entry in Commands.
var diffCommand = Command{
	Name:    "diff",
	Args:    "<ref-a> [<ref-b>]",
	Summary: "Show how the tokens changed from git ref A to ref B, or to the working tree; reads git, checks nothing out.",
	Flags: []Flag{
		{Name: "format", Arg: "F", Summary: "print as F: text, md (a pull request job summary) or json"},
		{Name: "exit-code", Summary: "exit 1 when the tokens changed, as git diff --exit-code does"},
	},
}

// runDiff compares the project's tokens at ref A with those at ref B, or in
// the working tree. Each side loads with its own airframe.yaml. Git
// missing, a directory outside a work tree or a bad ref exits 2; a side
// that does not load exits 1 with its diagnostics.
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
	repo, err := gitfs.Open(dir)
	if err != nil {
		if errors.Is(err, gitfs.ErrNotWorkTree) {
			return inv.fail(fmt.Errorf("the project at %s is not inside a git work tree", dir))
		}
		return inv.fail(err)
	}
	// Resolve every ref before reading anything, so a bad second ref is
	// reported without loading the first.
	sides := make([]tokendiff.Side, len(inv.args))
	for i, ref := range inv.args {
		sha, err := repo.Resolve(ref)
		if err != nil {
			return inv.fail(err)
		}
		sides[i] = tokendiff.Side{Ref: ref, Commit: sha}
	}
	if len(sides) == 1 {
		sides = append(sides, tokendiff.Side{})
	}

	projects := make([]*project.Project, 2)
	for i, s := range sides {
		var fsys fs.FS
		if s.Commit == "" {
			fsys = os.DirFS(dir)
		} else {
			gfs, err := repo.FS(s.Commit)
			if err != nil {
				return inv.fail(err)
			}
			defer gfs.Close()
			fsys = gfs
		}
		p, ds, err := project.Load(fsys)
		if err != nil {
			return inv.fail(fmt.Errorf("%s: %w", s.Label(), err))
		}
		if p == nil || hasErrors(ds) {
			_, _ = fmt.Fprintf(inv.stderr, "airframe diff: the project does not load at %s:\n", where(s))
			diag.WriteText(inv.stderr, ds, nil)
			return ExitFindings
		}
		projects[i] = p
	}

	d := tokendiff.Compare(projects[0], projects[1])
	switch format {
	case "md":
		_ = tokendiff.WriteMarkdown(inv.stdout, d, sides[0], sides[1])
	case "json":
		_ = tokendiff.WriteJSON(inv.stdout, d, sides[0], sides[1])
	default:
		_ = tokendiff.WriteText(inv.stdout, d, sides[0], sides[1])
	} // a write error is Main's to report
	if *inv.bools["exit-code"] && !d.Empty() {
		return ExitFindings
	}
	return ExitOK
}

// where names a side for a message: the ref and its short commit, or the
// working tree.
func where(s tokendiff.Side) string {
	if s.Commit == "" {
		return s.Label()
	}
	return fmt.Sprintf("%s (%s)", s.Ref, s.Commit[:min(12, len(s.Commit))])
}
