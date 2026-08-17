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
	points := []analysis.Point{
		{Encoder: "x264", Height: 1080, Target: 2000, Kbps: 2010, VMAF: 86, VMAFMin: 74},
		{Encoder: "x264", Height: 1080, Target: 3000, Kbps: 3010, VMAF: 92, VMAFMin: 82},
		{Encoder: "x264", Height: 1080, Target: 4000, Kbps: 4020, VMAF: 93.5, VMAFMin: 84},
		{Encoder: "x264", Height: 720, Target: 1000, Kbps: 1005, VMAF: 78, VMAFMin: 66},
		{Encoder: "x264", Height: 720, Target: 2000, Kbps: 2010, VMAF: 87, VMAFMin: 75},
		{Encoder: "x264", Height: 720, Target: 3000, Kbps: 3005, VMAF: 88.5, VMAFMin: 77},
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
