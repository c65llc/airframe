// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package project

import (
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// MemFS is an in-memory fs.FS for tests and for hosts with no disk. Keys are
// slash-separated file paths; directories are implied by them.
type MemFS map[string]string

var (
	_ fs.ReadDirFS = MemFS(nil)
	_ fs.StatFS    = MemFS(nil)
)

// Open opens a file or an implied directory.
func (m MemFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if s, ok := m[name]; ok {
		return &memFile{info: memInfo{name: path.Base(name), size: int64(len(s))}, r: strings.NewReader(s)}, nil
	}
	ents, ok := m.dir(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{info: memInfo{name: path.Base(name), dir: true}, ents: ents}, nil
}

// Stat reports on a file or an implied directory.
func (m MemFS) Stat(name string) (fs.FileInfo, error) {
	f, err := m.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// ReadDir lists a directory, sorted by name.
func (m MemFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	ents, ok := m.dir(name)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return ents, nil
}

// dir returns the entries directly under name; ok is false when no file lives
// beneath it (the root always exists).
func (m MemFS) dir(name string) ([]fs.DirEntry, bool) {
	prefix := ""
	if name != "." {
		prefix = name + "/"
	}
	seen := map[string]fs.DirEntry{}
	for p, s := range m {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}
		first, _, nested := strings.Cut(rest, "/")
		if nested {
			seen[first] = memInfo{name: first, dir: true}
		} else {
			seen[first] = memInfo{name: first, size: int64(len(s))}
		}
	}
	if len(seen) == 0 && name != "." {
		return nil, false
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	slices.Sort(names)
	ents := make([]fs.DirEntry, 0, len(names))
	for _, n := range names {
		ents = append(ents, seen[n])
	}
	return ents, true
}

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i memInfo) ModTime() time.Time         { return time.Time{} }
func (i memInfo) IsDir() bool                { return i.dir }
func (i memInfo) Sys() any                   { return nil }
func (i memInfo) Type() fs.FileMode          { return i.Mode().Type() }
func (i memInfo) Info() (fs.FileInfo, error) { return i, nil }

type memFile struct {
	info memInfo
	r    *strings.Reader
	ents []fs.DirEntry
	pos  int
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

func (f *memFile) Read(b []byte) (int, error) {
	if f.r == nil {
		return 0, &fs.PathError{Op: "read", Path: f.info.name, Err: fs.ErrInvalid}
	}
	return f.r.Read(b)
}

func (f *memFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if !f.info.dir {
		return nil, &fs.PathError{Op: "readdir", Path: f.info.name, Err: fs.ErrInvalid}
	}
	rest := f.ents[f.pos:]
	if n <= 0 {
		f.pos = len(f.ents)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	if n > len(rest) {
		n = len(rest)
	}
	f.pos += n
	return rest[:n], nil
}
