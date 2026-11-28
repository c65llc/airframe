// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package report

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPageBody(t *testing.T) {
	assert.Equal(t, "# x\n\nbody\n", PageBody("---\nseverity: error\n---\n\n# x\n\nbody\n"))
	assert.Equal(t, "# x\n", PageBody("# x\n"))
	assert.Equal(t, "Tokens alias each other in a loop, so none of them has a value.", DocSummary("---\na: b\n---\n\n# alias-cycle\n\nTokens alias each other in a loop, so none of them has a value.\n"))
}
