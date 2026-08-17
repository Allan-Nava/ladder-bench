package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
	"github.com/Allan-Nava/ladder-bench/internal/bench"
	"github.com/Allan-Nava/ladder-bench/internal/config"
	"github.com/Allan-Nava/ladder-bench/internal/ffmpeg"
	"github.com/Allan-Nava/ladder-bench/internal/output"
)

func runRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "ladder-bench.yml", "config file")
	input := fs.String("input", "", "override the source file from the config")
	format := fs.String("output", "text", "output format: "+output.Formats())
	outPath := fs.String("out", "", "write the report to this file instead of stdout")
	concurrency := fs.Int("concurrency", 0, "override how many encodes run at once")
	force := fs.Bool("force", false, "re-encode points whose files are already on disk")
	verbose := fs.Bool("verbose", false, "echo every ffmpeg command to stderr")
	quiet := fs.Bool("quiet", false, "no progress on stderr")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *input != "" {
		cfg.Input = *input
	}
	if *concurrency > 0 {
		cfg.Concurrency = *concurrency
	}

	tools, err := ffmpeg.Find(cfg.FFmpeg, cfg.FFprobe)
	if err != nil {
		return err
	}
	// Check libvmaf before encoding anything: it is a build-time option, and
	// discovering it is missing after an hour of encodes is the worst way to
	// find out.
	ok, err := tools.HasFilter(ctx, "libvmaf")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("this ffmpeg was built without libvmaf (%s) — run 'ladder-bench doctor' for details", tools.FFmpeg)
	}

	b := &bench.Bench{
		Cfg:    cfg,
		FFmpeg: tools.FFmpeg,
		Prober: tools,
		Exec:   ffmpeg.ExecRunner{Verbose: *verbose},
		Force:  *force,
	}
	jobs := bench.Grid(cfg)
	if !*quiet {
		fmt.Fprintf(os.Stderr, "ladder-bench: %d points, %d encoder(s), concurrency %d\n",
			len(jobs), len(cfg.Encoders), cfg.Concurrency)
		b.Progress = func(done, total int, r bench.Result) {
			note := fmt.Sprintf("encode %s", r.Encode.Round(100*time.Millisecond))
			if r.Reused {
				note = "reused"
			}
			fmt.Fprintf(os.Stderr, "[%2d/%2d] %-14s %5dp @ %6dk  VMAF %6.2f  (%s)\n",
				done, total, r.Encoder, r.Height, r.Kbps, r.Score.Mean, note)
		}
	}
	if err := b.PrepareReference(ctx); err != nil {
		return err
	}
	results, err := b.Run(ctx, jobs)
	if err != nil {
		return err
	}
	if !cfg.KeepEncodes {
		// The VMAF logs stay: they are the measurement. The encodes are just
		// the bytes it was measured on, and they are the bulk of the disk.
		if err := b.Cleanup(jobs); err != nil {
			fmt.Fprintln(os.Stderr, "ladder-bench: cleaning up encodes:", err)
		}
	}

	report := output.Report{
		Tool:      "ladder-bench",
		Version:   version,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Input:     cfg.Input,
		Reference: b.Ref,
		Options:   options(cfg),
		Results:   results,
		Analyses:  analyze(cfg, results),
	}

	w := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	return output.Render(w, report, *format)
}

// options carries the analysis thresholds from the config.
func options(cfg *config.Config) analysis.Options {
	opt := analysis.Options{
		KneeGain:   cfg.Analysis.KneeGain,
		TargetVMAF: cfg.Analysis.TargetVMAF,
		LadderStep: cfg.Analysis.LadderStep,
	}
	for _, r := range cfg.Current {
		opt.Current = append(opt.Current, analysis.Rendition{Height: r.Height, Kbps: r.Bitrate})
	}
	return opt
}

// analyze runs the analysis once per encoder: a hull mixing two codecs would
// recommend a ladder no single encoder can produce.
func analyze(cfg *config.Config, results []bench.Result) []analysis.Result {
	byEncoder := map[string][]analysis.Point{}
	for _, r := range results {
		byEncoder[r.Encoder] = append(byEncoder[r.Encoder], r.Point())
	}
	names := make([]string, 0, len(byEncoder))
	for name := range byEncoder {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]analysis.Result, 0, len(names))
	for _, name := range names {
		out = append(out, analysis.Analyze(name, byEncoder[name], options(cfg)))
	}
	return out
}
