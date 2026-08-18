package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
	"github.com/Allan-Nava/ladder-bench/internal/output"
)

// exitRegression is the exit code for "the comparison found a regression", kept
// apart from the 1 that means the tooling itself failed.
//
// A pipeline wants to treat the two differently: a regression is a result worth
// posting a comment about, a failure is something to go and fix. Collapsing them
// into one code makes every red build ambiguous.
const exitRegression = 2

// runCompare diffs two JSON reports.
//
// It exists because a single report says what the ladder is today, and the
// question that comes next is always whether that changed — an ffmpeg upgrade, a
// new preset, a different source. Comparing two reports is the only way to
// notice, and the only way to notice *automatically*.
func runCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	format := fs.String("output", "text", "output format: "+output.Formats())
	outPath := fs.String("out", "", "write the comparison to this file instead of stdout")
	gate := fs.Bool("exit-on-regression", false,
		fmt.Sprintf("exit %d when the current run needs more bitrate for the baseline's quality", exitRegression))
	threshold := fs.Float64("threshold", analysis.DefaultRegressionThreshold,
		"BD-rate percent a run may drift by before --exit-on-regression fires")
	quiet := fs.Bool("quiet", false, "apply the gate without writing the comparison")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(files) != 2 {
		return fmt.Errorf("usage: ladder-bench compare [flags] BASELINE.json CURRENT.json")
	}
	basePath, curPath := files[0], files[1]

	base, err := loadReport(basePath)
	if err != nil {
		return err
	}
	cur, err := loadReport(curPath)
	if err != nil {
		return err
	}

	cmp := output.Compare(basePath, base, curPath, cur, *threshold)
	cmp.Version = version

	// A pipeline usually renders the comparison once for a human and then runs it
	// again purely for the exit code; --quiet is that second call, and it keeps
	// the log from carrying the same tables twice.
	if !*quiet {
		w := os.Stdout
		if *outPath != "" {
			f, err := os.Create(*outPath)
			if err != nil {
				return err
			}
			defer f.Close()
			w = f
		}
		if err := output.RenderComparison(w, cmp, *format); err != nil {
			return err
		}
	}

	if !*gate {
		return nil
	}
	// A gate that cannot tell whether anything regressed must not pass. Two runs
	// of different experiments differ in ways that say nothing about better or
	// worse, and reporting "no regression" there would be a green build built on
	// a comparison that was never made.
	if !cmp.Comparable {
		return fmt.Errorf("cannot gate on these runs: they measured different experiments (the config fingerprints differ) — re-measure the baseline with the current config")
	}
	if cmp.Regressed() {
		// The reasons go to stderr as well as into the report: a pipeline that
		// redirected the report to a file would otherwise see a red build with no
		// stated cause. Not every regression is a matter of degree, so the
		// threshold is named only by the ones it applies to.
		fmt.Fprintf(os.Stderr, "ladder-bench: %d regression(s)\n", len(cmp.Regressions))
		for _, r := range cmp.Regressions {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", r.Encoder, r.Reason)
			if r.Threshold > 0 {
				fmt.Fprintf(os.Stderr, "    (threshold %.2f%%)\n", r.Threshold)
			}
		}
		os.Exit(exitRegression)
	}
	return nil
}

// parseInterspersed parses flags that may appear before, after or between the
// positional arguments, and returns the positionals.
//
// The standard library's flag package stops at the first non-flag argument, so
// `compare a.json b.json --exit-on-regression` would hand back three positionals
// and no flags — and the gate would silently never fire. Nobody types the files
// last, so the parser has to accept them first.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
}

// loadReport reads a JSON report written by `ladder-bench run --output json`.
func loadReport(path string) (output.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return output.Report{}, err
	}
	var r output.Report
	// Strict decoding: a field this build does not know is a report from another
	// version, and silently ignoring it would compare two things while believing
	// they were the same shape.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return output.Report{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(r.Analyses) == 0 {
		return output.Report{}, fmt.Errorf("%s has no analysis in it — is it a `run --output json` report?", path)
	}
	return r, nil
}
