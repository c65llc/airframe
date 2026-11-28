// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package mcp

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/c65llc/airframe/internal/project"
)

// overlay is the project as a write would leave it: base with files
// replaced or added, and nothing written. A preview lints it, so the
// findings it reports are exactly those the write would.
type overlay struct {
	base  fs.FS
	files project.MemFS
}

func (o overlay) Open(name string) (fs.File, error) {
	if _, ok := o.files[name]; ok {
		return o.files.Open(name)
	}
	f, err := o.base.Open(name)
	if errors.Is(err, fs.ErrNotExist) && o.holds(name) {
		return o.files.Open(name) // a directory only the overlay's files imply
	}
	return f, err
}

// holds reports whether an overlay file lies under the directory name.
func (o overlay) holds(name string) bool {
	for file := range o.files {
		if name == "." || strings.HasPrefix(file, name+"/") {
			return true
		}
	}
	return false
}

// ReadDir lists base's entries for name with the overlay's files in it
// added, and the directories they imply, so a new token or component file
// is found where the loader looks, even in a new directory.
func (o overlay) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(o.base, name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	found := err == nil
	prefix := name + "/"
	if name == "." {
		prefix = ""
	}
	for file := range o.files {
		rest, ok := strings.CutPrefix(file, prefix)
		if !ok {
			continue
		}
		found = true
		child, _, _ := strings.Cut(rest, "/")
		if slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == child }) {
			continue
		}
		info, _ := fs.Stat(o.files, path.Join(name, child)) // a file or a directory o.files implies, so Stat cannot fail
		entries = append(entries, fs.FileInfoToDirEntry(info))
	}
	if !found {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}
