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
	// Clip is which cut of the source this point was measured on, named the way
	// its reference file is (`0s_30s`). Empty for a single-clip run, so those
	// runs keep the report they always had.
	Clip string `json:"clip,omitempty"`
	// Cut is the clip itself, so measure can find the right reference.
	Cut config.Clip `json:"-"`
	Out string      `json:"out"`
	Log string      `json:"vmaf_log"`
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
		// Carried for the report only: the analysis reasons about bitrate
		// against VMAF, and these are the columns a reader checks it against.
		VMAFHarmonic: r.Score.Harmonic,
		P1:           r.Score.P1,
		P5:           r.Score.P5,
		PSNR:         r.Score.PSNR,
		SSIM:         r.Score.SSIM,
	}
}

// Reference is a clip encodes are made from and compared against. A run has one
// per cut of the source.
type Reference struct {
	Path string `json:"path"`
	// Clip names the cut, matching Job.Clip. Empty for a single-clip run.
	Clip  string       `json:"clip,omitempty"`
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
	// Refs holds one prepared reference per cut, in config order.
	Refs []Reference
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
	multi := cfg.MultiClip()
	for _, enc := range cfg.Encoders {
		for _, cut := range cfg.Cuts() {
			// The clip goes in the file name only when there is more than one.
			// A single-clip run therefore keeps the names it has always used and
			// its work dir stays valid; adding a second clip renames the first
			// one's files, which re-measures them — correctly, because a run over
			// three cuts is not the run over one that came before it.
			clip := ""
			part := ""
			if multi {
				clip = ClipName(cut)
				part = clip + "_"
			}
			for _, rung := range rungs {
				rates := append([]int(nil), rung.Bitrates...)
				sort.Ints(rates)
				for _, kbps := range rates {
					base := filepath.Join(cfg.WorkDir, fmt.Sprintf("%s_%s%dp_%dk", slug(enc.Name), part, rung.Height, kbps))
					jobs = append(jobs, Job{
						Encoder: enc.Name,
						Codec:   enc.Codec,
						Preset:  enc.Preset,
						Extra:   enc.ExtraArgs,
						Height:  rung.Height,
						Kbps:    kbps,
						Clip:    clip,
						Cut:     cut,
						Out:     base + ".mp4",
						Log:     base + ".vmaf.json",
					})
				}
			}
		}
	}
	return jobs
}

// ClipName identifies a cut the same way its reference file does, so a name in
// the report and a file on disk can always be matched up by eye.
func ClipName(cut config.Clip) string {
	return fmt.Sprintf("%s_%s", compactDuration(cut.Start.D()), compactDuration(cut.Duration.D()))
}

// ReferencePath is where the reference clip of this config lives.
//
// The name carries the cut it was made from, so changing `clip:` produces a
// different file instead of silently reusing the previous run's clip — the
// kind of stale input that makes a whole run's numbers wrong without anything
// looking broken. `plan` and `run` must agree on this, hence one function.
func ReferencePath(cfg *config.Config, cut config.Clip) string {
	return filepath.Join(cfg.WorkDir, "reference_"+ClipName(cut)+".mkv")
}

// PrepareReference probes the source and cuts every reference clip the config
// asks for, probing each one.
//
// The clips are cut before anything is encoded, and each is probed rather than
// assumed: the geometry decides the VMAF scaling and the duration turns bytes
// into a bitrate, so a clip that came out different from what was asked would
// quietly change every number measured against it.
func (b *Bench) PrepareReference(ctx context.Context) error {
	src, err := b.Prober.Probe(ctx, b.Cfg.Input)
	if err != nil {
		return fmt.Errorf("probing %s: %w", b.Cfg.Input, err)
	}
	if err := os.MkdirAll(b.Cfg.WorkDir, 0o755); err != nil {
		return err
	}
	multi := b.Cfg.MultiClip()
	b.Refs = nil
	for _, cut := range b.Cfg.Cuts() {
		path := ReferencePath(b.Cfg, cut)
		reused := false
		if _, err := os.Stat(path); err == nil && !b.Force {
			reused = true
		} else {
			args := ffmpeg.ReferenceArgs(b.Cfg.Input, path, cut.Start.D(), cut.Duration.D())
			if err := b.Exec.Run(ctx, b.FFmpeg, args); err != nil {
				return fmt.Errorf("cutting the reference clip %s: %w", ClipName(cut), err)
			}
		}
		ref, err := b.Prober.Probe(ctx, path)
		if err != nil {
			return fmt.Errorf("probing the reference clip %s: %w", ClipName(cut), err)
		}
		name := ""
		if multi {
			name = ClipName(cut)
		}
		b.Refs = append(b.Refs, Reference{Path: path, Clip: name, Media: ref, Source: src, Reused: reused})
	}
	return nil
}

// reference finds the prepared clip a job was measured against.
func (b *Bench) reference(job Job) (Reference, error) {
	want := ReferencePath(b.Cfg, job.Cut)
	for _, r := range b.Refs {
		if r.Path == want {
			return r, nil
		}
	}
	return Reference{}, fmt.Errorf("reference clip %s not prepared", ClipName(job.Cut))
}

// Run measures every job, honouring the configured concurrency.
func (b *Bench) Run(ctx context.Context, jobs []Job) ([]Result, error) {
	if len(b.Refs) == 0 {
		return nil, fmt.Errorf("reference clip not prepared")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]Result, len(jobs))
	sem := make(chan struct{}, max(1, b.Cfg.Concurrency))
	var wg sync.WaitGroup
	// failure is the error that stopped the run, kept apart from the fallout it
	// causes. Cancelling kills the encodes already in flight and unblocks the
	// ones queued behind the semaphore, so most of the errors a stopped run
	// produces are collateral — and reporting one of those instead would hide
	// the only message that says what actually broke.
	var failure error
	done := 0
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job Job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			r, err := b.measure(ctx, job)
			if err != nil {
				b.mu.Lock()
				if failure == nil && ctx.Err() == nil {
					failure = fmt.Errorf("%s %dp @ %dk: %w", job.Encoder, job.Height, job.Kbps, err)
				}
				b.mu.Unlock()
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
	if failure != nil {
		return nil, failure
	}
	// No point failed, so a cancelled context can only have come from outside:
	// someone pressed Ctrl-C.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// logCovers reports whether an existing VMAF log already holds every metric
// the config asks for. An unreadable or unparseable log answers false: a point
// whose measurement cannot be read has not been measured.
func (b *Bench) logCovers(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	score, err := ffmpeg.ParseVMAFLog(data)
	if err != nil {
		return false
	}
	return score.Covers(b.Cfg.VMAF.Metrics)
}

func (b *Bench) measure(ctx context.Context, job Job) (Result, error) {
	res := Result{Job: job}
	ref, err := b.reference(job)
	if err != nil {
		return res, err
	}
	fresh := b.Force || !exists(job.Out) || !exists(job.Log)
	// A log written before `vmaf.metrics` asked for PSNR or SSIM does not
	// contain them, and no amount of re-reading will add them. Reusing it would
	// print an empty column, which reads as a measurement that came back blank
	// rather than one that was never taken — so the point is measured again.
	// Guarded on the config so the common case still touches the log once.
	if !fresh && len(b.Cfg.VMAF.Metrics) > 0 && !b.logCovers(job.Log) {
		fresh = true
	}
	if fresh {
		gop := int(math.Round(ref.Media.FrameRate * b.Cfg.Analysis.GOPSeconds))
		start := time.Now()
		encodeArgs := ffmpeg.EncodeArgs(ffmpeg.EncodeSpec{
			Reference: ref.Path,
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
			Reference: ref.Path,
			LogPath:   job.Log,
			Width:     ref.Media.Width,
			Height:    ref.Media.Height,
			Model:     b.Cfg.VMAF.Model,
			Threads:   b.Cfg.VMAF.Threads,
			Subsample: b.Cfg.VMAF.Subsample,
			Metrics:   b.Cfg.VMAF.Metrics,
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
	if d := ref.Media.Duration; d > 0 {
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
