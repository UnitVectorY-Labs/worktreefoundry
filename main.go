package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/UnitVectorY-Labs/worktreefoundry/internal/app"
)

var Version = "dev" // This will be set by the build systems to the release version

func main() {
	// Set the build version from the build info if not set by the build system
	if Version == "dev" || Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				Version = bi.Main.Version
			}
		}
	}

	if err := app.Run(context.Background(), os.Args[1:], Version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
