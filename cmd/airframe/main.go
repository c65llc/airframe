// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Command airframe is the Airframe design-system CLI.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/c65llc/airframe/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "airframe %s\n", version.Version)
		return 0
	}
	fmt.Fprintln(stderr, "usage: airframe version")
	return 2
}
