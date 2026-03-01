package main

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		code := cmd.ExitCode(err)
		if code != 0 {
			os.Exit(code)
		}
		// Non-exitError errors: print to stderr (cobra may have
		// already printed, but SilenceErrors commands rely on us).
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
