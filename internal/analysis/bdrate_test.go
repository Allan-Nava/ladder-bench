package analysis

import (
	"math"
	"strings"
	"testing"
)

// scaled returns the same curve at a constant fraction of the bitrate: the same
// quality for less money at every single point.
func scaled(points []Point, factor float64) []Point {
	out := make([]Point, len(points))
	for i, p := range points {
		out[i] = p
		out[i].Kbps = p.Kbps * factor
		out[i].Target = int(p.Kbps * factor)
	}
	return out
}

func ladderCurve() []Point {
	return []Point{
		p(1080, 1000, 72), p(1080, 2000, 84), p(1080, 3000, 90),
		p(1080, 4000, 93), p(1080, 5000, 94.5), p(1080, 6000, 95.2),
	}
}

// A curve that costs a constant 80% of the anchor at every quality is a -20%
// BD-rate, exactly — whichever way the area under it was computed.
func TestBDRateOnAConstantBitrateRatio(t *testing.T) {
	anchor := ladderCurve()
	bd := BDRate(anchor, scaled(anchor, 0.8))
	if !bd.OK() {
		t.Fatalf("BD-rate declined a clean comparison: %s", bd.Note)
	}
	if math.Abs(bd.RatePct-(-20)) > 1e-6 {
		t.Errorf("BD-rate = %.4f%%, want -20%%", bd.RatePct)
	}
	if bd.Method != BDMethodCubic {
		t.Errorf("method = %q, want a cubic fit for six points", bd.Method)
	}
	if bd.LowVMAF != 72 || bd.HighVMAF != 95.2 {
		t.Errorf("interval = [%.1f, %.1f], want the full shared range", bd.LowVMAF, bd.HighVMAF)
	}
}

// The same ratio, on a curve too short to fit a cubic through. Three points is
// an interpolation, and it must say so rather than dressing up as a fit.
func TestBDRateFallsBackToInterpolation(t *testing.T) {
	anchor := []Point{p(1080, 2000, 84), p(1080, 3000, 90), p(1080, 4000, 93)}
	bd := BDRate(anchor, scaled(anchor, 0.75))
	if !bd.OK() {
		t.Fatalf("BD-rate declined a three-point comparison: %s", bd.Note)
	}
	if math.Abs(bd.RatePct-(-25)) > 1e-6 {
		t.Errorf("BD-rate = %.4f%%, want -25%%", bd.RatePct)
	}
	if bd.Method != BDMethodLinear {
		t.Errorf("method = %q, want %q", bd.Method, BDMethodLinear)
	}
}

// The sign is the whole message: negative means the challenger ships the same
// quality for fewer bits.
func TestBDRateSignFollowsTheBits(t *testing.T) {
	anchor := ladderCurve()
	if bd := BDRate(anchor, scaled(anchor, 1.3)); bd.RatePct <= 0 {
		t.Errorf("a costlier encoder must read positive, got %.2f%%", bd.RatePct)
	}
	if bd := BDRate(anchor, anchor); math.Abs(bd.RatePct) > 1e-9 {
		t.Errorf("a curve against itself is 0%%, got %.6f%%", bd.RatePct)
	}
}

// BD-rate averages over the quality both encoders reached, never over the union:
// the anchor's cheap end has no counterpart to be compared against.
func TestBDRateUsesOnlyTheSharedInterval(t *testing.T) {
	anchor := ladderCurve() // 72 … 95.2
	test := []Point{
		p(1080, 2500, 86), p(1080, 3200, 90), p(1080, 4100, 92.5), p(1080, 5200, 94),
	}
	bd := BDRate(anchor, test)
	if !bd.OK() {
		t.Fatalf("BD-rate declined a comparison with a real overlap: %s", bd.Note)
	}
	if bd.LowVMAF != 86 || bd.HighVMAF != 94 {
		t.Errorf("interval = [%.1f, %.1f], want [86.0, 94.0]", bd.LowVMAF, bd.HighVMAF)
	}
}

// Two grids that barely meet produce a percentage that looks like a verdict and
// is really the noise of a couple of frames. Declining is the honest answer.
func TestBDRateDeclinesATinyOverlap(t *testing.T) {
	anchor := []Point{p(1080, 1000, 70), p(1080, 2000, 80), p(1080, 3000, 86)}
	test := []Point{p(1080, 4000, 86.4), p(1080, 5000, 90), p(1080, 6000, 92)}
	bd := BDRate(anchor, test)
	if bd.OK() {
		t.Errorf("a 0.4 VMAF overlap must be declined, got %.2f%%", bd.RatePct)
	}
	if bd.RatePct != 0 {
		t.Errorf("a declined comparison must not carry a number, got %.2f", bd.RatePct)
	}
	if !strings.Contains(bd.Note, "VMAF") {
		t.Errorf("note = %q, should say what was missing", bd.Note)
	}
}

func TestBDRateNeedsTwoCurves(t *testing.T) {
	one := []Point{p(1080, 3000, 90)}
	bd := BDRate(one, ladderCurve())
	if bd.OK() || bd.Note == "" {
		t.Errorf("a single point is not a curve, got %+v", bd)
	}
	if bd := BDRate(nil, nil); bd.OK() {
		t.Error("two empty curves must not compare")
	}
}

// A point with no measured size never really was measured, and log10(0) is not
// a number. Dropping it must not take the comparison down with it.
func TestBDRateSkipsPointsWithoutABitrate(t *testing.T) {
	anchor := append(ladderCurve(), Point{Height: 1080, Target: 7000, Kbps: 0, VMAF: 99})
	bd := BDRate(anchor, scaled(ladderCurve(), 0.8))
	if !bd.OK() {
		t.Fatalf("a zero-bitrate point took the comparison down: %s", bd.Note)
	}
	if bd.AnchorPoints != 6 {
		t.Errorf("anchor points = %d, want the six real measurements", bd.AnchorPoints)
	}
	if bd.HighVMAF != 95.2 {
		t.Errorf("high = %.1f, want the top of the real measurements", bd.HighVMAF)
	}
}

func TestBDRatesComparesEveryEncoderAgainstTheAnchor(t *testing.T) {
	opt := Options{KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6}
	base := ladderCurve()
	results := []Result{
		Analyze("x264", base, opt),
		Analyze("svt-av1", scaled(base, 0.7), opt),
	}
	got := BDRates("x264", results)
	if len(got) != 1 {
		t.Fatalf("comparisons = %+v, want one challenger", got)
	}
	c := got[0]
	if c.Anchor != "x264" || c.Test != "svt-av1" {
		t.Errorf("comparison is %s vs %s", c.Anchor, c.Test)
	}
	if !c.Frontier.OK() || c.Frontier.RatePct >= 0 {
		t.Errorf("frontier BD-rate = %+v, want a saving", c.Frontier)
	}
	if len(c.ByHeight) != 1 || c.ByHeight[0].Height != 1080 {
		t.Fatalf("per-resolution figures = %+v, want 1080p", c.ByHeight)
	}
	if math.Abs(c.ByHeight[0].RatePct-(-30)) > 1e-6 {
		t.Errorf("1080p BD-rate = %.4f%%, want -30%%", c.ByHeight[0].RatePct)
	}
}

func TestBDRatesNeedsAnAnchorAndAChallenger(t *testing.T) {
	opt := Options{KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6}
	one := []Result{Analyze("x264", ladderCurve(), opt)}
	if got := BDRates("x264", one); got != nil {
		t.Errorf("a single encoder has nothing to compare against, got %+v", got)
	}
	two := append(one, Analyze("svt-av1", ladderCurve(), opt))
	if got := BDRates("nvenc", two); got != nil {
		t.Errorf("an anchor that was never measured must produce nothing, got %+v", got)
	}
}

// Four points spread over two quality levels make the cubic's normal matrix
// singular. The fallback then finds two bitrates at one VMAF, which is not a
// function of quality — so the answer is a note, not a number pulled out of a
// system that had no solution.
func TestBDRateDeclinesACurveItCannotIntegrate(t *testing.T) {
	// Two pairs of encodes that happened to score identically.
	tied := []Point{p(1080, 2000, 80), p(1080, 2400, 80), p(1080, 3000, 90), p(1080, 3600, 90)}
	bd := BDRate(tied, ladderCurve())
	if bd.OK() {
		t.Errorf("a curve with two bitrates at one quality cannot be integrated, got %+v", bd)
	}
	if !strings.Contains(bd.Note, "anchor") {
		t.Errorf("note = %q, should say which curve failed", bd.Note)
	}
	// The same curve as the challenger must fail the same way, and say so.
	if bd := BDRate(ladderCurve(), tied); bd.OK() || !strings.Contains(bd.Note, "test") {
		t.Errorf("note = %q, should point at the test curve", bd.Note)
	}
}

// A curve measured entirely at one quality has no interval to average over at
// all — it is declined before any integration is attempted.
func TestBDRateDeclinesACurveWithNoQualityRange(t *testing.T) {
	flat := []Point{p(1080, 1000, 90), p(1080, 2000, 90), p(1080, 3000, 90), p(1080, 4000, 90)}
	if bd := BDRate(flat, ladderCurve()); bd.OK() {
		t.Errorf("a curve with no quality range cannot be compared, got %+v", bd)
	}
}
