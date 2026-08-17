package analysis

import (
	"math"
	"testing"
)

func p(height int, kbps, vmaf float64) Point {
	return Point{Height: height, Target: int(kbps), Kbps: kbps, VMAF: vmaf}
}

func kbpsOf(points []Point) []float64 {
	out := make([]float64, len(points))
	for i, pt := range points {
		out[i] = pt.Kbps
	}
	return out
}

func eq(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHullDropsPointsUnderTheFrontier(t *testing.T) {
	// 720p wins the low end, 1080p the high end; the 1080p point at 1000k is
	// worse than the 720p one at the same cost and must not survive.
	points := []Point{
		p(720, 500, 70), p(720, 1000, 82), p(720, 2000, 88), p(720, 3000, 89),
		p(1080, 1000, 74), p(1080, 2000, 87), p(1080, 3000, 93), p(1080, 4000, 94.5),
	}
	hull := Hull(points)
	for _, pt := range hull {
		if pt.Kbps == 1000 && pt.Height == 1080 {
			t.Fatalf("a dominated point reached the frontier: %+v", hull)
		}
	}
	// Monotone: the frontier never costs more for less.
	for i := 1; i < len(hull); i++ {
		if hull[i].Kbps <= hull[i-1].Kbps || hull[i].VMAF <= hull[i-1].VMAF {
			t.Fatalf("frontier is not monotone at %d: %+v", i, hull)
		}
	}
	if hull[0].Kbps != 500 {
		t.Errorf("the cheapest measured point is always on the frontier, got %v", kbpsOf(hull))
	}
}

func TestHullCutsTheDownhillTail(t *testing.T) {
	// A grid can contain a point that costs more and scores lower (rate
	// control overshoot, a bad preset). Recommending it would read as "pay
	// more, see less".
	hull := Hull([]Point{p(1080, 1000, 80), p(1080, 2000, 90), p(1080, 3000, 88)})
	if got := kbpsOf(hull); !eq(got, []float64{1000, 2000}) {
		t.Errorf("hull = %v, want the downhill tail dropped", got)
	}
}

func TestHullHandlesTinyInputs(t *testing.T) {
	if got := Hull(nil); len(got) != 0 {
		t.Errorf("Hull(nil) = %v", got)
	}
	one := []Point{p(720, 1000, 80)}
	if got := Hull(one); len(got) != 1 {
		t.Errorf("Hull(one point) = %v", got)
	}
}

func TestGainIsRelative(t *testing.T) {
	// +10% of 1000k buying 2 VMAF points is a gain of 2.
	if got := Gain(p(720, 1000, 80), p(720, 1100, 82)); math.Abs(got-2) > 1e-9 {
		t.Errorf("Gain = %v, want 2", got)
	}
	// The same +100k at 5000k is only +2%, so it buys a fifth as much.
	if got := Gain(p(720, 5000, 80), p(720, 5100, 82)); math.Abs(got-10) > 1e-9 {
		t.Errorf("Gain = %v, want 10 VMAF points per +10%%", got)
	}
	if got := Gain(p(720, 1000, 80), p(720, 1000, 90)); got != 0 {
		t.Errorf("a zero-width step must not divide by zero, got %v", got)
	}
}

func TestKnee(t *testing.T) {
	// Climbs to 3000k, then flattens.
	points := []Point{
		p(1080, 1000, 70), p(1080, 2000, 85), p(1080, 3000, 92),
		p(1080, 4000, 92.6), p(1080, 5000, 92.9), p(1080, 6000, 93.0),
	}
	knee, ok := Knee(points, 0.5)
	if !ok {
		t.Fatal("no knee found on a curve that clearly flattens")
	}
	if knee.Kbps != 3000 {
		t.Errorf("knee = %v, want 3000", knee.Kbps)
	}
}

// A single noisy step in the middle must not stop the scan early: the knee is
// the last place the curve still pays, not the first place it hesitates.
func TestKneeIgnoresAMidCurveDip(t *testing.T) {
	points := []Point{
		p(1080, 1000, 70), p(1080, 1500, 70.2), p(1080, 2000, 80),
		p(1080, 3000, 92), p(1080, 4000, 92.2),
	}
	knee, ok := Knee(points, 0.5)
	if !ok || knee.Kbps != 3000 {
		t.Errorf("knee = %v (ok=%v), want 3000", knee.Kbps, ok)
	}
}

func TestKneeEdges(t *testing.T) {
	climbing := []Point{p(720, 1000, 60), p(720, 2000, 75), p(720, 3000, 88)}
	if _, ok := Knee(climbing, 0.5); ok {
		t.Error("a curve still climbing at the top of the grid has no knee")
	}
	flat := []Point{p(720, 3000, 95), p(720, 4000, 95.1), p(720, 5000, 95.2)}
	knee, ok := Knee(flat, 0.5)
	if !ok || knee.Kbps != 3000 {
		t.Errorf("a flat curve saturates at its cheapest point, got %v (ok=%v)", knee.Kbps, ok)
	}
	if _, ok := Knee([]Point{p(720, 1000, 80)}, 0.5); ok {
		t.Error("one point is not a curve")
	}
}

func TestInterpolateVMAFRefusesToExtrapolate(t *testing.T) {
	points := []Point{p(1080, 2000, 80), p(1080, 4000, 90)}
	got, ok := InterpolateVMAF(points, 3000)
	if !ok || math.Abs(got-85) > 1e-9 {
		t.Errorf("InterpolateVMAF(3000) = %v (ok=%v), want 85", got, ok)
	}
	if _, ok := InterpolateVMAF(points, 5000); ok {
		t.Error("extrapolating above the grid must fail, not guess")
	}
	if _, ok := InterpolateVMAF(points, 1000); ok {
		t.Error("extrapolating below the grid must fail, not guess")
	}
}

func TestInterpolateBitrate(t *testing.T) {
	hull := []Point{p(480, 1000, 70), p(720, 2000, 80), p(1080, 4000, 90)}
	kbps, height, ok := InterpolateBitrate(hull, 85)
	if !ok || math.Abs(kbps-3000) > 1e-9 {
		t.Errorf("InterpolateBitrate(85) = %v (ok=%v), want 3000", kbps, ok)
	}
	if height != 1080 {
		t.Errorf("height = %d, want the resolution that delivers it (1080)", height)
	}
	if _, _, ok := InterpolateBitrate(hull, 95); ok {
		t.Error("a quality above the frontier must not be reported as reachable")
	}
}

func TestRecommendSpacesRungsByQuality(t *testing.T) {
	hull := []Point{
		p(360, 400, 68), p(480, 800, 76), p(720, 1500, 84),
		p(1080, 3000, 93), p(1080, 5000, 96),
	}
	ladder := Recommend(hull, 6, 93)
	if len(ladder) == 0 {
		t.Fatal("empty ladder")
	}
	// The top rung is the CHEAPEST point reaching the target, not the best
	// point measured: paying for 96 when 93 was the goal is the waste this
	// tool is looking for.
	if ladder[0].Kbps != 3000 {
		t.Errorf("top rung = %v, want the cheapest point reaching 93", ladder[0].Kbps)
	}
	for i := 1; i < len(ladder); i++ {
		if gap := ladder[i-1].VMAF - ladder[i].VMAF; gap < 6 {
			t.Errorf("rungs %d and %d are %.1f VMAF apart, closer than the step", i-1, i, gap)
		}
	}
	if last := ladder[len(ladder)-1]; last.VMAF > 84 {
		t.Errorf("the ladder should reach down the frontier, stopped at %v", last)
	}
}

func TestRecommendFallsBackWhenTargetUnreachable(t *testing.T) {
	hull := []Point{p(720, 1000, 70), p(720, 2000, 80)}
	ladder := Recommend(hull, 6, 95)
	if len(ladder) == 0 || ladder[0].VMAF != 80 {
		t.Errorf("with an unreachable target the top rung is the best measured, got %+v", ladder)
	}
}

// A rung parked at a saturated low resolution is where the savings actually
// live: 720p@3000 delivers 88.5, which the frontier reaches at 1080p for far
// fewer bits.
func TestAnalyzeReportsSavingsAgainstTheCurrentLadder(t *testing.T) {
	points := []Point{
		p(1080, 2000, 86), p(1080, 3000, 92), p(1080, 4000, 93.5),
		p(720, 1000, 78), p(720, 2000, 87), p(720, 3000, 88.5),
	}
	res := Analyze("x264", points, Options{
		KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6,
		Current: []Rendition{{Height: 720, Kbps: 3000}},
	})
	if len(res.Curves) != 2 || res.Curves[0].Height != 1080 {
		t.Fatalf("curves = %+v, want 1080 first", res.Curves)
	}
	if !res.TargetReached {
		t.Error("the grid reaches VMAF 93, TargetReached should be true")
	}
	if len(res.Savings) != 1 {
		t.Fatalf("savings = %+v", res.Savings)
	}
	s := res.Savings[0]
	if s.CurrentVMAF != 88.5 {
		t.Errorf("current rung quality = %v, want 88.5", s.CurrentVMAF)
	}
	if s.EfficientHeight != 1080 {
		t.Errorf("the same quality is cheapest at 1080p, got %dp", s.EfficientHeight)
	}
	if math.Abs(s.EfficientKbps-2300) > 1 {
		t.Errorf("efficient bitrate = %v, want ~2300", s.EfficientKbps)
	}
	if s.SavedPct < 20 || s.SavedPct > 25 {
		t.Errorf("saving = %.1f%%, want ~23%%", s.SavedPct)
	}
	if res.ComparedRungs != 1 || res.CurrentTotalKbps != 3000 {
		t.Errorf("comparison base = %d rungs / %dk", res.ComparedRungs, res.CurrentTotalKbps)
	}
	if res.TotalSavedPct <= 0 {
		t.Errorf("total saving = %v, want a positive percentage", res.TotalSavedPct)
	}
}

// Rate control lands near the target, not on it: a rung configured at exactly
// the bitrate a grid point asked for must still be comparable.
func TestCompareSnapsToTheMeasuredRange(t *testing.T) {
	// The grid asked for 1500k and 3000k; the encoder delivered 1458k and 2971k.
	points := []Point{
		{Height: 720, Target: 1500, Kbps: 1458, VMAF: 84.8},
		{Height: 720, Target: 3000, Kbps: 2971, VMAF: 88.4},
	}
	hull := Hull(points)
	got := Compare([]Rendition{{Height: 720, Kbps: 3000}}, points, hull)
	if len(got) != 1 {
		t.Fatalf("savings = %+v", got)
	}
	if got[0].Note != "" {
		t.Errorf("a rung at the grid's own target must be comparable, got %q", got[0].Note)
	}
	if got[0].CurrentVMAF != 88.4 {
		t.Errorf("quality = %v, want the top measured 88.4", got[0].CurrentVMAF)
	}
	// Far outside is still refused: extrapolation is the thing we do not do.
	far := Compare([]Rendition{{Height: 720, Kbps: 6000}}, points, hull)
	if far[0].Note == "" {
		t.Error("a rung well past the grid must not be silently extrapolated")
	}
}

// A rung the grid never measured must not enter either side of the total: it
// would invent a saving out of a missing measurement.
func TestAnalyzeMarksRungsOutsideTheGrid(t *testing.T) {
	points := []Point{p(1080, 3000, 90), p(1080, 4000, 93)}
	res := Analyze("x264", points, Options{
		KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6,
		Current: []Rendition{{Height: 360, Kbps: 800}, {Height: 1080, Kbps: 3500}},
	})
	if len(res.Savings) != 2 || res.Savings[0].Note == "" {
		t.Fatalf("a rung at a resolution nobody measured must be flagged, got %+v", res.Savings)
	}
	if res.ComparedRungs != 1 || res.CurrentTotalKbps != 3500 {
		t.Errorf("only the measurable rung counts: %d rungs / %dk", res.ComparedRungs, res.CurrentTotalKbps)
	}
}
