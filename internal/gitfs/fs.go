// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package gitfs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FS is the repository's directory (Repo.Prefix) as it was at one commit.
// Paths are relative to that directory. Only regular files are served:
// symlinks and submodules in the tree are left out. Close stops the
// cat-file process FS starts on its first read.
type FS struct {
	files map[string]entry         // path → blob
	dirs  map[string][]fs.DirEntry // path ("." for the root) → sorted entries

	bin, dir string

	mu     sync.Mutex
	batch  *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	closed bool
}

var (
	_ fs.ReadDirFS  = (*FS)(nil)
	_ fs.StatFS     = (*FS)(nil)
	_ fs.ReadFileFS = (*FS)(nil)
)

type entry struct {
	oid  string
	size int64
}

// FS lists the tree of commit (a SHA from Resolve) under r.Prefix. A
// prefix the commit does not have gives an empty FS.
func (r *Repo) FS(commit string) (*FS, error) {
	if !shaRE.MatchString(commit) {
		return nil, fmt.Errorf("gitfs: %q is not a commit SHA", commit)
	}
	args := []string{"ls-tree", "-r", "-z", "-l", "--full-tree", commit}
	if r.Prefix != "" {
		args = append(args, "--", strings.TrimSuffix(r.Prefix, "/"))
	}
	out, err := run(r.bin, r.dir, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %v", commit, err)
	}
	f := &FS{files: map[string]entry{}, dirs: map[string][]fs.DirEntry{}, bin: r.bin, dir: r.dir}
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		// <mode> SP <type> SP <object> SP+ <size> TAB <path>
		meta, name, ok := strings.Cut(string(rec), "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 {
			return nil, fmt.Errorf("list %s: unexpected ls-tree line %q", commit, rec)
		}
		mode, typ, oid := fields[0], fields[1], fields[2]
		if typ != "blob" || (mode != "100644" && mode != "100755") {
			continue // a symlink, submodule or other non-file
		}
		rel, ok := strings.CutPrefix(name, r.Prefix)
		if !ok || !fs.ValidPath(rel) || rel == "." {
			continue
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("list %s: bad size in %q", commit, rec)
		}
		f.files[rel] = entry{oid: oid, size: size}
	}
	f.index()
	return f, nil
}

// index builds the directory listings the file paths imply.
func (f *FS) index() {
	seen := map[string]map[string]fs.DirEntry{".": {}}
	add := func(dir string, e fs.DirEntry) {
		if seen[dir] == nil {
			seen[dir] = map[string]fs.DirEntry{}
		}
		seen[dir][e.Name()] = e
	}
	for p, e := range f.files {
		add(dirOf(p), info{name: path.Base(p), size: e.size})
		for d := dirOf(p); d != "."; d = dirOf(d) {
			add(dirOf(d), info{name: path.Base(d), dir: true})
		}
	}
	for d, m := range seen {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		slices.Sort(names)
		ents := make([]fs.DirEntry, 0, len(names))
		for _, n := range names {
			ents = append(ents, m[n])
		}
		f.dirs[d] = ents
	}
}

func dirOf(p string) string {
	d := path.Dir(p)
	if d == "" {
		return "."
	}
	return d
}

// Open opens a file or directory.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if e, ok := f.files[name]; ok {
		b, err := f.read(e)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		return &file{info: info{name: path.Base(name), size: e.size}, r: bytes.NewReader(b)}, nil
	}
	if ents, ok := f.dirs[name]; ok {
		return &file{info: info{name: path.Base(name), dir: true}, ents: ents}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadFile reads a file's contents.
func (f *FS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	e, ok := f.files[name]
	if !ok {
		if _, dir := f.dirs[name]; dir {
			return nil, &fs.PathError{Op: "readfile", Path: name, Err: errors.New("is a directory")}
		}
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	b, err := f.read(e)
	if err != nil {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: err}
	}
	return b, nil
}

// Stat reports on a file or directory without reading it.
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if e, ok := f.files[name]; ok {
		return info{name: path.Base(name), size: e.size}, nil
	}
	if _, ok := f.dirs[name]; ok {
		return info{name: path.Base(name), dir: true}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// ReadDir lists a directory, sorted by name.
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	ents, ok := f.dirs[name]
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return slices.Clone(ents), nil
}

// read returns a blob's bytes through the cat-file --batch process,
// starting it on first use.
func (f *FS) read(e entry) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, fs.ErrClosed
	}
	if f.batch == nil {
		cmd := command(f.bin, f.dir, "cat-file", "--batch")
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start git cat-file: %w", err)
		}
		f.batch, f.in, f.out = cmd, in, bufio.NewReader(out)
	}
	if _, err := io.WriteString(f.in, e.oid+"\n"); err != nil {
		return nil, f.broken(err)
	}
	// <oid> SP <type> SP <size> LF <contents> LF, or <oid> SP missing LF.
	header, err := f.out.ReadString('\n')
	if err != nil {
		return nil, f.broken(err)
	}
	fields := strings.Fields(header)
	if len(fields) != 3 || fields[0] != e.oid || fields[1] != "blob" {
		return nil, f.broken(fmt.Errorf("git cat-file: unexpected reply %q for %s", strings.TrimSpace(header), e.oid))
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return nil, f.broken(fmt.Errorf("git cat-file: bad size in %q", strings.TrimSpace(header)))
	}
	b := make([]byte, size+1)
	if _, err := io.ReadFull(f.out, b); err != nil {
		return nil, f.broken(err)
	}
	return b[:size], nil
}

// broken stops a batch process whose stream can no longer be trusted, so
// the next read starts a fresh one.
func (f *FS) broken(err error) error {
	f.stop()
	return err
}

// stop ends the batch process, if any. f.mu must be held.
func (f *FS) stop() {
	if f.batch == nil {
		return
	}
	_ = f.in.Close()
	if f.batch.Process != nil {
		_ = f.batch.Process.Kill()
	}
	_ = f.batch.Wait()
	f.batch, f.in, f.out = nil, nil, nil
}

// Close stops the cat-file process. Reads after Close fail.
func (f *FS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stop()
	f.closed = true
	return nil
}

type info struct {
	name string
	size int64
	dir  bool
}

func (i info) Name() string { return i.name }
func (i info) Size() int64  { return i.size }
func (i info) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i info) ModTime() time.Time         { return time.Time{} }
func (i info) IsDir() bool                { return i.dir }
func (i info) Sys() any                   { return nil }
func (i info) Type() fs.FileMode          { return i.Mode().Type() }
func (i info) Info() (fs.FileInfo, error) { return i, nil }

type file struct {
	info info
	r    *bytes.Reader
	ents []fs.DirEntry
	pos  int
}

func (f *file) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *file) Close() error               { return nil }

func (f *file) Read(b []byte) (int, error) {
	if f.r == nil {
		return 0, &fs.PathError{Op: "read", Path: f.info.name, Err: fs.ErrInvalid}
	}
	return f.r.Read(b)
}

func (f *file) ReadDir(n int) ([]fs.DirEntry, error) {
	if !f.info.dir {
		return nil, &fs.PathError{Op: "readdir", Path: f.info.name, Err: fs.ErrInvalid}
	}
	rest := f.ents[f.pos:]
	if n <= 0 {
		f.pos = len(f.ents)
		return slices.Clone(rest), nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(rest))
	f.pos += n
	return slices.Clone(rest[:n]), nil
}
