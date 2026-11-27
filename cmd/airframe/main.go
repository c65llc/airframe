// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Command airframe is the Airframe design-system CLI.
package main

import (
	"os"

	"github.com/c65llc/airframe/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
