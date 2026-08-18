package output

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
)

func TestBuildLadderDerivesWidthAndPeak(t *testing.T) {
	l, err := BuildLadder(sampleReport(), "")
	if err != nil {
		t.Fatalf("BuildLadder: %v", err)
	}
	if l.Encoder != "x264" || l.Codec != "libx264" || l.Preset != "slow" {
		t.Errorf("ladder identity = %+v", l)
	}
	if len(l.Rungs) == 0 {
		t.Fatal("no rungs")
	}
	for _, r := range l.Rungs {
		// The reference is 1920x1080, so a 720p rung is 1280 wide and a 1080p one
		// 1920 — the same arithmetic `scale=-2:H` does.
		want := map[int]int{1080: 1920, 720: 1280}[r.Height]
		if r.Width != want {
			t.Errorf("%dp width = %d, want %d", r.Height, r.Width, want)
		}
		// The peak a player must sustain is the cap the encode was given, not the
		// average it came out at.
		if r.PeakKbps != r.TargetKbps*110/100 {
			t.Errorf("%dp peak = %d, want 110%% of %d", r.Height, r.PeakKbps, r.TargetKbps)
		}
		if r.Kbps <= 0 || r.VMAF <= 0 {
			t.Errorf("%dp lost its measurement: %+v", r.Height, r)
		}
	}
	// The fingerprint travels with the ladder, so a playlist in production can be
	// traced back to the measurement behind it.
	if l.ConfigSHA256 != sampleReport().Env.ConfigSHA256 {
		t.Error("the config fingerprint did not reach the ladder")
	}
}

// With no geometry to derive from, the width is absent rather than invented — an
// SVG or a playlist with a made-up RESOLUTION is worse than one without.
func TestBuildLadderOmitsWidthWithoutGeometry(t *testing.T) {
	r := sampleReport()
	r.References = nil
	l, err := BuildLadder(r, "")
	if err != nil {
		t.Fatalf("BuildLadder: %v", err)
	}
	for _, rung := range l.Rungs {
		if rung.Width != 0 {
			t.Errorf("%dp invented a width of %d", rung.Height, rung.Width)
		}
	}
	var buf bytes.Buffer
	if err := ExportLadder(&buf, l, "hls"); err != nil {
		t.Fatalf("hls: %v", err)
	}
	if strings.Contains(buf.String(), "RESOLUTION") {
		t.Errorf("no geometry, so no RESOLUTION attribute:\n%s", buf.String())
	}
}

// Which codec you ship is not something to guess at: guessing wrong produces a
// playlist that looks right and describes a different experiment.
func TestBuildLadderRefusesToGuessTheEncoder(t *testing.T) {
	multi := challengerReport()
	if _, err := BuildLadder(multi, ""); err == nil {
		t.Error("a two-encoder report must not pick one on its own")
	} else if !strings.Contains(err.Error(), "svt-av1") || !strings.Contains(err.Error(), "x264") {
		t.Errorf("the error should list the choices, got: %v", err)
	}
	if l, err := BuildLadder(multi, "svt-av1"); err != nil || l.Encoder != "svt-av1" {
		t.Errorf("naming an encoder should work: %+v %v", l, err)
	}
	if _, err := BuildLadder(multi, "nvenc"); err == nil {
		t.Error("an encoder that was never measured must be an error")
	}
}

// The CODECS attribute carries a profile and level this tool does not measure.
// Leaving it out and saying so beats emitting a string players reject in ways
// that look like content problems.
func TestPlaylistExportsLeaveCodecsToBeFilledIn(t *testing.T) {
	l, err := BuildLadder(sampleReport(), "")
	if err != nil {
		t.Fatalf("BuildLadder: %v", err)
	}
	var hls, dash bytes.Buffer
	if err := ExportLadder(&hls, l, "hls"); err != nil {
		t.Fatalf("hls: %v", err)
	}
	if err := ExportLadder(&dash, l, "dash"); err != nil {
		t.Fatalf("dash: %v", err)
	}
	for _, want := range []string{"#EXTM3U", "#EXT-X-STREAM-INF:BANDWIDTH=", "AVERAGE-BANDWIDTH=", "RESOLUTION=1920x1080", "ffprobe"} {
		if !strings.Contains(hls.String(), want) {
			t.Errorf("HLS export is missing %q:\n%s", want, hls.String())
		}
	}
	if strings.Contains(hls.String(), "CODECS=") {
		t.Errorf("HLS must not invent a CODECS string:\n%s", hls.String())
	}
	for _, want := range []string{"<AdaptationSet", "<Representation", `codecs="TODO"`, "ffprobe"} {
		if !strings.Contains(dash.String(), want) {
			t.Errorf("DASH export is missing %q:\n%s", want, dash.String())
		}
	}
	// The fragment has to parse, or it cannot be pasted into a manifest.
	var set struct {
		Representations []struct {
			ID        string `xml:"id,attr"`
			Bandwidth int    `xml:"bandwidth,attr"`
			Width     int    `xml:"width,attr"`
			Height    int    `xml:"height,attr"`
		} `xml:"Representation"`
	}
	if err := xml.Unmarshal(dash.Bytes(), &set); err != nil {
		t.Fatalf("the DASH fragment is not valid XML: %v\n%s", err, dash.String())
	}
	if len(set.Representations) != len(l.Rungs) {
		t.Fatalf("got %d representations, want %d", len(set.Representations), len(l.Rungs))
	}
	// Cheapest first in a manifest — the opposite of the report's order.
	for i := 1; i < len(set.Representations); i++ {
		if set.Representations[i].Bandwidth < set.Representations[i-1].Bandwidth {
			t.Errorf("representations are not cheapest first: %+v", set.Representations)
		}
	}
}

// A recommended ladder can hold two rungs at the same resolution, and two
// playlist entries pointing at one URI is a playlist players cannot use.
func TestExportsIdentifyEveryRungUniquely(t *testing.T) {
	r := sampleReport()
	// A ladder with two 1080p rungs, which is what the frontier produces when a
	// resolution wins at both ends of the grid.
	opt := r.Options
	opt.LadderStep = 4
	r.Analyses = []analysis.Result{analysis.Analyze("x264", []analysis.Point{
		{Encoder: "x264", Height: 1080, Target: 1500, Kbps: 1510, VMAF: 80},
		{Encoder: "x264", Height: 1080, Target: 3000, Kbps: 3010, VMAF: 88},
		{Encoder: "x264", Height: 1080, Target: 6000, Kbps: 6020, VMAF: 94},
	}, opt)}
	r.Options = opt
	l, err := BuildLadder(r, "")
	if err != nil {
		t.Fatalf("BuildLadder: %v", err)
	}
	if len(l.Rungs) < 2 {
		t.Fatalf("this fixture needs at least two rungs, got %d", len(l.Rungs))
	}
	sameHeight := 0
	for _, rung := range l.Rungs {
		if rung.Height == 1080 {
			sameHeight++
		}
	}
	if sameHeight < 2 {
		t.Fatalf("this fixture needs two rungs at one height, got %d", sameHeight)
	}

	var hls bytes.Buffer
	if err := ExportLadder(&hls, l, "hls"); err != nil {
		t.Fatalf("hls: %v", err)
	}
	uris := map[string]bool{}
	for _, line := range strings.Split(hls.String(), "\n") {
		if strings.HasSuffix(line, ".m3u8") {
			if uris[line] {
				t.Errorf("two rungs point at the same URI %q:\n%s", line, hls.String())
			}
			uris[line] = true
		}
	}
	if len(uris) != len(l.Rungs) {
		t.Errorf("%d URIs for %d rungs", len(uris), len(l.Rungs))
	}

	var dash bytes.Buffer
	if err := ExportLadder(&dash, l, "dash"); err != nil {
		t.Fatalf("dash: %v", err)
	}
	var set struct {
		Representations []struct {
			ID string `xml:"id,attr"`
		} `xml:"Representation"`
	}
	if err := xml.Unmarshal(dash.Bytes(), &set); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	ids := map[string]bool{}
	for _, rep := range set.Representations {
		if ids[rep.ID] {
			t.Errorf("duplicate Representation id %q", rep.ID)
		}
		ids[rep.ID] = true
	}
}

func TestJSONExportRoundTrips(t *testing.T) {
	l, err := BuildLadder(sampleReport(), "")
	if err != nil {
		t.Fatalf("BuildLadder: %v", err)
	}
	var buf bytes.Buffer
	if err := ExportLadder(&buf, l, "json"); err != nil {
		t.Fatalf("json: %v", err)
	}
	var back Ladder
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if len(back.Rungs) != len(l.Rungs) || back.Encoder != l.Encoder {
		t.Errorf("round trip lost content: %+v", back)
	}
	if err := ExportLadder(&bytes.Buffer{}, l, "mpeg"); err == nil {
		t.Error("an unknown format should be refused")
	}
}

// A ladder from a run that never reached its target is still a ladder, and it has
// to say so where somebody about to ship it will see it.
func TestExportWarnsWhenTheTargetWasNotReached(t *testing.T) {
	r := sampleReport()
	var low []analysis.Point
	for _, c := range r.Analyses[0].Curves {
		for _, p := range c.Points {
			p.VMAF -= 20
			low = append(low, p)
		}
	}
	r.Analyses = []analysis.Result{analysis.Analyze("x264", low, r.Options)}
	l, err := BuildLadder(r, "")
	if err != nil {
		t.Fatalf("BuildLadder: %v", err)
	}
	if l.TargetReached {
		t.Fatal("this grid does not reach 93")
	}
	for _, format := range []string{"hls", "dash"} {
		var buf bytes.Buffer
		if err := ExportLadder(&buf, l, format); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if !strings.Contains(buf.String(), "WARNING") {
			t.Errorf("%s export should warn about the unreached target:\n%s", format, buf.String())
		}
	}
}
