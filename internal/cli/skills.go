// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/skills"
	skillsfs "github.com/c65llc/airframe/skills"
)

// skillsCommand is the skills group's entry in Commands.
var skillsCommand = Command{
	Name:    "skills",
	Summary: "Install Airframe's agent skills and print hook and MCP configuration.",
	Subcommands: []Command{
		{
			Name:    "install",
			Summary: "Install the skills for each agent the project uses, or each --agent; an edited file is kept and the new version staged under .airframe/pending/.",
			Flags: []Flag{
				{
					Name: "agent", Arg: "A", Repeat: true,
					Summary: "install for agent A: claude-code, cursor, copilot, codex, gemini or aider",
				},
				{Name: "dry-run", Summary: "list what install would write or stage, and change nothing"},
			},
		},
		{Name: "list", Summary: "List the skills this release carries."},
		{
			Name:    "hooks",
			Args:    "<agent|pre-commit>",
			Summary: "Print configuration that runs fmt and lint after edits; edits nothing.",
		},
		{
			Name:    "mcp",
			Args:    "<agent>",
			Summary: "Print MCP client configuration that starts airframe mcp; edits nothing.",
			Flags:   []Flag{{Name: "read-only", Summary: "start the server with --read-only"}},
		},
	},
}

// Registered here, not in the handlers literal, to keep that table's diff
// small while sibling commands land.
func init() {
	handlers["skills install"] = runSkillsInstall
	handlers["skills list"] = runSkillsList
	handlers["skills hooks"] = runSkillsHooks
	handlers["skills mcp"] = runSkillsMCP
}

func runSkillsList(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	tw := tabwriter.NewWriter(inv.stdout, 0, 0, 2, ' ', 0)
	for _, s := range skillsfs.All() {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", s.Name, s.Description)
	}
	_ = tw.Flush()
	return ExitOK
}

func runSkillsHooks(inv *invocation) int {
	if len(inv.args) != 1 {
		return inv.usageError("skills hooks takes one argument")
	}
	text, err := skills.Hooks(inv.args[0])
	if err != nil {
		return inv.usageError("%v", err)
	}
	_, _ = io.WriteString(inv.stdout, text)
	_, _ = fmt.Fprintln(inv.stderr, skills.HookNote(inv.args[0]))
	return ExitOK
}

func runSkillsMCP(inv *invocation) int {
	if len(inv.args) != 1 {
		return inv.usageError("skills mcp takes one argument")
	}
	text, err := skills.MCPConfig(inv.args[0], *inv.bools["read-only"])
	if err != nil {
		return inv.usageError("%v", err)
	}
	_, _ = io.WriteString(inv.stdout, text)
	_, _ = fmt.Fprintln(inv.stderr, skills.MCPNote(inv.args[0]))
	return ExitOK
}

// runSkillsInstall installs the skills into the project root, found as for
// every other command, through an os.Root there. A directory with no
// airframe.yaml is refused: the skills describe an Airframe project.
func runSkillsInstall(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	var agents []skills.Agent
	for _, name := range *inv.lists["agent"] {
		a, err := skills.ParseAgent(name)
		if err != nil {
			return inv.usageError("%v", err)
		}
		agents = append(agents, a)
	}
	dir, err := inv.rootDir()
	if err != nil {
		return inv.fail(err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.File)); errors.Is(err, fs.ErrNotExist) {
		return inv.fail(fmt.Errorf("no %s in %s; run airframe init first, or pass --root", config.File, dir))
	} else if err != nil {
		return inv.fail(err)
	}
	d, err := skills.OpenDir(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = d.Close() }()
	if len(agents) == 0 {
		agents = skills.Detect(d)
		if len(agents) == 0 {
			return inv.fail(errors.New("no agents detected: pass --agent"))
		}
		names := make([]string, len(agents))
		for i, a := range agents {
			names[i] = string(a)
		}
		_, _ = fmt.Fprintf(inv.stdout, "agents: %s (detected)\n", strings.Join(names, ", "))
	}
	dry := *inv.bools["dry-run"]
	res, err := skills.Install(d, agents, skillsfs.All(), dry)
	if err != nil {
		return inv.fail(err)
	}
	wrote, staged, removed := "wrote", "staged", "removed"
	if dry {
		wrote, staged, removed = "would write", "would stage", "would remove"
	}
	for _, p := range res.Written {
		_, _ = fmt.Fprintln(inv.stdout, wrote, p)
	}
	for _, p := range res.Staged {
		_, _ = fmt.Fprintf(inv.stdout, "%s %s as %s/%s\n", staged, p, skills.PendingDir, p)
	}
	for _, p := range res.Removed {
		_, _ = fmt.Fprintf(inv.stdout, "%s %s (no longer shipped)\n", removed, p)
	}
	for _, p := range res.Retired {
		_, _ = fmt.Fprintf(inv.stdout, "no longer shipped: %s (edited; delete it yourself)\n", p)
	}
	line := fmt.Sprintf("%d written, %d unchanged, %d staged", len(res.Written), len(res.Unchanged), len(res.Staged))
	if n := len(res.Removed); n > 0 {
		line += fmt.Sprintf(", %d removed", n)
	}
	if dry {
		line += " (dry run)"
	}
	_, _ = fmt.Fprintln(inv.stdout, line)
	if n := len(res.Staged); n > 0 {
		subject, kept, merge := "1 file was", "it was", "its new version"
		if n > 1 {
			subject, kept, merge = fmt.Sprintf("%d files were", n), "they were", "their new versions"
		}
		_, _ = fmt.Fprintf(inv.stderr, "warning: %s edited or not installed by Airframe, so %s kept; "+
			"merge %s from %s/ by hand\n", subject, kept, merge, skills.PendingDir)
	}
	return ExitOK
}
