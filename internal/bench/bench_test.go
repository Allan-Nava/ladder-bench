package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Allan-Nava/ladder-bench/internal/config"
	"github.com/Allan-Nava/ladder-bench/internal/ffmpeg"
)

// fakeExec stands in for ffmpeg: it writes the files a real run would have
// produced, so the whole loop — encode, measure, read back, resume — can be
// tested on a machine without ffmpeg, which is every CI runner.
type fakeExec struct {
	mu       sync.Mutex
	calls    [][]string
	sizeFor  func(out string) int64
	scoreFor func(distorted string) float64
	failOn   func(args []string) error
}

func (f *fakeExec) Run(_ context.Context, _ string, args []string) error {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.mu.Unlock()
	if f.failOn != nil {
		if err := f.failOn(args); err != nil {
			return err
		}
	}
	if i := index(args, "-lavfi"); i >= 0 {
		distorted := args[index(args, "-i")+1]
		score := 90.0
		if f.scoreFor != nil {
			score = f.scoreFor(distorted)
		}
		log := logPath(args[i+1])
		// The fake log carries the extra metrics only when the filter asked for
		// them, exactly like libvmaf: that is what makes a log written by an
		// earlier run stale rather than merely old.
		pooled := fmt.Sprintf(`"vmaf":{"min":%.2f,"max":100,"mean":%.2f,"harmonic_mean":%.2f}`,
			score-5, score, score-1)
		if strings.Contains(args[i+1], "name=psnr") {
			pooled += `,"psnr_y":{"min":30,"max":45,"mean":38.25,"harmonic_mean":38}`
		}
		if strings.Contains(args[i+1], "name=float_ssim") {
			pooled += `,"float_ssim":{"min":0.9,"max":0.99,"mean":0.9712,"harmonic_mean":0.97}`
		}
		body := fmt.Sprintf(`{"frames":[{"frameNum":0}],"pooled_metrics":{%s}}`, pooled)
		return os.WriteFile(log, []byte(body), 0o644)
	}
	out := args[len(args)-1]
	size := int64(1024)
	if f.sizeFor != nil {
		size = f.sizeFor(out)
	}
	return os.WriteFile(out, make([]byte, size), 0o644)
}

func (f *fakeExec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func index(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// logPath pulls the log file back out of the filtergraph, unescaping it the
// way ffmpeg would.
func logPath(filter string) string {
	_, rest, _ := strings.Cut(filter, "log_path=")
	var out []rune
	runes := []rune(rest)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			out = append(out, runes[i])
			continue
		}
		if runes[i] == ':' {
			break
		}
		out = append(out, runes[i])
	}
	return string(out)
}

type fakeProber struct{ media ffmpeg.Media }

func (f fakeProber) Probe(context.Context, string) (ffmpeg.Media, error) { return f.media, nil }

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(`
input: source.mp4
rungs:
  - height: 720
    bitrates: [2000, 1000]
  - height: 1080
    bitrates: [4000]
clip:
  duration: "10s"
`))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.WorkDir = t.TempDir()
	return cfg
}

func newBench(t *testing.T, cfg *config.Config, exec ffmpeg.Executor) *Bench {
	t.Helper()
	return &Bench{
		Cfg:    cfg,
		FFmpeg: "ffmpeg",
		Prober: fakeProber{media: ffmpeg.Media{Width: 1920, Height: 1080, FrameRate: 25, Duration: 10, Codec: "h264"}},
		Exec:   exec,
	}
}

func TestGridIsOrderedAndNamed(t *testing.T) {
	cfg := testConfig(t)
	jobs := Grid(cfg)
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}
	// Tallest resolution first, then bitrate ascending — the order the report
	// reads in, and stable across runs so results can be diffed.
	want := []struct {
		height, kbps int
	}{{1080, 4000}, {720, 1000}, {720, 2000}}
	for i, w := range want {
		if jobs[i].Height != w.height || jobs[i].Kbps != w.kbps {
			t.Errorf("job %d = %dp@%dk, want %dp@%dk", i, jobs[i].Height, jobs[i].Kbps, w.height, w.kbps)
		}
	}
	if got := filepath.Base(jobs[0].Out); got != "x264-slow_1080p_4000k.mp4" {
		t.Errorf("output name = %q", got)
	}
	if got := filepath.Base(jobs[0].Log); got != "x264-slow_1080p_4000k.vmaf.json" {
		t.Errorf("log name = %q", got)
	}
}

// The file name carries the cut, so editing `clip:` cannot silently reuse the
// previous run's reference.
func TestReferencePathCarriesTheCut(t *testing.T) {
	cfg := testConfig(t)
	first := ReferencePath(cfg)
	cfg.Clip.Start = config.Duration(60e9)
	if second := ReferencePath(cfg); second == first {
		t.Errorf("changing the cut must change the reference path, both were %q", first)
	}
}

func TestRunMeasuresEveryPoint(t *testing.T) {
	cfg := testConfig(t)
	exec := &fakeExec{
		scoreFor: func(distorted string) float64 {
			if strings.Contains(distorted, "1080p") {
				return 95
			}
			return 88
		},
		sizeFor: func(string) int64 { return 12500 }, // 10s of 10 kbps
	}
	b := newBench(t, cfg, exec)
	if err := b.PrepareReference(context.Background()); err != nil {
		t.Fatalf("PrepareReference: %v", err)
	}
	if b.Ref.Media.Width != 1920 {
		t.Errorf("reference geometry not probed: %+v", b.Ref)
	}
	jobs := Grid(cfg)
	results, err := b.Run(context.Background(), jobs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != len(jobs) {
		t.Fatalf("got %d results, want %d", len(results), len(jobs))
	}
	for _, r := range results {
		if r.Score.Mean == 0 {
			t.Errorf("%dp@%dk has no score", r.Height, r.Kbps)
		}
		if r.Bytes != 12500 {
			t.Errorf("%dp@%dk size = %d", r.Height, r.Kbps, r.Bytes)
		}
		// 12500 bytes over 10 seconds is 10 kbps.
		if r.ActualKbps < 9.9 || r.ActualKbps > 10.1 {
			t.Errorf("%dp@%dk actual = %.2f kbps, want ~10", r.Height, r.Kbps, r.ActualKbps)
		}
		if r.Reused {
			t.Errorf("%dp@%dk reported as reused on a fresh run", r.Height, r.Kbps)
		}
	}
	if results[0].Score.Mean != 95 {
		t.Errorf("scores are not matched to their encode: %+v", results[0])
	}
	// One reference cut plus an encode and a measurement per point.
	if want := 1 + 2*len(jobs); exec.count() != want {
		t.Errorf("ran %d commands, want %d", exec.count(), want)
	}
}

func TestRunReusesFinishedPoints(t *testing.T) {
	cfg := testConfig(t)
	exec := &fakeExec{}
	b := newBench(t, cfg, exec)
	ctx := context.Background()
	if err := b.PrepareReference(ctx); err != nil {
		t.Fatalf("PrepareReference: %v", err)
	}
	jobs := Grid(cfg)
	if _, err := b.Run(ctx, jobs); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	before := exec.count()

	// A second run over the same work dir is what an interrupted grid looks
	// like when it resumes: nothing left to encode.
	b2 := newBench(t, cfg, exec)
	if err := b2.PrepareReference(ctx); err != nil {
		t.Fatalf("second PrepareReference: %v", err)
	}
	if !b2.Ref.Reused {
		t.Error("the reference clip should have been reused")
	}
	results, err := b2.Run(ctx, jobs)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if exec.count() != before {
		t.Errorf("the resumed run executed %d extra commands", exec.count()-before)
	}
	for _, r := range results {
		if !r.Reused || r.Score.Mean == 0 {
			t.Errorf("%dp@%dk was not reused with its score: %+v", r.Height, r.Kbps, r)
		}
	}

	// --force ignores what is on disk.
	b3 := newBench(t, cfg, exec)
	b3.Force = true
	if err := b3.PrepareReference(ctx); err != nil {
		t.Fatalf("forced PrepareReference: %v", err)
	}
	if _, err := b3.Run(ctx, jobs); err != nil {
		t.Fatalf("forced Run: %v", err)
	}
	if exec.count() <= before {
		t.Error("--force must re-encode everything")
	}
}

func TestRunFailsOnABrokenPoint(t *testing.T) {
	cfg := testConfig(t)
	exec := &fakeExec{failOn: func(args []string) error {
		if index(args, "-lavfi") < 0 && strings.Contains(args[len(args)-1], "720p_1000k") {
			return fmt.Errorf("encoder exploded")
		}
		return nil
	}}
	b := newBench(t, cfg, exec)
	ctx := context.Background()
	if err := b.PrepareReference(ctx); err != nil {
		t.Fatalf("PrepareReference: %v", err)
	}
	_, err := b.Run(ctx, Grid(cfg))
	if err == nil {
		t.Fatal("Run succeeded with a broken point; a hole in the curve is a wrong answer, not a smaller one")
	}
	if !strings.Contains(err.Error(), "720p @ 1000k") {
		t.Errorf("the error should name the point, got: %v", err)
	}
}

// Stopping the run kills the encodes in flight and unblocks everything queued
// behind the semaphore, so most of the errors a failed run produces are its own
// fallout. The one the user gets has to be the one that says what broke — a
// report of "context canceled" leaves nothing to debug.
func TestRunReportsTheFailureNotItsOwnCancellation(t *testing.T) {
	cfg := testConfig(t)
	exec := &fakeExec{failOn: func(args []string) error {
		if index(args, "-lavfi") < 0 && strings.Contains(args[len(args)-1], "1080p_4000k") {
			return fmt.Errorf("Max Bitrate only supported with CRF mode")
		}
		return nil
	}}
	b := newBench(t, cfg, exec)
	ctx := context.Background()
	if err := b.PrepareReference(ctx); err != nil {
		t.Fatalf("PrepareReference: %v", err)
	}
	_, err := b.Run(ctx, Grid(cfg))
	if err == nil {
		t.Fatal("Run succeeded with a broken point")
	}
	if !strings.Contains(err.Error(), "Max Bitrate only supported") {
		t.Errorf("the reason ffmpeg gave was lost: %v", err)
	}
	if strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("the run reported its own cancellation instead of the failure: %v", err)
	}
}

// Nothing failed, so a cancelled context can only have come from outside.
func TestRunPassesOnAnInterruption(t *testing.T) {
	cfg := testConfig(t)
	b := newBench(t, cfg, &fakeExec{})
	if err := b.PrepareReference(context.Background()); err != nil {
		t.Fatalf("PrepareReference: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.Run(ctx, Grid(cfg))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("an interrupted run should report the interruption, got %v", err)
	}
}

// Turning on `vmaf.metrics` invalidates the logs already on disk: they cannot be
// made to contain a metric nobody asked libvmaf for. Reusing one would print an
// empty column, which reads as a measurement that came back blank.
func TestRunReMeasuresLogsMissingTheRequestedMetrics(t *testing.T) {
	cfg := testConfig(t)
	exec := &fakeExec{}
	b := newBench(t, cfg, exec)
	ctx := context.Background()
	if err := b.PrepareReference(ctx); err != nil {
		t.Fatalf("PrepareReference: %v", err)
	}
	jobs := Grid(cfg)
	first, err := b.Run(ctx, jobs)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	for _, r := range first {
		if r.Score.PSNR != nil {
			t.Fatalf("nothing asked for PSNR, yet %s has one", r.Out)
		}
	}
	calls := exec.count()

	// Same work dir, same points — but now the config wants PSNR.
	cfg.VMAF.Metrics = []string{"psnr"}
	second, err := b.Run(ctx, jobs)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, r := range second {
		if r.Reused {
			t.Errorf("%s was reused although its log has no PSNR", r.Out)
		}
		if r.Score.PSNR == nil || *r.Score.PSNR != 38.25 {
			t.Errorf("%s: PSNR = %v, want 38.25 from the re-measurement", r.Out, r.Score.PSNR)
		}
	}
	if exec.count() <= calls {
		t.Error("the second run measured nothing at all")
	}

	// Now the logs cover the request, so a third run is back to reusing them.
	third, err := b.Run(ctx, jobs)
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	for _, r := range third {
		if !r.Reused {
			t.Errorf("%s was re-measured although its log already has PSNR", r.Out)
		}
		if r.Score.PSNR == nil {
			t.Errorf("%s lost its PSNR on reuse", r.Out)
		}
	}
}

func TestRunRefusesWithoutAReference(t *testing.T) {
	cfg := testConfig(t)
	b := newBench(t, cfg, &fakeExec{})
	if _, err := b.Run(context.Background(), Grid(cfg)); err == nil {
		t.Fatal("Run without PrepareReference should fail")
	}
}

func TestCleanupKeepsTheLogs(t *testing.T) {
	cfg := testConfig(t)
	exec := &fakeExec{}
	b := newBench(t, cfg, exec)
	ctx := context.Background()
	if err := b.PrepareReference(ctx); err != nil {
		t.Fatalf("PrepareReference: %v", err)
	}
	jobs := Grid(cfg)
	if _, err := b.Run(ctx, jobs); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := b.Cleanup(jobs); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	for _, j := range jobs {
		if _, err := os.Stat(j.Out); err == nil {
			t.Errorf("%s survived the cleanup", j.Out)
		}
		if _, err := os.Stat(j.Log); err != nil {
			t.Errorf("%s is the measurement and must be kept: %v", j.Log, err)
		}
	}
	if err := b.Cleanup(jobs); err != nil {
		t.Errorf("cleaning up twice must not fail: %v", err)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"x264-slow":        "x264-slow",
		"svt av1 (tune 0)": "svt-av1-tune-0",
		"../etc/passwd":    "..-etc-passwd",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
