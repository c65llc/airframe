// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package scaffold

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfiles(t *testing.T) {
	assert.Equal(t, []string{"minimal"}, Profiles())
	_, ok := Files("nope")
	assert.False(t, ok)
}

// TestMinimalIsTestdataMinimal keeps the embedded profile a copy of
// testdata/minimal without design-system/generated.
func TestMinimalIsTestdataMinimal(t *testing.T) {
	want := map[string]string{}
	src := os.DirFS(filepath.Join("..", "..", "testdata", "minimal"))
	require.NoError(t, fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(p, "design-system/generated/") {
			return err
		}
		b, err := fs.ReadFile(src, p)
		want[p] = string(b)
		return err
	}))
	files, ok := Files("minimal")
	require.True(t, ok)
	got := map[string]string{}
	for p, b := range files {
		got[p] = string(b)
	}
	assert.Equal(t, want, got)
	assert.Contains(t, got, "airframe.yaml")
}
