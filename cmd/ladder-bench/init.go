package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
)

// The starting config lives here and only here. A second copy at the repo root
// would drift from this one the first time a default changed, and the copy
// people read is never the one the binary writes.
//
//go:embed example.yml
var exampleConfig []byte

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	out := fs.String("out", "", "write to this file instead of stdout")
	force := fs.Bool("force", false, "overwrite the file if it exists")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		_, err := os.Stdout.Write(exampleConfig)
		return err
	}
	if _, err := os.Stat(*out); err == nil && !*force {
		return fmt.Errorf("%s already exists (pass --force to overwrite)", *out)
	}
	if err := os.WriteFile(*out, exampleConfig, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s — edit `input:` and the grid, then run 'ladder-bench doctor'\n", *out)
	return nil
}
