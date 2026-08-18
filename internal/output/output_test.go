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
	// Every libvmaf log pools a harmonic mean and writes a per-frame section, so
	// every fixture point has both.
	p1, p5 := 61.0, 68.0
	points := []analysis.Point{
		{Encoder: "x264", Height: 1080, Target: 2000, Kbps: 2010, VMAF: 86, VMAFHarmonic: 84.6, VMAFMin: 74, P1: &p1, P5: &p5},
		{Encoder: "x264", Height: 1080, Target: 3000, Kbps: 3010, VMAF: 92, VMAFHarmonic: 90.8, VMAFMin: 82, P1: &p1, P5: &p5},
		{Encoder: "x264", Height: 1080, Target: 4000, Kbps: 4020, VMAF: 93.5, VMAFHarmonic: 92.4, VMAFMin: 84, P1: &p1, P5: &p5},
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
		References: []bench.Reference{{
			Path:   ".ladder-bench/reference_full_30s.mkv",
			Media:  ffmpeg.Media{Width: 1920, Height: 1080, FrameRate: 25, Duration: 30},
			Source: ffmpeg.Media{Width: 1920, Height: 1080, FrameRate: 25, Duration: 3600, Codec: "h264"},
		}},
		Options:  opt,
		Results:  results,
		Analyses: []analysis.Result{analysis.Analyze("x264", points, opt)},
		Env: Environment{
			FFmpeg:       "ffmpeg version 7.1 Copyright (c) 2000-2024 the FFmpeg developers",
			LibVMAF:      []string{"3.0.0"},
			ConfigSHA256: "9f2c1e0ab34d56789f2c1e0ab34d56789f2c1e0ab34d56789f2c1e0ab34d5678",
		},
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

// A report has to say what measured it, or an old one cannot be replayed or
// knowingly discarded — it can only be trusted or not.
func TestReportRecordsWhatMeasuredIt(t *testing.T) {
	var text, md bytes.Buffer
	if err := Text(&text, sampleReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if err := Markdown(&md, sampleReport()); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	for _, want := range []string{"ffmpeg version 7.1", "3.0.0", "9f2c1e0ab34d"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report is missing %q:\n%s", want, text.String())
		}
		if !strings.Contains(md.String(), want) {
			t.Errorf("markdown report is missing %q", want)
		}
	}
	// The fingerprint is shown short, never in full: it is for telling two runs
	// apart at a glance, and the whole hash is in the JSON.
	full := sampleReport().Env.ConfigSHA256
	if strings.Contains(text.String(), full) {
		t.Error("the text report should show a short fingerprint, not all 64 characters")
	}
}

// A resumed grid can mix two libvmaf builds, and points measured before an
// upgrade are not the same experiment as points measured after it.
func TestReportFlagsAMixedLibVMAF(t *testing.T) {
	r := sampleReport()
	r.Env.LibVMAF = []string{"2.3.1", "3.0.0"}
	var text, md bytes.Buffer
	if err := Text(&text, r); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if err := Markdown(&md, r); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	if !strings.Contains(text.String(), "not all measured by the same libvmaf") {
		t.Errorf("the text report says nothing about the mix:\n%s", text.String())
	}
	if !strings.Contains(md.String(), "not all measured by the same libvmaf") {
		t.Error("the markdown report says nothing about the mix")
	}
	// One version is the normal case and must not raise anything.
	var quiet bytes.Buffer
	if err := Text(&quiet, sampleReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if strings.Contains(quiet.String(), "not all measured") {
		t.Error("a single libvmaf version must not be flagged")
	}
}

// The percentiles are the point of LB-9: a mean absorbs the worst frames, and
// these are the columns that refuse to.
func TestPercentileColumns(t *testing.T) {
	var text, md bytes.Buffer
	if err := Text(&text, sampleReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if err := Markdown(&md, sampleReport()); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	for _, want := range []string{"P5", "P1", "61.00", "68.00"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report is missing %q:\n%s", want, text.String())
		}
		if !strings.Contains(md.String(), want) {
			t.Errorf("markdown report is missing %q", want)
		}
	}
	// A log with no per-frame section gets no columns at all, the same rule the
	// extra metrics follow.
	bare := sampleReport()
	var stripped []analysis.Point
	for _, c := range bare.Analyses[0].Curves {
		for _, p := range c.Points {
			p.P1, p.P5 = nil, nil
			stripped = append(stripped, p)
		}
	}
	bare.Analyses = []analysis.Result{analysis.Analyze("x264", stripped, bare.Options)}
	var plain bytes.Buffer
	if err := Text(&plain, bare); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if strings.Contains(plain.String(), "P5") || strings.Contains(plain.String(), "P1") {
		t.Errorf("no per-frame data, so no percentile columns:\n%s", plain.String())
	}
}

// multiClipReport is a run over three cuts of the same source, where one cut
// disagrees with the others by more than a whole rung.
func multiClipReport() Report {
	r := sampleReport()
	var spread []analysis.Point
	for _, c := range r.Analyses[0].Curves {
		for i, p := range c.Points {
			// The 3000k rung of each resolution is where the clips fall out.
			p.Clips = 3
			p.VMAFSpread = 1.2
			if p.Target == 3000 {
				p.VMAFSpread = 7.4
			}
			_ = i
			spread = append(spread, p)
		}
	}
	r.Analyses = []analysis.Result{analysis.Analyze("x264", spread, r.Options)}
	r.References = []bench.Reference{
		{Path: ".ladder-bench/reference_0s_30s.mkv", Clip: "0s_30s", Media: ffmpeg.Media{Width: 1920, Height: 1080, Duration: 30}},
		{Path: ".ladder-bench/reference_5m_30s.mkv", Clip: "5m_30s", Media: ffmpeg.Media{Width: 1920, Height: 1080, Duration: 30}},
		{Path: ".ladder-bench/reference_20m_30s.mkv", Clip: "20m_30s", Media: ffmpeg.Media{Width: 1920, Height: 1080, Duration: 30}},
	}
	return r
}

// A ladder is only as good as the content it was chosen on, so the report has to
// admit when that content disagreed with itself.
func TestReportShowsTheDispersionAcrossClips(t *testing.T) {
	var text, md bytes.Buffer
	if err := Text(&text, multiClipReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if err := Markdown(&md, multiClipReport()); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	for _, want := range []string{"across 3 clips", "SPREAD", "7.40", "wider than the 6.0 VMAF between rungs"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report is missing %q:\n%s", want, text.String())
		}
	}
	for _, want := range []string{"### Across 3 clips", "Spread across clips", "7.40",
		"wider than the 6.0 VMAF between rungs", "**Reference clips** (3):"} {
		if !strings.Contains(md.String(), want) {
			t.Errorf("markdown report is missing %q:\n%s", want, md.String())
		}
	}
	// Every clip's reference is listed: a report that names one of three cuts
	// cannot be matched back to what was measured.
	for _, want := range []string{"reference_0s_30s.mkv", "reference_5m_30s.mkv", "reference_20m_30s.mkv"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report does not list %q", want)
		}
	}
	if !strings.Contains(text.String(), "and 3 clips") {
		t.Errorf("the header should say how many clips:\n%s", text.String())
	}
}

// A single-clip run must not grow a dispersion section it has nothing to put in.
func TestNoDispersionForOneClip(t *testing.T) {
	var text, md bytes.Buffer
	if err := Text(&text, sampleReport()); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if err := Markdown(&md, sampleReport()); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	// "across" alone would match the header's "points across 1 encoder(s)", so
	// the assertion is on the section heading and the column.
	for _, unwanted := range []string{"SPREAD", "widest disagreement", " clips\n"} {
		if strings.Contains(text.String(), unwanted) {
			t.Errorf("one clip, yet the text report mentions %q:\n%s", unwanted, text.String())
		}
	}
	if strings.Contains(md.String(), "Spread across clips") {
		t.Error("one clip, yet the markdown report has a spread column")
	}
	// An empty bold label is what a naive "first entry keeps the name" loop
	// renders for the second clip.
	if strings.Contains(md.String(), "****") {
		t.Errorf("empty label in the reference list:\n%s", md.String())
	}
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
	for _, key := range []string{"tool", "version", "input", "references", "results", "analysis", "options", "environment"} {
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
