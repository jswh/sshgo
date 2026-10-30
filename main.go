package main

import (
	"os"

	"sshgo/internal/cli"
)

// version is the current version of sshgo.
// It is set at build time via -ldflags in release builds.
// Development builds use "dev".
var version = "dev"

func main() {
	os.Exit(cli.Run(version, os.Args[1:], os.Stdout, os.Stderr))
}
