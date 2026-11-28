// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOpenFence: fences follow CommonMark, as the installer and the skill
// checks both need them to.
func TestOpenFence(t *testing.T) {
	for line, want := range map[string]struct {
		fence, info string
		ok          bool
	}{
		"```":            {"```", "", true},
		"```bash":        {"```", "bash", true},
		"````md x":       {"````", "md", true},
		"~~~ Mermaid":    {"~~~", "Mermaid", true},
		"   ```":         {"```", "", true},
		"    ```":        {"", "", false}, // indented code, not a fence
		"``":             {"", "", false},
		"``` a`b":        {"", "", false}, // a backtick fence's info has no backtick
		"~~~ a`b":        {"~~~", "a`b", true},
		"text ```":       {"", "", false},
		"\t```":          {"", "", false},
		"```\tjson":      {"```", "json", true},
		"~~~~~~~~~~~~~~": {"~~~~~~~~~~~~~~", "", true},
	} {
		fence, info, ok := OpenFence(line)
		assert.Equal(t, want.ok, ok, line)
		assert.Equal(t, want.fence, fence, line)
		assert.Equal(t, want.info, info, line)
	}
}

func TestIsFenceClose(t *testing.T) {
	assert.True(t, IsFenceClose("```", "```"))
	assert.True(t, IsFenceClose("````", "```"), "a longer run closes")
	assert.True(t, IsFenceClose("   ```  \t", "```"), "three spaces in, trailing blanks")
	assert.False(t, IsFenceClose("``", "```"), "a shorter run does not")
	assert.False(t, IsFenceClose("~~~", "```"), "the other character does not")
	assert.False(t, IsFenceClose("``` md", "```"), "a closer has no info string")
	assert.False(t, IsFenceClose("    ```", "```"), "four spaces in is code")
}
