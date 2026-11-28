// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package gittest builds throwaway git repositories for tests. Every
// command runs in a temporary directory with the caller's GIT_* variables
// removed and the user's and system git config ignored, so a test can
// never reach the repository it runs from or a personal setting such as
// commit signing.
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a git repository in a temporary directory.
type Repo struct {
	t   testing.TB
	Dir string
}

// New makes an empty repository on branch main, skipping the test when
// git is not installed.
func New(t testing.TB) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := &Repo{t: t, Dir: t.TempDir()}
	r.Git("-c", "init.defaultBranch=main", "init", "-q")
	return r
}

// Env is the environment test git commands run in.
func Env(home string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
}

// Git runs git in the repository and returns its trimmed output, failing
// the test on an error.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = Env(r.Dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Write writes each slash-separated path, creating its directories.
func (r *Repo) Write(files map[string]string) {
	r.t.Helper()
	for name, body := range files {
		p := filepath.Join(r.Dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

// Remove deletes each slash-separated path, a directory with its contents.
func (r *Repo) Remove(paths ...string) {
	r.t.Helper()
	for _, name := range paths {
		if err := os.RemoveAll(filepath.Join(r.Dir, filepath.FromSlash(name))); err != nil {
			r.t.Fatal(err)
		}
	}
}

// Commit stages everything and commits it, returning the commit's SHA.
func (r *Repo) Commit(msg string) string {
	r.t.Helper()
	r.Git("add", "-A")
	r.Git("-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false",
		"commit", "-q", "--allow-empty", "-m", msg)
	return r.Git("rev-parse", "HEAD")
}
