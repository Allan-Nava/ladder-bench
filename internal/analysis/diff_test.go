package analysis

import (
	"math"
	"testing"
)

func runResult(encoder string, factor float64, opt Options) Result {
	base := []Point{
		p(1080, 1500, 86), p(1080, 3000, 92), p(1080, 6000, 96),
		p(720, 800, 79), p(720, 1500, 85), p(720, 3000, 88),
	}
	pts := make([]Point, len(base))
	for i, q := range base {
		q.Encoder = encoder
		q.Kbps *= factor
		pts[i] = q
	}
	return Analyze(encoder, pts, opt)
}

// The headline of a comparison is the same arithmetic as the cross-encoder
// BD-rate, pointed at time instead of at a competitor.
func TestDiffReportsTheCostOfTheChange(t *testing.T) {
	opt := Options{KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6}
	base := runResult("x264", 1.0, opt)
	// The same quality for 10% more bitrate everywhere: a clean regression.
	worse := runResult("x264", 1.10, opt)

	d := Diff(base, worse)
	if !d.BDRate.OK() {
		t.Fatalf("BD-rate declined a clean comparison: %s", d.BDRate.Note)
	}
	if math.Abs(d.BDRate.RatePct-10) > 1e-6 {
		t.Errorf("BD-rate = %.4f%%, want +10%%", d.BDRate.RatePct)
	}
	// Cheaper reads negative, the way every other percentage in the tool does.
	if better := Diff(base, runResult("x264", 0.8, opt)); math.Abs(better.BDRate.RatePct-(-20)) > 1e-6 {
		t.Errorf("a cheaper run = %.4f%%, want -20%%", better.BDRate.RatePct)
	}
	if same := Diff(base, base); math.Abs(same.BDRate.RatePct) > 1e-9 {
		t.Errorf("a run against itself = %.6f%%, want 0", same.BDRate.RatePct)
	}
}

// A grid coordinate only one run measured is named, never dropped: a comparison
// that quietly ignores half a curve is worse than one that says so.
func TestDiffFlagsPointsOnlyOneRunHas(t *testing.T) {
	opt := Options{KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6}
	base := runResult("x264", 1.0, opt)
	narrower := Analyze("x264", []Point{
		p(1080, 1500, 86), p(1080, 3000, 92), p(1080, 6000, 96),
		p(720, 800, 79), p(720, 1500, 85), // 720p@3000 gone
		p(480, 400, 70), // and a new rung
	}, opt)

	d := Diff(base, narrower)
	var missing, added, compared int
	for _, pt := range d.Points {
		switch pt.Note {
		case "not measured in this run":
			missing++
			if pt.Height != 720 || pt.Target != 3000 {
				t.Errorf("wrong point reported missing: %+v", pt)
			}
		case "new in this run":
			added++
			if pt.Height != 480 {
				t.Errorf("wrong point reported new: %+v", pt)
			}
		default:
			compared++
		}
	}
	if missing != 1 || added != 1 || compared != 5 {
		t.Errorf("missing=%d added=%d compared=%d, want 1/1/5", missing, added, compared)
	}
	// An uncomparable point carries no deltas to be read by mistake.
	for _, pt := range d.Points {
		if !pt.Comparable() && (pt.DeltaVMAF() != 0 && pt.BaseVMAF != 0 && pt.VMAF != 0) {
			t.Errorf("an uncomparable point should not carry a delta: %+v", pt)
		}
	}
}

// Pairing a 1080p rung against a 720p one because they sit at the same index
// would invent a comparison. When the shape changed, that is the finding.
func TestDiffRefusesToPairADifferentLadder(t *testing.T) {
	opt := Options{KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6}
	base := runResult("x264", 1.0, opt)
	same := Diff(base, runResult("x264", 1.05, opt))
	if same.LadderChanged {
		t.Errorf("the same ladder at another bitrate is not a shape change: %+v", same.Ladder)
	}
	if len(same.Ladder) != len(base.Ladder) {
		t.Fatalf("paired %d rungs, want %d", len(same.Ladder), len(base.Ladder))
	}
	for _, r := range same.Ladder {
		if math.Abs(r.DeltaKbpsPct()-5) > 1e-6 {
			t.Errorf("rung %dp changed %.4f%%, want +5%%", r.Height, r.DeltaKbpsPct())
		}
	}

	shorter := Analyze("x264", []Point{p(1080, 3000, 92), p(1080, 6000, 96)}, opt)
	changed := Diff(base, shorter)
	if !changed.LadderChanged {
		t.Error("a ladder with different rungs must not be paired")
	}
	if len(changed.Ladder) != 0 {
		t.Errorf("a changed ladder must carry no per-rung deltas: %+v", changed.Ladder)
	}
	if len(changed.BaseLadder) == 0 || len(changed.CurrentLadder) == 0 {
		t.Error("both ladders should be carried so the reader can see the change")
	}
}

// Only two things count as a regression, and both are about the answer rather
// than about the experiment.
func TestRegressions(t *testing.T) {
	opt := Options{KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6}
	base := runResult("x264", 1.0, opt)

	// Well past the threshold.
	if got := Regressions([]EncoderDiff{Diff(base, runResult("x264", 1.10, opt))}, 2); len(got) != 1 {
		t.Fatalf("a +10%% run should regress: %+v", got)
	}
	// Inside it: encoders are not bit-exact across runs, and a gate that fires on
	// that noise gets switched off.
	if got := Regressions([]EncoderDiff{Diff(base, runResult("x264", 1.005, opt))}, 2); len(got) != 0 {
		t.Errorf("half a percent is noise, not a regression: %+v", got)
	}
	// Cheaper is never a regression.
	if got := Regressions([]EncoderDiff{Diff(base, runResult("x264", 0.9, opt))}, 2); len(got) != 0 {
		t.Errorf("a cheaper run must not regress: %+v", got)
	}
	// A zero threshold falls back to the default rather than failing everything.
	if got := Regressions([]EncoderDiff{Diff(base, runResult("x264", 1.005, opt))}, 0); len(got) != 0 {
		t.Errorf("threshold 0 should mean the default, got %+v", got)
	}

	// Losing the target is not a matter of degree: the grid stopped answering.
	lost := Diff(base, Analyze("x264", []Point{p(1080, 1500, 80), p(1080, 3000, 86)}, opt))
	got := Regressions([]EncoderDiff{lost}, 2)
	if len(got) == 0 {
		t.Fatal("a run that no longer reaches the target must regress")
	}
	if !lost.TargetLost() {
		t.Error("TargetLost should be true")
	}
	found := false
	for _, r := range got {
		if r.Value == 0 && r.Threshold == 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("the lost target is not a percentage and should carry no value: %+v", got)
	}
}
