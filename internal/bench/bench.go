// Package bench runs the grid: it cuts the reference clip once, encodes every
// (resolution, bitrate) point, measures each against the reference, and
// returns the raw measurements for analysis.
package bench

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
	"github.com/Allan-Nava/ladder-bench/internal/config"
	"github.com/Allan-Nava/ladder-bench/internal/ffmpeg"
)

// Job is one grid point to measure.
type Job struct {
	Encoder string   `json:"encoder"`
	Codec   string   `json:"codec"`
	Preset  string   `json:"preset,omitempty"`
	Extra   []string `json:"extra_args,omitempty"`
	Height  int      `json:"height"`
	Kbps    int      `json:"target_kbps"`
	Out     string   `json:"out"`
	Log     string   `json:"vmaf_log"`
}

// Result is one measured point.
type Result struct {
	Job
	Score      ffmpeg.Score  `json:"vmaf"`
	Bytes      int64         `json:"bytes"`
	ActualKbps float64       `json:"actual_kbps"`
	Encode     time.Duration `json:"encode_ns"`
	Measure    time.Duration `json:"measure_ns"`
	// Reused is true when the encode and its VMAF log were already on disk.
	Reused bool `json:"reused"`
}

// Point converts a result into the analysis input.
func (r Result) Point() analysis.Point {
	return analysis.Point{
		Encoder: r.Encoder,
		Height:  r.Height,
		Target:  r.Kbps,
		Kbps:    r.ActualKbps,
		VMAF:    r.Score.Mean,
		VMAFMin: r.Score.Min,
	}
}

// Reference is the clip every encode is made from and compared against.
type Reference struct {
	Path  string       `json:"path"`
	Media ffmpeg.Media `json:"media"`
	// Source is what the clip was cut out of.
	Source ffmpeg.Media `json:"source"`
	Reused bool         `json:"reused"`
}

// Prober reads a file's video properties. ffmpeg.Tools implements it; the
// tests substitute a stub so the whole run can be exercised without ffmpeg.
type Prober interface {
	Probe(ctx context.Context, path string) (ffmpeg.Media, error)
}

// Bench drives one run.
type Bench struct {
	Cfg    *config.Config
	FFmpeg string
	Prober Prober
	Exec   ffmpeg.Executor
	Ref    Reference
	// Force re-encodes points whose files are already on disk.
	Force bool
	// Progress, when set, is called once per finished point.
	Progress func(done, total int, r Result)

	mu sync.Mutex
}

// Grid expands the config into the jobs to run, in a stable order: encoder as
// configured, then resolution tallest first, then bitrate ascending.
func Grid(cfg *config.Config) []Job {
	var jobs []Job
	rungs := append([]config.Rung(nil), cfg.Rungs...)
	sort.SliceStable(rungs, func(i, j int) bool { return rungs[i].Height > rungs[j].Height })
	for _, enc := range cfg.Encoders {
		for _, rung := range rungs {
			rates := append([]int(nil), rung.Bitrates...)
			sort.Ints(rates)
			for _, kbps := range rates {
				base := filepath.Join(cfg.WorkDir, fmt.Sprintf("%s_%dp_%dk", slug(enc.Name), rung.Height, kbps))
				jobs = append(jobs, Job{
					Encoder: enc.Name,
					Codec:   enc.Codec,
					Preset:  enc.Preset,
					Extra:   enc.ExtraArgs,
					Height:  rung.Height,
					Kbps:    kbps,
					Out:     base + ".mp4",
					Log:     base + ".vmaf.json",
				})
			}
		}
	}
	return jobs
}

// ReferencePath is where the reference clip of this config lives.
//
// The name carries the cut it was made from, so changing `clip:` produces a
// different file instead of silently reusing the previous run's clip — the
// kind of stale input that makes a whole run's numbers wrong without anything
// looking broken. `plan` and `run` must agree on this, hence one function.
func ReferencePath(cfg *config.Config) string {
	return filepath.Join(cfg.WorkDir, fmt.Sprintf("reference_%s_%s.mkv",
		compactDuration(cfg.Clip.Start.D()), compactDuration(cfg.Clip.Duration.D())))
}

// PrepareReference probes the source, cuts the reference clip and probes it.
func (b *Bench) PrepareReference(ctx context.Context) error {
	src, err := b.Prober.Probe(ctx, b.Cfg.Input)
	if err != nil {
		return fmt.Errorf("probing %s: %w", b.Cfg.Input, err)
	}
	if err := os.MkdirAll(b.Cfg.WorkDir, 0o755); err != nil {
		return err
	}
	path := ReferencePath(b.Cfg)
	reused := false
	if _, err := os.Stat(path); err == nil && !b.Force {
		reused = true
	} else {
		args := ffmpeg.ReferenceArgs(b.Cfg.Input, path, b.Cfg.Clip.Start.D(), b.Cfg.Clip.Duration.D())
		if err := b.Exec.Run(ctx, b.FFmpeg, args); err != nil {
			return fmt.Errorf("cutting the reference clip: %w", err)
		}
	}
	ref, err := b.Prober.Probe(ctx, path)
	if err != nil {
		return fmt.Errorf("probing the reference clip: %w", err)
	}
	b.Ref = Reference{Path: path, Media: ref, Source: src, Reused: reused}
	return nil
}

// Run measures every job, honouring the configured concurrency.
func (b *Bench) Run(ctx context.Context, jobs []Job) ([]Result, error) {
	if b.Ref.Path == "" {
		return nil, fmt.Errorf("reference clip not prepared")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]Result, len(jobs))
	errs := make([]error, len(jobs))
	sem := make(chan struct{}, max(1, b.Cfg.Concurrency))
	var wg sync.WaitGroup
	done := 0
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job Job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			r, err := b.measure(ctx, job)
			if err != nil {
				errs[i] = fmt.Errorf("%s %dp @ %dk: %w", job.Encoder, job.Height, job.Kbps, err)
				cancel() // one broken point makes the curve a lie; stop early
				return
			}
			results[i] = r
			b.mu.Lock()
			done++
			if b.Progress != nil {
				b.Progress(done, len(jobs), r)
			}
			b.mu.Unlock()
		}(i, job)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (b *Bench) measure(ctx context.Context, job Job) (Result, error) {
	res := Result{Job: job}
	fresh := b.Force || !exists(job.Out) || !exists(job.Log)
	if fresh {
		gop := int(math.Round(b.Ref.Media.FrameRate * b.Cfg.Analysis.GOPSeconds))
		start := time.Now()
		encodeArgs := ffmpeg.EncodeArgs(ffmpeg.EncodeSpec{
			Reference: b.Ref.Path,
			Out:       job.Out,
			Height:    job.Height,
			Kbps:      job.Kbps,
			Codec:     job.Codec,
			Preset:    job.Preset,
			Extra:     job.Extra,
			GOP:       gop,
		})
		if err := b.Exec.Run(ctx, b.FFmpeg, encodeArgs); err != nil {
			return res, fmt.Errorf("encode: %w", err)
		}
		res.Encode = time.Since(start)

		start = time.Now()
		vmafArgs := ffmpeg.VMAFArgs(ffmpeg.VMAFSpec{
			Distorted: job.Out,
			Reference: b.Ref.Path,
			LogPath:   job.Log,
			Width:     b.Ref.Media.Width,
			Height:    b.Ref.Media.Height,
			Model:     b.Cfg.VMAF.Model,
			Threads:   b.Cfg.VMAF.Threads,
			Subsample: b.Cfg.VMAF.Subsample,
		})
		if err := b.Exec.Run(ctx, b.FFmpeg, vmafArgs); err != nil {
			return res, fmt.Errorf("vmaf: %w", err)
		}
		res.Measure = time.Since(start)
	} else {
		res.Reused = true
	}

	log, err := os.ReadFile(job.Log)
	if err != nil {
		return res, fmt.Errorf("reading the VMAF log: %w", err)
	}
	score, err := ffmpeg.ParseVMAFLog(log)
	if err != nil {
		return res, err
	}
	res.Score = score

	info, err := os.Stat(job.Out)
	if err != nil {
		return res, fmt.Errorf("stat encode: %w", err)
	}
	res.Bytes = info.Size()
	// Video-only files over a known duration: size is the honest bitrate,
	// including the container overhead the CDN also has to ship.
	if d := b.Ref.Media.Duration; d > 0 {
		res.ActualKbps = float64(info.Size()) * 8 / d / 1000
	}
	return res, nil
}

// Cleanup removes the encodes, keeping the reference clip and the VMAF logs.
func (b *Bench) Cleanup(jobs []Job) error {
	var firstErr error
	for _, j := range jobs {
		if err := os.Remove(j.Out); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func slug(s string) string {
	out := unsafeName.ReplaceAllString(s, "-")
	return strings.Trim(out, "-")
}

// compactDuration renders a duration for a file name: no colons, no dots, and
// stable across runs ("0s", "1m30s" -> "0s", "1m30s").
func compactDuration(d time.Duration) string {
	if d <= 0 {
		return "full"
	}
	return strings.ReplaceAll(d.String(), ".", "_")
}
