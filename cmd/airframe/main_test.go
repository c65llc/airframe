// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"
)

// TestMain makes this test binary double as the airframe command, which
// the scripts run with exec.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"airframe": main})
}

// TestScripts runs testdata/script/*.txtar against the real binary.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		RequireExplicitExec: true,
		Cmds:                map[string]func(*testscript.TestScript, bool, []string){"exits": exits, "crlf": crlf, "fixture": fixture, "replace": replace},
	})
}

// exits runs a command, like exec, and requires its exit status to be the
// given number: "exits 2 airframe frobnicate".
func exits(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: exits status command [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("exits: bad status %q", args[0])
	}
	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			ts.Fatalf("exits: %v", err)
		}
		got = ee.ExitCode()
	}
	if got != want {
		ts.Fatalf("exit status %d, want %d", got, want)
	}
}

// crlf rewrites each file with CRLF line endings, as a Windows checkout with
// core.autocrlf=true does: "crlf design-system/a.tokens.json".
func crlf(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: crlf file...")
	}
	for _, name := range args {
		data := ts.ReadFile(name)
		out := bytes.ReplaceAll(bytes.ReplaceAll([]byte(data), []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
		ts.Check(os.WriteFile(ts.MkAbs(name), out, 0o644))
	}
}

// TestImportScriptUsesFixture keeps import_ok.txtar's copy of the
// testdata/import fixture identical to it, so the script and the guide's
// walkthrough import the same files.
func TestImportScriptUsesFixture(t *testing.T) {
	ar, err := txtar.ParseFile(filepath.Join("testdata", "script", "import_ok.txtar"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range ar.Files {
		want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "import", f.Name))
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if !bytes.Equal(f.Data, want) {
			t.Errorf("%s in import_ok.txtar differs from testdata/import/%s", f.Name, f.Name)
		}
		seen++
	}
	if seen != 2 {
		t.Errorf("import_ok.txtar carries %d fixture files, want airframe.yaml and theme.css", seen)
	}
}

// fixture copies a repository fixture's airframe.yaml and design-system
// into the script's directory: "fixture apple". The fixture's committed
// outputs are left behind, so the script builds them itself.
func fixture(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: fixture name")
	}
	src := filepath.Join("..", "..", "testdata", args[0])
	for _, name := range []string{"airframe.yaml", "design-system"} {
		ts.Check(copyTree(filepath.Join(src, name), ts.MkAbs(name)))
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// replace edits a file in place: "replace file old new" swaps every old
// for new and fails when old is not there.
func replace(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 3 {
		ts.Fatalf("usage: replace file old new")
	}
	data := ts.ReadFile(args[0])
	if !strings.Contains(data, args[1]) {
		ts.Fatalf("replace: %q not found in %s", args[1], args[0])
	}
	ts.Check(os.WriteFile(ts.MkAbs(args[0]), []byte(strings.ReplaceAll(data, args[1], args[2])), 0o644))
}
