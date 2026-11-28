// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package rename

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// Overlay is base with files laid over it: a path in files reads its bytes
// there, any other reads through to base. Directories are base's plus
// those files' parents. It never writes, so the planner can load the
// renamed project without touching the disk.
func Overlay(base fs.FS, files map[string][]byte) fs.FS {
	return overlay{base: base, files: files}
}

type overlay struct {
	base  fs.FS
	files map[string][]byte
}

var (
	_ fs.ReadDirFS  = overlay{}
	_ fs.StatFS     = overlay{}
	_ fs.ReadFileFS = overlay{}
)

func (o overlay) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if b, ok := o.files[name]; ok {
		return &file{info: info{name: path.Base(name), size: int64(len(b))}, r: bytes.NewReader(b)}, nil
	}
	if o.implied(name) {
		ents, err := o.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &file{info: info{name: path.Base(name), dir: true}, ents: ents}, nil
	}
	return o.base.Open(name)
}

func (o overlay) ReadFile(name string) ([]byte, error) {
	if b, ok := o.files[name]; ok {
		return bytes.Clone(b), nil
	}
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	return fs.ReadFile(o.base, name)
}

func (o overlay) Stat(name string) (fs.FileInfo, error) {
	f, err := o.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// implied reports whether name is the parent directory of a laid file.
func (o overlay) implied(name string) bool {
	if name == "." {
		return true
	}
	for f := range o.files {
		if strings.HasPrefix(f, name+"/") {
			return true
		}
	}
	return false
}

// ReadDir lists base's entries under name merged with the laid files and
// directories directly under it, sorted by name.
func (o overlay) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	ents, err := fs.ReadDir(o.base, name)
	if err != nil && !(errors.Is(err, fs.ErrNotExist) && o.implied(name)) {
		return nil, err
	}
	byName := map[string]fs.DirEntry{}
	for _, e := range ents {
		byName[e.Name()] = e
	}
	prefix := ""
	if name != "." {
		prefix = name + "/"
	}
	for f, b := range o.files {
		rest, ok := strings.CutPrefix(f, prefix)
		if !ok {
			continue
		}
		if dir, _, nested := strings.Cut(rest, "/"); nested {
			if _, ok := byName[dir]; !ok {
				byName[dir] = fs.FileInfoToDirEntry(info{name: dir, dir: true})
			}
			continue
		}
		byName[rest] = fs.FileInfoToDirEntry(info{name: rest, size: int64(len(b))})
	}
	out := make([]fs.DirEntry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return out, nil
}

type info struct {
	name string
	size int64
	dir  bool
}

func (i info) Name() string       { return i.name }
func (i info) Size() int64        { return i.size }
func (i info) ModTime() time.Time { return time.Time{} }
func (i info) IsDir() bool        { return i.dir }
func (i info) Sys() any           { return nil }
func (i info) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

type file struct {
	info info
	r    *bytes.Reader
	ents []fs.DirEntry
	read int
}

func (f *file) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *file) Close() error               { return nil }

func (f *file) Read(b []byte) (int, error) {
	if f.info.dir {
		return 0, &fs.PathError{Op: "read", Path: f.info.name, Err: errors.New("is a directory")}
	}
	return f.r.Read(b)
}

// ReadDir makes a directory an fs.ReadDirFile.
func (f *file) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := f.ents[f.read:]
	if n <= 0 {
		f.read = len(f.ents)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	f.read += len(rest)
	return rest, nil
}
