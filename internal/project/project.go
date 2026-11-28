// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package project loads a whole Airframe project (config, token files,
// invariants and components) from any fs.FS, so the linter, emitters and CLI share one entry.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/c65llc/airframe/internal/component"
	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/diag"
	"github.com/c65llc/airframe/internal/invariants"
	"github.com/c65llc/airframe/internal/tokens"
)

// Project is everything Airframe reads from a repository.
type Project struct {
	FS         fs.FS
	Config     *config.Config
	Tokens     *tokens.Set
	Invariants []invariants.Invariant
	Components map[string]*component.Component // by id; a file that did not load is absent
	Sources    map[string][]byte               // repo-relative path -> bytes of every file loaded
	TokenFiles []string                        // sorted repo-relative token file paths
	// ComponentFiles are the sorted repo-relative paths of every
	// <content>/components/*.yaml, whether or not it loaded.
	ComponentFiles []string
}

func diagnostic(rule, file, msg string) diag.Diagnostic {
	return diag.Diagnostic{
		Rule: rule, Severity: diag.Error, File: file,
		Message: msg, Docs: diag.DocsURL(rule),
	}
}

// Load reads airframe.yaml, every <content>/**/*.tokens.json, the optional
// <content>/invariants.yaml and every <content>/components/*.yaml, then
// checks the components against the parsed tokens. Problems with the project's own files come back
// as diagnostics; the error is reserved for I/O failures other than a file
// not existing.
//
// The Project and the diagnostics together say how far loading got:
//   - A nil Project means loading stopped (config-missing, invalid config,
//     no-tokens). There is nothing to lint; report the diagnostics.
//   - A non-nil Project with diagnostics is a partial load. Lint what loaded
//     and report these diagnostics too: Tokens holds whatever parsed after
//     token-file errors, Invariants is nil after an invariants error, and
//     Components lacks each component file that did not load.
func Load(fsys fs.FS) (*Project, []diag.Diagnostic, error) {
	cfgData, err := fs.ReadFile(fsys, config.File)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, []diag.Diagnostic{diagnostic("config-missing", config.File, "no airframe.yaml at the project root")}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", config.File, err)
	}
	cfg, ds := config.Load(cfgData)
	if len(ds) > 0 {
		return nil, ds, nil
	}

	p := &Project{FS: fsys, Config: cfg, Sources: map[string][]byte{config.File: cfgData}}

	files, err := doublestar.Glob(fsys, cfg.Content+"/**/*.tokens.json")
	if err != nil {
		return nil, nil, fmt.Errorf("find token files: %w", err)
	}
	slices.Sort(files)
	if len(files) == 0 {
		return nil, []diag.Diagnostic{diagnostic("no-tokens", config.File,
			fmt.Sprintf("no *.tokens.json under %s/ (content:)", cfg.Content))}, nil
	}
	p.TokenFiles = files
	tokenData := map[string][]byte{}
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", f, err)
		}
		tokenData[f] = b
		p.Sources[f] = b
	}

	var all []diag.Diagnostic
	set, tds := tokens.Parse(cfg, tokenData)
	all = append(all, tds...)
	p.Tokens = set

	invPath := path.Join(cfg.Content, invariants.File)
	invData, err := fs.ReadFile(fsys, invPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, nil, fmt.Errorf("read %s: %w", invPath, err)
	default:
		p.Sources[invPath] = invData
		invs, ids := invariants.Parse(invData)
		for i := range ids {
			ids[i].File = invPath
		}
		all = append(all, ids...)
		p.Invariants = invs
	}

	cds, err := p.loadComponents()
	if err != nil {
		return nil, nil, err
	}
	all = append(all, cds...)
	return p, all, nil
}

// loadComponents reads every <content>/components/*.yaml into p.Components
// and runs the consistency checks, which need p.Tokens.
func (p *Project) loadComponents() ([]diag.Diagnostic, error) {
	p.Components = map[string]*component.Component{}
	dir := path.Join(p.Config.Content, component.Dir)
	files, err := doublestar.Glob(p.FS, dir+"/*.yaml")
	if err != nil {
		return nil, fmt.Errorf("find component files: %w", err)
	}
	slices.Sort(files)
	p.ComponentFiles = files
	var all []diag.Diagnostic
	for _, f := range files {
		b, err := fs.ReadFile(p.FS, f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		p.Sources[f] = b
		c, ds := component.Parse(f, b)
		all = append(all, ds...)
		if c != nil {
			p.Components[c.ID] = c
		}
	}
	return append(all, component.Check(p.Components, p.Tokens)...), nil
}
