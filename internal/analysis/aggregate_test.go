package analysis

import (
	"math"
	"testing"
)

func clipPoint(height, target int, kbps, vmaf, vmafMin, p1 float64) Point {
	one := p1
	return Point{
		Encoder: "x264", Height: height, Target: target, Kbps: kbps,
		VMAF: vmaf, VMAFHarmonic: vmaf - 1, VMAFMin: vmafMin, P1: &one,
	}
}

// Three cuts of the same source at the same grid point become one point, and the
// distance between them becomes a number the report can print.
func TestAggregateFoldsClipsAndRecordsTheSpread(t *testing.T) {
	points := []Point{
		clipPoint(1080, 3000, 3000, 90, 80, 70),
		clipPoint(1080, 3000, 3100, 96, 70, 40), // the busy cut: cheaper quality, worse tail
		clipPoint(1080, 3000, 2900, 93, 85, 75),
	}
	got := Aggregate(points)
	if len(got) != 1 {
		t.Fatalf("aggregated to %d points, want 1", len(got))
	}
	p := got[0]
	if math.Abs(p.VMAF-93) > 1e-9 {
		t.Errorf("VMAF = %v, want the mean 93", p.VMAF)
	}
	if math.Abs(p.Kbps-3000) > 1e-9 {
		t.Errorf("kbps = %v, want the mean 3000", p.Kbps)
	}
	if p.Clips != 3 {
		t.Errorf("clips = %d, want 3", p.Clips)
	}
	if math.Abs(p.VMAFSpread-6) > 1e-9 {
		t.Errorf("spread = %v, want 96-90 = 6", p.VMAFSpread)
	}
	// Tails take the worst clip, never the average of the tails: averaging them
	// would hide the cut that fell apart, which is the cut the extra clips were
	// measured to find.
	if p.VMAFMin != 70 {
		t.Errorf("min = %v, want the worst clip's 70", p.VMAFMin)
	}
	if p.P1 == nil || *p.P1 != 40 {
		t.Errorf("P1 = %v, want the worst clip's 40", p.P1)
	}
}

// A single-clip run must come out exactly as it went in, so those reports look
// the way they always did.
func TestAggregateIsIdempotentForOneClip(t *testing.T) {
	in := []Point{
		clipPoint(1080, 3000, 3010, 92, 82, 71),
		clipPoint(720, 1500, 1505, 85, 76, 66),
	}
	got := Aggregate(in)
	if len(got) != 2 {
		t.Fatalf("got %d points, want 2", len(got))
	}
	for i, p := range got {
		if p.Clips != 0 || p.VMAFSpread != 0 {
			t.Errorf("point %d gained a dispersion out of one clip: %+v", i, p)
		}
		if p.VMAF != in[i].VMAF || p.Kbps != in[i].Kbps || p.VMAFMin != in[i].VMAFMin {
			t.Errorf("point %d changed: %+v vs %+v", i, p, in[i])
		}
	}
}

// Grid coordinates stay separate — and stay in the order they were measured in,
// so a report does not reshuffle itself between runs.
func TestAggregateKeepsCoordinatesApartAndOrdered(t *testing.T) {
	points := []Point{
		clipPoint(1080, 6000, 6000, 96, 90, 85),
		clipPoint(1080, 3000, 3000, 92, 84, 78),
		clipPoint(1080, 6000, 6100, 97, 91, 86),
		clipPoint(720, 3000, 3000, 88, 80, 74),
	}
	got := Aggregate(points)
	if len(got) != 3 {
		t.Fatalf("got %d points, want 3 distinct coordinates", len(got))
	}
	want := []struct{ height, target, clips int }{{1080, 6000, 2}, {1080, 3000, 0}, {720, 3000, 0}}
	for i, w := range want {
		if got[i].Height != w.height || got[i].Target != w.target || got[i].Clips != w.clips {
			t.Errorf("point %d = %dp@%dk over %d clips, want %dp@%dk over %d",
				i, got[i].Height, got[i].Target, got[i].Clips, w.height, w.target, w.clips)
		}
	}
	// Two encoders at the same coordinate are two curves, not one point.
	mixed := []Point{clipPoint(1080, 3000, 3000, 92, 84, 78), clipPoint(1080, 3000, 2100, 94, 86, 80)}
	mixed[1].Encoder = "svt-av1"
	if got := Aggregate(mixed); len(got) != 2 {
		t.Errorf("two encoders folded into one point: %+v", got)
	}
}

// A metric only some clips carry is averaged over the clips that have it, not
// over all of them — otherwise the number is quietly a fraction of the truth.
func TestAggregateHandlesPartiallyMeasuredMetrics(t *testing.T) {
	a, b := clipPoint(1080, 3000, 3000, 92, 84, 78), clipPoint(1080, 3000, 3000, 92, 84, 78)
	psnr := 40.0
	a.PSNR = &psnr
	b.PSNR = nil
	b.P1 = nil
	got := Aggregate([]Point{a, b})[0]
	if got.PSNR == nil || *got.PSNR != 40 {
		t.Errorf("PSNR = %v, want 40 from the one clip that measured it", got.PSNR)
	}
	if got.P1 == nil || *got.P1 != 78 {
		t.Errorf("P1 = %v, want the one clip that had it", got.P1)
	}
	none := clipPoint(1080, 3000, 3000, 92, 84, 78)
	none.P1 = nil
	other := none
	if got := Aggregate([]Point{none, other})[0]; got.P1 != nil {
		t.Errorf("no clip measured P1, so it must stay absent: %v", got.P1)
	}
}

func TestClipCountAndWidestSpread(t *testing.T) {
	opt := Options{KneeGain: 0.5, TargetVMAF: 93, LadderStep: 6}
	points := Aggregate([]Point{
		clipPoint(1080, 3000, 3000, 90, 80, 70), clipPoint(1080, 3000, 3000, 91, 80, 70),
		clipPoint(1080, 6000, 6000, 90, 80, 70), clipPoint(1080, 6000, 6000, 98, 80, 70),
	})
	res := Analyze("x264", points, opt)
	if n := ClipCount(res); n != 2 {
		t.Errorf("ClipCount = %d, want 2", n)
	}
	worst, ok := WidestSpread(res)
	if !ok {
		t.Fatal("no spread found on a two-clip result")
	}
	if worst.Target != 6000 || math.Abs(worst.VMAFSpread-8) > 1e-9 {
		t.Errorf("widest spread = %.2f at %dk, want 8 at 6000k", worst.VMAFSpread, worst.Target)
	}

	// A single-clip result has no dispersion to report at all.
	single := Analyze("x264", Aggregate([]Point{clipPoint(1080, 3000, 3000, 90, 80, 70)}), opt)
	if n := ClipCount(single); n != 1 {
		t.Errorf("ClipCount = %d, want 1", n)
	}
	if _, ok := WidestSpread(single); ok {
		t.Error("a single clip cannot disagree with itself")
	}
}
