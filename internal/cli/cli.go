// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package cli is the airframe command line. Commands describes every
// command and flag; Main parses a command line against it and runs it. The
// exit status is 0 when the command succeeds cleanly, 1 for findings (lint
// errors, warnings under --strict, files fmt --check or build --check would
// change, a project that did not load, files fmt cannot format, an import
// that failed), and 2 for
// a usage error or a command that could not run.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/c65llc/airframe/docs"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/format"
	"github.com/c65llc/airframe/internal/gate"
	"github.com/c65llc/airframe/internal/lint"
	"github.com/c65llc/airframe/internal/project"
	"github.com/c65llc/airframe/internal/scaffold"
	"github.com/c65llc/airframe/internal/schema"
	"github.com/c65llc/airframe/internal/selftest"
	"github.com/c65llc/airframe/internal/tokens"
	"github.com/c65llc/airframe/internal/version"
)

// The exit statuses Main returns.
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitUsage    = 2
)

// Flag is one command-line flag. A flag with an Arg takes a value; one
// without is a boolean. A value flag without Repeat may be given once.
type Flag struct {
	Name    string // without the leading dashes
	Arg     string // the value's placeholder; empty for a boolean flag
	Repeat  bool   // whether it may be given more than once
	Summary string
}

// spelling is the flag and its placeholder, such as "--root D".
func (f Flag) spelling() string {
	s := "--" + f.Name
	if f.Arg != "" {
		s += " " + f.Arg
	}
	return s
}

// synopsis is the flag as a usage line shows it, such as "[--root D]".
func (f Flag) synopsis() string {
	s := "[" + f.spelling() + "]"
	if f.Repeat {
		s += "..."
	}
	return s
}

// Command is one airframe command.
type Command struct {
	Name string
	Args string // the positional arguments as the usage shows them; empty for none
	// Short, when set, stands for the flags and arguments in the usage's
	// command table, for a command whose full synopsis would widen it;
	// the command's own usage still lists every flag.
	Short   string
	Summary string
	Flags   []Flag
}

// Synopsis is the command as its usage line shows it after
// "airframe [--root D] ", such as "lint [--json] [--format F] [--strict]".
func (c Command) Synopsis() string {
	parts := []string{c.Name}
	for _, f := range c.Flags {
		parts = append(parts, f.synopsis())
	}
	if c.Args != "" {
		parts = append(parts, c.Args)
	}
	return strings.Join(parts, " ")
}

// GlobalFlags come before the command.
var GlobalFlags = []Flag{
	{Name: "root", Arg: "D", Summary: "the project root (default: the nearest directory up from here holding airframe.yaml)"},
}

// lintFormats are the values lint --format takes.
var lintFormats = []string{"text", "json", "github", "gate"}

// Commands is every command, in the order the usage lists them.
var Commands = []Command{
	{
		Name:    "init",
		Summary: "Write a starter project here (or in --root) and build it; never overwrites airframe.yaml.",
		Flags:   []Flag{{Name: "profile", Arg: "P", Summary: "start from profile P (default: minimal)"}},
	},
	{
		Name:    "fmt",
		Summary: "Rewrite the config, token, invariants and component files in canonical form, listing each one.",
		Flags:   []Flag{{Name: "check", Summary: "list the files fmt would change, change nothing, and exit 1 if any"}},
	},
	{
		Name:    "lint",
		Summary: "Check the project, report its findings, and tally what each rule scanned.",
		Flags: []Flag{
			{Name: "json", Summary: "print the findings as a versioned JSON report"},
			{Name: "format", Arg: "F", Summary: "print as F: text, json, github (workflow commands) or gate (self-test first)"},
			{Name: "strict", Summary: "treat warnings as errors"},
		},
	},
	{
		Name:    "build",
		Summary: "Write the generated outputs every platform emits, listing each file written.",
		Flags: []Flag{
			{Name: "check", Summary: "list the outputs build would write, change nothing, and exit 1 if any"},
			{Name: "platform", Arg: "P", Repeat: true, Summary: "build only platform or format P"},
		},
	},
	{
		Name:    "import",
		Args:    "css <file>...",
		Summary: "Turn stylesheets of CSS custom properties into token files, proving the generated CSS equivalent.",
		Flags: []Flag{
			{Name: "dry-run", Summary: "check the import, print what it would write, and write nothing"},
			{Name: "force", Summary: "overwrite existing token files, removing one the import no longer produces"},
		},
	},
	{
		Name:    "show",
		Args:    "<token|component>",
		Summary: "Print a token's tier, component, type, description, value per mode key and alias chain; or a component's ruling, tokens, owners, implementations and consumers.",
		Flags: []Flag{
			{Name: "component", Summary: "show the component of that name, even when a token has it too"},
			{Name: "json", Summary: "print the same as a JSON object"},
		},
	},
	{
		Name:    "find",
		Args:    "<query>...",
		Summary: "Rank the tokens that match a free-text query by their names, descriptions and components; exit 1 when none do.",
		Flags: []Flag{
			{Name: "limit", Arg: "N", Summary: "print at most N hits (default: 10)"},
			{Name: "type", Arg: "T", Summary: "keep only tokens of DTCG type T"},
			{Name: "file", Arg: "F", Summary: "keep only tokens the consumer file F (a path from here, inside the project) may read, saying why"},
			{Name: "retired", Summary: "include retired tokens"},
			{Name: "json", Summary: "print the hits as a JSON array"},
		},
	},
	{
		Name:    "usages",
		Args:    "<token>",
		Summary: "List every consumer read of a token, direct or through a wrapper, and the tokens that alias it.",
		Flags: []Flag{
			{Name: "json", Summary: "print the same as a JSON object"},
			{Name: "platform", Arg: "P", Summary: "list only the reads in platform P"},
		},
	},
	diffCommand,
	renameCommand,
	retireCommand,
	specimenCommand,
	{
		Name:    "explain",
		Args:    "<rule-id|token>",
		Summary: "Print a rule's documentation, or show a token and which tiers may read it.",
	},
	{
		Name:    "rules",
		Summary: "List every rule ID: its default severity and summary.",
		Flags:   []Flag{{Name: "json", Summary: "print the rules as a versioned JSON report, each with its documentation page"}},
	},
	{
		Name:    "schema",
		Args:    "<config|tokens|invariants|component|retired|find|usages|diff>",
		Summary: "Print the JSON Schema of airframe.yaml, token files, invariants.yaml, component files, retired.yaml, or the output of find --json, usages --json or diff --format json.",
	},
	{
		Name:    "version",
		Summary: "Print airframe's version.",
	},
}

// handlers run each command by name.
var handlers = map[string]func(*invocation) int{
	"init":    runInit,
	"fmt":     runFmt,
	"lint":    runLint,
	"build":   runBuild,
	"import":  runImport,
	"show":    runShow,
	"usages":  runUsages,
	"find":    runFind,
	"diff":    runDiff,
	"retire":  runRetire,
	"explain": runExplain,
	"rules":   runRules,
	"schema":  runSchema,
	"version": runVersion,
}

// selftestRun is the self-test lint --format gate runs; tests replace it.
var selftestRun = selftest.Run

// invocation is one parsed command line.
type invocation struct {
	cmd            Command
	root           string // --root, or "" to search from the working directory
	args           []string
	bools          map[string]*bool
	lists          map[string]*[]string
	stdout, stderr io.Writer
}

// listValue is a flag that collects every value it is given.
type listValue struct{ values *[]string }

func (l listValue) String() string {
	if l.values == nil { // the zero value, which flag may make to print a default
		return ""
	}
	return strings.Join(*l.values, ",")
}

func (l listValue) Set(s string) error {
	*l.values = append(*l.values, s)
	return nil
}

// stickyWriter remembers the first error a write returns, and fails every
// write after it.
type stickyWriter struct {
	w   io.Writer
	err error
}

func (s *stickyWriter) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n, err := s.w.Write(p)
	s.err = err
	return n, err
}

// Main runs the command line args, which exclude the program name, and
// returns the exit status. No command reads stdin. If writing to stdout
// fails, the command's output is incomplete, so Main reports the error and
// returns 2.
func Main(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	out := &stickyWriter{w: stdout}
	code := dispatch(args, out, stderr)
	if out.err != nil {
		_, _ = fmt.Fprintf(stderr, "airframe: writing output: %v\n", out.err)
		return ExitUsage
	}
	return code
}

func dispatch(args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("airframe", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	root := global.String("root", "", "")
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(stdout)
			return ExitOK
		}
		_, _ = fmt.Fprintf(stderr, "airframe: %v\n", err)
		usage(stderr)
		return ExitUsage
	}
	rest := global.Args()
	if len(rest) == 0 {
		usage(stderr)
		return ExitUsage
	}
	if rest[0] == "help" {
		usage(stdout)
		return ExitOK
	}
	i := slices.IndexFunc(Commands, func(c Command) bool { return c.Name == rest[0] })
	if i < 0 {
		_, _ = fmt.Fprintf(stderr, "airframe: unknown command %s\n", rest[0])
		usage(stderr)
		return ExitUsage
	}
	inv := &invocation{
		cmd: Commands[i], root: *root, stdout: stdout, stderr: stderr,
		bools: map[string]*bool{}, lists: map[string]*[]string{},
	}
	flags := flag.NewFlagSet(inv.cmd.Name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	for _, f := range inv.cmd.Flags {
		if f.Arg == "" {
			inv.bools[f.Name] = flags.Bool(f.Name, false, f.Summary)
			continue
		}
		values := new([]string)
		inv.lists[f.Name] = values
		flags.Var(listValue{values}, f.Name, f.Summary)
	}
	// Flags may come after the arguments: parse, take one argument, and
	// parse what follows it, until the arguments run out or a "--" ends
	// the flags.
	for args := rest[1:]; ; {
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				commandUsage(stdout, inv.cmd)
				return ExitOK
			}
			return inv.usageError("%v", err)
		}
		left := flags.Args()
		if n := len(args) - len(left); n > 0 && args[n-1] == "--" {
			inv.args = append(inv.args, left...)
			break
		}
		if len(left) == 0 {
			break
		}
		inv.args = append(inv.args, left[0])
		args = left[1:]
	}
	for _, f := range inv.cmd.Flags {
		if f.Arg != "" && !f.Repeat && len(*inv.lists[f.Name]) > 1 {
			return inv.usageError("--%s given more than once", f.Name)
		}
	}
	return handlers[inv.cmd.Name](inv)
}

// usage writes the full usage.
func usage(w io.Writer) {
	var b strings.Builder
	b.WriteString("Usage: airframe [--root D] <command> [flags]\n\nCommands:\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range Commands {
		syn := c.Synopsis()
		if c.Short != "" {
			syn = c.Name + " " + c.Short
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", syn, c.Summary)
	}
	_ = tw.Flush()
	b.WriteString("\nFlags:\n")
	writeFlags(&b, GlobalFlags)
	b.WriteString("\nExit status: 0 clean, 1 findings or drift, 2 usage or I/O error.\n")
	_, _ = io.WriteString(w, b.String())
}

// commandUsage writes one command's usage line and its flags.
func commandUsage(w io.Writer, c Command) {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "usage: airframe [--root D] %s\n", c.Synopsis())
	if len(c.Flags) > 0 {
		b.WriteString("\n")
		writeFlags(&b, c.Flags)
	}
	_, _ = io.WriteString(w, b.String())
}

// writeFlags writes one aligned line per flag.
func writeFlags(w io.Writer, flags []Flag) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, f := range flags {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", f.spelling(), f.Summary)
	}
	_ = tw.Flush()
}

// usageError reports a usage error and the command's usage line.
func (inv *invocation) usageError(format string, args ...any) int {
	_, _ = fmt.Fprintf(inv.stderr, "airframe %s: %s\n", inv.cmd.Name, fmt.Sprintf(format, args...))
	_, _ = fmt.Fprintf(inv.stderr, "usage: airframe [--root D] %s\n", inv.cmd.Synopsis())
	return ExitUsage
}

// fail reports an error that stopped the command.
func (inv *invocation) fail(err error) int {
	_, _ = fmt.Fprintf(inv.stderr, "airframe %s: %v\n", inv.cmd.Name, err)
	return ExitUsage
}

// noArgs reports a usage error, and returns false, if the command was
// given arguments.
func (inv *invocation) noArgs() bool {
	if len(inv.args) == 0 {
		return true
	}
	inv.usageError("%s takes no arguments", inv.cmd.Name)
	return false
}

// value is the value of the flag name, or "" when it was not given.
func (inv *invocation) value(name string) string {
	if v := *inv.lists[name]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

// rootDir is the project root: --root when given (it must be a directory),
// else the nearest directory from the working directory up that holds
// airframe.yaml, else the working directory itself, where loading then
// reports config-missing.
func (inv *invocation) rootDir() (string, error) {
	if inv.root != "" {
		fi, err := os.Stat(inv.root)
		if err != nil {
			return "", err
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("--root %s is not a directory", inv.root)
		}
		return inv.root, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; {
		if _, err := os.Stat(filepath.Join(d, config.File)); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return wd, nil
		}
		d = parent
	}
}

// load finds the project root and loads the project there.
func (inv *invocation) load() (string, *project.Project, []diag.Diagnostic, error) {
	dir, err := inv.rootDir()
	if err != nil {
		return "", nil, nil, err
	}
	p, ds, err := project.Load(os.DirFS(dir))
	if err != nil {
		return "", nil, nil, err
	}
	return dir, p, ds, nil
}

// writeFile writes data to the project-relative slash path rel inside root
// atomically: a temporary file in the same directory, renamed into place.
// It creates the parent directories. Every step goes through root, so
// neither ".." nor a symlink can carry the write outside the project. An
// existing file keeps its permission bits; a new one gets 0644.
func writeFile(root *os.Root, rel string, data []byte) error {
	tmp, err := stageFile(root, rel, data)
	if err != nil {
		return err
	}
	if err := root.Rename(tmp, filepath.FromSlash(rel)); err != nil {
		_ = root.Remove(tmp)
		return err
	}
	return nil
}

// refuseLinks reports, and exits 1 for, a project-relative file in files
// that is a symbolic link: writing it would replace the link with a
// regular file. It is ExitOK when none is.
func (inv *invocation) refuseLinks(dir string, files map[string][]byte) int {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		fi, err := root.Lstat(filepath.FromSlash(name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return inv.fail(err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			_, _ = fmt.Fprintf(inv.stderr, "airframe %s: %s is a symbolic link; edit the file it points to, or replace the link with the file, first. Nothing was written.\n", inv.cmd.Name, name)
			return ExitFindings
		}
	}
	return ExitOK
}

// lf is b with CRLF line endings made LF, as the formatter and emitters
// write them.
func lf(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// hasErrors reports whether any of ds is an error.
func hasErrors(ds []diag.Diagnostic) bool {
	return slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Severity == diag.Error })
}

func runInit(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	profile := inv.value("profile")
	if profile == "" {
		profile = scaffold.Default
	}
	files, ok := scaffold.Files(profile)
	if !ok {
		return inv.usageError("unknown profile %s; use %s", profile, strings.Join(scaffold.Profiles(), ", "))
	}
	dir := inv.root
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return inv.fail(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	names := slices.Sorted(maps.Keys(files))
	// Check every path first, so init writes all of the profile or nothing.
	var existing []string
	for _, name := range names {
		_, err := root.Lstat(filepath.FromSlash(name))
		switch {
		case err == nil:
			existing = append(existing, name)
		case !errors.Is(err, fs.ErrNotExist):
			return inv.fail(err)
		}
	}
	if len(existing) > 0 {
		_, _ = fmt.Fprintf(inv.stderr, "airframe init: already exist, and init never overwrites: %s\n",
			strings.Join(existing, ", "))
		return ExitUsage
	}
	for _, name := range names {
		if err := writeFile(root, name, files[name]); err != nil {
			return inv.fail(err)
		}
		_, _ = fmt.Fprintln(inv.stdout, name)
	}
	// Build once, so the new project starts clean.
	p, ds, err := project.Load(os.DirFS(dir))
	if err != nil {
		return inv.fail(err)
	}
	return inv.build(dir, p, ds, nil, false)
}

func runFmt(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	dir, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	if p == nil { // the config could not be read, so nothing can be formatted
		diag.WriteText(inv.stderr, ds, nil)
		return ExitFindings
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	check := *inv.bools["check"]
	code := ExitOK
	for _, name := range format.Paths(p) {
		src := p.Sources[name]
		got, err := format.File(name, src)
		if err != nil {
			_, _ = fmt.Fprintf(inv.stderr, "%s: %v\n", name, err)
			code = ExitFindings
			continue
		}
		if bytes.Equal(got, lf(src)) { // CRLF-only differences stay as they are
			continue
		}
		if check {
			code = ExitFindings
		} else if err := writeFile(root, name, got); err != nil {
			return inv.fail(err)
		}
		_, _ = fmt.Fprintln(inv.stdout, name)
	}
	return code
}

func runLint(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	strict := *inv.bools["strict"]
	form := inv.value("format")
	if *inv.bools["json"] {
		if form != "" && form != "json" {
			return inv.usageError("--json and --format %s disagree", form)
		}
		form = "json"
	}
	if form == "" {
		form = "text"
	}
	if !slices.Contains(lintFormats, form) {
		return inv.usageError("unknown format %s; use %s", form, strings.Join(lintFormats, ", "))
	}
	var st selftest.Result
	if form == "gate" {
		st = selftestRun()
	}
	dir, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	var res lint.Result
	files := 0
	if p == nil {
		res = loadOnly(ds)
		if _, err := os.Stat(filepath.Join(dir, config.File)); err == nil {
			files = 1
		}
	} else {
		res = lint.Run(p, ds, lint.Options{Strict: strict})
		if res.Err != nil { // an I/O failure: the findings are incomplete
			return inv.fail(res.Err)
		}
		files = len(p.Sources)
	}
	switch form {
	case "json":
		_ = diag.WriteJSON(inv.stdout, res.Diagnostics) // a write error is Main's to report
	case "github":
		diag.WriteGitHub(inv.stdout, res.Diagnostics)
	case "gate":
		_ = gate.Write(inv.stdout, res, st, files)
	default:
		diag.WriteText(inv.stdout, res.Diagnostics, res.Tallies)
	}
	if res.Errors > 0 || (strict && res.Warnings > 0) || (form == "gate" && !st.OK) {
		return ExitFindings
	}
	return ExitOK
}

// loadOnly is the lint result of a project that did not load: its load
// diagnostics, every one an error, and no tallies since no rule ran.
func loadOnly(ds []diag.Diagnostic) lint.Result {
	res := lint.Result{Diagnostics: slices.Clone(ds), Exemptions: []lint.Exemption{}}
	for i := range res.Diagnostics {
		res.Diagnostics[i].Docs = diag.DocsURL(res.Diagnostics[i].Rule)
		res.Errors++
	}
	diag.Sort(res.Diagnostics)
	return res
}

func runBuild(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	dir, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	filter := *inv.lists["platform"]
	if p != nil {
		var known []string
		for _, pl := range p.Config.Platforms {
			known = append(known, pl.Name)
			known = append(known, pl.Emit...)
		}
		slices.Sort(known)
		known = slices.Compact(known)
		for _, f := range filter {
			if !slices.Contains(known, f) {
				return inv.usageError("unknown platform %s; use %s", f, strings.Join(known, ", "))
			}
		}
	}
	return inv.build(dir, p, ds, filter, *inv.bools["check"])
}

// build writes, or with check lists, every output the filter selects that
// differs from the file on disk (CRLF line endings do not count); a missing
// output is stale, any other read error stops the build (exit 2). It
// refuses a project that did not load cleanly: building from what was read
// would drop the rest. A failing emitter is reported on stderr and fails
// the run; the other outputs are still written.
func (inv *invocation) build(dir string, p *project.Project, ds []diag.Diagnostic, filter []string, check bool) int {
	if p == nil || hasErrors(ds) {
		diag.WriteText(inv.stderr, ds, nil)
		return ExitFindings
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	gen, bds := emit.Build(p, filter)
	code := ExitOK
	if len(bds) > 0 {
		diag.WriteText(inv.stderr, bds, nil)
	}
	// A warning (an emit-axis-fixed axis) is reported but does not fail
	// the build.
	if slices.ContainsFunc(bds, func(d diag.Diagnostic) bool { return d.Severity == diag.Error }) {
		code = ExitFindings
	}
	for _, name := range slices.Sorted(maps.Keys(gen)) {
		// A missing output is stale; any other read error stops the build.
		disk, err := fs.ReadFile(p.FS, name)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return inv.fail(err)
		}
		if err == nil && bytes.Equal(lf(disk), gen[name]) {
			continue
		}
		if check {
			_, _ = fmt.Fprintln(inv.stdout, name)
			code = ExitFindings
			continue
		}
		if err := writeFile(root, name, gen[name]); err != nil {
			return inv.fail(err)
		}
		_, _ = fmt.Fprintln(inv.stdout, "wrote", name)
	}
	// A colorset no token makes any more is stale too. build does not
	// remove it yet (removal is deferred): --check names it, and a build
	// warns so it can be deleted by hand.
	stale, err := emit.StaleColorsets(p.FS, gen)
	if err != nil {
		return inv.fail(err)
	}
	var warns []diag.Diagnostic
	for _, name := range stale {
		if check {
			_, _ = fmt.Fprintln(inv.stdout, name)
			code = ExitFindings
			continue
		}
		warns = append(warns, diag.Diagnostic{
			Rule: "emitted-stale", Severity: diag.Warn, File: name,
			Message: lint.StaleColorsetMessage(name), Docs: diag.DocsURL("emitted-stale"),
		})
	}
	if len(warns) > 0 {
		diag.WriteText(inv.stderr, warns, nil)
	}
	return code
}

// shownValue is one of a token's values as show reports it: its mode key
// ("default" for $value), the value in CSS spelling, the alias chain from
// the token to the literal in that key's context, and the literal. Problem
// says why the chain stops short.
type shownValue struct {
	Key      string   `json:"key"`
	Value    string   `json:"value"`
	Chain    []string `json:"chain"`
	Resolved string   `json:"resolved,omitempty"`
	Problem  string   `json:"problem,omitempty"`
}

// shown is a token as show reports it.
type shown struct {
	Name        string       `json:"name"`
	Tier        string       `json:"tier"`
	Component   string       `json:"component"`
	Type        string       `json:"type"`
	Description string       `json:"description"`
	Values      []shownValue `json:"values"`
}

// describe gathers what show reports about t.
func describe(set *tokens.Set, t *tokens.Token) shown {
	s := shown{
		Name: t.Name, Tier: t.Tier, Component: t.Component, Type: t.Type,
		Description: t.Description, Values: []shownValue{},
	}
	keys := slices.Sorted(maps.Keys(t.Modes))
	if t.Default != nil {
		keys = slices.Insert(keys, 0, "")
	}
	for _, k := range keys {
		v := t.Default
		label := "default"
		if k != "" {
			v, label = t.Modes[k], string(k)
		}
		sv := shownValue{Key: label, Value: emit.CSSValue(v), Chain: []string{t.Name}}
		ctx := k.Context()
		for v != nil && v.Alias != "" {
			if slices.Contains(sv.Chain, v.Alias) {
				sv.Problem = "alias cycle at " + v.Alias
				v = nil
				break
			}
			sv.Chain = append(sv.Chain, v.Alias)
			next, _ := set.ValueFor(v.Alias, ctx)
			if next == nil {
				sv.Problem = v.Alias + " is not defined or has no value here"
			}
			v = next
		}
		if v != nil {
			sv.Resolved = emit.CSSValue(v)
		}
		s.Values = append(s.Values, sv)
	}
	return s
}

// orNone is s, or "-" when s is empty.
func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// writeShown writes s as show's text output.
func writeShown(w io.Writer, s shown) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\ntier: %s\ncomponent: %s\ntype: %s\ndescription: %s\nvalues:\n",
		s.Name, orNone(s.Tier), orNone(s.Component), orNone(s.Type), orNone(s.Description))
	for _, v := range s.Values {
		fmt.Fprintf(&b, "  %s: %s\n", v.Key, orNone(v.Value))
		if len(v.Chain) < 2 && v.Problem == "" {
			continue
		}
		end := v.Resolved
		if v.Problem != "" {
			end = "? (" + v.Problem + ")"
		}
		fmt.Fprintf(&b, "    chain: %s → %s\n", strings.Join(v.Chain, " → "), end)
	}
	_, _ = io.WriteString(w, b.String())
}

// lookupToken finds the token name names, written with "-" or with the
// DTCG path's ".". It returns nil and, when one is close, a suggestion.
func lookupToken(set *tokens.Set, name string) (*tokens.Token, string) {
	if set == nil {
		return nil, ""
	}
	if t := set.Tokens[name]; t != nil {
		return t, ""
	}
	if t := set.Tokens[strings.ReplaceAll(name, ".", "-")]; t != nil {
		return t, ""
	}
	return nil, closest(name, slices.Collect(maps.Keys(set.Tokens)))
}

// closest is the name in names nearest to name, within two edits, the
// first in sorted order on a tie; "" when none is that close.
func closest(name string, names []string) string {
	best, bestD := "", 3
	for _, n := range slices.Sorted(slices.Values(names)) {
		if d := levenshtein(name, n); d < bestD {
			best, bestD = n, d
		}
	}
	return best
}

// unknownName is show's message for a name that is neither looked-up kind,
// with the closest candidate when one is near.
func unknownName(what, name string, candidates []string) string {
	msg := "unknown " + what + " " + name
	if s := closest(name, candidates); s != "" {
		msg += "; did you mean " + s + "?"
	}
	return msg
}

// levenshtein is the edit distance between a and b, by rune.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// loadToken loads the project and finds the named token. On failure it
// reports why and returns a nil token with the exit status.
func (inv *invocation) loadToken(name string) (*project.Project, *tokens.Token, int) {
	_, p, ds, err := inv.load()
	if err != nil {
		return nil, nil, inv.fail(err)
	}
	if p == nil {
		diag.WriteText(inv.stderr, ds, nil)
		return nil, nil, ExitFindings
	}
	t, suggestion := lookupToken(p.Tokens, name)
	if t == nil {
		msg := "unknown token " + name
		if inv.cmd.Name == "explain" {
			msg = "unknown rule or token " + name
		}
		if suggestion != "" {
			msg += "; did you mean " + suggestion + "?"
		}
		_, _ = fmt.Fprintf(inv.stderr, "airframe %s: %s\n", inv.cmd.Name, msg)
		return nil, nil, ExitFindings
	}
	return p, t, ExitOK
}

func runShow(inv *invocation) int {
	if len(inv.args) != 1 {
		return inv.usageError("show takes one token or component name")
	}
	name := inv.args[0]
	_, p, ds, err := inv.load()
	if err != nil {
		return inv.fail(err)
	}
	if p == nil {
		diag.WriteText(inv.stderr, ds, nil)
		return ExitFindings
	}
	forced := *inv.bools["component"]
	c := p.Components[name]
	if forced {
		if c == nil {
			_, _ = fmt.Fprintf(inv.stderr, "airframe show: %s\n", unknownName("component", name, slices.Collect(maps.Keys(p.Components))))
			return ExitFindings
		}
		return inv.runShowComponent(p, c)
	}
	t, _ := lookupToken(p.Tokens, name)
	if t == nil {
		if c != nil {
			return inv.runShowComponent(p, c)
		}
		names := slices.Collect(maps.Keys(p.Components))
		if p.Tokens != nil {
			names = append(names, slices.Collect(maps.Keys(p.Tokens.Tokens))...)
		}
		_, _ = fmt.Fprintf(inv.stderr, "airframe show: %s\n", unknownName("token or component", name, names))
		return ExitFindings
	}
	if c != nil {
		// stderr, so --json stays one document.
		_, _ = fmt.Fprintf(inv.stderr, "(also a component: airframe show --component %s)\n", name)
	}
	s := describe(p.Tokens, t)
	if *inv.bools["json"] {
		enc := json.NewEncoder(inv.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(s) // a write error is Main's to report
		return ExitOK
	}
	writeShown(inv.stdout, s)
	return ExitOK
}

func runExplain(inv *invocation) int {
	if len(inv.args) != 1 {
		return inv.usageError("explain takes one rule ID or token name")
	}
	arg := inv.args[0]
	if page, ok := docs.Rule(arg); ok {
		_, _ = fmt.Fprintf(inv.stdout, "%s\n%s\n", pageBody(page), diag.DocsURL(arg))
		return ExitOK
	}
	p, t, code := inv.loadToken(arg)
	if t == nil {
		return code
	}
	writeShown(inv.stdout, describe(p.Tokens, t))
	readers := "- (no tier)"
	if t.Tier != "" {
		tier, ok := p.Config.Tiers[t.Tier]
		switch {
		case !ok:
			readers = "- (tier " + t.Tier + " is not configured)"
		case len(tier.ReadableBy) == 0:
			readers = "- (no tier may read it)"
		default:
			readers = strings.Join(tier.ReadableBy, ", ")
		}
	}
	_, _ = fmt.Fprintf(inv.stdout, "readable by: %s\n", readers)
	return ExitOK
}

// pageBody is a docs page without its front matter.
func pageBody(page string) string {
	if rest, ok := strings.CutPrefix(page, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			page = strings.TrimLeft(body, "\n")
		}
	}
	return page
}

// ruleEntry is one rule ID in the rules --json report. Kind is "check" for
// a registered rule and "load" for one reported while loading or building.
type ruleEntry struct {
	ID      string        `json:"id"`
	Kind    string        `json:"kind"`
	Default diag.Severity `json:"default"`
	Summary string        `json:"summary"`
	Docs    string        `json:"docs"`
}

// ruleEntries lists every rule ID, sorted. A load-time ID is always an
// error and a build-time one a warning; their summary is the line under
// the docs page's title.
func ruleEntries() []ruleEntry {
	registered := map[string]lint.Rule{}
	for _, r := range lint.Rules() {
		registered[r.ID] = r
	}
	var out []ruleEntry
	for _, id := range lint.AllIDs() {
		e := ruleEntry{ID: id, Kind: "load", Default: lint.DefaultSeverity(id), Docs: diag.DocsURL(id)}
		if slices.Contains(lint.BuildWarningIDs(), id) {
			e.Kind = "build"
		}
		if r, ok := registered[id]; ok {
			e.Kind, e.Default, e.Summary = "check", r.Default, r.Summary
		} else if page, ok := docs.Rule(id); ok {
			e.Summary = docSummary(page)
		}
		out = append(out, e)
	}
	return out
}

// docSummary is the first paragraph line after a docs page's H1.
func docSummary(page string) string {
	lines := strings.Split(pageBody(page), "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "# ") {
			continue
		}
		for _, next := range lines[i+1:] {
			if next != "" {
				return next
			}
		}
	}
	return ""
}

// runRules lists every rule ID as a table or, with --json, as
// {"version":1,"rules":[…]}. It needs no project.
func runRules(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	rules := ruleEntries()
	if *inv.bools["json"] {
		report := struct {
			Version int         `json:"version"`
			Rules   []ruleEntry `json:"rules"`
		}{Version: 1, Rules: rules}
		enc := json.NewEncoder(inv.stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(report) // a write error is Main's to report
		return ExitOK
	}
	tw := tabwriter.NewWriter(inv.stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RULE\tDEFAULT\tSUMMARY")
	for _, r := range rules {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", r.ID, r.Default, r.Summary)
	}
	_ = tw.Flush()
	return ExitOK
}

func runSchema(inv *invocation) int {
	if len(inv.args) != 1 {
		return inv.usageError("schema takes one argument")
	}
	data, ok := schema.Source(inv.args[0])
	if !ok {
		return inv.usageError("unknown schema %s; use %s", inv.args[0], strings.Join(schema.Kinds, ", "))
	}
	_, _ = inv.stdout.Write(data)
	return ExitOK
}

func runVersion(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	_, _ = fmt.Fprintf(inv.stdout, "airframe %s\n", version.Version)
	return ExitOK
}
