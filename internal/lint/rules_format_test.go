// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package lint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotFormatted(t *testing.T) {
	m := minimal(t)
	res := lintFS(t, m, Options{})
	assert.Empty(t, byRule(res.Diagnostics, "not-formatted"))
	assert.Equal(t, "files", tallyOf(res, "not-formatted").Noun)
	assert.Positive(t, tallyOf(res, "not-formatted").Items)

	f := "design-system/tokens/semantic.tokens.json"
	crlf := strings.ReplaceAll(m[f], "\n", "\r\n")
	m[f] = crlf
	assert.Empty(t, byRule(lintFS(t, m, Options{}).Diagnostics, "not-formatted"), "CRLF alone is formatted")

	m[f] = strings.Replace(m[f], "  ", "    ", 1)
	got := byRule(lintFS(t, m, Options{}).Diagnostics, "not-formatted")
	require.Len(t, got, 1)
	assert.Equal(t, f, got[0].File)
	assert.Equal(t, f+" is not formatted — run `airframe fmt`", got[0].Message)
}
