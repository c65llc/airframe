// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package consumer finds the token references in each platform's consumer
// files: the files its consumers globs match, read through the project FS.
// One regex adapter per file type finds direct references, the declared
// wrapper roots find reads through hand-written wrappers, and the names an
// adapter recognises come from the emitters (emit.ReferenceNames), so a
// consumer reference and the generated code cannot drift.
package consumer

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
)

// Problem kinds. Undeclared, WrapperUnmapped and FallbackLiteral locate a
// reference. EmptyScan (one per glob that matches no file, Raw the glob),
// SchemaInvalid and NamesUnavailable (Raw the format) are about a
// platform's config: File is airframe.yaml, and for the first two the path
// is platforms.<platform>.consumers.
const (
	Undeclared      = "undeclared"
	WrapperUnmapped = "wrapper-unmapped"
	FallbackLiteral = "fallback-literal"
	EmptyScan       = "empty-scan"
	SchemaInvalid   = "schema-invalid"
	// NamesUnavailable is a platform whose emitter failed while rendering
	// the names its scan needs: none of its references were read.
	NamesUnavailable = "names-unavailable"
	// RetiredSpelling is a match of a retired.yaml spelling: Raw is the
	// matched text and Treatment the treatment's id.
	RetiredSpelling = "retired-spelling"
)

// RetiredRule is the rule a retired treatment's own marker exempts from;
// Scan records such a marker as an Exemption for it, with Marker set.
const RetiredRule = "retired-used"

// Direct is the Via of a reference an adapter found; a wrapper read's Via
// is "wrapper:<root>".
const Direct = "direct"

// Result is everything Scan found. Refs, Problems and Exempt are sorted by
// platform, file, line and col.
type Result struct {
	Refs []Reference
	// Files is the number of consumer files scanned per platform: those
	// its globs match, less generated outputs and the content root. A
	// platform with no such file, or whose names are unavailable, is absent.
	Files map[string]int
	// Paths are those files, sorted, per platform. The golden pins Files;
	// Paths is for the rules, which tally by file type.
	Paths    map[string][]string `json:"-"`
	Problems []Problem
	Exempt   []Exemption
	// SpellingChecks counts the (file, treatment) pairs searched for
	// retired spellings: each consumer file against every treatment with
	// a spelling for its adapter.
	SpellingChecks int `json:"-"`
}

// Reference is one token reference in a consumer file. Line and Col are
// 1-based, Col in bytes, and locate Raw, the text as written
// (--accent, DesignTokens.accent, {StaticResource DesignAccent},
// theme.space.small). Token is "" when the reference did not resolve; a
// Problem then says why.
type Reference struct {
	Platform, File string
	Line, Col      int
	Raw            string
	Token          string
	Via            string
}

// Problem is a reference, or a platform's consumers list, that is wrong.
// Kind is one of the constants above; Detail says what is wrong.
type Problem struct {
	Kind, Platform, File string
	Line, Col            int
	Raw                  string
	Detail               string
	// Treatment is the retired treatment a RetiredSpelling matched.
	Treatment string `json:",omitempty"`
}

// Exemption is an airframe-exempt <rule>: <reason> marker on the line of a
// reference or problem (Line), or on the line before it (MarkerLine says
// which). Scan does not know which rules a reference breaks, so it records
// every marker that applies and suppresses nothing itself: a rule
// suppresses its finding at (File, Line) when an Exemption for it there
// has a Reason. One whose Reason is "" exempts nothing; the finding's
// message then ends "(airframe-exempt on line N has no reason)".
//
// A retired treatment's own marker, <marker>: <reason>, is recorded the
// same way for the lines of that treatment's RetiredSpelling problems,
// with Rule RetiredRule and Marker the marker; Marker is "" for an
// airframe-exempt marker. It exempts only that treatment's spellings.
type Exemption struct {
	Rule, Platform, File string
	Line, MarkerLine     int
	Reason               string
	Marker               string `json:",omitempty"`
}

// Scan scans every platform's consumer files. A platform that declares no
// consumers is skipped; each of its globs that matches no file gets an
// EmptyScan problem, even when another glob matches. A file whose extension has no adapter, or whose adapter's format
// the platform does not emit, is listed in one SchemaInvalid problem per
// platform, and still scanned for wrapper reads. A platform whose emitter
// fails to render the names its scan needs gets a NamesUnavailable problem
// and is not read further. The error is only for an I/O failure.
func Scan(p *project.Project) (Result, error) {
	s := &scanner{p: p, names: map[[2]string]map[string]string{}}
	res := Result{Files: map[string]int{}, Paths: map[string][]string{}}
	for _, pl := range p.Config.Platforms {
		if len(pl.Consumers) == 0 {
			continue
		}
		if err := s.platform(pl, &res); err != nil {
			return Result{}, err
		}
	}
	sortResult(&res)
	return res, nil
}

type scanner struct {
	p     *project.Project
	names map[[2]string]map[string]string // (platform, format) → ReferenceNames
	excl  []string                        // globs of generated outputs
}

// refNames is ReferenceNames for (pl, format), rendered once.
func (s *scanner) refNames(pl config.Platform, format string) (map[string]string, error) {
	k := [2]string{pl.Name, format}
	if m, ok := s.names[k]; ok {
		return m, nil
	}
	m, err := emit.ReferenceNames(s.p, pl, format)
	if err != nil {
		return nil, fmt.Errorf("platform %s: %s reference names: %w", pl.Name, format, err)
	}
	s.names[k] = m
	return m, nil
}

// file is one consumer file being scanned.
type file struct {
	path string
	src  string // as read: exemption markers are read here
	// code is src as the adapters and wrappers read it: for a stylesheet,
	// its /* … */ comments blanked to spaces, newlines and byte offsets kept.
	code   string
	starts []int // byte offset of each line's start
}

func newFile(name, src string) *file {
	f := &file{path: name, src: src, code: src, starts: []int{0}}
	if stylesheetExts[strings.ToLower(path.Ext(name))] {
		f.code = blankComments(src)
	}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			f.starts = append(f.starts, i+1)
		}
	}
	return f
}

// pos is the 1-based line and byte col of offset off.
func (f *file) pos(off int) (line, col int) {
	i := sort.Search(len(f.starts), func(i int) bool { return f.starts[i] > off }) - 1
	return i + 1, off - f.starts[i] + 1
}

// line is the text of 1-based line n as read, without its newline.
func (f *file) line(n int) string { return f.lineOf(f.src, n) }

// codeLine is line n of f's code: a stylesheet's with its comments blanked.
func (f *file) codeLine(n int) string { return f.lineOf(f.code, n) }

func (f *file) lineOf(s string, n int) string {
	if n < 1 || n > len(f.starts) {
		return ""
	}
	end := len(s)
	if n < len(f.starts) {
		end = f.starts[n] - 1
	}
	return strings.TrimSuffix(s[f.starts[n-1]:end], "\r")
}

// platformRun collects one platform's findings.
type platformRun struct {
	s       *scanner
	pl      config.Platform
	res     *Result
	local   map[string]bool // custom properties some consumer file defines
	taken   map[string]map[int]bool
	regexes map[string]*regexp.Regexp
}

// regex is pat compiled, once per platform run.
func (r *platformRun) regex(pat string) *regexp.Regexp {
	if re, ok := r.regexes[pat]; ok {
		return re
	}
	if r.regexes == nil {
		r.regexes = map[string]*regexp.Regexp{}
	}
	re := regexp.MustCompile(pat)
	r.regexes[pat] = re
	return re
}

func (r *platformRun) ref(f *file, off int, raw, token, via string) {
	line, col := f.pos(off)
	r.res.Refs = append(r.res.Refs, Reference{Platform: r.pl.Name, File: f.path, Line: line, Col: col, Raw: raw, Token: token, Via: via})
	r.claim(f, off)
}

func (r *platformRun) problem(f *file, off int, kind, raw, detail string) {
	line, col := f.pos(off)
	r.res.Problems = append(r.res.Problems, Problem{Kind: kind, Platform: r.pl.Name, File: f.path, Line: line, Col: col, Raw: raw, Detail: detail})
}

// claim marks off as the start of a reference, so a wrapper root that
// begins there too is not read a second time.
func (r *platformRun) claim(f *file, off int) {
	if r.taken[f.path] == nil {
		r.taken[f.path] = map[int]bool{}
	}
	r.taken[f.path][off] = true
}

func (r *platformRun) claimed(f *file, off int) bool { return r.taken[f.path][off] }

func (s *scanner) platform(pl config.Platform, res *Result) error {
	files, bad, dead, links, err := s.consumerFiles(pl)
	if err != nil {
		return err
	}
	cfgFile := &file{path: config.File, starts: []int{0}}
	r := &platformRun{s: s, pl: pl, res: res, local: map[string]bool{}, taken: map[string]map[int]bool{}}
	for _, g := range bad {
		r.problem(cfgFile, 0, SchemaInvalid, g, fmt.Sprintf(
			"platform %s: consumers glob %q is not a valid glob", pl.Name, g))
	}
	if len(links) > 0 {
		r.problem(cfgFile, 0, SchemaInvalid, strings.Join(links, ", "), fmt.Sprintf(
			"platform %s: consumer symlinks are not followed, so %d matched file(s) were not scanned: %s",
			pl.Name, len(links), strings.Join(links, ", ")))
	}
	for _, g := range dead {
		r.problem(cfgFile, 0, EmptyScan, g, fmt.Sprintf(
			"platform %s: consumers glob %q matches no file (generated outputs and the %s/ content root are not scanned); a declared input that is never scanned is a failure, not a pass",
			pl.Name, g, s.p.Config.Content))
	}
	if len(files) == 0 {
		return nil
	}

	loaded := make([]*file, 0, len(files))
	for _, fp := range files {
		b, err := fs.ReadFile(s.p.FS, fp)
		if err != nil {
			return fmt.Errorf("read consumer file %s: %w", fp, err)
		}
		f := newFile(fp, string(b))
		loaded = append(loaded, f)
		for _, re := range []*regexp.Regexp{localPropRE, scriptPropRE} {
			for _, m := range re.FindAllStringSubmatch(f.code, -1) {
				r.local[m[1]] = true
			}
		}
	}

	// Render every name set the scan needs before reading any reference: a
	// platform whose emitter fails is reported and skipped, never half read.
	var unadapted []string
	adapted := make([]*adapter, len(loaded))
	need := map[string]bool{}
	for i, f := range loaded {
		a, why := adapterFor(pl, f.path)
		if a == nil {
			unadapted = append(unadapted, f.path+" ("+why+")")
			continue
		}
		adapted[i] = a
		need[a.format] = true
	}
	if slices.ContainsFunc(pl.Wrappers, func(w config.Wrapper) bool { return w.Generated }) {
		for _, f := range append(slices.Clone(nativeFormats), "css") {
			if slices.Contains(pl.Emit, f) {
				need[f] = true
			}
		}
	}
	for _, format := range slices.Sorted(maps.Keys(need)) {
		if _, err := s.refNames(pl, format); err != nil {
			r.problem(cfgFile, 0, NamesUnavailable, format, fmt.Sprintf(
				"%v; the references in platform %s's consumer files cannot be checked until it builds", err, pl.Name))
			return nil
		}
	}
	res.Files[pl.Name] = len(files)
	res.Paths[pl.Name] = slices.Clone(files)
	for i, f := range loaded {
		if a := adapted[i]; a != nil {
			if err := a.scan(r, f); err != nil {
				return err
			}
		}
	}
	if len(unadapted) > 0 {
		r.problem(cfgFile, 0, SchemaInvalid, strings.Join(unadapted, ", "), fmt.Sprintf(
			"platform %s: no adapter reads %d consumer file(s): %s",
			pl.Name, len(unadapted), strings.Join(unadapted, ", ")))
	}
	for _, f := range loaded {
		if err := r.wrappers(f); err != nil {
			return err
		}
	}
	for i, f := range loaded {
		if a := adapted[i]; a != nil {
			r.spellings(f, a.name)
		}
	}
	r.exemptions(loaded)
	return nil
}

// placeholderRE is an out path's {axis} placeholder.
var placeholderRE = regexp.MustCompile(`\{[^{}]*\}`)

// outputGlobs are globs of every platform's generated outputs: each out
// path and options.sharedOut, a {placeholder} matching any one segment
// and a directory output matching everything under it. Default outputs
// sit under the content root, which is excluded on its own.
func (s *scanner) outputGlobs() []string {
	if s.excl != nil {
		return s.excl
	}
	s.excl = []string{}
	for _, pl := range s.p.Config.Platforms {
		outs := []string{}
		for _, f := range slices.Sorted(maps.Keys(pl.Out)) {
			outs = append(outs, pl.Out[f])
		}
		if v, ok := pl.Option("sharedOut"); ok {
			outs = append(outs, v)
		}
		for _, o := range outs {
			g := strings.TrimSuffix(placeholderRE.ReplaceAllString(o, "*"), "/")
			if g == "" {
				continue
			}
			s.excl = append(s.excl, g, g+"/**")
		}
	}
	return s.excl
}

// excluded reports whether consumer path f is a generated output or under
// the content root.
func (s *scanner) excluded(f string) bool {
	if c := path.Clean(s.p.Config.Content); c != "." && (f == c || strings.HasPrefix(f, c+"/")) {
		return true
	}
	for _, g := range s.outputGlobs() {
		if ok, _ := doublestar.Match(g, f); ok {
			return true
		}
	}
	return false
}

// consumerFiles are the sorted, distinct files pl's consumers globs match,
// less the excluded ones; bad lists the globs that are not valid, and dead
// the valid ones that match no file once the exclusions are taken out.
// Symlinks are never followed: links lists each matched one, which is not
// scanned (a matched symlink still makes its glob live).
func (s *scanner) consumerFiles(pl config.Platform) (files, bad, dead, links []string, err error) {
	seen := map[string]bool{}
	for _, g := range pl.Consumers {
		ms, err := doublestar.Glob(s.p.FS, g, doublestar.WithFilesOnly(), doublestar.WithNoFollow())
		if errors.Is(err, doublestar.ErrBadPattern) {
			bad = append(bad, g)
			continue
		}
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("platform %s: find consumers %q: %w", pl.Name, g, err)
		}
		matched := false
		for _, m := range ms {
			if s.excluded(m) {
				continue
			}
			matched = true
			if seen[m] {
				continue
			}
			seen[m] = true
			// fs.Lstat is fs.Stat on an FS that cannot read links.
			info, err := fs.Lstat(s.p.FS, m)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("platform %s: stat consumer %s: %w", pl.Name, m, err)
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				links = append(links, m)
				continue
			}
			files = append(files, m)
		}
		if !matched {
			dead = append(dead, g)
		}
	}
	slices.Sort(files)
	slices.Sort(links)
	return files, bad, dead, links, nil
}

// markerRE is an exemption marker's start: airframe-exempt and the rule.
var markerRE = regexp.MustCompile(`airframe-exempt\s+([A-Za-z0-9][A-Za-z0-9-]*)`)

type marker struct{ rule, reason string }

// markers parses every airframe-exempt marker on line. A marker's reason
// follows a ":" and runs to the first comment closer (*/ or -->), the next
// marker or the end of the line, whichever comes first, trimmed; with no
// ":" it has none. So two markers in two comments on one line keep their
// own reasons, and a block comment before code never takes the code.
func markers(line string) []marker {
	idx := markerRE.FindAllStringSubmatchIndex(line, -1)
	out := make([]marker, 0, len(idx))
	for i, m := range idx {
		end := len(line)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, marker{rule: line[m[2]:m[3]], reason: markerReason(line[m[1]:end])})
	}
	return out
}

// commentOpeners are the comments a line may consist of, with their
// closers ("" for one that runs to the end of the line). {/* … */} is a
// JSX comment.
var commentOpeners = [][2]string{{"//", ""}, {"{/*", "*/}"}, {"/*", "*/"}, {"<!--", "-->"}}

// commentOnly reports whether line holds nothing but comments: no code
// before, between or after them. A block comment left open runs to the
// end of the line.
func commentOnly(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" {
		return false
	}
	for s != "" {
		i := slices.IndexFunc(commentOpeners, func(c [2]string) bool { return strings.HasPrefix(s, c[0]) })
		if i < 0 {
			return false
		}
		open, closer := commentOpeners[i][0], commentOpeners[i][1]
		if closer == "" {
			return true
		}
		j := strings.Index(s[len(open):], closer)
		if j < 0 {
			return true
		}
		s = strings.TrimSpace(s[len(open)+j+len(closer):])
	}
	return true
}

// exemptions records, for each line of this platform that holds a
// reference or a reference problem, the markers on it and those on the
// line before it when that line is nothing but comment: it holds no code,
// and no reference or problem of its own. A marker beside code exempts its
// own line only, so it never spills onto the next.
func (r *platformRun) exemptions(files []*file) {
	byPath := map[string]*file{}
	for _, f := range files {
		byPath[f.path] = f
	}
	type at struct {
		file string
		line int
	}
	lines := map[at]bool{}
	for _, ref := range r.res.Refs {
		if ref.Platform == r.pl.Name {
			lines[at{ref.File, ref.Line}] = true
		}
	}
	for _, pr := range r.res.Problems {
		if pr.Platform == r.pl.Name && byPath[pr.File] != nil {
			lines[at{pr.File, pr.Line}] = true
		}
	}
	// words are the treatment markers that may escape each line: those of
	// the treatments whose spellings it matches.
	words := map[at][]string{}
	marker := map[string]string{}
	for _, t := range r.s.p.Retired {
		marker[t.ID] = t.Marker
	}
	for _, pr := range r.res.Problems {
		if pr.Platform == r.pl.Name && pr.Kind == RetiredSpelling && marker[pr.Treatment] != "" {
			k := at{pr.File, pr.Line}
			if !slices.Contains(words[k], marker[pr.Treatment]) {
				words[k] = append(words[k], marker[pr.Treatment])
			}
		}
	}
	for l := range lines {
		f := byPath[l.file]
		for _, ml := range []int{l.line - 1, l.line} {
			if ml < l.line && (lines[at{l.file, ml}] || !commentOnly(f.line(ml))) {
				continue
			}
			for _, m := range markers(f.line(ml)) {
				r.res.Exempt = append(r.res.Exempt, Exemption{
					Rule: m.rule, Platform: r.pl.Name, File: l.file, Line: l.line, MarkerLine: ml, Reason: m.reason,
				})
			}
			for _, w := range words[l] {
				for _, reason := range wordMarkers(f.line(ml), r.regex(wordPat(w))) {
					r.res.Exempt = append(r.res.Exempt, Exemption{
						Rule: RetiredRule, Platform: r.pl.Name, File: l.file, Line: l.line, MarkerLine: ml, Reason: reason, Marker: w,
					})
				}
			}
		}
	}
}

// wordPat matches marker word standing alone, as group 1: no identifier
// character or "-" touches it.
func wordPat(word string) string {
	return `(?:^|[^A-Za-z0-9_-])(` + regexp.QuoteMeta(word) + `)(?:[^A-Za-z0-9_-]|$)`
}

// wordMarkers are the reasons of every marker re (a wordPat) matches
// inside a comment on line: the word must follow a comment opener (//,
// /*, <!-- or {/*) in the same comment, so the word in code or in a string
// marks nothing. A reason is read as an airframe-exempt marker's is: after
// a ":", up to the comment's closer or the next marker, trimmed; "" with
// no ":".
func wordMarkers(line string, re *regexp.Regexp) []string {
	spans := commentSpans(line)
	var starts, ends, stops []int
	for i := 0; i < len(line); {
		m := re.FindStringSubmatchIndex(line[i:])
		if m == nil {
			break
		}
		start, end := i+m[2], i+m[3]
		i = end
		for _, sp := range spans {
			if start >= sp[0] && end <= sp[1] {
				starts, ends, stops = append(starts, start), append(ends, end), append(stops, sp[1])
				break
			}
		}
	}
	out := make([]string, 0, len(starts))
	for i := range starts {
		end := stops[i]
		if i+1 < len(starts) {
			end = min(end, starts[i+1])
		}
		if m := markerRE.FindStringIndex(line[ends[i]:end]); m != nil {
			end = ends[i] + m[0]
		}
		out = append(out, markerReason(line[ends[i]:end]))
	}
	return out
}

// commentSpans are the comments on line, as [after its opener, its
// closer's start) byte ranges: a // comment runs to the end of the line,
// and so does a block comment left open.
func commentSpans(line string) [][2]int {
	var out [][2]int
	for i := 0; i < len(line); {
		at, which := -1, -1
		for k, c := range commentOpeners {
			if j := strings.Index(line[i:], c[0]); j >= 0 && (at < 0 || i+j < at) {
				at, which = i+j, k
			}
		}
		if at < 0 {
			break
		}
		open, closer := commentOpeners[which][0], commentOpeners[which][1]
		body := at + len(open)
		stop := len(line)
		if closer != "" {
			if j := strings.Index(line[body:], closer); j >= 0 {
				stop = body + j
			}
		}
		out = append(out, [2]int{body, stop})
		i = stop + len(closer)
		if closer == "" || i > len(line) {
			break
		}
	}
	return out
}

// markerReason is the reason in rest, the text after a marker's word:
// after a ":", up to the first comment closer, trimmed; "" with no ":".
func markerReason(rest string) string {
	rest, ok := strings.CutPrefix(strings.TrimLeft(rest, " \t"), ":")
	if !ok {
		return ""
	}
	for _, closer := range []string{"*/", "-->"} {
		rest, _, _ = strings.Cut(rest, closer)
	}
	return strings.TrimSpace(rest)
}

// spellings reports every match of a retired treatment's spellings for
// adapter in f, line by line over f's code, so a stylesheet comment never
// matches. A pattern that matches the empty string matches nothing.
func (r *platformRun) spellings(f *file, adapter string) {
	for _, t := range r.s.p.Retired {
		pats := t.Spellings[adapter]
		if len(pats) == 0 {
			continue
		}
		r.res.SpellingChecks++
		ruling := ""
		if t.Ruling != "" {
			ruling = " (" + t.Ruling + ")"
		}
		escape := "airframe-exempt " + RetiredRule + ": <reason>"
		if t.Marker != "" {
			escape = t.Marker + ": <reason> (or " + escape + ")"
		}
		for n := 1; n <= len(f.starts); n++ {
			code := f.codeLine(n)
			seen := map[int]bool{}
			for _, re := range pats {
				for _, m := range re.FindAllStringIndex(code, -1) {
					if m[0] == m[1] || seen[m[0]] {
						continue
					}
					seen[m[0]] = true
					raw := code[m[0]:m[1]]
					line, col := n, m[0]+1
					r.res.Problems = append(r.res.Problems, Problem{
						Kind: RetiredSpelling, Platform: r.pl.Name, File: f.path, Line: line, Col: col, Raw: raw, Treatment: t.ID,
						Detail: fmt.Sprintf("platform %s: %q is a spelling of retired treatment %s%s, matched by %s pattern %q; it must not come back: use what replaced it, or mark an audited use with %s",
							r.pl.Name, raw, t.ID, ruling, adapter, re.String(), escape),
					})
				}
			}
		}
	}
}

func sortResult(res *Result) {
	slices.SortFunc(res.Refs, func(a, b Reference) int {
		return cmp.Or(cmp.Compare(a.Platform, b.Platform), cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col),
			cmp.Compare(a.Via, b.Via), cmp.Compare(a.Raw, b.Raw), cmp.Compare(a.Token, b.Token))
	})
	slices.SortFunc(res.Problems, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(a.Platform, b.Platform), cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col),
			cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Raw, b.Raw), cmp.Compare(a.Treatment, b.Treatment), cmp.Compare(a.Detail, b.Detail))
	})
	slices.SortFunc(res.Exempt, func(a, b Exemption) int {
		return cmp.Or(cmp.Compare(a.Platform, b.Platform), cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line), cmp.Compare(a.MarkerLine, b.MarkerLine),
			cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Marker, b.Marker), cmp.Compare(a.Reason, b.Reason))
	})
}
