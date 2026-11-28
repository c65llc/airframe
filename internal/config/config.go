// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package config loads airframe.yaml: the tiers, mode axes, platforms and
// lint severities every other part of Airframe reads.
package config

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/schema"
)

// File is the name diagnostics report the config under.
const File = "airframe.yaml"

// Config is a loaded airframe.yaml.
type Config struct {
	Version        int
	Content        string
	Tiers          map[string]Tier
	Axes           []Axis // in file order
	AccentRequired []string
	Platforms      []Platform // in file order
	Naming         map[string]string
	Lint           LintConfig
}

// Tier is one layer of the token hierarchy and who may read it.
type Tier struct {
	Name       string
	ReadableBy []string
}

// Axis is one dimension a token value may vary on. A deferred axis takes its
// values from Source, a file resolved by a later stage; until then Values is
// empty.
type Axis struct {
	Name    string
	Values  []string
	Base    string // the value $value is, rendered when no attribute is set
	Source  string // path after "from ", for a deferred axis
	Binding string // "runtime" or "build"
	Group   string
	Web     WebMode
}

// Deferred reports whether the axis takes its values from a file not yet loaded.
func (a Axis) Deferred() bool { return a.Source != "" }

// WebMode says how an axis is spelled in CSS. Selectors holds the per-value
// selectors (a value maps to one or several), Attr the data attribute, and
// Also extra conditions per value such as a media query.
type WebMode struct {
	Attr      string
	Selectors map[string][]string
	Also      map[string]string
}

// Platform is one output target. Emit lists its formats; Out maps each
// format to its output path. Consumers are the globs of files that read it.
type Platform struct {
	Name      string
	Emit      []string
	Out       map[string]string
	Consumers []string
	FanOut    string         // name of the build-bound axis the emitters fan out over
	Options   map[string]any // scalars as string, bool or float64; mappings as map[string]any
}

// Option returns the scalar option key as a string; ok is false when it is
// unset or not a scalar.
func (p Platform) Option(key string) (string, bool) {
	switch v := p.Options[key].(type) {
	case string:
		return v, true
	case bool, float64:
		return fmt.Sprint(v), true
	}
	return "", false
}

// defaultNaming is the spelling per emit format when naming does not set one.
var defaultNaming = map[string]string{
	"css": "--{name}", "json": "{name}", "colorsets": "{Pascal}",
	"swift": "DesignTokens.{camel}", "kotlin": "DesignTokens.{camel}", "xaml": "Design{Pascal}",
}

// FixedNaming lists the formats whose emitters do not read naming or a
// token's names yet: each accepts only its default spelling, so a custom
// one is rejected rather than silently ignored.
var FixedNaming = []string{"css", "json", "colorsets"}

// FixedNamingError is the problem with spelling got for format, or "" when
// format reads custom naming or got is want, its default spelling. accepted
// is how the message shows want.
func FixedNamingError(format, got, want, accepted string) string {
	if !slices.Contains(FixedNaming, format) || got == want {
		return ""
	}
	return fmt.Sprintf("%s does not support custom naming yet; only %s is accepted", format, accepted)
}

// DefaultName is token name spelled by format's default naming template
// ("accent-ink" in colorsets is "AccentInk").
func DefaultName(format, name string) string {
	return FillNaming(defaultNaming[format], name)
}

// FillNaming fills a naming template's placeholder ({name}, {camel} or
// {Pascal}) with token name.
func FillNaming(template, name string) string {
	return strings.NewReplacer("{name}", name, "{camel}", Camel(name), "{Pascal}", Pascal(name)).Replace(template)
}

// Camel spells a token name in lower camel case: segments split on "-",
// every one after the first capitalised, digits kept attached
// ("accent-ink" → "accentInk", "space-4" → "space4").
func Camel(name string) string {
	segs := strings.Split(name, "-")
	for i := 1; i < len(segs); i++ {
		segs[i] = upperFirst(segs[i])
	}
	return strings.Join(segs, "")
}

// Pascal spells a token name in upper camel case ("accent-ink" →
// "AccentInk", "space-4" → "Space4").
func Pascal(name string) string { return upperFirst(Camel(name)) }

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// NamingFor returns the naming template for an emit format.
func (c *Config) NamingFor(format string) string {
	if t, ok := c.Naming[format]; ok {
		return t
	}
	return defaultNaming[format]
}

// LintConfig holds per-rule severity overrides.
type LintConfig struct {
	Rules map[string]diag.Severity
}

var defaultTiers = map[string]Tier{
	"primitive": {Name: "primitive", ReadableBy: []string{"semantic"}},
	"semantic":  {Name: "semantic", ReadableBy: []string{"component", "consumers", "emitters"}},
	"component": {Name: "component", ReadableBy: []string{"owner"}},
}

func diagnostic(rule, msg, path string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: File, Path: path, Line: line,
		Message: msg, Docs: diag.DocsURL(rule),
	}
}

// Load parses and validates airframe.yaml. On any diagnostic the returned
// Config is nil.
func Load(data []byte) (*Config, []diag.Diagnostic) {
	root, bad := schema.DecodeYAML(data)
	if bad != nil {
		return nil, []diag.Diagnostic{diagnostic("yaml-syntax", bad.Message, "", bad.Line)}
	}
	if ds := schema.Validate("config", &root); len(ds) > 0 {
		for i := range ds {
			ds[i].File = File
		}
		return nil, ds
	}
	if ds := namingProblems(root.Content[0]); len(ds) > 0 {
		return nil, ds
	}
	c, err := build(root.Content[0])
	if err != nil {
		return nil, []diag.Diagnostic{diagnostic("schema-invalid", err.msg, err.path, err.line)}
	}
	return c, nil
}

type buildError struct {
	msg, path string
	line      int
}

// mapping returns the entries of a mapping node in file order.
func mapping(n *yaml.Node) (keys, vals []*yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i])
		vals = append(vals, n.Content[i+1])
	}
	return keys, vals
}

func field(n *yaml.Node, name string) *yaml.Node {
	ks, vs := mapping(n)
	for i, k := range ks {
		if k.Value == name {
			return vs[i]
		}
	}
	return nil
}

// stringList returns a scalar as a one-element list, or a sequence's items.
func stringList(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	var out []string
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}

func build(root *yaml.Node) (*Config, *buildError) {
	c := &Config{Content: "design-system", Tiers: map[string]Tier{}, Lint: LintConfig{Rules: map[string]diag.Severity{}}}
	c.Version = 1
	if n := field(root, "content"); n != nil {
		// content is a directory under the project root, compared and
		// joined in its clean form.
		clean, err := underRoot("content", n.Value, "content", n.Line)
		if err != nil {
			return nil, err
		}
		c.Content = strings.TrimSuffix(clean, "/")
	}

	if tn := field(root, "tiers"); tn != nil {
		ks, vs := mapping(tn)
		for i, k := range ks {
			c.Tiers[k.Value] = Tier{Name: k.Value, ReadableBy: stringList(field(vs[i], "readableBy"))}
		}
	} else {
		for k, v := range defaultTiers {
			c.Tiers[k] = v
		}
	}

	if mn := field(root, "modes"); mn != nil {
		ks, vs := mapping(mn)
		for i, k := range ks {
			a, err := buildAxis(k, vs[i])
			if err != nil {
				return nil, err
			}
			c.Axes = append(c.Axes, a)
		}
	}

	c.AccentRequired = stringList(field(field(root, "accentGroups"), "required"))

	if pn := field(root, "platforms"); pn != nil {
		ks, vs := mapping(pn)
		for i, k := range ks {
			p, err := buildPlatform(k, vs[i], c.Axes)
			if err != nil {
				return nil, err
			}
			c.Platforms = append(c.Platforms, p)
		}
	}

	if nn := field(root, "naming"); nn != nil {
		c.Naming = map[string]string{}
		ks, vs := mapping(nn)
		for i, k := range ks {
			c.Naming[k.Value] = vs[i].Value // checked by namingProblems
		}
	}

	ks, vs := mapping(field(field(root, "lint"), "rules"))
	for i, k := range ks {
		c.Lint.Rules[k.Value] = diag.Severity(vs[i].Value)
	}
	return c, nil
}

func buildAxis(key, n *yaml.Node) (Axis, *buildError) {
	name := key.Value
	at := "modes." + name
	a := Axis{Name: name, Binding: "runtime"}
	if v := field(n, "values"); v != nil {
		if v.Kind == yaml.ScalarNode {
			a.Source = strings.TrimPrefix(v.Value, "from ")
		} else {
			a.Values = stringList(v)
			for _, it := range v.Content {
				if it.Value == "attr" || it.Value == "also" {
					return a, &buildError{
						msg:  fmt.Sprintf("axis %s: value %s is reserved (web.%s spells the axis, not a value)", name, it.Value, it.Value),
						path: at + ".values", line: it.Line,
					}
				}
			}
		}
	}
	if b := field(n, "binding"); b != nil {
		a.Binding = b.Value
	}
	if g := field(n, "group"); g != nil {
		a.Group = g.Value
	}
	var alsoNode *yaml.Node
	selNodes := map[string]*yaml.Node{}
	if w := field(n, "web"); w != nil {
		a.Web = WebMode{Selectors: map[string][]string{}, Also: map[string]string{}}
		// notValue is a web key that should name one of the axis's
		// values and does not. A deferred axis's values are not loaded,
		// so its keys cannot be checked yet.
		notValue := func(k *yaml.Node, key string) *buildError {
			if a.Deferred() || slices.Contains(a.Values, k.Value) {
				return nil
			}
			return &buildError{
				msg:  fmt.Sprintf("%s is not a value of %s", key, name),
				path: key, line: k.Line,
			}
		}
		ks, vs := mapping(w)
		for i, k := range ks {
			switch k.Value {
			case "attr":
				a.Web.Attr = vs[i].Value
			case "also":
				alsoNode = vs[i]
				aks, avs := mapping(vs[i])
				for j, ak := range aks {
					if err := notValue(ak, at+".web.also."+ak.Value); err != nil {
						return a, err
					}
					a.Web.Also[ak.Value] = avs[j].Value
				}
			default:
				if err := notValue(k, at+".web."+k.Value); err != nil {
					return a, err
				}
				a.Web.Selectors[k.Value] = stringList(vs[i])
				selNodes[k.Value] = vs[i]
			}
		}
	}

	// The bare-:root value, if any, is the base: CSS cannot tell it from
	// "attribute unset". It must be that value's only selector.
	rootValue := ""
	for _, v := range slices.Sorted(maps.Keys(a.Web.Selectors)) {
		sels := a.Web.Selectors[v]
		if !slices.ContainsFunc(sels, isBareRoot) {
			continue
		}
		if len(sels) > 1 {
			return a, &buildError{
				msg:  fmt.Sprintf("modes.%s.web.%s: a bare :root must be the value's only selector (it is the base, the render when no attribute is set)", name, v),
				path: at + ".web." + v, line: selNodes[v].Line,
			}
		}
		if rootValue != "" {
			return a, &buildError{
				msg:  fmt.Sprintf("modes.%s.web: %s and %s are both bare :root; an axis has one base", name, rootValue, v),
				path: at + ".web." + v, line: selNodes[v].Line,
			}
		}
		rootValue = v
	}
	a.Base = rootValue
	if b := field(n, "base"); b != nil {
		switch {
		case !a.Deferred() && !slices.Contains(a.Values, b.Value):
			return a, &buildError{
				msg:  fmt.Sprintf("axis %s: base %s is not one of its values", name, b.Value),
				path: at + ".base", line: b.Line,
			}
		case rootValue != "" && rootValue != b.Value:
			return a, &buildError{
				msg:  fmt.Sprintf("axis %s: base %s disagrees with %s, whose only web selector is bare :root", name, b.Value, rootValue),
				path: at + ".base", line: b.Line,
			}
		}
		a.Base = b.Value
	}
	if a.Base == "" && !a.Deferred() {
		return a, &buildError{
			msg:  fmt.Sprintf("axis %s needs a base: value — the value rendered when no attribute is set", name),
			path: at, line: key.Line,
		}
	}

	if alsoNode != nil {
		// The also media condition is a fallback for while the attribute
		// is unset, so it needs the attribute, and every non-base value
		// must be spelled by it: an explicit selector would bypass the
		// :not([attr]) the fallback depends on.
		if a.Web.Attr == "" {
			return a, &buildError{
				msg:  fmt.Sprintf("axis %s: web also needs the axis's web attr, so the fallback can apply only while it is unset", name),
				path: at + ".web.also", line: alsoNode.Line,
			}
		}
		for _, v := range slices.Sorted(maps.Keys(a.Web.Selectors)) {
			if v != rootValue {
				return a, &buildError{
					msg:  fmt.Sprintf("axis %s: with web also, %s must be spelled by the attr (%s), not an explicit selector", name, v, a.Web.Attr),
					path: at + ".web." + v, line: selNodes[v].Line,
				}
			}
		}
	}
	return a, nil
}

func isBareRoot(s string) bool { return strings.TrimSpace(s) == ":root" }

var driveLetter = regexp.MustCompile(`^[A-Za-z]:`)

// underRoot cleans v, a project-relative slash path the config names under
// key (keeping a trailing slash, which marks a directory output), and
// rejects one that could leave the project root: absolute, with a
// ".." escaping it, with a backslash, or with a drive-letter prefix.
func underRoot(key, v, at string, line int) (string, *buildError) {
	clean := path.Clean(v)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") ||
		strings.Contains(v, `\`) || driveLetter.MatchString(v) {
		return "", &buildError{
			msg:  fmt.Sprintf("%s %s must be a slash path under the project root", key, v),
			path: at, line: line,
		}
	}
	// An output must be written somewhere: "", "." and "./" name the
	// root itself; content, unlike out, may be the root.
	if key == "out" && clean == "." {
		return "", &buildError{
			msg:  fmt.Sprintf("out %q: out must name a file or directory under the root", v),
			path: at, line: line,
		}
	}
	if strings.HasSuffix(v, "/") && clean != "." {
		clean += "/"
	}
	return clean, nil
}

// outUnderRoot is underRoot for format's out. Every format but colorsets
// (a catalog folder) writes one file, so its out may not end in "/".
func outUnderRoot(format, v, at string, line int) (string, *buildError) {
	clean, err := underRoot("out", v, at, line)
	if err != nil {
		return "", err
	}
	if format != "colorsets" && strings.HasSuffix(clean, "/") {
		return "", &buildError{msg: fmt.Sprintf("out %s must name a file, not a directory; %s writes one file", v, format), path: at, line: line}
	}
	return clean, nil
}

// Formats are the emit formats airframe.yaml may name: css, json, swift,
// colorsets and kotlin are built now; xaml is accepted so a config can
// declare it, and is reported as not yet built until its emitter lands.
var Formats = []string{"css", "json", "swift", "colorsets", "kotlin", "xaml"}

func buildPlatform(key, n *yaml.Node, axes []Axis) (Platform, *buildError) {
	p := Platform{Name: key.Value, Emit: stringList(field(n, "emit")), Consumers: stringList(field(n, "consumers"))}
	if e := field(n, "emit"); e != nil {
		items := []*yaml.Node{e}
		if e.Kind == yaml.SequenceNode {
			items = e.Content
		}
		for _, it := range items {
			if !slices.Contains(Formats, it.Value) {
				return p, &buildError{
					msg: fmt.Sprintf("platform %s: unknown emit format %s; known: %s",
						p.Name, it.Value, strings.Join(Formats, ", ")),
					path: "platforms." + p.Name + ".emit", line: it.Line,
				}
			}
		}
	}
	if f := field(n, "fanOut"); f != nil {
		at := "platforms." + p.Name + ".fanOut"
		i := slices.IndexFunc(axes, func(a Axis) bool { return a.Name == f.Value })
		switch {
		case i < 0 || axes[i].Deferred():
			why := "is not a loaded axis"
			if i >= 0 {
				why = "is a deferred axis, whose values are not loaded"
			}
			return p, &buildError{msg: fmt.Sprintf("platform %s: fanOut %s %s; it must name a build-bound axis", p.Name, f.Value, why), path: at, line: f.Line}
		case axes[i].Binding != "build":
			return p, &buildError{msg: fmt.Sprintf("platform %s: fanOut %s is %s-bound; it must name a build-bound axis", p.Name, f.Value, axes[i].Binding), path: at, line: f.Line}
		}
		p.FanOut = f.Value
	}
	if o := field(n, "options"); o != nil {
		p.Options = decodeMap(o)
		if p.Name == "apple" {
			if err := checkAppleOptions(o); err != nil {
				return p, err
			}
		}
		if slices.Contains(p.Emit, "kotlin") {
			if err := checkKotlinOptions(p, o); err != nil {
				return p, err
			}
		}
	} else if slices.Contains(p.Emit, "kotlin") {
		return p, &buildError{msg: fmt.Sprintf("platform %s: options.package is required for kotlin", p.Name),
			path: "platforms." + p.Name + ".options.package", line: key.Line}
	}
	if o := field(n, "out"); o != nil {
		p.Out = map[string]string{}
		if o.Kind == yaml.ScalarNode {
			if len(p.Emit) != 1 {
				return p, &buildError{
					msg:  fmt.Sprintf("platform %s: a scalar out needs exactly one emit format, found %d", p.Name, len(p.Emit)),
					path: "platforms." + p.Name + ".out", line: o.Line,
				}
			}
			clean, err := outUnderRoot(p.Emit[0], o.Value, "platforms."+p.Name+".out", o.Line)
			if err != nil {
				return p, err
			}
			p.Out[p.Emit[0]] = clean
		} else {
			ks, vs := mapping(o)
			for i, k := range ks {
				if !slices.Contains(p.Emit, k.Value) {
					return p, &buildError{
						msg: fmt.Sprintf("platform %s: out names %s, which is not one of its emit formats (%s)",
							p.Name, k.Value, strings.Join(p.Emit, ", ")),
						path: "platforms." + p.Name + ".out." + k.Value, line: k.Line,
					}
				}
				clean, err := outUnderRoot(k.Value, vs[i].Value, "platforms."+p.Name+".out."+k.Value, vs[i].Line)
				if err != nil {
					return p, err
				}
				p.Out[k.Value] = clean
			}
		}
	}
	return p, nil
}

// decodeMap decodes a mapping node: scalars as string, bool or float64,
// nested mappings as map[string]any, sequences as []any.
func decodeMap(n *yaml.Node) map[string]any {
	out := map[string]any{}
	ks, vs := mapping(n)
	for i, k := range ks {
		out[k.Value] = decodeValue(vs[i])
	}
	return out
}

func decodeValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.MappingNode:
		return decodeMap(n)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, decodeValue(c))
		}
		return out
	}
	var v any
	if n.Tag == "!!bool" || n.Tag == "!!float" || n.Tag == "!!int" {
		if n.Decode(&v) == nil {
			switch x := v.(type) {
			case bool:
				return x
			case int:
				return float64(x)
			case float64:
				return x
			}
		}
	}
	return n.Value
}

var appleGamuts = []string{"css-color-4", "clip-p3"}

// checkAppleOptions validates the apple platform's options mapping.
func checkAppleOptions(o *yaml.Node) *buildError {
	ks, vs := mapping(o)
	for i, k := range ks {
		at := "platforms.apple.options." + k.Value
		switch k.Value {
		case "gamut":
			if !slices.Contains(appleGamuts, vs[i].Value) {
				return &buildError{msg: fmt.Sprintf("platform apple: gamut %s is not one of %s", vs[i].Value, strings.Join(appleGamuts, ", ")), path: at, line: vs[i].Line}
			}
		case "accentColor":
			if vs[i].Kind != yaml.ScalarNode || vs[i].Value == "" {
				return &buildError{msg: "platform apple: accentColor must be a non-empty string", path: at, line: vs[i].Line}
			}
		default:
			return &buildError{msg: fmt.Sprintf("platform apple: unknown option %s; known: gamut, accentColor", k.Value), path: at, line: k.Line}
		}
	}
	return nil
}

// KotlinKeywords are Kotlin's hard keywords: a package segment may not be
// one, and an emitted member that is one is wrapped in backticks.
var KotlinKeywords = []string{
	"as", "break", "class", "continue", "do", "else", "false", "for", "fun", "if", "in",
	"interface", "is", "null", "object", "package", "return", "super", "this", "throw",
	"true", "try", "typealias", "typeof", "val", "var", "when", "while",
}

// Material3Roles is the parameter list of material3 1.3's lightColorScheme,
// in declaration order: the order Platform.Material3Roles and the emitter use.
var Material3Roles = []string{
	"primary", "onPrimary", "primaryContainer", "onPrimaryContainer", "inversePrimary",
	"secondary", "onSecondary", "secondaryContainer", "onSecondaryContainer",
	"tertiary", "onTertiary", "tertiaryContainer", "onTertiaryContainer",
	"background", "onBackground", "surface", "onSurface", "surfaceVariant", "onSurfaceVariant",
	"surfaceTint", "inverseSurface", "inverseOnSurface", "error", "onError",
	"errorContainer", "onErrorContainer", "outline", "outlineVariant", "scrim",
	"surfaceBright", "surfaceContainer", "surfaceContainerHigh", "surfaceContainerHighest",
	"surfaceContainerLow", "surfaceContainerLowest", "surfaceDim",
}

// KotlinPackage is the kotlin package option ("" when unset).
func (p Platform) KotlinPackage() string {
	s, _ := p.Options["package"].(string)
	return s
}

// Material3Roles returns the material3Scheme mapping as (role, token) pairs
// in Material3Roles order; nil when the option is unset.
func (p Platform) Material3Roles() [][2]string {
	m, ok := p.Options["material3Scheme"].(map[string]any)
	if !ok {
		return nil
	}
	var out [][2]string
	for _, role := range Material3Roles {
		if tok, ok := m[role].(string); ok {
			out = append(out, [2]string{role, tok})
		}
	}
	return out
}

var (
	kotlinColorSpaces = []string{"srgb", "display-p3"}
	kotlinGamuts      = []string{"css-color-4", "clip"}
	kotlinIdent       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// checkKotlinOptions validates the options of a platform that emits kotlin.
func checkKotlinOptions(p Platform, o *yaml.Node) *buildError {
	base := "platforms." + p.Name + ".options."
	bad := func(key string, line int, format string, a ...any) *buildError {
		return &buildError{msg: fmt.Sprintf("platform %s: %s", p.Name, fmt.Sprintf(format, a...)), path: base + key, line: line}
	}
	ks, vs := mapping(o)
	hasPkg := false
	for i, k := range ks {
		v := vs[i]
		switch k.Value {
		case "package":
			hasPkg = true
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" || v.Value == "" {
				return bad("package", v.Line, "package must be a string: a non-empty dotted Kotlin package")
			}
			for _, seg := range strings.Split(v.Value, ".") {
				if !kotlinIdent.MatchString(seg) {
					return bad("package", v.Line, "package %s: segment %q is not a valid identifier", v.Value, seg)
				}
				if slices.Contains(KotlinKeywords, seg) {
					return bad("package", v.Line, "package %s: segment %q is a Kotlin keyword", v.Value, seg)
				}
			}
		case "sharedOut":
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" || v.Value == "" {
				return bad("sharedOut", v.Line, "sharedOut must be a string: a non-empty path")
			}
			clean, err := underRoot("sharedOut", v.Value, base+"sharedOut", v.Line)
			if err != nil {
				return err
			}
			switch {
			case clean == ".":
				return bad("sharedOut", v.Line, "sharedOut %q must name a file under the root", v.Value)
			case strings.HasSuffix(clean, "/"):
				return bad("sharedOut", v.Line, "sharedOut %s must name a file, not a directory", v.Value)
			case p.FanOut != "" && strings.Contains(v.Value, "{"+p.FanOut+"}"):
				return bad("sharedOut", v.Line, "sharedOut %s contains {%s}; the shared file is written once, so it cannot fan out", v.Value, p.FanOut)
			case strings.ContainsAny(v.Value, "{}"):
				return bad("sharedOut", v.Line, "sharedOut %s may not contain { or }; the shared file is written once, to a literal path", v.Value)
			}
			// Store the cleaned path (p.Options is buildPlatform's map):
			// the emitter writes it, and Build's cross-platform collision
			// check compares it with every other output path.
			p.Options["sharedOut"] = clean
		case "colorSpace":
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" {
				return bad("colorSpace", v.Line, "colorSpace must be a string, one of %s", strings.Join(kotlinColorSpaces, ", "))
			}
			if !slices.Contains(kotlinColorSpaces, v.Value) {
				return bad("colorSpace", v.Line, "colorSpace %s is not one of %s", v.Value, strings.Join(kotlinColorSpaces, ", "))
			}
		case "gamut":
			if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" {
				return bad("gamut", v.Line, "gamut must be a string, one of %s", strings.Join(kotlinGamuts, ", "))
			}
			if !slices.Contains(kotlinGamuts, v.Value) {
				return bad("gamut", v.Line, "gamut %s is not one of %s", v.Value, strings.Join(kotlinGamuts, ", "))
			}
		case "material3Scheme":
			if v.Kind != yaml.MappingNode {
				return bad("material3Scheme", v.Line, "material3Scheme maps Material 3 roles to tokens, as in { primary: accent }")
			}
			rks, rvs := mapping(v)
			if len(rks) == 0 {
				return bad("material3Scheme", v.Line, "material3Scheme maps Material 3 roles to tokens, as in { primary: accent }; it has no roles")
			}
			for j, rk := range rks {
				if !slices.Contains(Material3Roles, rk.Value) {
					return bad("material3Scheme."+rk.Value, rk.Line, "material3Scheme role %s is not a Material 3 ColorScheme role", rk.Value)
				}
				if rvs[j].Kind != yaml.ScalarNode || rvs[j].ShortTag() != "!!str" || rvs[j].Value == "" {
					return bad("material3Scheme."+rk.Value, rvs[j].Line, "material3Scheme role %s must be a string naming a token", rk.Value)
				}
			}
		default:
			return bad(k.Value, k.Line, "unknown option %s; known: package, sharedOut, colorSpace, gamut, material3Scheme", k.Value)
		}
	}
	if !hasPkg {
		return &buildError{msg: fmt.Sprintf("platform %s: options.package is required for kotlin", p.Name), path: base + "package", line: o.Line}
	}
	return nil
}

var placeholder = regexp.MustCompile(`\{[^{}]*\}`)

// namingProblems reports every bad naming entry, not just the first: a key
// that is not an emit format, a template checkNaming rejects, and a custom
// template for a FixedNaming format.
func namingProblems(root *yaml.Node) []diag.Diagnostic {
	var ds []diag.Diagnostic
	ks, vs := mapping(field(root, "naming"))
	for i, k := range ks {
		at := "naming." + k.Value
		if !slices.Contains(Formats, k.Value) {
			ds = append(ds, diagnostic("schema-invalid", fmt.Sprintf("naming key %q is not an emit format; known: %s",
				k.Value, strings.Join(Formats, ", ")), at, k.Line))
			continue
		}
		if err := checkNaming(k.Value, vs[i]); err != nil {
			ds = append(ds, diagnostic("schema-invalid", err.msg, err.path, err.line))
			continue
		}
		if msg := FixedNamingError(k.Value, vs[i].Value, defaultNaming[k.Value], defaultNaming[k.Value]); msg != "" {
			ds = append(ds, diagnostic("schema-invalid", "naming "+k.Value+": "+msg, at, vs[i].Line))
		}
	}
	return ds
}

// checkNaming requires exactly one of {name}, {camel}, {Pascal} and no other
// placeholder.
func checkNaming(format string, v *yaml.Node) *buildError {
	n := 0
	for _, m := range placeholder.FindAllString(v.Value, -1) {
		switch m {
		case "{name}", "{camel}", "{Pascal}":
			n++
		default:
			return &buildError{msg: fmt.Sprintf("naming %s: unknown placeholder %s; use {name}, {camel} or {Pascal}", format, m), path: "naming." + format, line: v.Line}
		}
	}
	if n != 1 {
		return &buildError{msg: fmt.Sprintf("naming %s: %q must contain exactly one of {name}, {camel}, {Pascal}", format, v.Value), path: "naming." + format, line: v.Line}
	}
	return nil
}
