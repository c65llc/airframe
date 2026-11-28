// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package skills

import (
	"regexp"
	"strings"
)

// fenceRE is a fence opener: up to three spaces, a run of three or more `
// or ~, and the first word of the info string.
var fenceRE = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*(\\S*)")

// OpenFence reports whether text opens a fenced code block, and returns the
// fence's run of ` or ~ and the first word of its info string. A backtick
// fence whose info string holds a backtick is not a fence (CommonMark).
func OpenFence(text string) (fence, info string, ok bool) {
	m := fenceRE.FindStringSubmatch(text)
	if m == nil {
		return "", "", false
	}
	if _, rest, _ := strings.Cut(text, m[1]); m[1][0] == '`' && strings.Contains(rest, "`") {
		return "", "", false
	}
	return m[1], m[2], true
}

// IsFenceClose reports whether text closes the block fence opened: a run of
// the same character at least as long, indented at most three spaces, with
// nothing after it but spaces and tabs.
func IsFenceClose(text, fence string) bool {
	t := strings.TrimRight(strings.TrimLeft(text, " "), " \t")
	return len(text)-len(strings.TrimLeft(text, " ")) <= 3 &&
		len(t) >= len(fence) && strings.Trim(t, fence[:1]) == ""
}
