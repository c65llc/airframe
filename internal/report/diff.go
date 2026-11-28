// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package report

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/gitfs"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/tokendiff"
)

// Compared is what diff compares and finds: its two sides and the diff.
type Compared struct {
	Diff     tokendiff.Diff
	From, To tokendiff.Side
}

// NotWorkTreeError is a project directory outside any git work tree.
type NotWorkTreeError struct{ Dir string }

func (e *NotWorkTreeError) Error() string {
	return fmt.Sprintf("the project at %s is not inside a git work tree", e.Dir)
}

func (e *NotWorkTreeError) Unwrap() error { return gitfs.ErrNotWorkTree }

// BothMissingError is two refs neither of which has airframe.yaml at the
// project's directory: the root is likely wrong.
type BothMissingError struct{ From, To, Prefix string }

func (e *BothMissingError) Error() string {
	return fmt.Sprintf("neither %s nor %s has %s at %s; is the project root right? (--root)",
		e.From, e.To, config.File, prefixName(e.Prefix))
}

// NotLoadedError is a side whose project did not load cleanly. Where names
// the side: its ref and short commit, or the working tree.
type NotLoadedError struct {
	Where       string
	Diagnostics []diag.Diagnostic
}

func (e *NotLoadedError) Error() string { return "the project does not load at " + e.Where }

// Diff compares the project's tokens at git ref refs[0] with those at
// refs[1], or, given one ref, in the working tree work. dir is the project
// root, inside a git work tree. Each side loads with its own airframe.yaml.
// A ref with no airframe.yaml at the project's directory (from before the
// project was adopted) is an empty side, and note, when not nil, is told
// so; when both refs are missing the error is a *BothMissingError. Every
// ref is resolved, with gitfs's checks, before anything is read; a side
// that does not load cleanly is a *NotLoadedError. The working tree loads
// before any note, so a note never comes before its failure.
//
// A ref side is read from git's objects, which store a symlink as a link:
// a symlinked token or config file is skipped there, though the working
// tree follows it.
func Diff(dir string, work fs.FS, refs []string, note func(string)) (Compared, error) {
	repo, err := gitfs.Open(dir)
	if err != nil {
		if errors.Is(err, gitfs.ErrNotWorkTree) {
			return Compared{}, &NotWorkTreeError{Dir: dir}
		}
		return Compared{}, err
	}
	// Resolve every ref before reading anything, so a bad second ref is
	// reported without loading the first.
	sides := make([]tokendiff.Side, len(refs))
	for i, ref := range refs {
		sha, err := repo.Resolve(ref)
		if err != nil {
			return Compared{}, err
		}
		sides[i] = tokendiff.Side{Ref: ref, Commit: sha}
	}
	if len(sides) == 1 {
		sides = append(sides, tokendiff.Side{})
	}

	// Open every side, and find the refs with no airframe.yaml, before
	// loading anything.
	fsyss := make([]fs.FS, 2)
	missing := 0
	for i, s := range sides {
		if s.Commit == "" {
			fsyss[i] = work
			continue
		}
		gfs, err := repo.FS(s.Commit)
		if err != nil {
			return Compared{}, err
		}
		defer gfs.Close()
		fsyss[i] = gfs
		if _, err := fs.Stat(gfs, config.File); errors.Is(err, fs.ErrNotExist) {
			sides[i].Missing, sides[i].Prefix = true, repo.Prefix
			missing++
		}
	}
	if missing == 2 {
		return Compared{}, &BothMissingError{From: sides[0].Ref, To: sides[1].Ref, Prefix: repo.Prefix}
	}

	projects := make([]*project.Project, 2)
	load := func(i int) error {
		s := sides[i]
		p, ds, err := project.Load(fsyss[i])
		if err != nil {
			return fmt.Errorf("%s: %w", s.Label(), err)
		}
		if p == nil || hasErrors(ds) {
			return &NotLoadedError{Where: where(s), Diagnostics: ds}
		}
		projects[i] = p
		return nil
	}
	if sides[1].Commit == "" {
		if err := load(1); err != nil {
			return Compared{}, err
		}
	}
	for i, s := range sides {
		switch {
		case s.Missing:
			if note != nil {
				note(fmt.Sprintf("%s has no %s at %s; comparing against an empty token set",
					s.Ref, config.File, prefixName(repo.Prefix)))
			}
		case s.Commit != "":
			if err := load(i); err != nil {
				return Compared{}, err
			}
		}
	}
	return Compared{Diff: tokendiff.Compare(projects[0], projects[1]), From: sides[0], To: sides[1]}, nil
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
// missing-side messages name it.
func prefixName(prefix string) string {
	if prefix == "" {
		return "the repository root"
	}
	return prefix
}

func hasErrors(ds []diag.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == diag.Error {
			return true
		}
	}
	return false
}
