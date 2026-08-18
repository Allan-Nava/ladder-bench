package ffmpeg

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Metric is an extra quality metric libvmaf can compute in the same pass as
// VMAF. Asking for one costs a fraction of the VMAF pass it rides along with,
// because the frames are already decoded and aligned.
type Metric string

// The metrics a run can ask for. The set is deliberately closed: an arbitrary
// libvmaf feature string would be accepted here and then produce pooled metrics
// nothing downstream knows how to read or label.
const (
	// MetricPSNR is peak signal-to-noise ratio. Only the Y plane is reported —
	// unqualified "PSNR" in a codec discussion usually means luma, and saying
	// which plane is cheaper than being asked.
	MetricPSNR Metric = "psnr"
	// MetricSSIM is structural similarity, libvmaf's float implementation.
	MetricSSIM Metric = "ssim"
)

// metric describes how one metric is requested and where its pooled result
// lands in the log. Both halves live here so a new metric is one entry, not a
// name to add in two files.
type metric struct {
	feature string // the libvmaf feature entry
	pooled  string // the key it pools under in the JSON log
}

var metrics = map[Metric]metric{
	MetricPSNR: {feature: "name=psnr", pooled: "psnr_y"},
	MetricSSIM: {feature: "name=float_ssim", pooled: "float_ssim"},
}

// KnownMetrics lists the requestable metrics, sorted, for validation messages.
func KnownMetrics() []string {
	out := make([]string, 0, len(metrics))
	for m := range metrics {
		out = append(out, string(m))
	}
	sort.Strings(out)
	return out
}

// KnownMetric reports whether a name is a metric this build can ask for.
func KnownMetric(name string) bool {
	_, ok := metrics[Metric(name)]
	return ok
}

// Score is the pooled VMAF result of one comparison.
type Score struct {
	Mean float64 `json:"mean"`
	Min  float64 `json:"min"`
	// Harmonic is libvmaf's harmonic mean: it weighs the worst frames more
	// heavily than the arithmetic mean, so a clip with a few badly broken
	// seconds does not hide behind a good average.
	Harmonic float64 `json:"harmonic_mean"`
	Frames   int     `json:"frames"`
	// PSNR (Y plane, dB) and SSIM are present only when the run asked for
	// them. They are pointers because a zero cannot say "not measured": absent
	// and catastrophic would otherwise render identically.
	PSNR *float64 `json:"psnr_y,omitempty"`
	SSIM *float64 `json:"ssim,omitempty"`
}

// Has reports whether this score carries a metric.
func (s Score) Has(m Metric) bool {
	switch m {
	case MetricPSNR:
		return s.PSNR != nil
	case MetricSSIM:
		return s.SSIM != nil
	}
	return false
}

// Covers reports whether this score carries every named metric. A log written
// before `vmaf.metrics` grew does not, which is what makes a reused point stale
// rather than merely old.
func (s Score) Covers(names []string) bool {
	for _, n := range names {
		if !s.Has(Metric(n)) {
			return false
		}
	}
	return true
}

type pooledMetric struct {
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Mean     float64 `json:"mean"`
	Harmonic float64 `json:"harmonic_mean"`
}

// The pooled metrics are a map rather than a struct: libvmaf writes one entry
// per feature it computed, and which features those are depends on the model
// and on what the run asked for.
type vmafLog struct {
	Frames []struct {
		FrameNum int `json:"frameNum"`
	} `json:"frames"`
	Pooled map[string]pooledMetric `json:"pooled_metrics"`
}

// ParseVMAFLog reads the JSON log libvmaf writes.
func ParseVMAFLog(data []byte) (Score, error) {
	var l vmafLog
	if err := json.Unmarshal(data, &l); err != nil {
		return Score{}, fmt.Errorf("parsing VMAF log: %w", err)
	}
	vmaf, ok := l.Pooled["vmaf"]
	if !ok {
		// This is what an empty comparison looks like: ffmpeg exits 0 and
		// writes a log with no pooled metrics when the two inputs never
		// overlapped in time.
		return Score{}, errors.New("VMAF log has no pooled metrics (did the two inputs share any frames?)")
	}
	s := Score{
		Mean:     vmaf.Mean,
		Min:      vmaf.Min,
		Harmonic: vmaf.Harmonic,
		Frames:   len(l.Frames),
	}
	if p, ok := l.Pooled[metrics[MetricPSNR].pooled]; ok {
		mean := p.Mean
		s.PSNR = &mean
	}
	if p, ok := l.Pooled[metrics[MetricSSIM].pooled]; ok {
		mean := p.Mean
		s.SSIM = &mean
	}
	return s, nil
}

// featureOption builds the libvmaf `feature` value for the named metrics, or
// the empty string when none were asked for.
//
// Several features go in one option separated by `|`, which the filtergraph
// parser passes through untouched — so it must not go through escapeFilter,
// which would turn the separator into a literal.
func featureOption(names []string) string {
	entries := make([]string, 0, len(names))
	for _, n := range names {
		if m, ok := metrics[Metric(n)]; ok {
			entries = append(entries, m.feature)
		}
	}
	return strings.Join(entries, "|")
}
