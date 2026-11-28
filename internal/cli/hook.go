// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/invariants"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/retired"
)

// hookCommand is the hidden hook group: commands an agent's hook runs,
// which a person never types. skills hooks prints the configuration that
// runs them.
var hookCommand = Command{
	Name:    "hook",
	Summary: "Run an agent hook.",
	Hidden:  true,
	Subcommands: []Command{{
		Name:    "claude-code",
		Summary: "Check the file a Claude Code edit names on stdin; exit 2 with the findings.",
	}},
}

// Registered here, not in the handlers literal, to keep that table's diff
// small while sibling commands land.
func init() { handlers["hook claude-code"] = runHookClaudeCode }

// hookInput is the part of Claude Code's PostToolUse input the hook reads.
type hookInput struct {
	Cwd       string `json:"cwd"`
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// runHookClaudeCode checks the file a Claude Code edit named. It exits 0,
// quietly, unless the file is one lint checks (airframe.yaml, a token,
// invariants, retired or component file, a consumer file of some
// platform, or a generated output, so that a hand-edit is caught as
// emitted-stale); then it runs fmt and lint --json, and on a failure
// writes what they reported to stderr and exits 2, which Claude Code feeds
// back to the model. The project is the one holding the edited file, else
// the one holding the input's cwd (or --root), against which a relative
// path is resolved. Input it cannot decode is noted on stderr and passed;
// no file or no project is not the hook's business: exit 0.
func runHookClaudeCode(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	in := inv.stdin
	if in == nil {
		in = strings.NewReader("")
	}
	var hi hookInput
	if err := json.NewDecoder(in).Decode(&hi); err != nil {
		_, _ = fmt.Fprintf(inv.stderr, "airframe hook: cannot read the hook input (%v); not checked\n", err)
		return ExitOK
	}
	if hi.ToolInput.FilePath == "" {
		return ExitOK
	}
	cwd := inv.root
	if hi.Cwd != "" {
		cwd = hi.Cwd
	}
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ExitOK
		}
		cwd = wd
	}
	root, rel, ok := hookProjectFile(cwd, hi.ToolInput.FilePath)
	if !ok {
		return ExitOK
	}
	p, _, _ := project.Load(os.DirFS(root))
	if !isProjectFile(rel, p) {
		return ExitOK
	}
	var out, errOut bytes.Buffer
	if inv.then("fmt", root, &out, &errOut) != ExitOK {
		_, _ = fmt.Fprintf(inv.stderr, "airframe fmt reported problems after this edit; fix them:\n%s", errOut.String())
		return ExitUsage
	}
	out.Reset()
	errOut.Reset()
	if inv.then("lint", root, &out, &errOut, "json") != ExitOK {
		_, _ = fmt.Fprintf(inv.stderr, "airframe lint found problems after this edit; fix them:\n%s%s", out.String(), errOut.String())
		return ExitUsage
	}
	return ExitOK
}

// then runs the command name's handler in the project root, writing to
// stdout and stderr, with each boolean flag in set on and every other flag
// at its default. (Calling dispatch here would make handlers refer to
// itself.)
func (inv *invocation) then(name, root string, stdout, stderr io.Writer, set ...string) int {
	i := slices.IndexFunc(Commands, func(c Command) bool { return c.Name == name })
	next := &invocation{cmd: Commands[i], root: root, stdout: stdout, stderr: stderr, bools: map[string]*bool{}, lists: map[string]*[]string{}}
	for _, f := range next.cmd.Flags {
		if f.Arg == "" {
			next.bools[f.Name] = new(bool)
		} else {
			next.lists[f.Name] = new([]string)
		}
	}
	for _, s := range set {
		*next.bools[s] = true
	}
	return handlers[name](next)
}

// isProjectFile reports whether the project-relative path rel is a file
// lint checks: airframe.yaml always, and, when the project loaded, a
// source it formats (or one by its name that did not load), a consumer
// file of some platform, or an output build writes.
func isProjectFile(rel string, p *project.Project) bool {
	if rel == config.File {
		return true
	}
	if p == nil {
		return false
	}
	if _, ok := p.Sources[rel]; ok || slices.Contains(format.Paths(p), rel) {
		return true
	}
	content := path.Clean(p.Config.Content)
	if strings.HasPrefix(rel, content+"/") && strings.HasSuffix(rel, ".tokens.json") ||
		path.Dir(rel) == path.Join(content, "components") && strings.HasSuffix(rel, ".yaml") ||
		rel == path.Join(content, invariants.File) || rel == path.Join(content, retired.File) {
		return true
	}
	for _, pl := range p.Config.Platforms {
		for _, g := range pl.Consumers {
			if ok, _ := doublestar.Match(g, rel); ok {
				return true
			}
		}
	}
	outs, _ := emit.Build(p, nil)
	_, ok := outs[rel]
	return ok
}

// findProjectRoot walks up from start to the first directory holding an
// airframe.yaml file and returns it. A start that does not exist, or is not
// a directory, is an error, as is reaching the file system root.
func findProjectRoot(start string) (string, error) {
	fi, err := os.Stat(start)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", start)
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		fi, err := os.Stat(filepath.Join(dir, config.File))
		switch {
		case err == nil && !fi.IsDir():
			return dir, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("looking for %s: %w", config.File, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s in %s or a directory above it", config.File, start)
		}
		dir = parent
	}
}

// hookProjectFile finds the Airframe project holding file, a path an
// agent's hook reported: absolute, or relative to cwd. The project is the
// one holding file's directory, else the one holding cwd. It returns the
// project root and file's slash path relative to it; ok is false when
// there is no such project or file is not inside it. Symbolic links are
// resolved when the plain paths do not match.
func hookProjectFile(cwd, file string) (root, rel string, ok bool) {
	file = filepath.FromSlash(file)
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	file, err := filepath.Abs(file)
	if err != nil {
		return "", "", false
	}
	if root, err = findProjectRoot(filepath.Dir(file)); err != nil {
		if root, err = findProjectRoot(cwd); err != nil {
			return "", "", false
		}
	}
	rel, ok = hookWithin(root, file)
	return root, rel, ok
}

// foldCase is whether paths that differ only in case name the same file,
// as they do by default on Windows and macOS.
var foldCase = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// hookWithin returns file's slash path relative to root, and whether it lies
// inside root. Where foldCase is set, root and file may differ in case.
// Symbolic links in either are resolved when the plain paths do not match.
func hookWithin(root, file string) (string, bool) {
	if rel, ok := hookInside(root, file); ok {
		return rel, true
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	f, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", false
	}
	return hookInside(r, f)
}

// hookInside is hookWithin without resolving symbolic links.
func hookInside(root, file string) (string, bool) {
	rel, err := filepath.Rel(root, file)
	rel = filepath.ToSlash(rel)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
		return rel, true
	}
	if !foldCase {
		return "", false
	}
	root, file = filepath.Clean(root), filepath.Clean(file)
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	if len(file) <= len(root) || !strings.EqualFold(file[:len(root)], root) {
		return "", false
	}
	return filepath.ToSlash(file[len(root):]), true
}
