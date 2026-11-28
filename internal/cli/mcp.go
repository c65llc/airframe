// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/c65llc/airframe/internal/config"
	"github.com/c65llc/airframe/internal/mcp"
	"github.com/c65llc/airframe/internal/version"
)

// mcpCommand is mcp's entry in Commands.
var mcpCommand = Command{
	Name:    "mcp",
	Summary: "Serve the project to coding agents over MCP on stdin and stdout, reading it afresh on every call; exits 0 when stdin closes.",
	Flags:   []Flag{{Name: "read-only", Summary: "offer only the read tools, as CI and unattended agents should"}},
}

// runMCP serves the project over MCP until stdin ends. The protocol owns
// stdout; stderr gets one line naming the project, then one per tool call.
// The project is opened once, as an os.Root, so no tool can reach outside
// it. No project, or a stream that fails, exits 2.
func runMCP(inv *invocation) int {
	if !inv.noArgs() {
		return ExitUsage
	}
	dir, err := inv.rootDir()
	if err != nil {
		return inv.fail(err)
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return inv.fail(err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.File)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return inv.fail(fmt.Errorf("no %s in %s or a directory above it; run airframe init there, or pass --root", config.File, dir))
		}
		return inv.fail(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return inv.fail(err)
	}
	defer func() { _ = root.Close() }()
	readOnly := *inv.bools["read-only"]
	mode := ""
	if readOnly {
		mode = " (read-only)"
	}
	_, _ = fmt.Fprintf(inv.stderr, "airframe mcp: serving %s%s\n", dir, mode)
	in := inv.stdin
	if in == nil { // Main given no stdin reads an empty one
		in = strings.NewReader("")
	}
	if err := mcp.Serve(in, inv.stdout, inv.stderr, root, mcp.Options{ReadOnly: readOnly, Version: version.Version}); err != nil {
		return inv.fail(err)
	}
	return ExitOK
}

// Registered here, not in the handlers literal, to keep that table's diff
// small while sibling commands land.
func init() { handlers["mcp"] = runMCP }
