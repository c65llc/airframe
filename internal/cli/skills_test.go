// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/skills"
	skillsfs "github.com/c65llc/airframe/skills"
)

// skillsWrite writes each slash path in files under dir.
func skillsWrite(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
}

// TestSkillsCLI: each subcommand prints what its package function returns,
// with the note on stderr, so the command adds nothing of its own.
func TestSkillsCLI(t *testing.T) {
	for _, name := range skills.HookNames {
		want, err := skills.Hooks(name)
		require.NoError(t, err)
		code, out, errOut := run("skills", "hooks", name)
		assert.Equal(t, ExitOK, code, name)
		assert.Equal(t, want, out, name)
		assert.Equal(t, skills.HookNote(name)+"\n", errOut, name)
	}
	for _, a := range []string{"claude-code", "cursor", "copilot", "codex", "gemini"} {
		for _, ro := range []bool{false, true} {
			want, err := skills.MCPConfig(a, ro)
			require.NoError(t, err)
			args := []string{"skills", "mcp", a}
			if ro {
				args = append(args, "--read-only")
			}
			code, out, errOut := run(args...)
			assert.Equal(t, ExitOK, code, a)
			assert.Equal(t, want, out, a)
			assert.Equal(t, skills.MCPNote(a)+"\n", errOut, a)
		}
	}
	code, out, errOut := run("skills", "mcp", "aider")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "airframe skills mcp: aider has no MCP client; use airframe skills hooks aider instead\n")
	for _, args := range [][]string{{"skills", "mcp"}, {"skills", "mcp", "a", "b"}} {
		code, _, errOut = run(args...)
		assert.Equal(t, ExitUsage, code)
		assert.Contains(t, errOut, "skills mcp takes one argument")
	}
}

func TestSkillsList(t *testing.T) {
	code, out, errOut := run("skills", "list")
	assert.Equal(t, ExitOK, code)
	assert.Empty(t, errOut)
	for _, s := range skillsfs.All() {
		assert.Contains(t, out, s.Name+"  ")
		assert.Contains(t, out, s.Description)
	}
	assert.Len(t, strings.Split(strings.TrimSuffix(out, "\n"), "\n"), len(skillsfs.All()))
	code, _, errOut = run("skills", "list", "x")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "skills list takes no arguments")
	assert.Contains(t, errOut, "usage: airframe [--root D] skills list\n")
}

func TestSkillsGroupUsage(t *testing.T) {
	code, out, errOut := run("skills")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "airframe skills: missing subcommand; use install, list, hooks or mcp")
	assert.Contains(t, errOut, "usage: airframe [--root D] skills <subcommand>")

	code, _, errOut = run("skills", "remove")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe skills: unknown subcommand remove; use install, list, hooks or mcp")

	for _, h := range []string{"--help", "-h", "help"} {
		code, out, errOut = run("skills", h)
		assert.Equal(t, ExitOK, code, h)
		assert.Empty(t, errOut)
		assert.Contains(t, out, "usage: airframe [--root D] skills <subcommand>\n")
		assert.Contains(t, out, "  install [--agent A]... [--dry-run]")
		assert.Contains(t, out, "  hooks <agent|pre-commit>")
		assert.Contains(t, out, "  mcp [--read-only] <agent>")
	}

	code, out, _ = run("skills", "install", "--help")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "usage: airframe [--root D] skills install [--agent A]... [--dry-run]\n")
	assert.Contains(t, out, "--dry-run")

	code, _, errOut = run("skills", "install", "--bogus")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe skills install: flag provided but not defined: -bogus")
}

// TestUsageListsSubcommands: the full usage lists a group's subcommands
// one per line, and every other command as before.
func TestUsageListsSubcommands(t *testing.T) {
	_, out, _ := run("help")
	assert.Contains(t, out, "\n  skills install [--agent A]... [--dry-run]  ")
	assert.Contains(t, out, "\n  skills list  ")
	assert.Contains(t, out, "\n  skills hooks <agent|pre-commit>  ")
	assert.Contains(t, out, "\n  skills mcp [--read-only] <agent>  ")
	assert.NotContains(t, out, "\n  skills  ")
	for _, c := range Commands {
		if len(c.Subcommands) == 0 {
			assert.Contains(t, out, "\n  "+c.Name, c.Name)
		}
	}
}

func TestSkillsHooks(t *testing.T) {
	code, out, errOut := run("skills", "hooks", "claude-code")
	assert.Equal(t, ExitOK, code)
	assert.True(t, json.Valid([]byte(out)), out)
	assert.Contains(t, errOut, ".claude/settings.json")

	code, out, _ = run("skills", "hooks", "pre-commit")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "airframe fmt --check && airframe lint")

	code, out, errOut = run("skills", "hooks", "emacs")
	assert.Equal(t, ExitUsage, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "unknown hook target emacs")

	for _, args := range [][]string{{"skills", "hooks"}, {"skills", "hooks", "a", "b"}} {
		code, _, errOut = run(args...)
		assert.Equal(t, ExitUsage, code)
		assert.Contains(t, errOut, "skills hooks takes one argument")
	}
}

func TestSkillsInstall(t *testing.T) {
	dir := t.TempDir()
	skillsWrite(t, dir, map[string]string{"airframe.yaml": "version: 1\n", "AGENTS.md": "# Mine\n", "sub/x.txt": "x"})
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".claude"), 0o750))

	// From a subdirectory, the project is found as every command finds it.
	t.Chdir(filepath.Join(dir, "sub"))
	code, out, errOut := run("skills", "install", "--dry-run")
	assert.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, "agents: claude-code, codex (detected)\n")
	assert.Contains(t, out, "would write .claude/skills/airframe/SKILL.md\n")
	assert.Contains(t, out, "would write AGENTS.md\n")
	assert.Contains(t, out, "(dry run)")
	assert.NoFileExists(t, filepath.Join(dir, ".airframe", "skills.lock.json"))

	code, out, errOut = run("skills", "install")
	assert.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, "wrote .claude/skills/airframe/SKILL.md\n")
	assert.Contains(t, out, "5 written, 0 unchanged, 0 staged\n")
	assert.Empty(t, errOut)
	assert.FileExists(t, filepath.Join(dir, ".airframe", "skills.lock.json"))
	assert.True(t, strings.HasPrefix(readFile(t, dir, "AGENTS.md"), "# Mine\n\n"+skills.StartMarker))
	assert.NoFileExists(t, filepath.Join(dir, "sub", ".airframe", "skills.lock.json"))

	// Edited: staged, warned, exit 0.
	skillsWrite(t, dir, map[string]string{".claude/skills/airframe/SKILL.md": "mine\n"})
	code, out, errOut = run("--root", dir, "skills", "install", "--agent", "claude-code")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "staged .claude/skills/airframe/SKILL.md as .airframe/pending/.claude/skills/airframe/SKILL.md\n")
	assert.Contains(t, out, "0 written, 3 unchanged, 1 staged\n")
	assert.Contains(t, errOut, "warning: 1 file was edited or not installed by Airframe, so it was kept; merge its new version from .airframe/pending/ by hand\n")
	assert.Equal(t, "mine\n", readFile(t, dir, ".claude/skills/airframe/SKILL.md"))

	code, out, _ = run("--root", dir, "skills", "install", "--agent", "claude-code", "--dry-run")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "would stage .claude/skills/airframe/SKILL.md as ")

	skillsWrite(t, dir, map[string]string{".claude/skills/airframe-adopt/SKILL.md": "mine too\n"})
	_, _, errOut = run("--root", dir, "skills", "install", "--agent", "claude-code")
	assert.Contains(t, errOut, "warning: 2 files were edited or not installed by Airframe, so they were kept; merge their new versions from .airframe/pending/ by hand\n")
}

// TestSkillsInstallEveryFileIsTheRendering: an install for every agent
// writes exactly the rendered skills, so the golden files of the
// installer's own tests cover what the command writes.
func TestSkillsInstallEveryFileIsTheRendering(t *testing.T) {
	dir := t.TempDir()
	skillsWrite(t, dir, map[string]string{"airframe.yaml": "version: 1\n"})
	args := []string{"--root", dir, "skills", "install"}
	for _, a := range skills.Agents {
		args = append(args, "--agent", string(a))
	}
	code, out, errOut := run(args...)
	require.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, "15 written, 0 unchanged, 0 staged\n")
	for _, s := range skillsfs.All() {
		assert.Equal(t, skills.Render(skills.ClaudeCode, s), readFile(t, dir, ".claude/skills/"+s.Name+"/SKILL.md"))
		assert.Equal(t, skills.Render(skills.Cursor, s), readFile(t, dir, ".cursor/commands/"+s.Name+".md"))
		assert.Equal(t, skills.Render(skills.Copilot, s), readFile(t, dir, ".github/prompts/"+s.Name+".prompt.md"))
		for _, f := range []string{"AGENTS.md", "GEMINI.md", "CONVENTIONS.md"} {
			assert.Contains(t, readFile(t, dir, f), skills.Render(skills.Codex, s), f)
		}
	}
}

func TestSkillsInstallErrors(t *testing.T) {
	dir := t.TempDir()
	skillsWrite(t, dir, map[string]string{"airframe.yaml": "version: 1\n"})

	code, _, errOut := run("--root", dir, "skills", "install")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe skills install: no agents detected: pass --agent\n")

	code, _, errOut = run("--root", dir, "skills", "install", "--agent", "vim")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe skills install: unknown agent vim")

	code, _, errOut = run("--root", dir, "skills", "install", "extra")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "skills install takes no arguments")

	empty := t.TempDir()
	code, _, errOut = run("--root", empty, "skills", "install", "--agent", "codex")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe skills install: no airframe.yaml in "+empty)
	assert.NoFileExists(t, filepath.Join(empty, "AGENTS.md"))

	code, _, errOut = run("--root", filepath.Join(dir, "missing"), "skills", "install", "--agent", "codex")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe skills install: ")

	skillsWrite(t, dir, map[string]string{"AGENTS.md": skills.EndMarker + "\n"})
	code, _, errOut = run("--root", dir, "skills", "install", "--agent", "codex")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "AGENTS.md:1: ")
}

// TestSkillsInstallStaysInTheRoot: every write goes through os.Root, so a
// skill directory that is a symbolic link out of the project is refused,
// and nothing lands outside.
func TestSkillsInstallStaysInTheRoot(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	skillsWrite(t, dir, map[string]string{"airframe.yaml": "version: 1\n"})
	if err := os.Symlink(outside, filepath.Join(dir, ".cursor")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	code, _, errOut := run("--root", dir, "skills", "install", "--agent", "cursor")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, "airframe skills install: ")
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestSkillsInstallReportsDroppedSkills(t *testing.T) {
	dir := t.TempDir()
	sum := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}
	skillsWrite(t, dir, map[string]string{
		"airframe.yaml":                      "version: 1\n",
		".claude/skills/old/SKILL.md":        "old\n",
		".claude/skills/old-edited/SKILL.md": "edited\n",
		skills.LockFile: `{"version":1,"files":{".claude/skills/old/SKILL.md":"` + sum("old\n") +
			`",".claude/skills/old-edited/SKILL.md":"` + sum("before\n") + `"}}`,
	})

	code, out, errOut := run("--root", dir, "skills", "install", "--agent", "claude-code", "--dry-run")
	assert.Equal(t, ExitOK, code, errOut)
	assert.Contains(t, out, "would remove .claude/skills/old/SKILL.md (no longer shipped)\n")
	assert.Contains(t, out, "no longer shipped: .claude/skills/old-edited/SKILL.md (edited; delete it yourself)\n")
	assert.Contains(t, out, "4 written, 0 unchanged, 0 staged, 1 removed (dry run)\n")
	assert.FileExists(t, filepath.Join(dir, ".claude", "skills", "old", "SKILL.md"))

	code, out, _ = run("--root", dir, "skills", "install", "--agent", "claude-code")
	assert.Equal(t, ExitOK, code)
	assert.Contains(t, out, "removed .claude/skills/old/SKILL.md (no longer shipped)\n")
	assert.Contains(t, out, "no longer shipped: .claude/skills/old-edited/SKILL.md (edited; delete it yourself)\n")
	assert.NoFileExists(t, filepath.Join(dir, ".claude", "skills", "old", "SKILL.md"))
	assert.NoDirExists(t, filepath.Join(dir, ".claude", "skills", "old"))
	assert.FileExists(t, filepath.Join(dir, ".claude", "skills", "old-edited", "SKILL.md"))
}
