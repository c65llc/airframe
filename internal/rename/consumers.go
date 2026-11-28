// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package rename

import (
	"cmp"
	"fmt"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/consumer"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/emit"
	"github.com/c65llc/airframe/internal/project"
)

// Codemod is Plan followed by the consumer edits, checked by building and
// rescanning the result. Every consumer reference to old is rewritten to
// the renamed spelling of the same emitted name (emit.ReferenceVariants):
// a direct reference's name part, or a generated wrapper's member. A map
// wrapper's read needs no code edit, since the config edit covers it; a
// pattern wrapper's read is a Manual item. The consumer edits are in
// Edits, their files' final bytes in Consumers and the bytes they were
// spliced into in ConsumerSources.
//
// The project must build before the rename, and the renamed project,
// consumer edits included, must load, build, and hold no reference to old
// other than the Manual items, each edited reference now reading new;
// otherwise the result is a *Refusal.
func Codemod(p *project.Project, old, new, dotted string) (*EditPlan, error) {
	if _, bds := emit.Build(p, nil); len(errorsOnly(bds)) > 0 {
		return nil, &Refusal{Msg: "rename: the project does not build; fix it first", Diagnostics: errorsOnly(bds)}
	}
	pl, err := Plan(p, old, new, dotted)
	if err != nil {
		return nil, err
	}
	// Consumer files do not change what is emitted, so the renamed sources
	// building is the renamed project building.
	if _, bds := emit.Build(pl.After, nil); len(errorsOnly(bds)) > 0 {
		bds = errorsOnly(bds)
		return nil, &Refusal{Msg: fmt.Sprintf("rename: the renamed project does not build (%d problem(s)); nothing was written", len(bds)), Diagnostics: bds}
	}
	scan, err := consumer.Scan(p)
	if err != nil {
		return nil, err
	}
	c := &codemod{p: p, pl: pl, before: scan, oldV: map[[2]string]map[string]emit.Variant{}, newN: map[[2]string]map[emit.Variant]string{},
		patternReads: map[patternRef]bool{}}
	if err := c.foreignCSS(); err != nil {
		return nil, err
	}
	edits := map[string][]consumerEdit{}
	for _, ref := range scan.Refs {
		if ref.Token != old {
			continue
		}
		s, err := c.edit(ref)
		if err != nil {
			return nil, err
		}
		if s != nil {
			edits[ref.File] = append(edits[ref.File], *s)
		}
	}
	// Plan reports every pattern that can make old; keep those some read
	// resolves through.
	pl.Manual = slices.DeleteFunc(pl.Manual, func(m Manual) bool { return m.pattern != nil && !c.patternReads[*m.pattern] })
	pl.Consumers, pl.ConsumerSources = map[string][]byte{}, map[string][]byte{}
	for _, f := range slices.Sorted(maps.Keys(edits)) {
		src, err := fs.ReadFile(p.FS, f)
		if err != nil {
			return nil, fmt.Errorf("read consumer file %s: %w", f, err)
		}
		out, err := splice(f, src, edits[f])
		if err != nil {
			return nil, err
		}
		pl.ConsumerSources[f], pl.Consumers[f] = src, out
		for _, s := range edits[f] {
			pl.Edits = append(pl.Edits, s.Edit)
		}
	}
	sortPlan(pl)
	if err := c.validate(edits); err != nil {
		return nil, err
	}
	return pl, nil
}

// consumerEdit is one consumer edit: Edit replaces the old name, and Raw is the
// whole reference as written, starting at RefCol on Edit.Line.
type consumerEdit struct {
	Edit
	Raw    string
	RefCol int
}

type codemod struct {
	p      *project.Project
	pl     *EditPlan
	before consumer.Result                       // the scan of p
	oldV   map[[2]string]map[string]emit.Variant // (platform, format) → old ReferenceVariants
	newN   map[[2]string]map[emit.Variant]string // (platform, format) → renamed VariantNames
	// patternReads are the wrapper patterns some read resolves through to old.
	patternReads map[patternRef]bool
}

func (c *codemod) platform(name string) config.Platform {
	i := slices.IndexFunc(c.p.Config.Platforms, func(pl config.Platform) bool { return pl.Name == name })
	return c.p.Config.Platforms[i]
}

func (c *codemod) variants(pl config.Platform, format string) (map[string]emit.Variant, map[emit.Variant]string, error) {
	k := [2]string{pl.Name, format}
	if _, ok := c.oldV[k]; !ok {
		v, err := emit.ReferenceVariants(c.p, pl, format)
		if err != nil {
			return nil, nil, err
		}
		c.oldV[k] = v
		apl := pl
		if i := slices.IndexFunc(c.pl.After.Config.Platforms, func(q config.Platform) bool { return q.Name == pl.Name }); i >= 0 {
			apl = c.pl.After.Config.Platforms[i]
		}
		nv, err := emit.ReferenceVariants(c.pl.After, apl, format)
		if err != nil {
			return nil, nil, err
		}
		c.newN[k] = emit.VariantNames(nv)
	}
	return c.oldV[k], c.newN[k], nil
}

// respell is the renamed spelling of name, an emitted name of old in
// format on pl.
func (c *codemod) respell(pl config.Platform, format, name string) (string, bool, error) {
	oldV, newN, err := c.variants(pl, format)
	if err != nil {
		return "", false, err
	}
	v, ok := oldV[name]
	if !ok || v.Token != c.pl.Old {
		return "", false, nil
	}
	n, ok := newN[emit.Variant{Token: c.pl.New, Key: v.Key}]
	return n, ok, nil
}

// foreignCSS refuses a rename whose css name for new, on a css platform,
// is a property the app defines (an app-local property, defined in a
// scanned consumer file) or a library or script does (one matching the
// platform's externalProperties): the reads of it would then read the
// token instead, changing what they mean with no edit to show for it.
func (c *codemod) foreignCSS() error {
	for _, apl := range c.pl.After.Config.Platforms {
		if !slices.Contains(apl.Emit, "css") {
			continue
		}
		vs, err := emit.ReferenceVariants(c.pl.After, apl, "css")
		if err != nil {
			return err
		}
		for _, name := range slices.Sorted(maps.Keys(vs)) {
			if vs[name].Token != c.pl.New {
				continue
			}
			if i := slices.IndexFunc(c.before.Locals, func(l consumer.Local) bool { return l.Platform == apl.Name && l.Name == name }); i >= 0 {
				l := c.before.Locals[i]
				return refuse("rename: %s is already an app-local property of platform %s, defined at %s:%d; renaming %s to %s would change what its reads mean. Rename it, or pick another name; nothing was written",
					name, apl.Name, l.File, l.Line, c.pl.Old, c.pl.New)
			}
			if i := consumer.External(apl, name); i >= 0 {
				return refuse("rename: %s matches platform %s's externalProperties glob %q (%s:%d), a property a library or script defines; renaming %s to %s would change what its reads mean. Pick another name; nothing was written",
					name, apl.Name, apl.ExternalProperties[i], config.File, c.externalLine(apl.Name, i), c.pl.Old, c.pl.New)
			}
		}
	}
	return nil
}

// externalLine is the line of platform's i'th externalProperties entry
// in airframe.yaml, or 0 when it cannot be found.
func (c *codemod) externalLine(platform string, i int) int {
	src, err := fs.ReadFile(c.p.FS, config.File)
	if err != nil {
		return 0
	}
	var doc yaml.Node
	if yaml.Unmarshal(src, &doc) != nil || len(doc.Content) == 0 {
		return 0
	}
	e := field(field(field(doc.Content[0], "platforms"), platform), "externalProperties")
	if e == nil || e.Kind != yaml.SequenceNode || i >= len(e.Content) {
		return 0
	}
	return e.Content[i].Line
}

// identRE is a whole identifier, as a generated wrapper's member is read.
var identRE = regexp.MustCompile(`^[\p{L}_][\p{L}\p{M}\p{N}_]*$`)

// xamlIdentRE finds the resource key in a XAML or C# reference's raw text.
var xamlIdentRE = regexp.MustCompile(`(?:StaticResource|ThemeResource)\s+([\p{L}_][\p{L}\p{M}\p{N}_]*)|Resources\["([\p{L}_][\p{L}\p{M}\p{N}_]*)"\]`)

// edit is the splice that renames ref, nil when it needs none (a map
// wrapper's read) or is a Manual item (a pattern wrapper's read).
func (c *codemod) edit(ref consumer.Reference) (*consumerEdit, error) {
	pl := c.platform(ref.Platform)
	var off int     // the old name's byte offset in Raw
	var name string // the old name as written
	var newName string
	if root, ok := strings.CutPrefix(ref.Via, "wrapper:"); ok {
		i := slices.IndexFunc(pl.Wrappers, func(w config.Wrapper) bool { return w.Root == root })
		w := pl.Wrappers[i]
		member := ref.Raw[len(root)+1:]
		switch {
		case w.Generated:
			n, err := c.generated(pl, member)
			if err != nil {
				return nil, err
			}
			if n == "" {
				return nil, c.missing(ref)
			}
			if !identRE.MatchString(n) {
				c.pl.Manual = append(c.pl.Manual, Manual{File: ref.File, Line: ref.Line, Message: fmt.Sprintf(
					"%s: a generated wrapper member must be an identifier; %s is not — rename the wrapper read by hand", ref.Raw, n)})
				return nil, nil
			}
			off, name, newName = len(root)+1, member, n
		case w.Map[member] != "":
			return nil, nil // the config's map edit renames it
		default:
			if i := slices.IndexFunc(w.Patterns, func(p config.WrapperPattern) bool { return p.Match.MatchString(member) }); i >= 0 {
				c.patternReads[patternRef{ref.Platform, root, i}] = true
			}
			c.pl.Manual = append(c.pl.Manual, Manual{File: ref.File, Line: ref.Line, Message: fmt.Sprintf(
				"platform %s: %s reads %s through a pattern of wrapper %s, which would now name no token; rewrite it, or the pattern, by hand",
				ref.Platform, ref.Raw, c.pl.Old, root)})
			return nil, nil
		}
	} else {
		format := consumer.Format(pl, ref.File)
		switch format {
		case "css":
			off, name = 0, ref.Raw
		case "swift", "kotlin":
			off = strings.LastIndex(ref.Raw, ".") + 1
			name = ref.Raw[off:]
		case "xaml":
			m := xamlIdentRE.FindStringSubmatchIndex(ref.Raw)
			if m == nil {
				return nil, c.missing(ref)
			}
			g := 2
			if m[2] < 0 {
				g = 4
			}
			off, name = m[g], ref.Raw[m[g]:m[g+1]]
		default:
			return nil, c.missing(ref)
		}
		n, ok, err := c.respell(pl, format, name)
		if err == nil && !ok {
			n, ok, err = c.respell(pl, format, strings.Trim(name, "`"))
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, c.missing(ref)
		}
		newName = n
	}
	if newName == name {
		return nil, nil // a custom spelling (a names entry) the rename keeps
	}
	return &consumerEdit{
		Edit: Edit{File: ref.File, Line: ref.Line, Col: ref.Col + off, Old: name, New: newName},
		Raw:  ref.Raw, RefCol: ref.Col,
	}, nil
}

// generated is the renamed spelling of a generated wrapper's member: the
// name it is in the first format that has it, as the scanner reads it
// (its native formats, then css as --member), unescaped, since a wrapper
// read is never backticked. It is "" when there is none.
func (c *codemod) generated(pl config.Platform, member string) (string, error) {
	for _, format := range []string{"swift", "kotlin", "xaml", "css"} {
		if !slices.Contains(pl.Emit, format) {
			continue
		}
		key := member
		if format == "css" {
			key = "--" + member
		}
		oldV, _, err := c.variants(pl, format)
		if err != nil {
			return "", err
		}
		if _, ok := oldV[key]; !ok {
			continue
		}
		n, ok, err := c.respell(pl, format, key)
		if err != nil || !ok {
			return "", err
		}
		return strings.Trim(strings.TrimPrefix(n, "--"), "`"), nil
	}
	return "", nil
}

func (c *codemod) missing(ref consumer.Reference) error {
	return refuse("rename: %s:%d:%d: %s has no renamed spelling on platform %s", ref.File, ref.Line, ref.Col, ref.Raw, ref.Platform)
}

// splice applies one file's consumer edits to src, its bytes, in a single
// pass, right to left within each line. Before each, it checks that the
// reference is still where the scan found it: the whole reference at its
// column and the old name at the edit's. A file changed since it was
// scanned is a *Refusal, and src is left as it was.
func splice(file string, src []byte, edits []consumerEdit) ([]byte, error) {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	at := func(line, col int, s string) (int, bool) {
		if line < 1 || line > len(starts) || col < 1 {
			return 0, false
		}
		off := starts[line-1] + col - 1
		end := len(src)
		if line < len(starts) {
			end = starts[line]
		}
		return off, off+len(s) <= end && string(src[off:off+len(s)]) == s
	}
	sorted := slices.Clone(edits)
	slices.SortFunc(sorted, func(a, b consumerEdit) int { return cmp.Or(cmp.Compare(b.Line, a.Line), cmp.Compare(b.Col, a.Col)) })
	for i, s := range sorted {
		if i > 0 && sorted[i-1].Line == s.Line && s.Col+len(s.Old) > sorted[i-1].Col {
			return nil, refuse("rename: %s:%d: two edits overlap at columns %d and %d", file, s.Line, s.Col, sorted[i-1].Col)
		}
		_, rawOK := at(s.Line, s.RefCol, s.Raw)
		if _, ok := at(s.Line, s.Col, s.Old); !ok || !rawOK {
			return nil, refuse("rename: %s:%d:%d no longer reads %s; the file changed since it was scanned, so nothing was written", file, s.Line, s.RefCol, s.Raw)
		}
	}
	out := slices.Clone(src)
	for _, s := range sorted {
		off, _ := at(s.Line, s.Col, s.Old)
		out = slices.Concat(out[:off], []byte(s.New), out[off+len(s.Old):])
	}
	return out, nil
}

// validate loads the renamed project with the consumer edits laid over it
// and scans it: no reference may still read old, each edited reference,
// at its column after the splices left of it, must read new, and every
// other reference must read what it read before (c.before), so nothing
// the plan did not edit changes meaning. The renamed project, consumers
// included, becomes pl.After.
func (c *codemod) validate(edits map[string][]consumerEdit) error {
	pl := c.pl
	files := maps.Clone(pl.Files)
	maps.Copy(files, pl.Consumers)
	after, ds, err := project.Load(Overlay(c.p.FS, files))
	if err != nil {
		return err
	}
	if ds = errorsOnly(ds); after == nil || len(ds) > 0 {
		return &Refusal{Msg: fmt.Sprintf("rename: the renamed project does not load (%d problem(s))", len(ds)), Diagnostics: ds}
	}
	scan, err := consumer.Scan(after)
	if err != nil {
		return err
	}
	type pos struct {
		file      string
		line, col int
	}
	// shift is the column a reference at col on line of f has after the
	// splices left of it.
	shift := func(f string, line, col int) int {
		out := col
		for _, o := range edits[f] {
			if o.Line == line && o.Col < col {
				out += len(o.New) - len(o.Old)
			}
		}
		return out
	}
	// was is what each reference read before, at its column after the
	// rename; renamed are those that read old, which the checks below and
	// the Manual items cover.
	was, renamed := map[pos]consumer.Reference{}, map[pos]bool{}
	for _, ref := range c.before.Refs {
		at := pos{ref.File, ref.Line, shift(ref.File, ref.Line, ref.Col)}
		if ref.Token == pl.Old {
			renamed[at] = true
			continue
		}
		was[at] = ref
	}
	reads := map[pos]string{}
	var left []diag.Diagnostic
	changed := func(ref consumer.Reference, before, after string) {
		left = append(left, diag.Diagnostic{Rule: "reference-undeclared", Severity: diag.Error, Docs: diag.DocsURL("reference-undeclared"), File: ref.File, Line: ref.Line, Col: ref.Col,
			Message: fmt.Sprintf("platform %s: %s read %s before the rename and reads %s after it", ref.Platform, ref.Raw, before, after)})
	}
	tokenOr := func(t string) string { return cmp.Or(t, "no token") }
	for _, ref := range scan.Refs {
		at := pos{ref.File, ref.Line, ref.Col}
		if b, ok := was[at]; !renamed[at] && (!ok || b.Token != ref.Token) {
			changed(ref, tokenOr(b.Token), tokenOr(ref.Token))
		}
		delete(was, at)
		reads[pos{ref.File, ref.Line, ref.Col}] = ref.Token
		if ref.Token == pl.Old {
			left = append(left, diag.Diagnostic{Rule: "reference-undeclared", Severity: diag.Error, Docs: diag.DocsURL("reference-undeclared"), File: ref.File, Line: ref.Line, Col: ref.Col,
				Message: fmt.Sprintf("platform %s: %s still reads %s after the rename", ref.Platform, ref.Raw, pl.Old)})
		}
	}
	for _, b := range was {
		if b.Token != "" {
			changed(b, b.Token, "no token")
		}
	}
	for f, ss := range edits {
		for _, s := range ss {
			col := shift(f, s.Line, s.RefCol)
			if tok := reads[pos{f, s.Line, col}]; tok != pl.New {
				left = append(left, diag.Diagnostic{Rule: "reference-undeclared", Severity: diag.Error, Docs: diag.DocsURL("reference-undeclared"), File: f, Line: s.Line, Col: col,
					Message: fmt.Sprintf("%s rewritten as %s reads %q, not %s, after the rename", s.Raw, s.New, tok, pl.New)})
			}
		}
	}
	if len(left) > 0 {
		diag.Sort(left)
		return &Refusal{Msg: fmt.Sprintf("rename: %d consumer reference(s) would not read %s after the rename; nothing was written", len(left), pl.New), Diagnostics: left}
	}
	pl.After = after
	return nil
}
