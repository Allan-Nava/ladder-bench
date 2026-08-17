package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Allan-Nava/ladder-bench/internal/config"
	"github.com/Allan-Nava/ladder-bench/internal/ffmpeg"
)

// runDoctor answers "will this run work?" before the first encode.
//
// Everything it checks is something that otherwise fails late: an ffmpeg built
// without libvmaf, a codec this build does not have, an unreadable source, a
// work directory nobody can write to.
func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := fs.String("config", "ladder-bench.yml", "config file")
	input := fs.String("input", "", "override the source file from the config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	failed := false
	report := func(ok bool, format string, a ...any) {
		mark := "ok  "
		if !ok {
			mark, failed = "FAIL", true
		}
		fmt.Printf("[%s] %s\n", mark, fmt.Sprintf(format, a...))
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		report(false, "config %s: %v", *configPath, err)
		return fmt.Errorf("config is unusable, nothing else can be checked")
	}
	if *input != "" {
		cfg.Input = *input
	}
	report(true, "config %s: %d points, %d encoder(s)", *configPath, cfg.Points(), len(cfg.Encoders))

	tools, err := ffmpeg.Find(cfg.FFmpeg, cfg.FFprobe)
	if err != nil {
		report(false, "%v", err)
		return fmt.Errorf("ffmpeg is required")
	}
	report(true, "ffmpeg %s", tools.FFmpeg)
	report(true, "ffprobe %s", tools.FFprobe)

	hasVMAF, err := tools.HasFilter(ctx, "libvmaf")
	switch {
	case err != nil:
		report(false, "listing filters: %v", err)
	case !hasVMAF:
		report(false, "libvmaf filter missing — this ffmpeg was built without --enable-libvmaf; "+
			"install one that has it (brew install ffmpeg, or a static build) or the run cannot measure anything")
	default:
		report(true, "libvmaf filter available")
	}

	for _, enc := range cfg.Encoders {
		ok, err := tools.HasEncoder(ctx, enc.Codec)
		switch {
		case err != nil:
			report(false, "listing encoders: %v", err)
		case !ok:
			report(false, "encoder %q (%s): not in this ffmpeg build", enc.Name, enc.Codec)
		default:
			report(true, "encoder %q: %s available", enc.Name, enc.Codec)
		}
	}

	media, err := tools.Probe(ctx, cfg.Input)
	if err != nil {
		report(false, "input %s: %v", cfg.Input, err)
	} else {
		report(true, "input %s: %dx%d @ %.2f fps, %s, %.1fs",
			cfg.Input, media.Width, media.Height, media.FrameRate, media.Codec, media.Duration)
		clipEnd := cfg.Clip.Start.D().Seconds() + cfg.Clip.Duration.D().Seconds()
		if media.Duration > 0 && clipEnd > media.Duration {
			report(false, "clip %s+%s ends past the source (%.1fs) — the reference would be short or empty",
				cfg.Clip.Start, cfg.Clip.Duration, media.Duration)
		}
		for _, r := range cfg.Rungs {
			if media.Height > 0 && r.Height > media.Height {
				report(false, "rung %dp is taller than the source (%dp) — upscaling measures the scaler, not the encoder",
					r.Height, media.Height)
			}
		}
	}

	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		report(false, "work_dir %s: %v", cfg.WorkDir, err)
	} else {
		probe := filepath.Join(cfg.WorkDir, ".write-test")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
			report(false, "work_dir %s is not writable: %v", cfg.WorkDir, err)
		} else {
			_ = os.Remove(probe)
			report(true, "work_dir %s is writable", cfg.WorkDir)
		}
	}

	if failed {
		return fmt.Errorf("some checks failed")
	}
	fmt.Println("\nAll good — 'ladder-bench run' has everything it needs.")
	return nil
}
