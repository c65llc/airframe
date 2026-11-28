// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package project

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

// Every write Airframe makes to a project goes through an os.Root, so
// neither ".." nor a symlink can carry it outside the project.

// WriteFile writes data to the project-relative slash path rel inside root
// atomically: a temporary file in the same directory, renamed into place.
// It creates the parent directories. Every step goes through root, so
// neither ".." nor a symlink can carry the write outside the project. An
// existing file keeps its permission bits; a new one gets 0644.
func WriteFile(root *os.Root, rel string, data []byte) error {
	tmp, err := StageFile(root, rel, data)
	if err != nil {
		return err
	}
	if err := root.Rename(tmp, filepath.FromSlash(rel)); err != nil {
		_ = root.Remove(tmp)
		return err
	}
	return nil
}

// WriteStaged writes every file inside root as close to all or nothing as a
// filesystem allows: each is first written to a temporary name beside it,
// and only when every one is written are they renamed into place, in path
// order. A failure before the renames removes the temporary files and
// changes nothing (beyond any parent directory it had to create); one
// during them, which a rename within a directory makes unlikely but not
// impossible, leaves the files already renamed, which renamed lists.
func WriteStaged(root *os.Root, files map[string][]byte) (renamed []string, err error) {
	names := slices.Sorted(maps.Keys(files))
	tmps := make([]string, 0, len(names))
	defer func() {
		for _, t := range tmps[len(renamed):] {
			_ = root.Remove(t)
		}
	}()
	for _, name := range names {
		tmp, err := StageFile(root, name, files[name])
		if err != nil {
			return nil, err
		}
		tmps = append(tmps, tmp)
	}
	for i, name := range names {
		if err := root.Rename(tmps[i], filepath.FromSlash(name)); err != nil {
			return renamed, fmt.Errorf("write %s: %w", name, err)
		}
		renamed = append(renamed, name)
	}
	return renamed, nil
}

// StageFile writes data to a new temporary file beside the
// project-relative slash path rel inside root, with rel's permission bits
// (0644 for a new file), creating the parent directories, and returns the
// temporary file's name. Renaming it onto rel is the caller's.
func StageFile(root *os.Root, rel string, data []byte) (string, error) {
	name := filepath.FromSlash(rel)
	parent := filepath.Dir(name)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	perm := os.FileMode(0o644)
	if fi, err := root.Stat(name); err == nil {
		perm = fi.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	for range 10 {
		tmpName := filepath.Join(parent, "."+filepath.Base(name)+".tmp-"+strconv.FormatUint(rand.Uint64(), 36))
		f, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = root.Chmod(tmpName, perm)
		}
		if err != nil {
			_ = root.Remove(tmpName)
			return "", err
		}
		return tmpName, nil
	}
	return "", fmt.Errorf("write %s: no free temporary name", rel)
}
