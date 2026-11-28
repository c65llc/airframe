# Airframe 7 · Agent skills and the MCP server — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two surfaces for coding agents, with the same conventions Starboard uses:

- **Skills.** Airframe ships its skills as Markdown, embedded in the binary. `airframe skills install|list|hooks|mcp` installs them for each agent and prints the agent's hook and MCP configuration.
- **MCP server.** `airframe mcp [--read-only]` is an MCP server on stdio. Its tools are typed reads and edits over the same core the CLI uses.

**Architecture: port, don't reinvent.** Starboard ships both surfaces at `~/projects/starboard`: same org, same licence, same author. Its design was reviewed, in `docs/specs/2026-11-27-mcp-server-design.md`, which covers the protocol subset, version negotiation, bounds, fencing, previews, record versions, the audit line and input checks. Port these Starboard packages with their tests, change only what is Starboard-specific, and keep their behaviour:

| Starboard path | Airframe path |
|---|---|
| `internal/mcp/{protocol,errors,fence,version,overlay}.go` | `internal/mcp/` |
| `internal/skills/{install,hooks,mcp}.go` | `internal/skills/` (the installer) |
| `skills/embed.go` | `skills/` (the skill files and their loader) |
| `internal/textdiff/` | `internal/textdiff/` |

Airframe's own tools and skills sit on top of that.

**Tech Stack:** Go 1.27 (`toolchain go1.27.4`), pinned deps only, the same set Starboard uses. No MCP SDK.

**Spec:** `docs/specs/2026-11-27-airframe-design.md`:
- §6.6 "AI skills" and "MCP (`airframe mcp`)";
- §3 "the three tools share conventions: … `skills install`";
- §6.5 `airframe skills … | mcp`;
- §6.7 item 7.

Starboard's MCP spec is the model for everything not stated here.

## Global Constraints

- License header on every Go file: `// SPDX-License-Identifier: AGPL-3.0-only`, then `// Copyright 2026 C65 LLC`.
- **Ported code keeps its tests.** Every ported file's tests come with it, adapted, and must pass.
- **Writes are bounded.** Writes happen only through `os.Root`, and only from the write tools and `skills install`. `--read-only` leaves the write tools out of `tools/list` and refuses them on `tools/call`.
- **Determinism.** Tool results and installed files are byte-stable.
- **Exit codes.** `airframe mcp` exits 0 when stdin closes and 2 when there is no project or the stdio streams fail.
- **Public repo.** No private-project names, paths or emails in code or docs. Starboard's own names (`starboard_*`, `screens.yaml`) must not leak into Airframe's text. Grep for them.

## Decisions this plan makes

### Skills (Task 1)

There are four skills, each written in Starboard's skill format: front matter (`name`, `description`) and the sections **When to use**, **Workflow** and **Rules**. They are agent-neutral.

| Skill | Use when | Workflow |
|---|---|---|
| `airframe` | before styling anything in a project with `airframe.yaml` | find the role token (`airframe find`, or `airframe_find`); check the file may read it (`find --file`, `airframe_can_read`); never hard-code a value a token holds; after token edits run `fmt`, `build`, `lint`; read findings with `explain` |
| `airframe-add-token` | adding or changing a token | choose the tier (primitive, semantic or component, and what each may read); density values; mode values; `emit` targets; naming; then `fmt`, `build`, `lint`; prefer `airframe_token_set` with preview |
| `airframe-component` | adding or changing a component | the `components/<id>.yaml` inventory: owned tokens, owners, implementations per platform or `n/a: reason`, and the `@ruling:` reference to Gantry; `airframe show <component>`; `airframe_component_set` |
| `airframe-adopt` | bringing Airframe into an existing app | `init`, then `import css` with its equivalence check, then platforms and consumers, wrappers, components, and CI (the Action, `diff --format md`) |

- **Install targets.** The skills install for the same agents as Starboard: claude-code, cursor, copilot, codex, gemini and aider. Each agent's files are rendered the way Starboard renders them, including the staging under `.airframe/pending/` for files that already exist.
- **Command name.** `skills hooks` prints `airframe fmt && airframe lint` hook configuration, and `skills mcp <agent>` prints the client configuration that starts `airframe mcp`. Agent detection follows Starboard. Everywhere Starboard writes "starboard", Airframe writes "airframe": `.airframe/`, and the command name.
- **References test.** The skill files' internal references are checked, ported from Starboard's `refs_test.go`. Every command, flag, MCP tool and rule ID a skill names must exist: commands are checked against `cli.Commands`, flags against their command's flags, tools against `mcp.Tools()`, and rules against `lint.AllIDs()`.

### MCP tools (Tasks 2–3)

Every tool name matches `^airframe_[a-z_]+$`. Results are JSON text, fenced exactly as Starboard fences them, and carry `structuredContent` from protocol version 2025-06-18. Every call reloads the project.

**Read tools (Task 2):**

| Tool | Arguments | Result |
|---|---|---|
| `airframe_find` | `query`; optional `file`, `type`, `limit`, `retired` | the `find --json` output. `file` here is project-root-relative, since the server has no cwd. |
| `airframe_show` | `name`; optional `component` | `show --json` for a token or a component. A token also carries `recordVersion`. |
| `airframe_usages` | `token`; optional `platform` | `usages --json` |
| `airframe_can_read` | `file`, `token` | `{allowed, why}` from `lint.CanRead` |
| `airframe_lint` | (none) | the lint JSON report, Starboard's lint tool shape |
| `airframe_diff` | `base`, optional `head` | `diff --format json`. Refs go through the same safe resolution as the CLI. |
| `airframe_specimen` | `sheet` (`colors`, `type` or `spacing`); optional `variant`, the fan-out value | the SVG, fenced as SVG the way Starboard's render tool does it |
| `airframe_rules` | (none) | `rules --json` |

**Write tools (Task 3):**

`airframe_token_set` sets fields on one token. Its arguments:
- `name`;
- `set`, a map of dotted keys to values: `$value`, `$type`, `$description`, `tier`, `component`, `emit`, `retired`, `density.<value>`, `modes.<key>` (a mode value in the right mode file), `names.<format>`; a `null` value removes the key;
- optional `create` (true, to add a new token at a path: `path`, `file`);
- `preview` and `format` (`full` or `diff`);
- `baseVersion`.

`airframe_component_set` sets fields on one component file: `id`, `set` (`noun`, `ruling`, `tokens`, `owners`, `implementations.<platform>`, `hostedBy`, `shared`), `create`, `preview`, `format`, `baseVersion`.

**Write pipeline,** following Starboard's `edit`:
1. Apply the edit to the source in memory, keeping key order with `ojson` or YAML nodes.
2. Format the touched files with the `fmt` canonical form.
3. Build the project from the overlay.
4. Report what changed and what `build --check` would say.
5. With `preview: true`, return the diffs or full files and the lint report, and write nothing.
6. Without `preview`, write the source files and regenerate the outputs the build produces. Return the files changed and the lint report.

**Rules for writes:**
- `recordVersion` is a hash of the record's current source bytes: a token's node, or a component's file. A `baseVersion` that doesn't match is a conflict error, and nothing is written.
- An edit that would make the project fail to load or build is refused, and nothing is written.
- Renames are not edits. `name` can't be changed through `set`; the tool's error says to use `airframe rename`.

**Audit line.** Each call writes one line to stderr in Starboard's format: the tool, the record and whether it wrote.

## Review Focus

1. **Protocol bounds.** Check these behave as in Starboard's tests: an oversize line, a non-JSON line, a batch, an unknown method, and `tools/call` for a write tool under `--read-only`. → Task 2, the ported `TestProtocol*`.
2. **A stale `baseVersion`** must refuse, writing nothing. A missing `baseVersion` on an update is allowed, as in Starboard, but documented. → Task 3, `TestTokenSetConflict`.
3. **Mode values.** A `modes.dark` edit on a token whose mode file doesn't exist yet must create it in the configured layout, or refuse with a clear message. It must never write it somewhere lint won't load it. → Task 3, `TestTokenSetModeFile`.
4. **Skill references.** If a skill names a command, flag, tool or rule that doesn't exist, the build fails. → Task 1, `TestSkillRefs`.
5. **Path escapes.** `airframe_can_read` and `airframe_find` with a `file` of `../x` or `/abs` return a clear error, not a result. → Task 2, `TestToolPaths`.

---

### Task 1: Skills and `airframe skills`

**Files:**
- `skills/` (embed.go and the four `.md` files)
- `internal/skills/` (ported install, hooks and mcp)
- `internal/cli/skills.go`
- the tests, ported and new

- [ ] Step 1: Port the tests and write the new ones: `TestSkillsLoad`, `TestSkillRefs`, the install and hooks tests per agent, `TestSkillsCLI`. Every installed file must be a golden.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Port the installer and write the skills.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: agent skills and airframe skills install`.

### Task 2: The MCP protocol and read tools

**Files:**
- `internal/mcp/` (the ported protocol, errors, fence, version and overlay, plus a new `tools_read.go`)
- `internal/cli/mcp.go`
- the tests

- [ ] Step 1: Port the protocol tests. Write one test per read tool (`TestToolFind` … `TestToolRules`), plus `TestToolPaths` and `TestToolsListReadOnly`.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: airframe mcp with read tools`.

### Task 3: Write tools

**Files:**
- `internal/mcp/tools_write.go`
- `internal/textdiff/`, ported for the `diff` format
- the tests

- [ ] Step 1: Write the tests:
  - `TestTokenSet`: each settable key, plus null removal;
  - `TestTokenSetCreate`;
  - `TestTokenSetModeFile`;
  - `TestTokenSetConflict`;
  - `TestTokenSetRefusesBreak`;
  - `TestTokenSetRename`;
  - `TestComponentSet` with its create and conflict cases;
  - `TestWritePreviewDiff`;
  - `TestWriteRegeneratesOutputs`.
- [ ] Step 2: Run them and see them FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: Run the suite and see it pass.
- [ ] Step 5: Commit `feat: airframe mcp write tools`.

### Task 4: End to end and docs

**Files:**
- `cmd/airframe/testdata/script/{skills,mcp}.txtar`. The mcp script pipes a session of JSON-RPC lines into `airframe mcp` and checks the responses.
- `docs/guides/agents.md`, covering:
  - skills: install, the agents, staging, hooks;
  - MCP: setup per agent via `skills mcp`, every tool with an example call and result, previews, versions, `--read-only`.
  - A guide test asserts the quoted results.
- `README.md` and `docs_test`: skills and MCP join the built list as "(next release)". Planned keeps the PWA, VS Code and Tauri.
- The spec: status line, §6.5, §6.6 and §6.7 item 7 (done).

- [ ] Step 1: Write the txtars and the guide test, and see them FAIL.
- [ ] Step 2: Write the docs.
- [ ] Step 3: Run `go test ./... && go vet ./... && gofmt -l .`. It must be clean.
- [ ] Step 4: Commit `feat: skills and MCP end to end`.
