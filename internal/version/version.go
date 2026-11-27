// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 C65 LLC

// Package version holds the build version, set by ldflags at release time.
package version

// Version is the airframe version. Release builds override it with
// -X github.com/c65llc/airframe/internal/version.Version=<version>.
var Version = "dev"
