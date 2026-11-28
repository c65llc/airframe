// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpecimenCommand(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS("../../testdata/apple")))

	code, stdout, stderr := run("--root", dir, "specimen")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "wrote design-system/generated/specimen/colors-notes.svg\n")
	assert.FileExists(t, filepath.Join(dir, "design-system/generated/specimen/spacing-tasks.svg"))

	// Overwritten each run, byte for byte the same.
	first, err := os.ReadFile(filepath.Join(dir, "design-system/generated/specimen/type-notes.svg"))
	require.NoError(t, err)
	code, _, stderr = run("--root", dir, "specimen")
	require.Equal(t, ExitOK, code, stderr)
	second, err := os.ReadFile(filepath.Join(dir, "design-system/generated/specimen/type-notes.svg"))
	require.NoError(t, err)
	assert.Equal(t, first, second)

	code, stdout, stderr = run("--root", dir, "specimen", "--out", "sheets")
	require.Equal(t, ExitOK, code, stderr)
	assert.Contains(t, stdout, "wrote sheets/colors-tasks.svg\n")

	// Not part of build: build --check does not name them.
	_, stdout, _ = run("--root", dir, "build", "--check")
	assert.NotContains(t, stdout, "specimen")

	// An out directory that escapes the project is refused.
	code, _, stderr = run("--root", dir, "specimen", "--out", "../escape")
	assert.Equal(t, ExitUsage, code)
	assert.NotEmpty(t, stderr)
	assert.NoDirExists(t, filepath.Join(filepath.Dir(dir), "escape"))

	code, _, _ = run("--root", dir, "specimen", "extra")
	assert.Equal(t, ExitUsage, code)
}
