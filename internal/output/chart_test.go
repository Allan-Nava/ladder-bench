package output

import (
	"bytes"
	"encoding/xml"
	"math"
	"strings"
	"testing"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
)

// An SVG no viewer will open is not a chart, so the first thing to assert is that
// it parses.
func TestChartIsValidSVG(t *testing.T) {
	var buf bytes.Buffer
	if err := Chart(&buf, sampleReport(), ""); err != nil {
		t.Fatalf("Chart: %v", err)
	}
	var doc struct {
		XMLName xml.Name `xml:"svg"`
		ViewBox string   `xml:"viewBox,attr"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("the chart is not valid XML: %v\n%s", err, buf.String())
	}
	if doc.ViewBox == "" {
		t.Error("a chart without a viewBox does not scale")
	}
	for _, want := range []string{"<title>", "1080p", "720p", "frontier", "knee", "bitrate (log scale)", "VMAF"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("chart is missing %q", want)
		}
	}
	// No painted background: one that paints its own white reads as a hole in a
	// dark README.
	if strings.Contains(buf.String(), `fill="#fff"`) || strings.Contains(buf.String(), `fill="white"`) {
		t.Error("the chart must not paint its own background")
	}
	// The provenance travels with the picture, which outlives the report.
	if !strings.Contains(buf.String(), "source.mxf") || !strings.Contains(buf.String(), "config 9f2c1e0ab34d") {
		t.Errorf("the chart should carry what measured it:\n%s", buf.String())
	}
}

// Encoder names and file paths come from a config, and a stray ampersand would
// produce a file no viewer will open.
func TestChartEscapesItsText(t *testing.T) {
	r := sampleReport()
	r.Input = `clip & "final" <v2>.mxf`
	var pts []analysis.Point
	for _, c := range r.Analyses[0].Curves {
		for _, p := range c.Points {
			p.Encoder = "x264 <fast> & slow"
			pts = append(pts, p)
		}
	}
	r.Analyses = []analysis.Result{analysis.Analyze("x264 <fast> & slow", pts, r.Options)}
	var buf bytes.Buffer
	if err := Chart(&buf, r, ""); err != nil {
		t.Fatalf("Chart: %v", err)
	}
	if err := xml.Unmarshal(buf.Bytes(), new(any)); err != nil {
		t.Fatalf("unescaped text broke the SVG: %v", err)
	}
	if strings.Contains(buf.String(), "& slow") {
		t.Error("the ampersand was not escaped")
	}
}

// The x axis is log10 of the bitrate, because that is how bitrate is read: 500k
// to 1000k is the same decision as 3000k to 6000k, and both must span the same
// distance on the chart.
func TestChartBitrateAxisIsLogarithmic(t *testing.T) {
	s := newScale([]analysis.Point{{Kbps: 500, VMAF: 70}, {Kbps: 8000, VMAF: 96}})
	oneDoubling := s.x(1000) - s.x(500)
	another := s.x(6000) - s.x(3000)
	if math.Abs(oneDoubling-another) > 0.5 {
		t.Errorf("a doubling spans %.2f px low down and %.2f px high up — that is not a log axis", oneDoubling, another)
	}
	// And quality rises upward, which on an SVG means a smaller y.
	if s.y(96) >= s.y(70) {
		t.Error("higher VMAF should sit higher on the chart")
	}
}

// Every point landing on the same value would divide by a zero-width axis.
func TestChartSurvivesADegenerateGrid(t *testing.T) {
	flat := newScale([]analysis.Point{{Kbps: 3000, VMAF: 90}, {Kbps: 3000, VMAF: 90}})
	for _, v := range []float64{flat.x(3000), flat.y(90)} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("a flat grid produced %v", v)
		}
	}
	r := sampleReport()
	r.Analyses = []analysis.Result{analysis.Analyze("x264",
		[]analysis.Point{{Encoder: "x264", Height: 1080, Target: 3000, Kbps: 3000, VMAF: 90}}, r.Options)}
	if err := Chart(&bytes.Buffer{}, r, ""); err == nil {
		t.Error("one point is not a curve and should be refused, not drawn")
	}
}

func TestChartRefusesToGuessTheEncoder(t *testing.T) {
	if err := Chart(&bytes.Buffer{}, challengerReport(), ""); err == nil {
		t.Error("a two-encoder report must not be charted on a guess")
	}
	var buf bytes.Buffer
	if err := Chart(&buf, challengerReport(), "svt-av1"); err != nil {
		t.Fatalf("naming an encoder should work: %v", err)
	}
	if !strings.Contains(buf.String(), "svt-av1") {
		t.Error("the chart should name the encoder it drew")
	}
	if err := Chart(&bytes.Buffer{}, challengerReport(), "nvenc"); err == nil {
		t.Error("an encoder that was never measured must be an error")
	}
}
