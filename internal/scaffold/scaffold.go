// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package scaffold carries the starter projects airframe init writes. Each
// profile is a directory of files embedded in the binary; the minimal one
// is testdata/minimal without its generated outputs, which init builds.
package scaffold

import (
	"embed"
	"io/fs"
	"path"
	"slices"
)

//go:embed profiles
var profiles embed.FS

// Default is the profile init uses when none is named.
const Default = "minimal"

// Profiles lists the profile names, sorted.
func Profiles() []string {
	ents, _ := fs.ReadDir(profiles, "profiles") // the embedded directory always exists
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// Files returns a profile's files by project-relative path, and false when
// there is no such profile.
func Files(profile string) (map[string][]byte, bool) {
	if !slices.Contains(Profiles(), profile) {
		return nil, false
	}
	root := path.Join("profiles", profile)
	out := map[string][]byte{}
	err := fs.WalkDir(profiles, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(profiles, p)
		if err != nil {
			return err
		}
		out[p[len(root)+1:]] = b
		return nil
	})
	if err != nil { // embedded files always read
		panic(err)
	}
	return out, true
}
