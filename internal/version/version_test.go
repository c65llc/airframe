// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package version_test

import (
	"testing"

	"github.com/c65llc/airframe/internal/version"
	"github.com/stretchr/testify/require"
)

func TestVersionDefaultsToDev(t *testing.T) {
	require.Equal(t, "dev", version.Version)
}
