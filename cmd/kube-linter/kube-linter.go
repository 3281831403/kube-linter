package main

import (
	"errors"
	"fmt"
	"os"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/command/root"

	// Register templates
	_ "golang.stackrox.io/kube-linter/pkg/templates/all"
)

func main() {
	c := root.Command()
	if err := c.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// A fatal check failure (e.g. a broken CEL expression) is distinct from lint findings:
		// the run could not be performed, so fail with exit code 2.
		var execErr *check.ExecutionError
		if errors.As(err, &execErr) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
