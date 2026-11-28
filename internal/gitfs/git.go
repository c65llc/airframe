// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package gitfs reads a directory of a git repository as it was at a
// commit, as a read-only fs.FS, through the git binary. Nothing is checked
// out and no git command that writes is run: the tree is listed with
// git ls-tree and blobs are read with git cat-file --batch.
//
// Every git command runs with an argument list (never a shell), with
// GIT_TERMINAL_PROMPT=0, GIT_PAGER=cat, --no-pager, core.fsmonitor off,
// optional locks off, lazy fetching off, replace objects off and literal
// pathspecs, and with the variables that would point git at another
// repository, at another config file or inject config removed, so the
// repository is always the one holding the directory Open was given. git
// still reads its default config files (system, user and repository).
package gitfs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ErrNoGit is returned by Open when no git binary is on PATH.
var ErrNoGit = errors.New("git is not installed (no git on PATH)")

// ErrNotWorkTree is returned by Open when the directory is not inside a
// git work tree.
var ErrNotWorkTree = errors.New("not inside a git work tree")

// RefError is a ref that was refused or that names no commit.
type RefError struct {
	Ref    string
	Reason string
}

func (e *RefError) Error() string { return fmt.Sprintf("ref %q: %s", e.Ref, e.Reason) }

// Repo is the git repository holding a directory.
type Repo struct {
	// Prefix is the directory's slash-separated path inside the
	// repository, with a trailing slash; "" at the repository root.
	Prefix string

	dir          string // the directory Open was given; git runs there
	bin          string // the git binary
	endOfOptions bool   // whether rev-parse understands --end-of-options
}

// scrubbed are the variables that would make git use a repository other
// than the one holding the directory, read a config file other than its
// default ones, or take config from the environment; they are removed
// from git's environment, with every GIT_CONFIG_KEY_<n> and
// GIT_CONFIG_VALUE_<n>. git still reads its default config files: the
// system file, the user's ~/.gitconfig (or XDG config) and the
// repository's own.
var scrubbed = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
	"GIT_PREFIX", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG",
}

// env is the environment every git command runs in.
func env() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(scrubbed, name) && !strings.HasPrefix(name, "GIT_CONFIG_KEY_") && !strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			out = append(out, kv)
		}
	}
	return append(out,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"PAGER=cat",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_NO_LAZY_FETCH=1",
		"GIT_LITERAL_PATHSPECS=1",
		"GIT_NO_REPLACE_OBJECTS=1",
	)
}

// command is git with the fixed leading options, running in dir.
func command(bin, dir string, args ...string) *exec.Cmd {
	full := append([]string{"--no-pager", "-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.Command(bin, full...)
	cmd.Env = env()
	return cmd
}

// run runs git and returns its stdout; the error carries its stderr.
func run(bin, dir string, args ...string) ([]byte, error) {
	cmd := command(bin, dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return stdout.Bytes(), nil
}

// Open finds the git work tree holding dir. It fails with ErrNoGit when
// git is not installed and with ErrNotWorkTree when dir is not inside a
// work tree (outside any repository, or inside a .git directory).
func Open(dir string) (*Repo, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrNoGit
	}
	r := &Repo{dir: dir, bin: bin}
	out, err := run(bin, dir, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotWorkTree)
	}
	out, err = run(bin, dir, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotWorkTree)
	}
	r.Prefix = strings.TrimRight(string(out), "\r\n")
	if v, err := run(bin, dir, "version"); err == nil {
		r.endOfOptions = atLeast(string(v), 2, 30)
	}
	return r, nil
}

var versionRE = regexp.MustCompile(`git version (\d+)\.(\d+)`)

// atLeast reports whether git version output names major.minor or later.
// rev-parse learned --end-of-options in git 2.30.
func atLeast(version string, major, minor int) bool {
	m := versionRE.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	maj, _ := strconv.Atoi(m[1])
	mnr, _ := strconv.Atoi(m[2])
	return maj > major || maj == major && mnr >= minor
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// Resolve turns ref into the SHA of the commit it names. A ref that is
// empty, starts with "-" or contains a NUL is refused before git sees it;
// anything else goes to git rev-parse --verify as <ref>^{commit}, after
// --end-of-options when git supports it.
func (r *Repo) Resolve(ref string) (string, error) {
	switch {
	case ref == "":
		return "", &RefError{Ref: ref, Reason: "is empty"}
	case strings.HasPrefix(ref, "-"):
		return "", &RefError{Ref: ref, Reason: `starts with "-"; refusing to pass it to git`}
	case strings.ContainsRune(ref, 0):
		return "", &RefError{Ref: ref, Reason: "contains a NUL byte"}
	}
	args := []string{"rev-parse", "--verify", "--quiet"}
	if r.endOfOptions {
		args = append(args, "--end-of-options")
	}
	out, err := run(r.bin, r.dir, append(args, ref+"^{commit}")...)
	sha := strings.TrimSpace(string(out))
	if err != nil || !shaRE.MatchString(sha) {
		return "", &RefError{Ref: ref, Reason: "names no commit in this repository"}
	}
	return sha, nil
}
