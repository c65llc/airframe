// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceEveryKind(t *testing.T) {
	for _, kind := range Kinds {
		b, ok := Source(kind)
		require.True(t, ok, kind)
		var doc map[string]any
		require.NoError(t, json.Unmarshal(b, &doc), kind)
		assert.Contains(t, doc, "$schema", kind)
	}
	assert.Len(t, sources, len(Kinds))
	_, ok := Source("widgets")
	assert.False(t, ok)
}
