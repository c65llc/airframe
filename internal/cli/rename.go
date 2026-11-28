// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/rename"
)

// renameCommand is rename's entry in Commands.
var renameCommand = Command{
	Name:    "rename",
	Args:    "<old> <new>",
	Summary: "Rename a token in its token files, airframe.yaml, components, invariants and every platform's consumer files, then rebuild; refuses a rename that would not load, build or rewrite every reference.",
	Flags: []Flag{
		{Name: "path", Arg: "a.b.c", Summary: "the new token's DTCG path (default: new split on \"-\", when old's path is old split on \"-\")"},
		{Name: "dry-run", Summary: "print the edits, moves, manual items and the stale colorsets it would remove or keep, and write nothing"},
		{Name: "allow-manual", Summary: "write the rename even though some references are left to finish by hand, listing them"},
	},
}

// runRename plans the whole rename first (rename.Codemod), then writes
// every source and consumer file it changes, then regenerates the outputs
// as build does. A refused plan, a plan with manual items and no
// --allow-manual, or a planned file changed on disk since it was read
// exits 1 and writes nothing.
func runRename(inv *invocation) int {
	if len(inv.args) != 2 {
		return inv.usageError("rename takes <old> <new>")
	}
	old, new := inv.args[0], inv.args[1]
	dir, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	if p == nil || hasErrors(ds) {
		diag.WriteText(inv.stderr, ds, nil)
		return ExitFindings
	}
	pl, err := rename.Codemod(p, old, new, inv.value("path"))
	var refusal *rename.Refusal
	if errors.As(err, &refusal) {
		_, _ = fmt.Fprintf(inv.stderr, "airframe %s\n", refusal.Msg)
		diag.WriteText(inv.stderr, refusal.Diagnostics, nil)
		return ExitFindings
	}
	if err != nil {
		return inv.fail(err)
	}
	dry := *inv.bools["dry-run"]
	verb := "renaming"
	if dry {
		verb = "would rename"
	}
	_, _ = fmt.Fprintf(inv.stdout, "%s %s to %s (path %s → %s)\n", verb, old, new,
		strings.Join(pl.OldPath, "."), strings.Join(pl.NewPath, "."))
	writePlan(inv.stdout, pl)
	files := maps.Clone(pl.Files)
	maps.Copy(files, pl.Consumers)
	if c := inv.refuseLinks(dir, files); c != ExitOK {
		return c
	}
	stale, err := staleByRename(p, pl)
	if err != nil {
		return inv.fail(err)
	}
	remove, keep, err := planRemovals(p.FS, stale)
	if err != nil {
		return inv.fail(err)
	}
	if dry {
		for _, s := range remove {
			_, _ = fmt.Fprintln(inv.stdout, "would remove", s.dir)
		}
		for _, s := range keep {
			_, _ = fmt.Fprintln(inv.stdout, "would keep", s.kept())
		}
		return ExitOK
	}
	if len(pl.Manual) > 0 && !*inv.bools["allow-manual"] {
		_, _ = fmt.Fprintf(inv.stderr, "airframe rename: %d reference(s) are left to finish by hand (manual, above); nothing was written. Pass --allow-manual to write the rest.\n", len(pl.Manual))
		return ExitFindings
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	// The plan was made from what was read: a file changed on disk since
	// would be overwritten with edits made to its old bytes.
	for _, name := range slices.Sorted(maps.Keys(files)) {
		was, ok := pl.ConsumerSources[name]
		if !ok {
			was = p.Sources[name]
		}
		now, err := fs.ReadFile(root.FS(), name)
		if err != nil {
			return inv.fail(err)
		}
		if !bytes.Equal(now, was) {
			_, _ = fmt.Fprintf(inv.stderr, "airframe rename: %s changed since it was read; nothing was written\n", name)
			return ExitFindings
		}
	}
	renamed, err := writeStaged(root, files)
	for _, name := range renamed {
		_, _ = fmt.Fprintln(inv.stdout, "wrote", name)
	}
	if err != nil {
		if len(renamed) > 0 {
			_, _ = fmt.Fprintf(inv.stderr, "airframe rename: %v; %d of %d file(s) were already written (listed above), the rest were not\n",
				err, len(renamed), len(files))
		} else {
			_, _ = fmt.Fprintf(inv.stderr, "airframe rename: %v; nothing was written\n", err)
		}
		return ExitUsage
	}

	_, p, ds, err = inv.load()
	if err != nil {
		return inv.fail(err)
	}
	// The sources are written: a colorset that cannot be removed is
	// warned about, and the outputs are still regenerated.
	failed := false
	for _, s := range remove {
		if err := removeStale(root, s.dir, s.files); err != nil {
			_, _ = fmt.Fprintf(inv.stderr, "airframe rename: remove stale colorset %s: %v; delete it by hand\n", s.dir, err)
			failed = true
			continue
		}
		_, _ = fmt.Fprintln(inv.stdout, "removed", s.dir)
	}
	for _, s := range keep {
		_, _ = fmt.Fprintf(inv.stderr, "airframe rename: kept %s\n", s.kept())
	}
	code := inv.build(dir, p, ds, nil, false)
	if len(pl.Manual) > 0 {
		_, _ = fmt.Fprintf(inv.stdout, "%d reference(s) left to finish by hand (manual, above)\n", len(pl.Manual))
	}
	if failed {
		return ExitUsage
	}
	return code
}

// staleDir is a stale colorset folder: files are the entries airframe may
// remove with it (its Contents.json and any dotfile, such as Finder's
// .DS_Store), foreign the entries it did not write.
type staleDir struct {
	dir            string
	files, foreign []string
}

func (s staleDir) kept() string {
	return fmt.Sprintf("%s: it holds files airframe did not write (%s)", s.dir, strings.Join(s.foreign, ", "))
}

// planRemovals reads each stale colorset folder before anything is
// written. One holding only its Contents.json and dotfiles is to be
// removed; one holding anything else is kept, untouched.
func planRemovals(fsys fs.FS, dirs []string) (remove, keep []staleDir, err error) {
	for _, d := range dirs {
		entries, err := fs.ReadDir(fsys, d)
		if err != nil {
			return nil, nil, err
		}
		s := staleDir{dir: d}
		for _, e := range entries {
			n := e.Name()
			if e.Type().IsRegular() && (n == "Contents.json" || strings.HasPrefix(n, ".")) {
				s.files = append(s.files, n)
			} else {
				s.foreign = append(s.foreign, n)
			}
		}
		if len(s.foreign) > 0 {
			keep = append(keep, s)
		} else {
			remove = append(remove, s)
		}
	}
	return remove, keep, nil
}

// staleByRename lists the colorset folders the rename makes stale: those
// no token makes once it is applied (pl.After, read over the disk) that
// were not stale before. They are the old name's, and the rename's to
// remove: build reports a stale colorset but leaves it, and one left would
// fail build --check. A colorset stale before the rename is left alone.
func staleByRename(p *project.Project, pl *rename.EditPlan) ([]string, error) {
	gen, _ := emit.Build(p, nil) // it builds: Codemod checked
	before, err := emit.StaleColorsets(p.FS, gen)
	if err != nil {
		return nil, err
	}
	gen, _ = emit.Build(pl.After, nil)
	after, err := emit.StaleColorsets(pl.After.FS, gen)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(after, func(d string) bool { return slices.Contains(before, d) }), nil
}

// removeStale is removeColorset; a test replaces it to fail.
var removeStale = removeColorset

// removeColorset removes the generated colorset folder dir: files, the
// entries planRemovals found safe to remove, then the folder, which must
// then be empty, so a file the user added there since is never removed.
func removeColorset(root *os.Root, dir string, files []string) error {
	d := filepath.FromSlash(dir)
	for _, f := range files {
		if err := root.Remove(filepath.Join(d, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return root.Remove(d)
}

// writePlan prints a plan: one line per edit, file:line:col  old → new;
// then each move, file:line  move from → to, noting a move that changes
// the emitted order; then each manual item,
// file:line  manual: message.
func writePlan(w io.Writer, pl *rename.EditPlan) {
	var b strings.Builder
	for _, e := range pl.Edits {
		fmt.Fprintf(&b, "%s:%d:%d  %s → %s\n", e.File, e.Line, e.Col, e.Old, e.New)
	}
	for _, m := range pl.Moves {
		note := ""
		if m.Reordered {
			note = " (appended to its new group: the emitted order changes)"
		}
		fmt.Fprintf(&b, "%s:%d  move %s → %s%s\n", m.File, m.Line, m.From, m.To, note)
	}
	for _, m := range pl.Manual {
		fmt.Fprintf(&b, "%s:%d  manual: %s\n", m.File, m.Line, m.Message)
	}
	_, _ = io.WriteString(w, b.String())
}

// writeStaged writes every file inside root as close to all or nothing as a
// filesystem allows: each is first written to a temporary name beside it,
// and only when every one is written are they renamed into place, in path
// order. A failure before the renames removes the temporary files and
// changes nothing (beyond any parent directory it had to create); one
// during them, which a rename within a directory makes unlikely but not
// impossible, leaves the files already renamed, which renamed lists.
func writeStaged(root *os.Root, files map[string][]byte) (renamed []string, err error) {
	names := slices.Sorted(maps.Keys(files))
	tmps := make([]string, 0, len(names))
	defer func() {
		for _, t := range tmps[len(renamed):] {
			_ = root.Remove(t)
		}
	}()
	for _, name := range names {
		tmp, err := stageFile(root, name, files[name])
		if err != nil {
			return nil, err
		}
		tmps = append(tmps, tmp)
	}
	for i, name := range names {
		if err := root.Rename(tmps[i], filepath.FromSlash(name)); err != nil {
			return renamed, fmt.Errorf("write %s: %w", name, err)
		}
		renamed = append(renamed, name)
	}
	return renamed, nil
}

// stageFile writes data to a new temporary file beside the
// project-relative slash path rel inside root, with rel's permission bits
// (0644 for a new file), creating the parent directories, and returns the
// temporary file's name. Renaming it onto rel is the caller's.
func stageFile(root *os.Root, rel string, data []byte) (string, error) {
	name := filepath.FromSlash(rel)
	parent := filepath.Dir(name)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	perm := os.FileMode(0o644)
	if fi, err := root.Stat(name); err == nil {
		perm = fi.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	for range 10 {
		tmpName := filepath.Join(parent, "."+filepath.Base(name)+".tmp-"+strconv.FormatUint(rand.Uint64(), 36))
		f, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = root.Chmod(tmpName, perm)
		}
		if err != nil {
			_ = root.Remove(tmpName)
			return "", err
		}
		return tmpName, nil
	}
	return "", fmt.Errorf("write %s: no free temporary name", rel)
}

// Registered here, not in the handlers literal, to keep that table's diff
// small while sibling commands land.
func init() { handlers["rename"] = runRename }
