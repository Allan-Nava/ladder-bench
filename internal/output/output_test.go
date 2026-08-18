package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
	"github.com/Allan-Nava/ladder-bench/internal/bench"
	"github.com/Allan-Nava/ladder-bench/internal/ffmpeg"
)

func sampleReport() Report {
	// Every libvmaf log pools a harmonic mean, so every fixture point has one.
	points := []analysis.Point{
		{Encoder: "x264", Height: 1080, Target: 2000, Kbps: 2010, VMAF: 86, VMAFHarmonic: 84.6, VMAFMin: 74},
		{Encoder: "x264", Height: 1080, Target: 3000, Kbps: 3010, VMAF: 92, VMAFHarmonic: 90.8, VMAFMin: 82},
		{Encoder: "x264", Height: 1080, Target: 4000, Kbps: 4020, VMAF: 93.5, VMAFHarmonic: 92.4, VMAFMin: 84},
		{Encoder: "x264", Height: 720, Target: 1000, Kbps: 1005, VMAF: 78, VMAFHarmonic: 76.2, VMAFMin: 66},
		{Encoder: "x264", Height: 720, Target: 2000, Kbps: 2010, VMAF: 87, VMAFHarmonic: 85.6, VMAFMin: 75},
		{Encoder: "x264", Height: 720, Target: 3000, Kbps: 3005, VMAF: 88.5, VMAFHarmonic: 87.2, VMAFMin: 77},
	}
	opt := analysis.Options{
		KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6,
		Current: []analysis.Rendition{{Height: 720, Kbps: 3000}, {Height: 360, Kbps: 800}},
	}
	results := []bench.Result{{
		Job:   bench.Job{Encoder: "x264", Codec: "libx264", Preset: "slow", Height: 1080, Kbps: 3000},
		Score: ffmpeg.Score{Mean: 90, Min: 80},
	}}
	return Report{
		Tool: "ladder-bench", Version: "0.1.0", Generated: "2026-08-17T10:00:00Z",
		Input: "source.mxf",
		Reference: bench.Reference{
			Path:   ".ladder-bench/reference_full_30s.mkv",
			Media:  ffmpeg.Media{Width: 1920, Height: 1080, FrameRate: 25, Duration: 30},
			Source: ffmpeg.Media{Width: 1920, Height: 1080, FrameRate: 25, Duration: 3600, Codec: "h264"},
		},
		Options:  opt,
		Results:  results,
		Analyses: []analysis.Result{analysis.Analyze("x264", points, opt)},
	}
}

// challengerReport is the two-encoder case: the same grid measured again by a
// codec that needs 30% fewer bits everywhere.
func challengerReport() Report {
	r := sampleReport()
	anchor := r.Analyses[0]
	var cheaper []analysis.Point
	for _, c := range anchor.Curves {
		for _, p := range c.Points {
			p.Encoder = "svt-av1"
			p.Kbps *= 0.7
			cheaper = append(cheaper, p)
		}
	}
	r.Analyses = append(r.Analyses, analysis.Analyze("svt-av1", cheaper, r.Options))
	r.BDRates = analysis.BDRates("x264", r.Analyses)
	return r
}

// The extra metrics get a column only when the run asked for them: a column of
// dashes reads as "we looked and found nothing", which is not what happened.
func TestExtraMetricColumnsAppearOnlyWhenMeasured(t *testing.T) {
	plain := sampleReport()
	for _, render := range []func(*bytes.Buffer, Report) error{
		func(b *bytes.Buffer, r Report) error { return Text(b, r) },
		func(b *bytes.Buffer, r Report) error { return Markdown(b, r) },
	} {
		var buf bytes.Buffer
		if err := render(&buf, plain); err != nil {
			t.Fatalf("render: %v", err)
		}
		out := buf.String()
		for _, unwanted := range []string{"PSNR", "SSIM"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("nothing measured %s, yet it has a column:\n%s", unwanted, out)
			}
		}
		// The harmonic mean costs nothing — libvmaf pools it always — so it is
		// always there.
		if !strings.Contains(out, "HMEAN") && !strings.Contains(out, "VMAF harmonic") {
			t.Errorf("the harmonic mean column is missing:\n%s", out)
		}
	}

	withMetrics := reportWithMetrics()
	var text, md bytes.Buffer
	if err := Text(&text, withMetrics); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if err := Markdown(&md, withMetrics); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	for _, want := range []string{"PSNR-Y", "SSIM", "38.25", "0.9712"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report is missing %q:\n%s", want, text.String())
		}
		if !strings.Contains(md.String(), want) {
			t.Errorf("markdown report is missing %q:\n%s", want, md.String())
		}
	}
	// One point of the grid was measured before the metrics were switched on.
	// Its row must read as absent, not as zero: a PSNR of 0 dB would be a
	// catastrophe, and this point simply was not asked.
	leftover := ""
	for _, line := range strings.Split(text.String(), "\n") {
		if strings.Contains(line, "1080p") && strings.Contains(line, "2000k") {
			leftover = line
		}
	}
	if leftover == "" {
		t.Fatalf("the leftover point is missing from the report:\n%s", text.String())
	}
	if strings.Contains(leftover, "38.25") || strings.Contains(leftover, "0.00") {
		t.Errorf("the leftover point should carry no metrics: %q", leftover)
	}
	if strings.Count(leftover, "—") != 3 { // gain, PSNR, SSIM
		t.Errorf("expected a dash for the gain and both unmeasured metrics: %q", leftover)
	}
}

// reportWithMetrics is a run that asked for PSNR and SSIM, with one point left
// over from before the request.
func reportWithMetrics() Report {
	r := sampleReport()
	psnr, ssim := 38.25, 0.9712
	points := []analysis.Point{}
	for i, c := range r.Analyses[0].Curves {
		for j, p := range c.Points {
			if i == 0 && j == 0 {
				points = append(points, p) // the leftover point
				continue
			}
			p.PSNR, p.SSIM = &psnr, &ssim
			points = append(points, p)
		}
	}
	r.Analyses = []analysis.Result{analysis.Analyze("x264", points, r.Options)}
	return r
}

func TestTextReportShowsBDRate(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, challengerReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"bd-rate vs x264", "svt-av1", "frontier", "over VMAF", "1080p"} {
		if !strings.Contains(out, want) {
			t.Errorf("text report is missing %q:\n%s", want, out)
		}
	}
	// A challenger that is cheaper everywhere must read as fewer bits, and the
	// sign is the entire message of the section.
	if !strings.Contains(out, "-3") {
		t.Errorf("expected a negative BD-rate around -30%%:\n%s", out)
	}
}

func TestMarkdownReportShowsBDRate(t *testing.T) {
	var buf bytes.Buffer
	if err := Markdown(&buf, challengerReport()); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"## BD-rate versus `x264`", "### `svt-av1`",
		"| Scope | BD-rate | Over | Method |", "Efficient frontier",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown report is missing %q:\n%s", want, out)
		}
	}
}

// A single-encoder run has nothing to compare, and an empty BD-rate section
// would read as "we looked and found nothing" instead of "we did not look".
func TestBDRateSectionIsAbsentForOneEncoder(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, sampleReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if strings.Contains(buf.String(), "bd-rate") {
		t.Errorf("a one-encoder run must not show a BD-rate section:\n%s", buf.String())
	}
	buf.Reset()
	if err := Markdown(&buf, sampleReport()); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	if strings.Contains(buf.String(), "BD-rate") {
		t.Errorf("a one-encoder run must not show a BD-rate section:\n%s", buf.String())
	}
}

func TestTextReportCoversEverySection(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, sampleReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"source.mxf", "reference", "encoder x264 (libx264, preset slow)",
		"saturation", "efficient frontier", "recommended ladder", "vs current ladder",
		"1 of 2 rungs comparable", "outside the measured grid",
		"1080p", "720p", "VMAF",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text report is missing %q:\n%s", want, out)
		}
	}
}

func TestMarkdownReportIsTables(t *testing.T) {
	var buf bytes.Buffer
	if err := Markdown(&buf, sampleReport()); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"# ladder-bench report", "## Encoder `x264`", "### Recommended ladder",
		"| Resolution | Bitrate | VMAF |", "### Versus the current ladder",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown report is missing %q:\n%s", want, out)
		}
	}
}

func TestJSONReportRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleReport()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("the JSON report is not valid JSON: %v", err)
	}
	for _, key := range []string{"tool", "version", "input", "reference", "results", "analysis", "options"} {
		if _, ok := back[key]; !ok {
			t.Errorf("JSON report is missing %q", key)
		}
	}
	if _, ok := back["bd_rates"]; ok {
		t.Error("a one-encoder run must not carry a bd_rates key")
	}

	buf.Reset()
	if err := JSON(&buf, challengerReport()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("the JSON report is not valid JSON: %v", err)
	}
	if _, ok := back["bd_rates"]; !ok {
		t.Error("JSON report is missing \"bd_rates\"")
	}
}

func TestRenderRejectsUnknownFormat(t *testing.T) {
	if err := Render(&bytes.Buffer{}, sampleReport(), "pdf"); err == nil {
		t.Fatal("Render accepted an unknown format")
	}
	if err := Render(&bytes.Buffer{}, sampleReport(), ""); err != nil {
		t.Errorf("the empty format should default to text: %v", err)
	}
}

// The savings line reads as a bitrate change, so a saving must show up as a
// negative number of bits and never as a bare positive one.
func TestSavingsRenderAsBitrateChange(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, sampleReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	lines := strings.Split(buf.String(), "\n")
	total := ""
	for i, l := range lines {
		if strings.Contains(l, "vs current ladder") && i+1 < len(lines) {
			total = lines[i+1]
		}
	}
	if total == "" {
		t.Fatal("no savings total in the report")
	}
	// 720p@3000 delivers 88.5, which the frontier reaches at ~2300k: the line
	// must show fewer bits, and show it as a negative change.
	if !strings.Contains(total, "3000k → 2") || !strings.Contains(total, "(-") {
		t.Errorf("expected a bitrate cut shown as a negative change, got %q", total)
	}
}
