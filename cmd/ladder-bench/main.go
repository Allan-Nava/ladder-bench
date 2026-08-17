// ladder-bench measures an ABR encoding ladder instead of inheriting it.
//
//	ladder-bench init  > ladder-bench.yml
//	ladder-bench doctor --config ladder-bench.yml
//	ladder-bench plan   --config ladder-bench.yml
//	ladder-bench run    --config ladder-bench.yml --output markdown
//
// It encodes a reference clip across a grid of (resolution, bitrate) points,
// scores each one with VMAF, and reports where each resolution stops paying
// for bits, which resolution wins at each bitrate, and what the ladder should
// therefore be.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev" // injected at build time via -ldflags

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(64)
	}
	// Ctrl-C cancels the context, which kills the running ffmpeg: a long grid
	// is meant to be interruptible, and the finished points stay on disk for
	// the next run to reuse.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "run":
		err = runRun(ctx, os.Args[2:])
	case "plan":
		err = runPlan(ctx, os.Args[2:])
	case "doctor":
		err = runDoctor(ctx, os.Args[2:])
	case "init":
		err = runInit(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("ladder-bench", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "ladder-bench: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(64)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ladder-bench:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ladder-bench — measure an ABR encoding ladder instead of inheriting it

Usage:
  ladder-bench init [--out FILE]        write a starting config
  ladder-bench doctor [--config FILE]   check ffmpeg, libvmaf, codecs and input
  ladder-bench plan [--config FILE]     print the grid and the exact commands
  ladder-bench run [--config FILE]      encode, measure and report
  ladder-bench version

Run 'ladder-bench <command> --help' for the flags of a command.
`)
}
