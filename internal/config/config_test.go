package config

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const minimal = `
input: source.mp4
rungs:
  - height: 1080
    bitrates: [3000, 5000]
`

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.WorkDir != DefaultWorkDir {
		t.Errorf("work_dir = %q, want %q", cfg.WorkDir, DefaultWorkDir)
	}
	if cfg.Concurrency != 1 {
		t.Errorf("concurrency = %d, want 1", cfg.Concurrency)
	}
	if cfg.VMAF.Model != DefaultVMAFModel {
		t.Errorf("vmaf.model = %q, want %q", cfg.VMAF.Model, DefaultVMAFModel)
	}
	if cfg.Analysis.TargetVMAF != DefaultTargetVMAF || cfg.Analysis.LadderStep != DefaultLadderStep {
		t.Errorf("analysis defaults not applied: %+v", cfg.Analysis)
	}
	if len(cfg.Encoders) != 1 || cfg.Encoders[0].Codec != "libx264" {
		t.Errorf("default encoder = %+v, want one libx264", cfg.Encoders)
	}
	if got := cfg.Points(); got != 2 {
		t.Errorf("Points() = %d, want 2", got)
	}
}

func TestParseDurations(t *testing.T) {
	cfg, err := Parse([]byte(minimal + "\nclip:\n  start: \"1m30s\"\n  duration: \"30s\"\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Clip.Start.D() != 90*time.Second {
		t.Errorf("clip.start = %v, want 1m30s", cfg.Clip.Start.D())
	}
	if cfg.Clip.Duration.D() != 30*time.Second {
		t.Errorf("clip.duration = %v, want 30s", cfg.Clip.Duration.D())
	}
}

// A bare number is a trap: "start: 60" means a minute to whoever wrote it and
// 60 nanoseconds to time.Duration. Rejecting it is the whole point.
func TestParseRejectsBareNumberDuration(t *testing.T) {
	_, err := Parse([]byte(minimal + "\nclip:\n  start: 60\n"))
	if err == nil {
		t.Fatal("Parse accepted a bare number as a duration")
	}
	if !strings.Contains(err.Error(), "30s") {
		t.Errorf("error should show the expected form, got: %v", err)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte(minimal + "\nconcurrancy: 4\n"))
	if err == nil {
		t.Fatal("Parse accepted an unknown field; a typo would silently change the run")
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]string{
		"no input":        "rungs:\n  - height: 720\n    bitrates: [1000]\n",
		"no rungs":        "input: a.mp4\n",
		"odd height":      "input: a.mp4\nrungs:\n  - height: 721\n    bitrates: [1000]\n",
		"no bitrates":     "input: a.mp4\nrungs:\n  - height: 720\n    bitrates: []\n",
		"zero bitrate":    "input: a.mp4\nrungs:\n  - height: 720\n    bitrates: [0]\n",
		"duplicate rung":  "input: a.mp4\nrungs:\n  - height: 720\n    bitrates: [1000]\n  - height: 720\n    bitrates: [2000]\n",
		"duplicate rate":  "input: a.mp4\nrungs:\n  - height: 720\n    bitrates: [1000, 1000]\n",
		"bad target":      minimal + "\nanalysis:\n  target_vmaf: 120\n",
		"bad concurrency": minimal + "\nconcurrency: -1\n",
		"bad subsample":   minimal + "\nvmaf:\n  n_subsample: -1\n",
		"bad threads":     minimal + "\nvmaf:\n  n_threads: -1\n",
		"nameless encoder": "input: a.mp4\nrungs:\n  - height: 720\n    bitrates: [1000]\n" +
			"encoders:\n  - codec: libx264\n",
		"duplicate encoder": "input: a.mp4\nrungs:\n  - height: 720\n    bitrates: [1000]\n" +
			"encoders:\n  - name: a\n    codec: libx264\n  - name: a\n    codec: libx265\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(yaml)); err == nil {
				t.Fatalf("Parse accepted an invalid config (%s)", name)
			}
		})
	}
}

// n_subsample: 0 would be a valid YAML zero and mean "default" to the
// applyDefaults pass; it must come out as 1, not as an error.
func TestSubsampleDefaultsToOne(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.VMAF.Subsample != 1 {
		t.Errorf("vmaf.n_subsample = %d, want 1", cfg.VMAF.Subsample)
	}
}

func TestPointsCountsEveryEncoder(t *testing.T) {
	cfg, err := Parse([]byte(`
input: a.mp4
encoders:
  - name: a
    codec: libx264
  - name: b
    codec: libx265
rungs:
  - height: 1080
    bitrates: [3000, 5000]
  - height: 720
    bitrates: [1500]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Points(); got != 6 {
		t.Errorf("Points() = %d, want 6", got)
	}
}

func TestVMAFMetricsAreValidated(t *testing.T) {
	base := `
input: in.mp4
rungs:
  - height: 1080
    bitrates: [3000]
vmaf:
  metrics: %s
`
	ok, err := Parse([]byte(fmt.Sprintf(base, "[psnr, ssim]")))
	if err != nil {
		t.Fatalf("a valid metric list was rejected: %v", err)
	}
	if len(ok.VMAF.Metrics) != 2 {
		t.Errorf("metrics = %v", ok.VMAF.Metrics)
	}
	// Nothing is the default: turning metrics on invalidates existing logs, so
	// it has to be asked for.
	plain, err := Parse([]byte("input: in.mp4\nrungs:\n  - height: 1080\n    bitrates: [3000]\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(plain.VMAF.Metrics) != 0 {
		t.Errorf("metrics default to %v, want none", plain.VMAF.Metrics)
	}
	// A misspelling must name the alternatives rather than being ignored: a
	// silently dropped metric is a column that never appears for no stated
	// reason.
	_, err = Parse([]byte(fmt.Sprintf(base, "[psnr, sslm]")))
	if err == nil {
		t.Fatal("an unknown metric was accepted")
	}
	if !strings.Contains(err.Error(), "sslm") || !strings.Contains(err.Error(), "ssim") {
		t.Errorf("error should name the typo and the alternatives, got: %v", err)
	}
	if _, err := Parse([]byte(fmt.Sprintf(base, "[psnr, psnr]"))); err == nil {
		t.Error("a duplicated metric was accepted")
	}
}

// The fingerprint answers "was this measured under the same settings", so it has
// to ignore everything that changes where a run happens and nothing that changes
// what it measures.
func TestHashIgnoresWhereARunHappens(t *testing.T) {
	base := `
input: in.mp4
rungs:
  - height: 1080
    bitrates: [3000, 6000]
analysis:
  target_vmaf: 93
`
	a, err := Parse([]byte(base))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ha, err := a.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if len(ha) != 64 {
		t.Errorf("hash = %q, want 64 hex characters", ha)
	}

	// Same experiment on another machine: different paths, different work dir,
	// a faster box. The fingerprint has to match or it cannot compare two runs.
	b, _ := Parse([]byte(base))
	b.WorkDir = "/mnt/scratch/lb"
	b.FFmpeg = "/opt/homebrew/bin/ffmpeg"
	b.FFprobe = "/opt/homebrew/bin/ffprobe"
	b.Concurrency = 8
	b.KeepEncodes = true
	hb, _ := b.Hash()
	if hb != ha {
		t.Error("the fingerprint changed although nothing about the measurement did")
	}

	// A different grid is a different experiment.
	c, _ := Parse([]byte(base))
	c.Rungs[0].Bitrates = []int{3000, 6000, 9000}
	if hc, _ := c.Hash(); hc == ha {
		t.Error("adding a grid point must change the fingerprint")
	}
	// So is a different threshold, even though the grid is identical.
	d, _ := Parse([]byte(base))
	d.Analysis.TargetVMAF = 95
	if hd, _ := d.Hash(); hd == ha {
		t.Error("changing target_vmaf must change the fingerprint")
	}
	// And so is asking for another metric.
	e, _ := Parse([]byte(base))
	e.VMAF.Metrics = []string{"psnr"}
	if he, _ := e.Hash(); he == ha {
		t.Error("asking for PSNR must change the fingerprint")
	}
}

// Defaults are part of the experiment: a config that spells out what another one
// leaves implicit describes the same run and must fingerprint the same.
func TestHashIsOverTheResolvedConfig(t *testing.T) {
	implicit, err := Parse([]byte("input: in.mp4\nrungs:\n  - height: 720\n    bitrates: [1500]\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	explicit, err := Parse([]byte(`
input: in.mp4
rungs:
  - height: 720
    bitrates: [1500]
vmaf:
  model: version=vmaf_v0.6.1
  n_subsample: 1
analysis:
  knee_gain: 0.5
  target_vmaf: 93.0
  ladder_step: 6.0
  gop_seconds: 2.0
encoders:
  - name: x264-slow
    codec: libx264
    preset: slow
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	hi, _ := implicit.Hash()
	he, _ := explicit.Hash()
	if hi != he {
		t.Error("spelling out the defaults describes the same experiment, so the fingerprint must match")
	}
}
