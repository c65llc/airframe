// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runIn runs the command line with stdin.
func runIn(stdin string, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// hookRead reads the slash path name under dir.
func hookRead(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	require.NoError(t, err)
	return string(b)
}

// hookProject is a lint-clean project with a consumer on every platform,
// formatted, built and checked.
func hookProject(t *testing.T) string {
	t.Helper()
	dir := lintCleanProject(t)
	code, _, errOut := run("--root", dir, "fmt")
	require.Equal(t, ExitOK, code, errOut)
	code, _, errOut = run("--root", dir, "build")
	require.Equal(t, ExitOK, code, errOut)
	code, out, errOut := run("--root", dir, "lint")
	require.Equal(t, ExitOK, code, out+errOut)
	return dir
}

// editInput is Claude Code's PostToolUse input for an edit of file.
func editInput(file string) string {
	data, _ := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"tool_name":       "Edit",
		"tool_input":      map[string]any{"file_path": file},
	})
	return string(data)
}

const brokenTokens = "{\"ink\": [\n"

func TestHookClaudeCode(t *testing.T) {
	dir := hookProject(t)
	tokensFile := "design-system/tokens/semantic.tokens.json"

	// A clean file lint checks: exit 0, quiet. The config, a token file,
	// the invariants, a consumer file and a generated output.
	for _, f := range []string{"airframe.yaml", tokensFile, "design-system/invariants.yaml", "web/app.css",
		"android/src/A.kt", "design-system/generated/css/tokens.css"} {
		code, out, errOut := runIn(editInput(filepath.Join(dir, f)), "--root", dir, "hook", "claude-code")
		assert.Equal(t, ExitOK, code, f+errOut)
		assert.Empty(t, out, f)
		assert.Empty(t, errOut, f)
	}

	// Not a file lint checks, or outside the project, or no file: exit 0
	// without looking.
	skillsWrite(t, dir, map[string]string{tokensFile: brokenTokens, "notes/todo.md": "x\n"})
	for name, in := range map[string]string{
		"not checked":   editInput(filepath.Join(dir, "notes", "todo.md")),
		"outside":       editInput(filepath.Join(t.TempDir(), "semantic.tokens.json")),
		"no file_path":  `{"tool_input":{}}`,
		"no tool_input": `{}`,
	} {
		code, out, errOut := runIn(in, "--root", dir, "hook", "claude-code")
		assert.Equal(t, ExitOK, code, name)
		assert.Empty(t, out, name)
		assert.Empty(t, errOut, name)
	}

	// Input it cannot decode is noted and passed.
	for name, in := range map[string]string{"not json": "nope", "empty": ""} {
		code, out, errOut := runIn(in, "--root", dir, "hook", "claude-code")
		assert.Equal(t, ExitOK, code, name)
		assert.Empty(t, out, name)
		assert.Contains(t, errOut, "airframe hook: cannot read the hook input", name)
		assert.NotContains(t, strings.TrimSuffix(errOut, "\n"), "\n", "one line")
	}

	// A broken token file: the problems go to stderr, exit 2.
	code, out, errOut := runIn(editInput(filepath.Join(dir, filepath.FromSlash(tokensFile))), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "airframe fmt reported problems after this edit; fix them:\n"+tokensFile+": ")

	// A relative path is relative to the root.
	code, _, errOut = runIn(editInput(tokensFile), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, tokensFile)
}

// TestHookClaudeCodeLintFindings: an edit that formats cleanly but leaves
// a finding, here a consumer reading a primitive, is reported as lint's
// JSON.
func TestHookClaudeCodeLintFindings(t *testing.T) {
	dir := hookProject(t)
	skillsWrite(t, dir, map[string]string{"web/app.css": ".a { color: var(--amber-500); }\n"})
	code, out, errOut := runIn(editInput(filepath.Join(dir, "web", "app.css")), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "airframe lint found problems after this edit; fix them:\n{\n")
	assert.Contains(t, errOut, `"rule": "primitive-read-by-consumer"`)
}

// TestHookClaudeCodeFormats: a clean edit is left in fmt's form.
func TestHookClaudeCodeFormats(t *testing.T) {
	dir := hookProject(t)
	canonical := hookRead(t, dir, "airframe.yaml")
	skillsWrite(t, dir, map[string]string{"airframe.yaml": strings.Replace(canonical, "version: 1", "version:   1", 1)})
	code, _, errOut := runIn(editInput(filepath.Join(dir, "airframe.yaml")), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitOK, code, errOut)
	assert.Equal(t, canonical, hookRead(t, dir, "airframe.yaml"))
}

// TestHookClaudeCodeChecksGeneratedOutputs: a hand-edit of an output build
// writes is checked, so lint's emitted-stale reports it; and an edit to a
// token that leaves the outputs behind is reported the same way.
func TestHookClaudeCodeChecksGeneratedOutputs(t *testing.T) {
	dir := hookProject(t)
	out := "design-system/generated/css/tokens.css"
	skillsWrite(t, dir, map[string]string{out: hookRead(t, dir, out) + "\n/* hand-edited */\n"})
	code, stdout, errOut := runIn(editInput(filepath.Join(dir, filepath.FromSlash(out))), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, errOut, "emitted-stale")

	dir = hookProject(t)
	tokensFile := "design-system/tokens/primitive.tokens.json"
	skillsWrite(t, dir, map[string]string{tokensFile: strings.Replace(hookRead(t, dir, tokensFile), "0.63", "0.64", 1)})
	code, _, errOut = runIn(editInput(filepath.Join(dir, filepath.FromSlash(tokensFile))), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "emitted-stale")
}

func TestHookClaudeCodeOutsideAProject(t *testing.T) {
	dir := t.TempDir()
	skillsWrite(t, dir, map[string]string{"airframe.yaml.txt": "x"})
	code, out, errOut := runIn(editInput(filepath.Join(dir, "airframe.yaml")), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitOK, code)
	assert.Empty(t, out+errOut)

	code, _, errOut = runIn("{}", "--root", dir, "hook", "claude-code", "extra")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "hook claude-code takes no arguments")
}

// TestHookClaudeCodeBrokenConfig: an airframe.yaml that cannot be read is
// still checked when it is the edited file, and nothing else is.
func TestHookClaudeCodeBrokenConfig(t *testing.T) {
	dir := hookProject(t)
	skillsWrite(t, dir, map[string]string{"airframe.yaml": "version: [1\n"})
	code, _, errOut := runIn(editInput(filepath.Join(dir, "airframe.yaml")), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "yaml-syntax")

	code, _, _ = runIn(editInput(filepath.Join(dir, "web", "app.css")), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitOK, code)
}

// TestHookClaudeCodeThroughASymlink: an edited path that reaches the
// project through a symbolic link is still in it.
func TestHookClaudeCodeThroughASymlink(t *testing.T) {
	dir := hookProject(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	skillsWrite(t, dir, map[string]string{"design-system/tokens/semantic.tokens.json": brokenTokens})
	code, _, errOut := runIn(editInput(filepath.Join(link, "design-system", "tokens", "semantic.tokens.json")), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "semantic.tokens.json")

	// A link to a file that does not exist is not in the project.
	code, _, _ = runIn(editInput(filepath.Join(link, "design-system", "nope.yaml")), "--root", dir, "hook", "claude-code")
	assert.Equal(t, ExitOK, code)
}

// TestHookClaudeCodeFindsTheFilesProject: the project is the edited
// file's, wherever the hook runs; a relative path resolves against the
// input's cwd.
func TestHookClaudeCodeFindsTheFilesProject(t *testing.T) {
	dir := hookProject(t)
	tokensFile := filepath.Join(dir, "design-system", "tokens", "semantic.tokens.json")
	skillsWrite(t, dir, map[string]string{"design-system/tokens/semantic.tokens.json": brokenTokens})
	elsewhere := t.TempDir()

	code, _, errOut := runIn(editInput(tokensFile), "--root", elsewhere, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "semantic.tokens.json")

	in, _ := json.Marshal(map[string]any{"cwd": dir, "tool_input": map[string]any{"file_path": "design-system/tokens/semantic.tokens.json"}})
	code, _, errOut = runIn(string(in), "--root", elsewhere, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "semantic.tokens.json")

	// A file whose directory is gone falls back to the cwd's project.
	in, _ = json.Marshal(map[string]any{"cwd": dir, "tool_input": map[string]any{"file_path": "gone/x.md"}})
	code, _, _ = runIn(string(in), "--root", elsewhere, "hook", "claude-code")
	assert.Equal(t, ExitOK, code)

	// A large edit payload is read whole.
	big, _ := json.Marshal(map[string]any{
		"tool_input": map[string]any{"file_path": tokensFile, "content": strings.Repeat("x", 3<<20)},
	})
	code, _, errOut = runIn(string(big), "--root", elsewhere, "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "semantic.tokens.json")
}

// TestHookClaudeCodeFindsTheProjectFromTheWorkingDirectory: with no --root
// and no cwd in the input, a relative path is the working directory's.
func TestHookClaudeCodeFindsTheProjectFromTheWorkingDirectory(t *testing.T) {
	dir := hookProject(t)
	skillsWrite(t, dir, map[string]string{"design-system/tokens/semantic.tokens.json": brokenTokens})
	t.Chdir(filepath.Join(dir, "design-system"))
	code, _, errOut := runIn(editInput("tokens/semantic.tokens.json"), "hook", "claude-code")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "semantic.tokens.json")
}

func TestHiddenHookIsNotInTheUsage(t *testing.T) {
	_, out, _ := run("help")
	assert.NotContains(t, out, "hook claude-code")
	assert.Contains(t, out, "\n  skills hooks <agent|pre-commit>  ")
	code, _, errOut := run("hook")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe hook: missing subcommand; use claude-code")
}
