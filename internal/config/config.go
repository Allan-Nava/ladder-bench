// Package config loads, defaults and validates the ladder-bench configuration.
//
// The config IS the experiment: which clip of which source, which encoders,
// which (resolution, bitrate) grid points to measure, and how to read the
// resulting rate-quality curve.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults applied by Load when a field is left out.
const (
	DefaultWorkDir     = ".ladder-bench"
	DefaultVMAFModel   = "version=vmaf_v0.6.1"
	DefaultKneeGain    = 0.5
	DefaultTargetVMAF  = 93.0
	DefaultLadderStep  = 6.0
	DefaultGOPSeconds  = 2.0
	DefaultConcurrency = 1
)

// Duration carries a human string ("30s", "1m30s") through YAML.
//
// A bare number is rejected on purpose: `start: 60` reads as "a minute" to a
// human and as 60 nanoseconds to time.Duration, and nothing downstream would
// notice the difference until the clip came out empty.
type Duration time.Duration

// UnmarshalYAML decodes a duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a quoted string like \"30s\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		// YAML happily decodes the scalar 60 into a string, so this is also
		// where `start: 60` lands — the message has to say what a duration
		// looks like, not just that ParseDuration disliked it.
		return fmt.Errorf("invalid duration %q: use a quoted string like \"30s\" or \"1m30s\"", s)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML renders the duration back as a string.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

// Config is the whole benchmark description.
type Config struct {
	// Input is the source file every measurement derives from.
	Input string `yaml:"input"`
	// WorkDir holds the reference clip, the encodes and the VMAF logs.
	WorkDir string `yaml:"work_dir"`
	// Concurrency is how many grid points encode at once. The default is 1:
	// ffmpeg already saturates the machine, and parallel encodes make the
	// per-point timings meaningless.
	Concurrency int `yaml:"concurrency"`
	// KeepEncodes leaves the encoded files on disk after the run.
	KeepEncodes bool `yaml:"keep_encodes"`
	// FFmpeg and FFprobe override binary discovery via PATH.
	FFmpeg  string `yaml:"ffmpeg"`
	FFprobe string `yaml:"ffprobe"`

	Clip     Clip        `yaml:"clip"`
	Encoders []Encoder   `yaml:"encoders"`
	Rungs    []Rung      `yaml:"rungs"`
	VMAF     VMAF        `yaml:"vmaf"`
	Analysis Analysis    `yaml:"analysis"`
	Current  []Rendition `yaml:"current_ladder"`
}

// Clip selects the slice of the source used as reference.
type Clip struct {
	Start    Duration `yaml:"start"`
	Duration Duration `yaml:"duration"`
}

// Encoder is one codec configuration measured across the whole grid.
type Encoder struct {
	Name  string `yaml:"name"`
	Codec string `yaml:"codec"`
	// Preset is passed as -preset when set. Encoders that spell it
	// differently take it through ExtraArgs instead.
	Preset    string   `yaml:"preset"`
	ExtraArgs []string `yaml:"extra_args"`
}

// Rung is one resolution and the bitrates measured at it, in kbps.
type Rung struct {
	Height   int   `yaml:"height"`
	Bitrates []int `yaml:"bitrates"`
}

// VMAF configures the libvmaf filter.
type VMAF struct {
	Model     string `yaml:"model"`
	Threads   int    `yaml:"n_threads"`
	Subsample int    `yaml:"n_subsample"`
}

// Analysis holds the thresholds that turn the measured points into advice.
type Analysis struct {
	// KneeGain is the VMAF gain per +10% bitrate below which a rung is
	// considered saturated.
	KneeGain float64 `yaml:"knee_gain"`
	// TargetVMAF is the quality the top rung of the recommended ladder aims at.
	TargetVMAF float64 `yaml:"target_vmaf"`
	// LadderStep is the VMAF distance between two rungs of the recommended
	// ladder. ~6 points is roughly one just-noticeable difference.
	LadderStep float64 `yaml:"ladder_step"`
	// GOPSeconds is the keyframe interval used for every encode. It mirrors
	// the segment duration of a real ABR delivery: a benchmark run with open
	// GOPs would report an efficiency nobody can ship.
	GOPSeconds float64 `yaml:"gop_seconds"`
}

// Rendition is one rung of an existing ladder, used for the savings report.
type Rendition struct {
	Height  int `yaml:"height"`
	Bitrate int `yaml:"bitrate"`
}

// Load reads, defaults and validates a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes a config from YAML bytes. Unknown fields are an error: a typo
// in a key would otherwise silently fall back to the default and quietly
// change what was measured.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.WorkDir == "" {
		c.WorkDir = DefaultWorkDir
	}
	if c.Concurrency == 0 {
		c.Concurrency = DefaultConcurrency
	}
	if c.VMAF.Model == "" {
		c.VMAF.Model = DefaultVMAFModel
	}
	if c.VMAF.Subsample == 0 {
		c.VMAF.Subsample = 1
	}
	if c.Analysis.KneeGain == 0 {
		c.Analysis.KneeGain = DefaultKneeGain
	}
	if c.Analysis.TargetVMAF == 0 {
		c.Analysis.TargetVMAF = DefaultTargetVMAF
	}
	if c.Analysis.LadderStep == 0 {
		c.Analysis.LadderStep = DefaultLadderStep
	}
	if c.Analysis.GOPSeconds == 0 {
		c.Analysis.GOPSeconds = DefaultGOPSeconds
	}
	if len(c.Encoders) == 0 {
		c.Encoders = []Encoder{{Name: "x264-slow", Codec: "libx264", Preset: "slow"}}
	}
}

// Validate reports the first problem that would make the run meaningless.
func (c *Config) Validate() error {
	if c.Input == "" {
		return errors.New("input: required (the source file to benchmark)")
	}
	if c.Clip.Duration < 0 || c.Clip.Start < 0 {
		return errors.New("clip: start and duration must not be negative")
	}
	if len(c.Rungs) == 0 {
		return errors.New("rungs: at least one resolution is required")
	}
	seenHeight := map[int]bool{}
	for _, r := range c.Rungs {
		switch {
		case r.Height <= 0:
			return fmt.Errorf("rungs: height %d is not a resolution", r.Height)
		case r.Height%2 != 0:
			// scale=-2:H keeps the aspect ratio with an even width; an odd
			// height cannot be encoded as yuv420p at all.
			return fmt.Errorf("rungs: height %d is odd, yuv420p needs even dimensions", r.Height)
		case seenHeight[r.Height]:
			return fmt.Errorf("rungs: height %d listed twice", r.Height)
		case len(r.Bitrates) == 0:
			return fmt.Errorf("rungs: height %d has no bitrates", r.Height)
		}
		seenHeight[r.Height] = true
		seenRate := map[int]bool{}
		for _, b := range r.Bitrates {
			if b <= 0 {
				return fmt.Errorf("rungs: height %d has a non-positive bitrate %d", r.Height, b)
			}
			if seenRate[b] {
				return fmt.Errorf("rungs: height %d lists bitrate %d twice", r.Height, b)
			}
			seenRate[b] = true
		}
	}
	seenName := map[string]bool{}
	for i, e := range c.Encoders {
		if e.Name == "" {
			return fmt.Errorf("encoders[%d]: name is required (it names the output files)", i)
		}
		if e.Codec == "" {
			return fmt.Errorf("encoders[%d] %q: codec is required", i, e.Name)
		}
		if seenName[e.Name] {
			return fmt.Errorf("encoders: name %q listed twice", e.Name)
		}
		seenName[e.Name] = true
	}
	if c.Concurrency < 1 {
		return fmt.Errorf("concurrency: %d, must be >= 1", c.Concurrency)
	}
	if c.VMAF.Subsample < 1 {
		return fmt.Errorf("vmaf.n_subsample: %d, must be >= 1", c.VMAF.Subsample)
	}
	if c.VMAF.Threads < 0 {
		return fmt.Errorf("vmaf.n_threads: %d, must be >= 0 (0 = auto)", c.VMAF.Threads)
	}
	if c.Analysis.KneeGain < 0 {
		return fmt.Errorf("analysis.knee_gain: %.2f, must be >= 0", c.Analysis.KneeGain)
	}
	if c.Analysis.TargetVMAF <= 0 || c.Analysis.TargetVMAF > 100 {
		return fmt.Errorf("analysis.target_vmaf: %.1f, must be within (0, 100]", c.Analysis.TargetVMAF)
	}
	if c.Analysis.LadderStep <= 0 {
		return fmt.Errorf("analysis.ladder_step: %.1f, must be > 0", c.Analysis.LadderStep)
	}
	if c.Analysis.GOPSeconds <= 0 {
		return fmt.Errorf("analysis.gop_seconds: %.1f, must be > 0", c.Analysis.GOPSeconds)
	}
	for i, r := range c.Current {
		if r.Height <= 0 || r.Bitrate <= 0 {
			return fmt.Errorf("current_ladder[%d]: height and bitrate must be positive", i)
		}
	}
	return nil
}

// Points is the number of encodes the config asks for.
func (c *Config) Points() int {
	n := 0
	for _, r := range c.Rungs {
		n += len(r.Bitrates)
	}
	return n * len(c.Encoders)
}
