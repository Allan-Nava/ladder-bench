// Package output renders a finished run as terminal text, a Markdown report or
// JSON. The three are projections of the same Report: nothing is computed here
// that the analysis did not already decide.
package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
	"github.com/Allan-Nava/ladder-bench/internal/bench"
)

// Report is everything a run produced.
type Report struct {
	Tool      string            `json:"tool"`
	Version   string            `json:"version"`
	Generated string            `json:"generated"`
	Input     string            `json:"input"`
	Reference bench.Reference   `json:"reference"`
	Options   analysis.Options  `json:"options"`
	Results   []bench.Result    `json:"results"`
	Analyses  []analysis.Result `json:"analysis"`
	// BDRates compares every other encoder against the anchor. Empty when the
	// run measured a single encoder: there is nothing to compare it to.
	BDRates []analysis.Comparison `json:"bd_rates,omitempty"`
}

// errWriter remembers the first write error so the renderers can stay a
// straight run of Fprintf calls and still fail loudly. Reports are written to
// files too, and a report truncated by a full disk must not exit 0.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}

// Render writes the report in the named format.
func Render(w io.Writer, r Report, format string) error {
	switch format {
	case "", "text":
		return Text(w, r)
	case "markdown", "md":
		return Markdown(w, r)
	case "json":
		return JSON(w, r)
	default:
		return fmt.Errorf("unknown output format %q (text, markdown, json)", format)
	}
}

// Formats lists the supported output formats, for help text.
func Formats() string { return "text, markdown, json" }

func res(height int) string { return fmt.Sprintf("%dp", height) }

func kbps(v float64) string { return fmt.Sprintf("%.0fk", v) }

// pct renders a saving with its sign, so a negative saving reads as the cost
// it is instead of hiding behind an absolute value.
func pct(v float64) string {
	if v == 0 {
		v = 0 // negative zero would print as "-0.0%"
	}
	return fmt.Sprintf("%+.1f%%", v)
}

func fps(v float64) string {
	if v <= 0 {
		return "unknown fps"
	}
	return fmt.Sprintf("%.2f fps", v)
}

func geometry(w, h int) string { return fmt.Sprintf("%dx%d", w, h) }

// gains maps each point of a curve to the quality its step bought, so the
// table can show where the curve went flat instead of making the reader
// subtract columns.
func gains(points []analysis.Point) map[int]float64 {
	out := make(map[int]float64, len(points))
	for i := 1; i < len(points); i++ {
		out[points[i].Target] = analysis.Gain(points[i-1], points[i])
	}
	return out
}

// optional renders a metric a run may not have asked for. A dash is the honest
// rendering of "not measured": printing a zero instead would read as a
// catastrophic measurement rather than an absent one.
func optional(v *float64, format string) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf(format, *v)
}

// harmonic renders the VMAF harmonic mean, or a dash when the log did not carry
// one. Zero is impossible for a real comparison, so a zero here means the number
// is missing rather than catastrophic — and printing 0.00 would say the opposite.
func harmonic(v float64) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", v)
}

// measured reports whether any point of this encoder carries the metric, which
// is what decides whether its column appears at all. A column of dashes says
// "we looked and found nothing"; no column says "we did not look".
func measured(a analysis.Result, pick func(analysis.Point) *float64) bool {
	for _, c := range a.Curves {
		for _, p := range c.Points {
			if pick(p) != nil {
				return true
			}
		}
	}
	return false
}

func pointPSNR(p analysis.Point) *float64 { return p.PSNR }

func pointSSIM(p analysis.Point) *float64 { return p.SSIM }

func encoderOf(results []bench.Result, name string) (bench.Result, bool) {
	for _, r := range results {
		if r.Encoder == name {
			return r, true
		}
	}
	return bench.Result{}, false
}

func frontier(hull []analysis.Point) string {
	parts := make([]string, 0, len(hull))
	for _, p := range hull {
		parts = append(parts, fmt.Sprintf("%s %s (VMAF %.1f)", res(p.Height), kbps(p.Kbps), p.VMAF))
	}
	return strings.Join(parts, " → ")
}
