package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"

	"github.com/Allan-Nava/ladder-bench/internal/bench"
	"github.com/Allan-Nava/ladder-bench/internal/config"
	"github.com/Allan-Nava/ladder-bench/internal/ffmpeg"
)

// runPlan prints what a run would do, without touching a single frame.
//
// It exists because the commands are the method: a benchmark whose encoder
// settings you cannot read is a benchmark you cannot argue with. `plan` is
// also how you check a config change without paying for the encodes.
func runPlan(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	configPath := fs.String("config", "ladder-bench.yml", "config file")
	input := fs.String("input", "", "override the source file from the config")
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

	w := os.Stdout
	jobs := bench.Grid(cfg)
	refPath := bench.ReferencePath(cfg)

	// The reference geometry decides the VMAF scaling, so probe when we can
	// and say so loudly when we cannot: a plan printed with assumed geometry
	// is a plan, not a measurement.
	var refW, refH int
	var fps float64
	tools, toolErr := ffmpeg.Find(cfg.FFmpeg, cfg.FFprobe)
	if toolErr == nil {
		if media, err := tools.Probe(ctx, cfg.Input); err == nil {
			refW, refH, fps = media.Width, media.Height, media.FrameRate
			fmt.Fprintf(w, "# source %s — %dx%d @ %.2f fps, %s, %.1fs\n", cfg.Input, media.Width, media.Height, media.FrameRate, media.Codec, media.Duration)
		} else {
			fmt.Fprintf(w, "# source %s — not probed (%v)\n", cfg.Input, err)
		}
	} else {
		fmt.Fprintf(w, "# ffmpeg not available (%v)\n", toolErr)
	}
	if refH == 0 {
		refH = tallest(cfg)
		refW = refH * 16 / 9
		fps = 25
		fmt.Fprintf(w, "# ASSUMED reference geometry %dx%d @ %.0f fps — the real run probes the clip\n", refW, refH, fps)
	}

	fmt.Fprintf(w, "# %d points, %d encoder(s), concurrency %d\n", len(jobs), len(cfg.Encoders), cfg.Concurrency)
	fmt.Fprintf(w, "# encodes on disk: roughly %s (plus the lossless reference clip)\n", humanBytes(estimateBytes(cfg, jobs)))

	ffmpegBin := "ffmpeg"
	if toolErr == nil {
		ffmpegBin = tools.FFmpeg
	}
	fmt.Fprintln(w, "\n# reference clip")
	fmt.Fprintln(w, ffmpeg.Quote(ffmpegBin, ffmpeg.ReferenceArgs(cfg.Input, refPath, cfg.Clip.Start.D(), cfg.Clip.Duration.D())))

	gop := int(math.Round(fps * cfg.Analysis.GOPSeconds))
	for _, job := range jobs {
		fmt.Fprintf(w, "\n# %s — %dp @ %dk\n", job.Encoder, job.Height, job.Kbps)
		fmt.Fprintln(w, ffmpeg.Quote(ffmpegBin, ffmpeg.EncodeArgs(ffmpeg.EncodeSpec{
			Reference: refPath,
			Out:       job.Out,
			Height:    job.Height,
			Kbps:      job.Kbps,
			Codec:     job.Codec,
			Preset:    job.Preset,
			Extra:     job.Extra,
			GOP:       gop,
		})))
		fmt.Fprintln(w, ffmpeg.Quote(ffmpegBin, ffmpeg.VMAFArgs(ffmpeg.VMAFSpec{
			Distorted: job.Out,
			Reference: refPath,
			LogPath:   job.Log,
			Width:     refW,
			Height:    refH,
			Model:     cfg.VMAF.Model,
			Threads:   cfg.VMAF.Threads,
			Subsample: cfg.VMAF.Subsample,
		})))
	}
	return nil
}

func tallest(cfg *config.Config) int {
	h := 0
	for _, r := range cfg.Rungs {
		if r.Height > h {
			h = r.Height
		}
	}
	return h
}

// estimateBytes sizes the encodes from their target bitrates: capped rate
// control keeps every point close to what it asked for, which is exactly what
// makes the estimate usable.
func estimateBytes(cfg *config.Config, jobs []bench.Job) int64 {
	seconds := cfg.Clip.Duration.D().Seconds()
	if seconds <= 0 {
		return 0
	}
	var total float64
	for _, j := range jobs {
		total += float64(j.Kbps) * 1000 * seconds / 8
	}
	return int64(total)
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "unknown (the clip has no duration set)"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
