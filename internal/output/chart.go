package output

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
)

// Chart geometry. Fixed rather than configurable: one size that reads in a pull
// request is worth more than six that need choosing between.
const (
	chartW, chartH = 760, 460
	padL, padR     = 62, 150 // the right margin holds the legend
	// The bottom margin holds three stacked rows: the bitrate ticks, the axis
	// title and the provenance line. Two of them collided at 52.
	padT, padB = 44, 66
)

// Colours are mid-tones on purpose. An SVG in a pull request is read on a light
// background and a dark one, and the repository has no second file per theme —
// so nothing here may rely on the page being either.
var curveColors = []string{"#2563eb", "#d97706", "#7c3aed", "#0891b2", "#be185d", "#4d7c0f"}

const (
	inkColor      = "#7b8794" // axes and labels
	frontierColor = "#059669" // the frontier, in the repository's emerald
	kneeColor     = "#dc2626"
)

// Chart writes the rate-quality curves of one encoder as an SVG, with the
// efficient frontier drawn over them and the knees marked.
//
// Hand-written, with no dependency and no template: the whole file is arithmetic
// on the measured points. Bitrate is on a log axis because that is how bitrate is
// read — the distance from 500k to 1000k is the same decision as the one from
// 3000k to 6000k.
func Chart(out io.Writer, r Report, encoder string) error {
	a, err := pickAnalysis(r, encoder)
	if err != nil {
		return err
	}
	pts := allPoints(a)
	if len(pts) < 2 {
		return fmt.Errorf("encoder %s has %d measured points — a chart needs at least two", a.Encoder, len(pts))
	}
	sc := newScale(pts)
	w := &errWriter{w: out}

	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="Rate-quality curves for %s">`+"\n",
		chartW, chartH, chartW, chartH, escapeXML(a.Encoder))
	fmt.Fprintf(w, "  <title>%s — rate-quality curves, %s</title>\n", escapeXML(a.Encoder), escapeXML(r.Input))
	// No background rectangle: an SVG that paints its own white reads as a hole
	// in a dark README, and one that paints nothing inherits whatever it lands on.
	fmt.Fprintf(w, `  <g font-family="ui-sans-serif, -apple-system, Segoe UI, Roboto, sans-serif" font-size="11" fill="%s">`+"\n", inkColor)

	writeAxes(w, sc, r.Options)
	writeCurveLines(w, a, sc)
	writeFrontierLine(w, a, sc)
	writeKnees(w, a, sc)
	writeLegend(w, a, r)

	fmt.Fprintln(w, "  </g>")
	fmt.Fprintln(w, "</svg>")
	return w.err
}

func pickAnalysis(r Report, encoder string) (analysis.Result, error) {
	switch {
	case len(r.Analyses) == 0:
		return analysis.Result{}, fmt.Errorf("this report has no analysis in it")
	case encoder != "":
		for _, a := range r.Analyses {
			if a.Encoder == encoder {
				return a, nil
			}
		}
		return analysis.Result{}, fmt.Errorf("no encoder %q in this report (it has %s)", encoder, strings.Join(encoderNames(r), ", "))
	case len(r.Analyses) == 1:
		return r.Analyses[0], nil
	default:
		return analysis.Result{}, fmt.Errorf("this report measured %s — name one with --encoder", strings.Join(encoderNames(r), ", "))
	}
}

func allPoints(a analysis.Result) []analysis.Point {
	var out []analysis.Point
	for _, c := range a.Curves {
		out = append(out, c.Points...)
	}
	return out
}

// scale maps measurements to pixels: log10 on the bitrate axis, linear on VMAF.
type scale struct {
	loKbps, hiKbps float64 // log10
	loVMAF, hiVMAF float64
}

func newScale(pts []analysis.Point) scale {
	s := scale{loKbps: math.Inf(1), hiKbps: math.Inf(-1), loVMAF: math.Inf(1), hiVMAF: math.Inf(-1)}
	for _, p := range pts {
		if p.Kbps > 0 {
			l := math.Log10(p.Kbps)
			s.loKbps, s.hiKbps = math.Min(s.loKbps, l), math.Max(s.hiKbps, l)
		}
		s.loVMAF, s.hiVMAF = math.Min(s.loVMAF, p.VMAF), math.Max(s.hiVMAF, p.VMAF)
	}
	// A little air, and a guard for the degenerate case where every point landed
	// on the same value: a zero-width axis would divide by zero.
	if s.hiKbps-s.loKbps < 0.05 {
		s.loKbps, s.hiKbps = s.loKbps-0.1, s.hiKbps+0.1
	}
	pad := (s.hiVMAF - s.loVMAF) * 0.08
	if pad < 0.5 {
		pad = 0.5
	}
	s.loVMAF, s.hiVMAF = s.loVMAF-pad, s.hiVMAF+pad
	return s
}

func (s scale) x(kbps float64) float64 {
	if kbps <= 0 {
		return padL
	}
	f := (math.Log10(kbps) - s.loKbps) / (s.hiKbps - s.loKbps)
	return padL + f*float64(chartW-padL-padR)
}

func (s scale) y(vmaf float64) float64 {
	f := (vmaf - s.loVMAF) / (s.hiVMAF - s.loVMAF)
	return float64(chartH-padB) - f*float64(chartH-padT-padB)
}

func writeAxes(w io.Writer, s scale, opt analysis.Options) {
	left, right := float64(padL), float64(chartW-padR)
	top, bottom := float64(padT), float64(chartH-padB)
	fmt.Fprintf(w, `    <path d="M%.1f %.1f V%.1f H%.1f" fill="none" stroke="%s" stroke-width="1"/>`+"\n",
		left, top, bottom, right, inkColor)

	// Bitrate ticks at every power of ten and its halves, which is what a log axis
	// wants and what a reader recognises.
	for _, kbps := range niceRates(s) {
		x := s.x(kbps)
		fmt.Fprintf(w, `    <line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1" opacity="0.25"/>`+"\n",
			x, top, x, bottom, inkColor)
		fmt.Fprintf(w, `    <text x="%.1f" y="%.1f" text-anchor="middle">%s</text>`+"\n", x, bottom+16, kbpsLabel(kbps))
	}
	// Quality ticks every 5 VMAF.
	for v := math.Ceil(s.loVMAF/5) * 5; v <= s.hiVMAF; v += 5 {
		y := s.y(v)
		fmt.Fprintf(w, `    <line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1" opacity="0.25"/>`+"\n",
			left, y, right, y, inkColor)
		fmt.Fprintf(w, `    <text x="%.1f" y="%.1f" text-anchor="end">%.0f</text>`+"\n", left-8, y+4, v)
	}
	// The target, when it falls inside the measured range: a horizontal line is
	// the whole question "does this grid get there" answered at a glance.
	if t := opt.TargetVMAF; t > s.loVMAF && t < s.hiVMAF {
		y := s.y(t)
		fmt.Fprintf(w, `    <line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="1" stroke-dasharray="5 4" opacity="0.8"/>`+"\n",
			left, y, right, y, frontierColor)
		fmt.Fprintf(w, `    <text x="%.1f" y="%.1f" fill="%s">target %.0f</text>`+"\n", left+6, y-6, frontierColor, t)
	}
	fmt.Fprintf(w, `    <text x="%.1f" y="%.1f" text-anchor="middle">bitrate (log scale)</text>`+"\n",
		(left+right)/2, bottom+36)
	fmt.Fprintf(w, `    <text x="14" y="%.1f" text-anchor="middle" transform="rotate(-90 14 %.1f)">VMAF</text>`+"\n",
		(top+bottom)/2, (top+bottom)/2)
}

// niceRates picks the bitrate gridlines: powers of ten and their halves inside
// the measured range, plus the ends so the axis is always labelled.
func niceRates(s scale) []float64 {
	var out []float64
	lo, hi := math.Pow(10, s.loKbps), math.Pow(10, s.hiKbps)
	for exp := math.Floor(s.loKbps); exp <= math.Ceil(s.hiKbps); exp++ {
		for _, mult := range []float64{1, 2, 5} {
			v := mult * math.Pow(10, exp)
			if v >= lo && v <= hi {
				out = append(out, v)
			}
		}
	}
	if len(out) < 2 {
		return []float64{lo, hi}
	}
	return out
}

func kbpsLabel(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.0fM", v/1000)
	}
	return fmt.Sprintf("%.0fk", v)
}

func writeCurveLines(w io.Writer, a analysis.Result, s scale) {
	for i, c := range a.Curves {
		color := curveColors[i%len(curveColors)]
		var d strings.Builder
		for j, p := range c.Points {
			verb := "L"
			if j == 0 {
				verb = "M"
			}
			fmt.Fprintf(&d, "%s%.1f %.1f ", verb, s.x(p.Kbps), s.y(p.VMAF))
		}
		fmt.Fprintf(w, `    <path d="%s" fill="none" stroke="%s" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" opacity="0.9"/>`+"\n",
			strings.TrimSpace(d.String()), color)
		for _, p := range c.Points {
			fmt.Fprintf(w, `    <circle cx="%.1f" cy="%.1f" r="3" fill="%s"/>`+"\n", s.x(p.Kbps), s.y(p.VMAF), color)
		}
	}
}

// writeFrontierLine draws the hull over the curves. Thick, emerald and
// semi-transparent, so it reads as the envelope of the curves rather than as a
// seventh curve competing with them.
func writeFrontierLine(w io.Writer, a analysis.Result, s scale) {
	if len(a.Hull) < 2 {
		return
	}
	var d strings.Builder
	for i, p := range a.Hull {
		verb := "L"
		if i == 0 {
			verb = "M"
		}
		fmt.Fprintf(&d, "%s%.1f %.1f ", verb, s.x(p.Kbps), s.y(p.VMAF))
	}
	fmt.Fprintf(w, `    <path d="%s" fill="none" stroke="%s" stroke-width="4" stroke-linecap="round" stroke-linejoin="round" opacity="0.45"/>`+"\n",
		strings.TrimSpace(d.String()), frontierColor)
}

// writeKnees rings the point past which each resolution stops paying for bits.
func writeKnees(w io.Writer, a analysis.Result, s scale) {
	for _, c := range a.Curves {
		if c.Knee == nil {
			continue
		}
		fmt.Fprintf(w, `    <circle cx="%.1f" cy="%.1f" r="6.5" fill="none" stroke="%s" stroke-width="1.8"/>`+"\n",
			s.x(c.Knee.Kbps), s.y(c.Knee.VMAF), kneeColor)
	}
}

func writeLegend(w io.Writer, a analysis.Result, r Report) {
	x := float64(chartW - padR + 16)
	y := float64(padT + 4)
	fmt.Fprintf(w, `    <text x="%.1f" y="%.1f" font-weight="600" fill="%s">%s</text>`+"\n", x, y, inkColor, escapeXML(a.Encoder))
	y += 18
	for i, c := range a.Curves {
		color := curveColors[i%len(curveColors)]
		fmt.Fprintf(w, `    <line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2"/>`+"\n", x, y-4, x+16, y-4, color)
		fmt.Fprintf(w, `    <text x="%.1f" y="%.1f">%s</text>`+"\n", x+22, y, res(c.Height))
		y += 16
	}
	y += 6
	fmt.Fprintf(w, `    <line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="4" opacity="0.45"/>`+"\n", x, y-4, x+16, y-4, frontierColor)
	fmt.Fprintf(w, `    <text x="%.1f" y="%.1f">frontier</text>`+"\n", x+22, y)
	y += 18
	fmt.Fprintf(w, `    <circle cx="%.1f" cy="%.1f" r="5" fill="none" stroke="%s" stroke-width="1.8"/>`+"\n", x+8, y-4, kneeColor)
	fmt.Fprintf(w, `    <text x="%.1f" y="%.1f">knee</text>`+"\n", x+22, y)

	// The provenance goes on the chart itself. An SVG travels further than the
	// report it came from, and a curve with no idea what measured it is a picture.
	foot := float64(chartH) - 10
	label := r.Input
	if r.Env.ConfigSHA256 != "" {
		label += "  ·  config " + r.Env.ConfigShort()
	}
	if r.Generated != "" {
		label += "  ·  " + r.Generated
	}
	fmt.Fprintf(w, `    <text x="%.1f" y="%.1f" font-size="9" opacity="0.75">%s</text>`+"\n", float64(padL), foot, escapeXML(label))
}

// escapeXML makes a value safe inside SVG text and attributes. Encoder names and
// file paths come from a config, and a stray `&` would produce a file no viewer
// will open.
func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
