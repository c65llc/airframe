// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package skills

import (
	"bytes"
	"errors"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"
)

// memFS is an in-memory WriteFS for the installer's tests: a map from
// slash path to file contents. Directories are implied by the paths of the
// files under them, so an empty directory cannot exist. Write through a
// non-nil map.
type memFS map[string][]byte

var _ WriteFS = memFS{}

// isDir reports whether some file lies under name.
func (m memFS) isDir(name string) bool {
	for k := range m {
		if strings.HasPrefix(k, name+"/") {
			return true
		}
	}
	return false
}

// Open opens a file or an implied directory.
func (m memFS) Open(name string) (fs.File, error) {
	mf := fstest.MapFS{}
	for k, v := range m {
		mf[k] = &fstest.MapFile{Data: v}
	}
	return mf.Open(name)
}

// WriteFile stores a copy of data at name, replacing any file there. It
// refuses a name that is a directory or lies under a file.
func (m memFS) WriteFile(name string, data []byte) error {
	if err := check("write", name); err != nil {
		return err
	}
	if m.isDir(name) {
		return &fs.PathError{Op: "write", Path: name, Err: errors.New("is a directory")}
	}
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		if _, ok := m[dir]; ok {
			return &fs.PathError{Op: "write", Path: name, Err: errors.New("not a directory")}
		}
	}
	m[name] = bytes.Clone(data)
	return nil
}

// Remove deletes the file at name. A missing file is not an error; a
// directory is.
func (m memFS) Remove(name string) error {
	if err := check("remove", name); err != nil {
		return err
	}
	if m.isDir(name) {
		return &fs.PathError{Op: "remove", Path: name, Err: errors.New("is a directory")}
	}
	delete(m, name)
	return nil
}

// RemoveEmptyDir removes nothing: a memFS directory exists only while it
// holds files, so it is never empty. An invalid name is an error.
func (m memFS) RemoveEmptyDir(name string) error { return check("remove", name) }
