// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// These tests check the skills' text from outside the package: they import
// internal/cli, which imports internal/skills, which imports this package,
// so an in-package test importing cli would be an import cycle.
package skills_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c65llc/airframe/internal/cli"
	"github.com/c65llc/airframe/internal/importer"
	"github.com/c65llc/airframe/internal/lint"
	installer "github.com/c65llc/airframe/internal/skills"
	"github.com/c65llc/airframe/skills"
)

// TestSkillSections: every body has a "# " title, then exactly the H2
// sections When to use, Workflow and Rules in that order, and the Workflow
// is a numbered list starting at 1.
func TestSkillSections(t *testing.T) {
	for _, s := range skills.All() {
		t.Run(s.Name, func(t *testing.T) {
			assert.NoError(t, checkSections(s.Body))
		})
	}
}

func TestCheckSectionsRejects(t *testing.T) {
	for name, body := range map[string]string{
		"no title":        "## When to use\n\nx\n\n## Workflow\n\n1. x\n\n## Rules\n\n- x\n",
		"missing rules":   "# T\n\n## When to use\n\nx\n\n## Workflow\n\n1. x\n",
		"wrong order":     "# T\n\n## Workflow\n\n1. x\n\n## When to use\n\nx\n\n## Rules\n\n- x\n",
		"extra section":   "# T\n\n## When to use\n\nx\n\n## Workflow\n\n1. x\n\n## Rules\n\n- x\n\n## More\n\nx\n",
		"unnumbered flow": "# T\n\n## When to use\n\nx\n\n## Workflow\n\n- x\n\n## Rules\n\n- x\n",
		"empty rules":     "# T\n\n## When to use\n\nx\n\n## Workflow\n\n1. x\n\n## Rules\n",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, checkSections(body))
		})
	}
	assert.NoError(t, checkSections("# T\n\n## When to use\n\nx\n\n## Workflow\n\n1. x\n2. y\n\n## Rules\n\n- x\n"))
}

// checkSections reports how body departs from the runbook shape.
func checkSections(body string) error {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "# ") {
		return fmt.Errorf("body must open with a # title")
	}
	var heads []string
	content := map[string]string{}
	cur := ""
	var f fences
	for _, l := range lines[1:] {
		if _, code := f.line(l); !code && strings.HasPrefix(l, "## ") {
			cur = strings.TrimPrefix(l, "## ")
			heads = append(heads, cur)
			continue
		}
		if cur != "" {
			content[cur] += l + "\n"
		}
	}
	want := []string{"When to use", "Workflow", "Rules"}
	if !slices.Equal(heads, want) {
		return fmt.Errorf("sections are %q, want %q", heads, want)
	}
	for _, h := range want {
		if strings.TrimSpace(content[h]) == "" {
			return fmt.Errorf("section %q is empty", h)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(content["Workflow"]), "1. ") {
		return fmt.Errorf("the Workflow must be a numbered list starting at 1")
	}
	return nil
}

// --- References to commands and rules ---------------------------------------

// The test below checks that every command and rule ID a skill names is
// real, so a renamed rule or command breaks the build instead of an agent.
//
// Code is the text inside inline code spans (`…`) and every line of a fenced
// code block. Every "airframe" followed by whitespace in code starts an
// invocation: optional global flags, then a command that must be in
// cli.Commands (and, for a group such as skills, a subcommand of it), then
// flags that must be that command's or global flags (a
// flag with an Arg consumes the next word), then bare words, which are
// allowed only when the command takes positional arguments (Args is set).
// The invocation ends at the end of the code, or at "#", "|", "&&", "||",
// ";" or ")". So `airframe.yaml` is not an invocation, and a made-up
// subcommand such as `airframe skills remove` fails.
//
// A rule ID is a kebab-case word with at least one hyphen. It is checked
// only in a rules context, defined so that ordinary kebab-case words (region
// names such as `ink-muted`, paths, flags) are never mistaken for one:
//
//   - in prose: a run of inline code spans joined only by ", ", " and ",
//     " or ", ", and " or ", or " (one span alone is a run of one) that is
//     immediately preceded by the word "rule", "rules", "finding" or
//     "findings", or immediately followed by one of them. Every kebab-case
//     span in such a run must be a rule ID. So "the `tier-violation` and
//     `alias-unresolved` rules" checks both, and "the `ink-muted` token"
//     checks nothing.
//   - in a fenced code block: the value of a JSON "rule" key,
//     "rule": "<id>".
//
// A rule ID is real when lint.AllIDs() has it (every rule lint can
// report, load-time IDs included) or importer.IDs() does (the findings of
// airframe import css, which explain also documents).
//
// Plain prose (outside code spans and fences) cannot be checked like code:
// "airframe" there is often followed by an ordinary word ("the airframe
// project"), and a made-up command reads just like one. So prose is held
// to two narrower rules. A command named after a lowercase "airframe"
// belongs in a code span, where the check above covers its flags, so
// "airframe lint" in prose is an error. And "run airframe <word>" (or
// runs, running, rerun) must name a real command. A skill's description is
// a plain sentence, shown where code spans are not rendered, so it is held
// to the second rule only.

var (
	inlineCode   = regexp.MustCompile("`([^`\n]+)`")
	ruleShape    = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)+$`)
	jsonRule     = regexp.MustCompile(`"rule"\s*:\s*"([^"]*)"`)
	invocation   = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])airframe\s`)
	ruleWord     = `(?i:rules?|findings?)`
	ruleBefore   = regexp.MustCompile(`(?:^|\W)` + ruleWord + `\s+$`)
	ruleAfter    = regexp.MustCompile(`^\s+` + ruleWord + `(?:\W|$)`)
	runSeparator = regexp.MustCompile(`^(?:,\s+|\s+and\s+|\s+or\s+|,\s+and\s+|,\s+or\s+)$`)
	// proseAirframe is "airframe" in prose: what precedes it, and what is
	// attached to it ("." of a sentence, ".yaml" of a file). proseWord is
	// the word after it; matching it separately keeps "airframe
	// airframe lint" from hiding the second "airframe".
	proseAirframe = regexp.MustCompile(`(^|[^A-Za-z0-9_./-])airframe(\S*)`)
	proseWord     = regexp.MustCompile(`^\s+([a-z][a-z0-9-]*)`)
	proseRun      = regexp.MustCompile(`(?i)\b(?:re)?run(?:s|ning)?\s+$`)
)

// checkProse reports every command reference in prose lines that breaks the
// prose rules above; requireCode is false for a description.
func checkProse(prose []string, requireCode bool) []string {
	var problems []string
	for _, l := range prose {
		l = inlineCode.ReplaceAllString(l, " ")
		for _, m := range proseAirframe.FindAllStringSubmatchIndex(l, -1) {
			attached := l[m[4]:m[5]]
			if strings.ContainsFunc(attached, func(r rune) bool { return r == '-' || r == '_' || r == '\'' || unicode.IsLetter(r) }) {
				continue // airframe.yaml, airframe-adopt, airframe's: not the command
			}
			word := ""
			if w := proseWord.FindStringSubmatch(l[m[1]:]); attached == "" && w != nil {
				word = w[1] // after "airframe." the sentence has ended
			}
			_, isCommand := commandByName(word)
			switch {
			case word == "":
				// A bare "run airframe" names no command to check.
			case proseRun.MatchString(l[:m[3]]) && !isCommand:
				problems = append(problems, fmt.Sprintf("run airframe %s: not a command", word))
			case requireCode && isCommand:
				problems = append(problems, fmt.Sprintf("airframe %s: put the command in a code span", word))
			}
		}
	}
	return problems
}

// fences follows fenced code blocks line by line, as CommonMark does (and
// as the installer does): only a run of the opener's character at least as
// long closes a fence, and a line indented four spaces never opens one.
type fences struct{ open, info string }

// line feeds l to f and reports whether it is a fence line, and whether it
// is inside a block (fence lines included).
func (f *fences) line(l string) (fence, code bool) {
	l = strings.TrimRight(l, "\r")
	if f.open != "" {
		if installer.IsFenceClose(l, f.open) {
			f.open, f.info = "", ""
			return true, true
		}
		return false, true
	}
	if run, info, ok := installer.OpenFence(l); ok {
		f.open, f.info = run, info
		return true, true
	}
	return false, false
}

// unclosedFence returns the line of the fence body leaves open, or "".
func unclosedFence(body string) string {
	var f fences
	opener := ""
	for _, l := range strings.Split(body, "\n") {
		if fence, _ := f.line(l); fence && f.open != "" {
			opener = l
		}
	}
	if f.open == "" {
		return ""
	}
	return opener
}

// codeAndProse splits body into code (inline spans and fenced lines) and the
// prose lines outside fences.
func codeAndProse(body string) (code, prose []string) {
	var f fences
	for _, l := range strings.Split(body, "\n") {
		fence, inCode := f.line(l)
		if fence {
			continue
		}
		if inCode {
			code = append(code, l)
			continue
		}
		prose = append(prose, l)
		for _, m := range inlineCode.FindAllStringSubmatch(l, -1) {
			code = append(code, m[1])
		}
	}
	return code, prose
}

// commandByName returns the cli command called name.
func commandByName(name string) (cli.Command, bool) {
	for _, c := range cli.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return cli.Command{}, false
}

// findFlag returns the flag spelled tok ("--json", "--root=x") among
// flags, and whether its value was attached with "=".
func findFlag(tok string, flags []cli.Flag) (cli.Flag, bool, bool) {
	name := strings.TrimLeft(tok, "-")
	name, _, attached := strings.Cut(name, "=")
	for _, f := range flags {
		if f.Name == name {
			return f, attached, true
		}
	}
	return cli.Flag{}, false, false
}

// checkInvocation checks the words after one "airframe".
func checkInvocation(words []string) []string {
	var problems []string
	i := 0
	// Global flags.
	for i < len(words) && strings.HasPrefix(words[i], "-") {
		f, attached, ok := findFlag(words[i], cli.GlobalFlags)
		if !ok {
			return []string{fmt.Sprintf("airframe %s: unknown global flag", words[i])}
		}
		i++
		if f.Arg != "" && !attached {
			i++
		}
	}
	if i >= len(words) {
		return []string{"airframe: no command"}
	}
	cmd, ok := commandByName(words[i])
	if !ok {
		return []string{fmt.Sprintf("airframe %s: not a command", words[i])}
	}
	if len(cmd.Subcommands) > 0 {
		group := cmd.Name
		i++
		if i >= len(words) || strings.HasPrefix(words[i], "-") {
			return []string{fmt.Sprintf("airframe %s: no subcommand", group)}
		}
		j := slices.IndexFunc(cmd.Subcommands, func(c cli.Command) bool { return c.Name == words[i] })
		if j < 0 {
			return []string{fmt.Sprintf("airframe %s %s: not a subcommand of %s", group, words[i], group)}
		}
		cmd = cmd.Subcommands[j]
		cmd.Name = group + " " + cmd.Name
	}
	for i++; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(w, "-") {
			f, attached, ok := findFlag(w, append(slices.Clone(cmd.Flags), cli.GlobalFlags...))
			if !ok {
				problems = append(problems, fmt.Sprintf("airframe %s %s: not a flag of %s", cmd.Name, w, cmd.Name))
				continue
			}
			if f.Arg != "" && !attached {
				i++
			}
			continue
		}
		if cmd.Args == "" {
			problems = append(problems, fmt.Sprintf("airframe %s %s: %s takes no arguments or subcommands", cmd.Name, w, cmd.Name))
		}
	}
	return problems
}

// redirect is a shell redirection word, such as ">", ">&2" or "2>/dev/null".
var redirect = regexp.MustCompile(`^[0-9]?>`)

// invocationWords splits the text after "airframe" into words, stopping at
// the first shell separator or comment.
func invocationWords(rest string) []string {
	var words []string
	for _, w := range strings.Fields(rest) {
		if strings.HasPrefix(w, "#") || slices.Contains([]string{"|", "&&", "||", ";"}, w) || redirect.MatchString(w) {
			break
		}
		if trimmed, cut := strings.CutSuffix(w, ")"); cut {
			if trimmed != "" {
				words = append(words, trimmed)
			}
			break
		}
		words = append(words, w)
	}
	return words
}

// checkCommands reports every invocation in code that is not a real command.
func checkCommands(code []string) []string {
	var problems []string
	for _, c := range code {
		starts := invocation.FindAllStringIndex(c, -1)
		for i, m := range starts {
			end := len(c) // an invocation runs to the next one at most
			if i+1 < len(starts) {
				end = starts[i+1][0]
			}
			problems = append(problems, checkInvocation(invocationWords(c[m[1]-1:end]))...)
		}
	}
	return problems
}

// knownRule reports whether id is a real rule ID.
func knownRule(id string) bool {
	return slices.Contains(lint.AllIDs(), id) || slices.Contains(importer.IDs(), id)
}

// ruleIDsInContext returns every kebab-case token in a rules context: the
// spans of each run of inline code in a prose line that is immediately
// preceded or followed by a rule word, and JSON "rule" values in code.
func ruleIDsInContext(code, prose []string) []string {
	var ids []string
	for _, c := range code {
		for _, m := range jsonRule.FindAllStringSubmatch(c, -1) {
			ids = append(ids, m[1])
		}
	}
	for _, l := range prose {
		spans := inlineCode.FindAllStringSubmatchIndex(l, -1)
		for start := 0; start < len(spans); {
			end := start // the run is spans[start..end]
			for end+1 < len(spans) && runSeparator.MatchString(l[spans[end][1]:spans[end+1][0]]) {
				end++
			}
			before := l[:spans[start][0]]
			after := l[spans[end][1]:]
			if ruleBefore.MatchString(before) || ruleAfter.MatchString(after) {
				for _, s := range spans[start : end+1] {
					if tok := l[s[2]:s[3]]; ruleShape.MatchString(tok) {
						ids = append(ids, tok)
					}
				}
			}
			start = end + 1
		}
	}
	return ids
}

// checkRules reports every rule ID in a rules context that is not real.
func checkRules(code, prose []string) []string {
	var problems []string
	for _, id := range ruleIDsInContext(code, prose) {
		if !knownRule(id) {
			problems = append(problems, fmt.Sprintf("%q is not a rule ID", id))
		}
	}
	return problems
}

// checkReferences is every command and rule problem in body.
func checkReferences(body string) []string {
	code, prose := codeAndProse(body)
	return slices.Concat(checkCommands(code), checkRules(code, prose), checkProse(prose, true))
}

func TestSkillsReferenceRealCommandsAndRules(t *testing.T) {
	for _, s := range skills.All() {
		t.Run(s.Name, func(t *testing.T) {
			assert.Empty(t, checkReferences(s.Body))
			assert.Empty(t, checkProse([]string{s.Description}, false))
		})
	}
}

// TestCheckProseInDescriptions: a description is a plain sentence, so it
// may name a command without backticks, but a command it runs must be real.
// TestHooksReferenceRealCommands: the hook configuration the installer
// prints runs airframe commands too, so it is held to the same check: the
// Claude Code command, the git hook's command lines, and the paragraph for
// the other agents.
func TestHooksReferenceRealCommands(t *testing.T) {
	code := []string{installer.HookCommand}
	for _, name := range installer.HookNames {
		out, err := installer.Hooks(name)
		require.NoError(t, err)
		if name == installer.PreCommit {
			for _, l := range strings.Split(out, "\n") {
				if !strings.HasPrefix(l, "#") {
					code = append(code, l)
				}
			}
			continue
		}
		if name != string(installer.ClaudeCode) {
			c, prose := codeAndProse(out)
			code = append(code, c...)
			assert.Empty(t, checkProse(prose, true), name)
		}
	}
	assert.Empty(t, checkCommands(code))
	// A redirection ends an invocation, as "&&" does.
	assert.Empty(t, checkCommands([]string{"airframe fmt > /dev/null && airframe lint >&2 || exit 2"}))
	assert.NotEmpty(t, checkCommands([]string{"airframe fmt x > /dev/null"}))
}

// TestCheckProseEdgeCases: a repeated "airframe" does not hide the
// command after it, and a bare "run airframe" ending a sentence names no
// command, so there is nothing to check.
func TestCheckProseEdgeCases(t *testing.T) {
	assert.Equal(t, []string{"airframe lint: put the command in a code span"},
		checkProse([]string{"Then airframe airframe lint reports it."}, true))
	assert.Equal(t, []string{"run airframe check: not a command"},
		checkProse([]string{"The airframe run airframe check line."}, true))
	assert.Empty(t, checkProse([]string{"Run airframe.", "Then rerun airframe"}, true))
}

func TestCheckProseInDescriptions(t *testing.T) {
	assert.Empty(t, checkProse([]string{"Use it, keeping airframe lint passing; run airframe fmt first."}, false))
	assert.NotEmpty(t, checkProse([]string{"Use it, and run airframe check after."}, false))
	assert.NotEmpty(t, checkProse([]string{"Use it, and run airframe after."}, false))
}

// TestSkillFencesAreBalanced: every fenced code block a skill body opens is
// closed. An unclosed one would run to the end of the section file it is
// installed into, and every later install would refuse that file.
func TestSkillFencesAreBalanced(t *testing.T) {
	for _, s := range skills.All() {
		t.Run(s.Name, func(t *testing.T) {
			assert.Empty(t, unclosedFence(s.Body))
		})
	}
	assert.Equal(t, "```bash", unclosedFence("# T\n\n```bash\nx\n"))
	assert.Equal(t, "````", unclosedFence("````\n```\n"))
	assert.Equal(t, "~~~", unclosedFence("~~~\n```\n```\n"))
	assert.Empty(t, unclosedFence("```\nx\n```\n    ```\n"))
}

// TestSkillsReferenceSomething guards the check above against passing
// vacuously: every skill runs lint, and between them the skills name rules
// in a rules context.
func TestSkillsReferenceSomething(t *testing.T) {
	var ids []string
	for _, s := range skills.All() {
		code, prose := codeAndProse(s.Body)
		assert.True(t, slices.ContainsFunc(code, func(c string) bool { return strings.Contains(c, "airframe lint") }),
			"%s never runs airframe lint", s.Name)
		ids = append(ids, ruleIDsInContext(code, prose)...)
	}
	for _, id := range []string{"tier-violation", "primitive-read-by-consumer", "token-owner-violation", "emitted-stale", "component-unruled"} {
		assert.Contains(t, ids, id)
	}
}

func TestCheckReferencesFindsMadeUpCommandsAndRules(t *testing.T) {
	bad := map[string]string{
		"made-up command":          "Run `airframe check`.",
		"made-up command in fence": "```bash\nairframe publish --all\n```\n",
		"subcommand of a command":  "Run `airframe lint all`.",
		"made-up subcommand":       "Run `airframe skills remove`.",
		"missing subcommand":       "Run `airframe skills`.",
		"flag before subcommand":   "Run `airframe skills --dry-run install`.",
		"flag of a sibling":        "Run `airframe skills list --dry-run`.",
		"argument to list":         "Run `airframe skills list all`.",
		"made-up hook":             "Run `airframe hook cursor`.",
		"argument to lint":         "Run `airframe lint everything`.",
		"made-up flag":             "Run `airframe lint --quiet`.",
		"flag of another command":  "Run `airframe build --json`.",
		"made-up global flag":      "Run `airframe --dir x lint`.",
		"no command":               "Run `airframe --root x`.",
		"argument to fmt":          "Run `airframe fmt design-system`.",
		"made-up rule before":      "Fix the rule `token-too-big`.",
		"made-up rule after":       "Fix every `token-too-big` finding.",
		"made-up rule in a run":    "The `tier-violation`, `alias-unresolved` or `token-too-big` rules.",
		"made-up rule in json":     "```json\n{\"rule\": \"token-too-big\"}\n```\n",
		"inside a longer fence":    "````md\n```\nairframe check\n```\n````\n",
		"command in plain prose":   "Then airframe lint reports it.",
		"made-up command in prose": "Then run airframe check.",
		"bare run in prose":        "Rerun airframe after the edit.",
		"inside a tilde fence":     "~~~\n```\nairframe check\n~~~\n",
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			assert.NotEmpty(t, checkReferences(body))
		})
	}
}

func TestCheckReferencesAcceptsRealOnesAndOrdinaryWords(t *testing.T) {
	good := strings.Join([]string{
		"Edit `airframe.yaml` and use the `airframe-add-token` skill.",
		"Run `airframe fmt`, then `airframe lint --json` (`airframe --root app lint --strict`).",
		"Run `airframe init --profile minimal` and `airframe find muted text --file web/src/app.css`.",
		"Run `airframe schema tokens` or `airframe build --check && git add .`.",
		"Run `airframe import css --dry-run theme.css` and `airframe show --component button`.",
		"Run `airframe skills install --agent codex --dry-run`, `airframe skills list` and `airframe skills hooks pre-commit`.",
		"Run `airframe skills mcp cursor --read-only` (it prints config for `airframe mcp --read-only`).",
		"The `ink-muted` token and the `accent-ink` role are not rules.",
		"The `tier-violation`, `alias-unresolved` and `token-owner-violation` rules; an `emitted-stale` finding; the `import-no-base` finding.",
		"```bash\nairframe fmt   # canonical form\nairframe lint --json | jq .\n```",
		"```json\n{\"rule\": \"literal-in-semantic\"}\n```",
		"The airframe project, Airframe's lint and the airframe-adopt skill are prose.",
	}, "\n")
	assert.Empty(t, checkReferences(good))
}

// --- Forbidden instructions --------------------------------------------------

// A skill never tells an agent to hand-edit generated/, to skip airframe
// lint, to hard-code a value, or to keep an alias under a renamed token's
// old name (renames ship with a codemod, never with compatibility aliases).
// Each forbidden pattern below matches the instruction, and its "verb"
// group is the word that gives it. A match is allowed only when it is
// negated: "never", "do not", "don't" or "not" comes before the verb, in the
// verb's own clause (after the last ".", ";", ",", ":" or dash before the
// verb). So "Never hand-edit `generated/`" is allowed, and "Edit
// `generated/`; never mind" and "If lint is not installed, skip lint" are
// not. This cannot catch every paraphrase; together with the
// positive checks after it, it catches the plain ones.

var (
	negation  = regexp.MustCompile(`(?i)\b(never|do not|don't|not)\b`)
	forbidden = []*regexp.Regexp{
		// Editing generated output by hand.
		regexp.MustCompile("(?i)\\b(?P<verb>hand-edit|edit|modify|change|update|write|fix)\\b[^.]*`?generated/"),
		regexp.MustCompile("(?i)`?generated/`?[^.]*\\b(?P<verb>by hand)\\b"),
		// Skipping lint.
		regexp.MustCompile(`(?i)\b(?P<verb>skip|omit|bypass)\b[^.]*\blint\b`),
		regexp.MustCompile("(?i)\\b(?P<verb>without) (running )?`?(airframe )?lint"),
		regexp.MustCompile(`(?i)(?P<verb>--no-verify)`),
		// Hard-coding a value a token holds.
		regexp.MustCompile(`(?i)\b(?P<verb>hard-code|hard-coding|hardcode|hardcoding)\b`),
		// A compatibility alias under a renamed token's old name.
		regexp.MustCompile(`(?i)\b(?P<verb>keep|leave|add)\b[^.]*\balias(es)?\b[^.]*\bold name\b`),
	}
)

// clauseBreak is punctuation that ends a clause.
var clauseBreak = regexp.MustCompile(`[.;,:]\s|\s[-–—]+\s`)

// negated reports whether the match m of re in l is negated: a negation
// comes before its verb, in the verb's own clause.
func negated(re *regexp.Regexp, l string, m []int) bool {
	verb := m[2*re.SubexpIndex("verb")]
	clause := 0
	for _, b := range clauseBreak.FindAllStringIndex(l[:verb], -1) {
		clause = b[1]
	}
	return negation.MatchString(l[clause:verb])
}

// checkForbidden returns every line of body that gives a forbidden
// instruction.
func checkForbidden(body string) []string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if slices.ContainsFunc(forbidden, func(re *regexp.Regexp) bool {
			return slices.ContainsFunc(re.FindAllStringSubmatchIndex(l, -1), func(m []int) bool { return !negated(re, l, m) })
		}) {
			out = append(out, l)
		}
	}
	return out
}

func TestSkillsGiveNoForbiddenInstruction(t *testing.T) {
	for _, s := range skills.All() {
		t.Run(s.Name, func(t *testing.T) {
			assert.Empty(t, checkForbidden(s.Body))
			assert.NotContains(t, s.Body, "airframe skills", "the skills are read after install; installing is the guide's job")
			assert.NotContains(t, s.Body, installer.StartMarker, "a marker in a skill would break its own section")
			assert.NotContains(t, s.Body, installer.EndMarker, "a marker in a skill would break its own section")
		})
	}
}

func TestCheckForbiddenCatchesThePlainForms(t *testing.T) {
	for _, l := range []string{
		"Edit `generated/css/tokens.css` to add the property.",
		"Fix the colour in generated/json by hand.",
		"Skip `airframe lint` when in a hurry.",
		"Commit without running lint.",
		"Commit with --no-verify.",
		"Hard-code the hex value when no token fits.",
		"Keep an alias under the old name so nothing breaks.",
		// A negation after the verb, or in another sentence, does not negate it.
		"Edit `generated/css/tokens.css`; never mind the hook.",
		"Skip `airframe lint`, as it is not needed.",
		"Fix the colour in generated/ by hand, not in the token.",
		"Hardcode `8px` for now, not later.",
		"Do not wait. Edit `generated/css/tokens.css` now.",
		"Never hand-edit `generated/`. Edit `generated/css/tokens.css` instead.",
		// A negation in an earlier clause governs that clause, not the verb.
		"Do not worry, edit generated/ by hand.",
		"If lint is not installed, skip lint.",
		"It is not hard: edit `generated/` directly.",
		"Not now - skip lint.",
	} {
		assert.NotEmpty(t, checkForbidden(l), l)
	}
	for _, l := range []string{
		"Never hand-edit `generated/`; run `airframe build`.",
		"Do not skip `airframe lint`.",
		"Never hard-code a value a token holds.",
		"Never add an alias under the old name.",
		"`generated/` is written by `airframe build`.",
		"`generated/` is never edited by hand.",
		"Never commit with --no-verify.",
	} {
		assert.Empty(t, checkForbidden(l), l)
	}
}

// TestSkillsSayTheLoadBearingRules pins the sentences each skill exists to
// teach, so an edit cannot quietly drop one.
func TestSkillsSayTheLoadBearingRules(t *testing.T) {
	want := map[string][]string{
		"airframe": {
			"run `airframe fmt` then `airframe build`, then `airframe lint --json`",
			"Never hard-code a value a token holds",
			"Never hand-edit `generated/`",
			"`airframe.yaml`",
			"cannot be configured",
			"airframe find muted text --file web/src/app.css",
			"never a primitive",
			"`airframe rename <old> <new>`",
		},
		"airframe-add-token": {
			"**`primitive`**", "**`semantic`**", "**`component`**", "`density`", "`chrome`", "`emit`",
			`"modes"`, "`preview: true`", "`baseVersion`", "`airframe lint`", "never with compatibility aliases",
		},
		"airframe-component": {
			"`components/<id>.yaml`", "@ruling:<id>", "`owners`", "`implementations`", "n/a: <reason>",
			"`airframe show <component>`", "do not make one up", "`preview: true`", "`airframe lint`",
		},
		"airframe-adopt": {
			"`airframe init`", "airframe import css --dry-run theme.css", "equivalent", "`consumers`", "`wrappers`",
			"one area at a time", "Commit only with `airframe lint` passing", "c65llc/airframe@v0", "--format md",
		},
	}
	// Phrases a skill must not say. A pinned release in the Action example
	// would go stale with the next release.
	mustNot := map[string][]string{
		"airframe-adopt": {"version: v0."},
	}
	byName := map[string]skills.Skill{}
	for _, s := range skills.All() {
		byName[s.Name] = s
	}
	for name, phrases := range want {
		s, ok := byName[name]
		require.True(t, ok, name)
		for _, p := range phrases {
			assert.Contains(t, s.Body, p, "%s must say %q", name, p)
		}
	}
	for name, phrases := range mustNot {
		for _, p := range phrases {
			assert.NotContains(t, byName[name].Body, p, "%s must not say %q", name, p)
		}
	}
}

// TestGuideReferencesRealCommands: the agents guide names commands the
// way the skills do, so it is held to the same check.
func TestGuideReferencesRealCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "guides", "agents.md"))
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("TODO(plan 7, task 4): docs/guides/agents.md is written in task 4; delete this skip then")
	}
	require.NoError(t, err)
	// Inline spans and shell blocks only: the guide also quotes JSON and
	// Markdown, whose own backticks and quotes are not invocations. Its
	// prose is held to the skills' prose rules.
	var code, prose []string
	var f fences
	for _, l := range strings.Split(string(data), "\n") {
		fence, inCode := f.line(l)
		switch {
		case fence:
		case inCode && (f.info == "bash" || f.info == "sh"):
			code = append(code, l)
		case !inCode:
			prose = append(prose, l)
			for _, m := range inlineCode.FindAllStringSubmatch(l, -1) {
				code = append(code, m[1])
			}
		}
	}
	assert.Empty(t, checkCommands(code))
	assert.Empty(t, checkProse(prose, true))
	assert.True(t, slices.ContainsFunc(code, func(c string) bool { return strings.Contains(c, "airframe skills install") }))
}
