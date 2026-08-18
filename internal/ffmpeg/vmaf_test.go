package ffmpeg

import (
	"strings"
	"testing"
)

const vmafJSON = `{
  "version": "3.0.0",
  "frames": [
    {"frameNum": 0, "metrics": {"vmaf": 91.2}},
    {"frameNum": 1, "metrics": {"vmaf": 88.4}}
  ],
  "pooled_metrics": {
    "vmaf": {"min": 71.35, "max": 97.02, "mean": 90.88, "harmonic_mean": 89.91}
  }
}`

func TestParseVMAFLog(t *testing.T) {
	s, err := ParseVMAFLog([]byte(vmafJSON))
	if err != nil {
		t.Fatalf("ParseVMAFLog: %v", err)
	}
	if s.Mean != 90.88 || s.Min != 71.35 || s.Harmonic != 89.91 {
		t.Errorf("score = %+v", s)
	}
	if s.Frames != 2 {
		t.Errorf("frames = %d, want 2", s.Frames)
	}
}

// ffmpeg exits 0 and writes a log with no pooled metrics when the two inputs
// never overlapped. Reading that as a zero score would put a fake point on the
// curve instead of an error.
func TestParseVMAFLogRejectsEmptyComparison(t *testing.T) {
	if _, err := ParseVMAFLog([]byte(`{"version":"3.0.0","frames":[],"pooled_metrics":{}}`)); err == nil {
		t.Fatal("ParseVMAFLog accepted a log with no pooled metrics")
	}
	if _, err := ParseVMAFLog([]byte(`not json`)); err == nil {
		t.Fatal("ParseVMAFLog accepted invalid JSON")
	}
}

// The extra metrics ride along in the same pass, and the entries are separated
// by a bare `|` — escaping it would make the separator a literal and libvmaf
// would see one feature with a very strange name.
func TestVMAFArgsRequestsExtraMetrics(t *testing.T) {
	args := VMAFArgs(VMAFSpec{
		Distorted: "d.mp4", Reference: "r.mkv", LogPath: "d.json",
		Width: 1920, Height: 1080, Metrics: []string{"psnr", "ssim"},
	})
	filter := args[indexOf(args, "-lavfi")+1]
	if !strings.Contains(filter, "feature=name=psnr|name=float_ssim") {
		t.Errorf("filter is missing the feature list:\n%s", filter)
	}
	if strings.Contains(filter, `\|`) {
		t.Errorf("the feature separator must not be escaped:\n%s", filter)
	}
	// Asking for nothing must not leave an empty option behind: `feature=` is
	// not the same as no feature at all.
	plain := VMAFArgs(VMAFSpec{Distorted: "d.mp4", Reference: "r.mkv", LogPath: "d.json", Width: 16, Height: 9})
	if f := plain[indexOf(plain, "-lavfi")+1]; strings.Contains(f, "feature") {
		t.Errorf("no metrics requested, yet the filter mentions feature:\n%s", f)
	}
}

// An unknown name never reaches libvmaf: it would refuse the whole measurement,
// and config has already rejected it long before this point.
func TestVMAFArgsIgnoresUnknownMetrics(t *testing.T) {
	args := VMAFArgs(VMAFSpec{
		Distorted: "d.mp4", Reference: "r.mkv", LogPath: "d.json",
		Width: 16, Height: 9, Metrics: []string{"ciede", "psnr"},
	})
	filter := args[indexOf(args, "-lavfi")+1]
	if !strings.Contains(filter, "feature=name=psnr") || strings.Contains(filter, "ciede") {
		t.Errorf("filter = %s", filter)
	}
}

// The pooled keys are the ones a real libvmaf 2.x log uses: psnr_y (not psnr)
// and float_ssim (not ssim). Getting either wrong reads as "not measured" on a
// run that measured it.
func TestParseVMAFLogReadsPSNRAndSSIM(t *testing.T) {
	log := []byte(`{"frames":[{"frameNum":0},{"frameNum":1}],"pooled_metrics":{
		"psnr_y":{"min":30.1,"max":40.2,"mean":36.5,"harmonic_mean":36.0},
		"psnr_cb":{"min":40,"max":45,"mean":42,"harmonic_mean":42},
		"float_ssim":{"min":0.90,"max":0.99,"mean":0.9712,"harmonic_mean":0.97},
		"vmaf":{"min":70,"max":95,"mean":91.5,"harmonic_mean":90.2}}}`)
	score, err := ParseVMAFLog(log)
	if err != nil {
		t.Fatalf("ParseVMAFLog: %v", err)
	}
	if score.PSNR == nil || *score.PSNR != 36.5 {
		t.Errorf("PSNR = %v, want the pooled mean of psnr_y (36.5)", score.PSNR)
	}
	if score.SSIM == nil || *score.SSIM != 0.9712 {
		t.Errorf("SSIM = %v, want the pooled mean of float_ssim (0.9712)", score.SSIM)
	}
	if !score.Covers([]string{"psnr", "ssim"}) {
		t.Error("a log with both metrics must cover both")
	}
}

// A log written before the run asked for the metrics has to answer "no", or a
// stale point would be reused and its column rendered blank.
func TestScoreCoversReportsMissingMetrics(t *testing.T) {
	log := []byte(`{"frames":[{"frameNum":0}],"pooled_metrics":{
		"vmaf":{"min":70,"max":95,"mean":91.5,"harmonic_mean":90.2}}}`)
	score, err := ParseVMAFLog(log)
	if err != nil {
		t.Fatalf("ParseVMAFLog: %v", err)
	}
	if score.PSNR != nil || score.SSIM != nil {
		t.Errorf("a VMAF-only log must not invent metrics: %+v", score)
	}
	if score.Covers([]string{"psnr"}) {
		t.Error("a VMAF-only log does not cover psnr")
	}
	if !score.Covers(nil) {
		t.Error("asking for nothing is always covered")
	}
	if score.Has("ciede") {
		t.Error("an unknown metric is never present")
	}
}

func TestKnownMetrics(t *testing.T) {
	if got := KnownMetrics(); len(got) != 2 || got[0] != "psnr" || got[1] != "ssim" {
		t.Errorf("KnownMetrics = %v, want a sorted [psnr ssim]", got)
	}
	if !KnownMetric("psnr") || KnownMetric("PSNR") || KnownMetric("") {
		t.Error("KnownMetric should match the exact configured spelling only")
	}
}
