package main

import (
	"context"
	"fmt"
	"os"

	"github.com/UnitVectorY-Labs/worktreefoundry/internal/app"
)

var Version = "dev" // This will be set by the build systems to the release version

func main() {
	if err := app.Run(context.Background(), os.Args[1:], Version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
