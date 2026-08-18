package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
)

// twoRuns builds a baseline and a current report of the same experiment, where
// the current one needs `factor` times the bitrate for the same quality.
func twoRuns(factor float64, sameConfig bool) (Report, Report) {
	base := sampleReport()
	base.Generated = "2026-07-01T10:00:00Z"

	cur := sampleReport()
	cur.Generated = "2026-08-01T10:00:00Z"
	var scaled []analysis.Point
	for _, c := range base.Analyses[0].Curves {
		for _, p := range c.Points {
			p.Kbps *= factor
			scaled = append(scaled, p)
		}
	}
	cur.Analyses = []analysis.Result{analysis.Analyze("x264", scaled, cur.Options)}
	cur.Env.LibVMAF = []string{"3.0.1"}
	if !sameConfig {
		cur.Env.ConfigSHA256 = "0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff"
	}
	return base, cur
}

func TestCompareReportsTheCostOfTheChange(t *testing.T) {
	base, cur := twoRuns(1.12, true)
	c := Compare("baseline.json", base, "current.json", cur, 2)
	if !c.Comparable {
		t.Fatal("the same config fingerprint must compare")
	}
	if len(c.Encoders) != 1 {
		t.Fatalf("compared %d encoders, want 1", len(c.Encoders))
	}
	if !c.Regressed() {
		t.Errorf("a +12%% run past a 2%% threshold must regress: %+v", c.Regressions)
	}

	var text, md bytes.Buffer
	if err := RenderComparison(&text, c, "text"); err != nil {
		t.Fatalf("text: %v", err)
	}
	if err := RenderComparison(&md, c, "markdown"); err != nil {
		t.Fatalf("markdown: %v", err)
	}
	for _, want := range []string{"baseline.json", "current.json", "frontier", "per point", "REGRESSION"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text comparison is missing %q:\n%s", want, text.String())
		}
	}
	for _, want := range []string{"# ladder-bench comparison", "### Per point", "## Regression"} {
		if !strings.Contains(md.String(), want) {
			t.Errorf("markdown comparison is missing %q", want)
		}
	}
	// Both libvmaf versions are shown: an upgrade between the runs is the most
	// likely explanation for anything that moved.
	if !strings.Contains(md.String(), "3.0.0") || !strings.Contains(md.String(), "3.0.1") {
		t.Errorf("both libvmaf versions should be visible:\n%s", md.String())
	}
}

// Two runs of different experiments differ in ways that say nothing about better
// or worse, so the mismatch is the finding — and the gate must not pass on it.
func TestCompareRefusesToJudgeDifferentExperiments(t *testing.T) {
	base, cur := twoRuns(1.5, false)
	c := Compare("baseline.json", base, "current.json", cur, 2)
	if c.Comparable {
		t.Fatal("different config fingerprints must not compare")
	}
	if c.Regressed() {
		t.Errorf("an incomparable pair must not be judged: %+v", c.Regressions)
	}
	var buf bytes.Buffer
	if err := RenderComparison(&buf, c, "text"); err != nil {
		t.Fatalf("text: %v", err)
	}
	if !strings.Contains(buf.String(), "did not measure the same experiment") {
		t.Errorf("the mismatch should lead the report:\n%s", buf.String())
	}
	// The tables are still there — a reader may well want them — they just do not
	// answer the question.
	if !strings.Contains(buf.String(), "per point") {
		t.Error("the comparison should still be shown")
	}
}

// A run with no fingerprint at all cannot be gated on either: absent is not the
// same as matching.
func TestCompareTreatsAMissingFingerprintAsIncomparable(t *testing.T) {
	base, cur := twoRuns(1.5, true)
	base.Env.ConfigSHA256 = ""
	cur.Env.ConfigSHA256 = ""
	c := Compare("old.json", base, "new.json", cur, 2)
	if c.Comparable {
		t.Error("two reports without fingerprints must not be declared comparable")
	}
	if c.Regressed() {
		t.Errorf("and must not be judged: %+v", c.Regressions)
	}
}

func TestCompareNamesEncodersOnlyOneRunHas(t *testing.T) {
	base, cur := twoRuns(1.0, true)
	// The current run dropped x264 and measured something else instead.
	var pts []analysis.Point
	for _, c := range base.Analyses[0].Curves {
		for _, p := range c.Points {
			p.Encoder = "svt-av1"
			pts = append(pts, p)
		}
	}
	cur.Analyses = []analysis.Result{analysis.Analyze("svt-av1", pts, cur.Options)}

	c := Compare("a.json", base, "b.json", cur, 2)
	if len(c.OnlyInBaseline) != 1 || c.OnlyInBaseline[0] != "x264" {
		t.Errorf("only_in_baseline = %v, want [x264]", c.OnlyInBaseline)
	}
	if len(c.OnlyInCurrent) != 1 || c.OnlyInCurrent[0] != "svt-av1" {
		t.Errorf("only_in_current = %v, want [svt-av1]", c.OnlyInCurrent)
	}
	if len(c.Encoders) != 0 {
		t.Errorf("nothing is comparable here: %+v", c.Encoders)
	}
	var buf bytes.Buffer
	if err := RenderComparison(&buf, c, "text"); err != nil {
		t.Fatalf("text: %v", err)
	}
	for _, want := range []string{"x264 is in the baseline and not in this run", "svt-av1 is new in this run"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q:\n%s", want, buf.String())
		}
	}
}

func TestComparisonJSONRoundTrips(t *testing.T) {
	base, cur := twoRuns(1.05, true)
	var buf bytes.Buffer
	if err := RenderComparison(&buf, Compare("a.json", base, "b.json", cur, 2), "json"); err != nil {
		t.Fatalf("json: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	for _, key := range []string{"tool", "baseline", "current", "comparable", "encoders", "threshold"} {
		if _, ok := back[key]; !ok {
			t.Errorf("comparison JSON is missing %q", key)
		}
	}
	if err := RenderComparison(&bytes.Buffer{}, Compare("a.json", base, "b.json", cur, 2), "pdf"); err == nil {
		t.Error("an unknown format should be refused")
	}
}

// A report is only comparable if it can be read back, so the JSON a run writes
// has to survive a round trip through the type that reads it.
func TestReportSurvivesItsOwnJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, challengerReport()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.DisallowUnknownFields()
	var back Report
	if err := dec.Decode(&back); err != nil {
		t.Fatalf("a report written by this build cannot be read by it: %v", err)
	}
	if len(back.Analyses) != 2 || len(back.Results) != len(challengerReport().Results) {
		t.Errorf("round trip lost content: %d analyses, %d results", len(back.Analyses), len(back.Results))
	}
	if back.Env.ConfigSHA256 != challengerReport().Env.ConfigSHA256 {
		t.Error("the config fingerprint did not survive, so no comparison could ever be gated")
	}
	if len(back.BDRates) != 1 {
		t.Errorf("bd_rates did not survive: %+v", back.BDRates)
	}
}
