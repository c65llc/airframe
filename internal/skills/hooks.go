// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// PreCommit is the hook name for a git pre-commit hook.
const PreCommit = "pre-commit"

// HookNames is every name Hooks takes: the agents, then the git hook.
var HookNames = append(func() []string {
	out := make([]string, len(Agents))
	for i, a := range Agents {
		out[i] = string(a)
	}
	return out
}(), PreCommit)

// HookCommand is the command the Claude Code hook runs after every edit:
// the hidden airframe hook claude-code, which reads the edited file from
// the hook's input and, when lint checks that file, runs fmt and lint,
// exiting 2 with their report so Claude Code hands it back to the agent.
const HookCommand = "airframe hook claude-code"

// claudeSettings is the settings.json fragment for Claude Code.
type claudeSettings struct {
	Hooks struct {
		PostToolUse []claudeMatcher `json:"PostToolUse"`
	} `json:"hooks"`
}

type claudeMatcher struct {
	Matcher string       `json:"matcher"`
	Hooks   []claudeHook `json:"hooks"`
}

type claudeHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// preCommitHook is the git pre-commit hook.
const preCommitHook = `#!/bin/sh
# Airframe: refuse a commit whose design system is not formatted or not
# lint-clean. lint also reports generated outputs that are stale
# (emitted-stale): run "airframe fmt" and "airframe build", and commit what
# they change.
airframe fmt --check && airframe lint
`

// agentParagraph is the instruction for agents that read an instructions
// file instead of running hooks.
const agentParagraph = `## Airframe checks

After editing ` + "`airframe.yaml`" + `, a token file (` + "`*.tokens.json`" + `), ` + "`invariants.yaml`" + `,
` + "`retired.yaml`" + `, a component file or app code that reads tokens, run
` + "`airframe fmt`" + `, ` + "`airframe build`" + ` and ` + "`airframe lint --json`" + `, in that order, and fix
every finding before you finish. Never hand-edit a generated output;
` + "`airframe build`" + ` rewrites them.
`

// hookNotes say, in one line, where each hook's output goes.
var hookNotes = map[string]string{
	string(ClaudeCode): "Merge this into .claude/settings.json (or .claude/settings.local.json); airframe never edits it.",
	string(Cursor):     "Add this to a Cursor rule under .cursor/rules/, or to AGENTS.md outside the skills section.",
	string(Copilot):    "Add this to .github/copilot-instructions.md.",
	string(Codex):      "Add this to AGENTS.md, outside the airframe skills section.",
	string(Gemini):     "Add this to GEMINI.md, outside the airframe skills section.",
	string(Aider):      "Add this to CONVENTIONS.md, outside the airframe skills section.",
	PreCommit:          "Save this as .git/hooks/pre-commit and make it executable, or run its last line from your hook manager.",
}

// Hooks returns the configuration that makes name run airframe fmt and
// lint after edits. For claude-code it is a settings.json fragment: a
// PostToolUse hook on Edit, Write and MultiEdit that runs HookCommand,
// which checks the edited file when lint checks it and reports findings
// back to the agent. For pre-commit it is a git hook
// script. For the other agents it is a paragraph for their instructions
// file. HookNote says where each goes.
func Hooks(name string) (string, error) {
	switch name {
	case string(ClaudeCode):
		var s claudeSettings
		s.Hooks.PostToolUse = []claudeMatcher{{
			Matcher: "Edit|Write|MultiEdit",
			Hooks:   []claudeHook{{Type: "command", Command: HookCommand}},
		}}
		// A settings file shows its text as written, never HTML-escaped.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		_ = enc.Encode(s) // plain structs of strings always encode
		return buf.String(), nil
	case PreCommit:
		return preCommitHook, nil
	}
	if _, ok := hookNotes[name]; ok {
		return agentParagraph, nil
	}
	return "", fmt.Errorf("unknown hook target %s; use %s or %s",
		name, strings.Join(HookNames[:len(HookNames)-1], ", "), HookNames[len(HookNames)-1])
}

// HookNote is one line saying where Hooks' output for name goes, or "" for
// an unknown name.
func HookNote(name string) string { return hookNotes[name] }
