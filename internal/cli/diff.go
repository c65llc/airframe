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

	"github.com/c65llc/airframe/internal/config"
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

	// Open every side, and find the refs with no airframe.yaml (from
	// before the project was adopted), before loading anything.
	fsyss := make([]fs.FS, 2)
	missing := 0
	for i, s := range sides {
		if s.Commit == "" {
			fsyss[i] = os.DirFS(dir)
			continue
		}
		gfs, err := repo.FS(s.Commit)
		if err != nil {
			return inv.fail(err)
		}
		defer gfs.Close()
		fsyss[i] = gfs
		if _, err := fs.Stat(gfs, config.File); errors.Is(err, fs.ErrNotExist) {
			sides[i].Missing, sides[i].Prefix = true, repo.Prefix
			missing++
		}
	}
	if missing == 2 {
		// Two empty sides say nothing: the root is likely wrong.
		return inv.fail(fmt.Errorf("neither %s nor %s has %s at %s; is the project root right? (--root)",
			sides[0].Ref, sides[1].Ref, config.File, prefixName(repo.Prefix)))
	}

	projects := make([]*project.Project, 2)
	load := func(i int) int {
		s := sides[i]
		p, ds, err := project.Load(fsyss[i])
		if err != nil {
			return inv.fail(fmt.Errorf("%s: %w", s.Label(), err))
		}
		if p == nil || hasErrors(ds) {
			_, _ = fmt.Fprintf(inv.stderr, "airframe diff: the project does not load at %s:\n", where(s))
			diag.WriteText(inv.stderr, ds, nil)
			return ExitFindings
		}
		projects[i] = p
		return ExitOK
	}
	// The working tree loads first, so a ref's empty-side note is never
	// printed above the working tree's own failure.
	if sides[1].Commit == "" {
		if code := load(1); code != ExitOK {
			return code
		}
	}
	for i, s := range sides {
		switch {
		case s.Missing:
			_, _ = fmt.Fprintf(inv.stderr, "airframe diff: %s has no %s at %s; comparing against an empty token set\n",
				s.Ref, config.File, prefixName(repo.Prefix))
		case s.Commit != "":
			if code := load(i); code != ExitOK {
				return code
			}
		}
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

// prefixName is the project directory inside the repository, as the
// missing-side note names it.
func prefixName(prefix string) string {
	if prefix == "" {
		return "the repository root"
	}
	return prefix
}
