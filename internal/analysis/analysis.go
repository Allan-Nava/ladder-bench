// Package analysis turns the measured (bitrate, VMAF) points into the three
// answers a ladder needs: where each resolution stops paying for bits, which
// resolution wins at each bitrate, and what the ladder should look like.
//
// It is pure arithmetic over the measurements — no ffmpeg, no I/O — so every
// claim the report makes can be tested against a synthetic curve.
package analysis

import "sort"

// Point is one measurement: an encode at a resolution and its quality.
//
// Only Kbps and VMAF take part in the arithmetic. The rest is carried through
// so the report can show what a number was measured alongside — a rung is
// judged on more than its mean.
type Point struct {
	Encoder string  `json:"encoder"`
	Height  int     `json:"height"`
	Target  int     `json:"target_kbps"`
	Kbps    float64 `json:"kbps"`
	VMAF    float64 `json:"vmaf"`
	VMAFMin float64 `json:"vmaf_min"`
	// VMAFHarmonic weighs the worst frames more heavily than the mean does.
	VMAFHarmonic float64 `json:"vmaf_harmonic_mean,omitempty"`
	// PSNR (Y plane, dB) and SSIM are nil unless the run asked for them. A zero
	// could not say "not measured", and absent is not the same as terrible.
	PSNR *float64 `json:"psnr_y,omitempty"`
	SSIM *float64 `json:"ssim,omitempty"`
}

// Rendition is one rung of an existing ladder, for the savings comparison.
type Rendition struct {
	Height int `json:"height"`
	Kbps   int `json:"kbps"`
}

// Options are the thresholds that turn measurements into advice.
type Options struct {
	KneeGain   float64     `json:"knee_gain"`
	TargetVMAF float64     `json:"target_vmaf"`
	LadderStep float64     `json:"ladder_step"`
	Current    []Rendition `json:"current_ladder,omitempty"`
}

// Curve is the rate-quality curve measured at one resolution.
type Curve struct {
	Height int     `json:"height"`
	Points []Point `json:"points"`
	// Knee is the last point whose bitrate still buys quality: past it every
	// measured step gains less than KneeGain VMAF per +10% bitrate.
	Knee *Point `json:"knee,omitempty"`
	// FlatFromStart means even the cheapest measured step was already below
	// the threshold — the grid started above the interesting range.
	FlatFromStart bool `json:"flat_from_start"`
	// StillClimbing means the curve never flattened inside the grid — the
	// grid stopped below the interesting range.
	StillClimbing bool `json:"still_climbing"`
	// WastedPct is the share of the top measured bitrate that buys nothing,
	// i.e. what sits between the knee and the top of this rung's grid.
	WastedPct float64 `json:"wasted_pct"`
}

// Saving compares one rung of an existing ladder against the efficient frontier.
type Saving struct {
	Current     Rendition `json:"current"`
	CurrentVMAF float64   `json:"current_vmaf"`
	// EfficientKbps is the cheapest bitrate on the hull reaching the same
	// quality, and EfficientHeight the resolution that delivers it.
	EfficientKbps   float64 `json:"efficient_kbps"`
	EfficientHeight int     `json:"efficient_height"`
	SavedPct        float64 `json:"saved_pct"`
	// Note explains why a rung could not be compared.
	Note string `json:"note,omitempty"`
}

// Result is the whole analysis for one encoder.
type Result struct {
	Encoder string  `json:"encoder"`
	Curves  []Curve `json:"curves"`
	// Hull is the efficient frontier: the points no other point beats on both
	// bitrate and quality at once.
	Hull []Point `json:"hull"`
	// Ladder is the recommended set of rungs, best first.
	Ladder  []Point  `json:"ladder"`
	Savings []Saving `json:"savings,omitempty"`
	// LadderTotalKbps sums the recommended rungs. It is informational only:
	// two ladders with different rung counts cannot be compared by their sums,
	// and the recommended one deliberately aims at TargetVMAF rather than at
	// whatever the current ladder happens to deliver.
	LadderTotalKbps float64 `json:"ladder_total_kbps"`
	// CurrentTotalKbps and EfficientTotalKbps are the like-for-like
	// comparison: the current rungs that could be measured, and what the
	// frontier charges for exactly the quality those rungs deliver today.
	CurrentTotalKbps   int     `json:"current_total_kbps,omitempty"`
	EfficientTotalKbps float64 `json:"efficient_total_kbps,omitempty"`
	// ComparedRungs is how many rungs of the current ladder the grid could
	// measure — the rest fell outside it.
	ComparedRungs int     `json:"compared_rungs,omitempty"`
	TotalSavedPct float64 `json:"total_saved_pct,omitempty"`
	// TargetReached is false when no measured point reached TargetVMAF: the
	// top rung is then the best the grid could do, not the requested quality.
	TargetReached bool `json:"target_reached"`
}

// Analyze runs the whole analysis for a single encoder's points.
func Analyze(encoder string, points []Point, opt Options) Result {
	res := Result{Encoder: encoder}
	for _, height := range heights(points) {
		at := sortByKbps(filterHeight(points, height))
		c := Curve{Height: height, Points: at}
		knee, ok := Knee(at, opt.KneeGain)
		switch {
		case ok && knee.Kbps == at[0].Kbps && len(at) > 1:
			k := knee
			c.Knee, c.FlatFromStart = &k, true
		case ok:
			k := knee
			c.Knee = &k
		case len(at) > 1:
			c.StillClimbing = true
		}
		if c.Knee != nil {
			top := at[len(at)-1].Kbps
			if top > 0 {
				c.WastedPct = (top - c.Knee.Kbps) / top * 100
			}
		}
		res.Curves = append(res.Curves, c)
	}
	res.Hull = Hull(points)
	res.Ladder = Recommend(res.Hull, opt.LadderStep, opt.TargetVMAF)
	for _, p := range res.Ladder {
		res.LadderTotalKbps += p.Kbps
	}
	if len(res.Hull) > 0 {
		res.TargetReached = res.Hull[len(res.Hull)-1].VMAF >= opt.TargetVMAF
	}
	if len(opt.Current) > 0 {
		res.Savings = Compare(opt.Current, points, res.Hull)
		// Sum only the rungs that could actually be compared. Adding a rung
		// the grid never measured to one side of the total and nothing to the
		// other would invent a saving out of a missing measurement.
		for _, s := range res.Savings {
			if s.Note != "" {
				continue
			}
			res.ComparedRungs++
			res.CurrentTotalKbps += s.Current.Kbps
			res.EfficientTotalKbps += s.EfficientKbps
		}
		if res.CurrentTotalKbps > 0 {
			res.TotalSavedPct = (float64(res.CurrentTotalKbps) - res.EfficientTotalKbps) / float64(res.CurrentTotalKbps) * 100
		}
	}
	return res
}

// Hull returns the efficient frontier: the upper-left convex hull of the
// rate-quality points, cheapest first.
//
// This is the per-title idea in one function. Pooling every resolution and
// keeping the upper boundary answers "at this bitrate, which resolution looks
// best?" — which is exactly the question a ladder encodes, and the reason a
// well-encoded 720p rung can outrank a starved 1080p one.
func Hull(points []Point) []Point {
	if len(points) < 2 {
		return append([]Point(nil), points...)
	}
	pts := sortByKbps(points)
	// One point per bitrate: at equal cost only the best quality can matter.
	uniq := make([]Point, 0, len(pts))
	for _, p := range pts {
		if n := len(uniq); n > 0 && uniq[n-1].Kbps == p.Kbps {
			if p.VMAF > uniq[n-1].VMAF {
				uniq[n-1] = p
			}
			continue
		}
		uniq = append(uniq, p)
	}
	hull := make([]Point, 0, len(uniq))
	for _, p := range uniq {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) >= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	// Cut the tail at the best quality: beyond it the hull would slope down,
	// which as advice reads "pay more, see less".
	best := 0
	for i, p := range hull {
		if p.VMAF > hull[best].VMAF {
			best = i
		}
	}
	return hull[:best+1]
}

// cross is the z component of (a-o)×(b-o); >= 0 means o→a→b turns left or is
// collinear, so a sits below the o→b line and is not on the upper hull.
func cross(o, a, b Point) float64 {
	return (a.Kbps-o.Kbps)*(b.VMAF-o.VMAF) - (a.VMAF-o.VMAF)*(b.Kbps-o.Kbps)
}

// Knee returns the last point whose bitrate still buys quality: past it every
// measured step gains less than gainPer10 VMAF per +10% bitrate.
//
// It scans from the top down rather than from the bottom up on purpose. A
// rate-quality curve is not perfectly smooth — one noisy step in the middle
// would stop a bottom-up scan early and report a knee far below the real one.
func Knee(points []Point, gainPer10 float64) (Point, bool) {
	if len(points) < 2 {
		return Point{}, false
	}
	pts := sortByKbps(points)
	last := -1
	for i := len(pts) - 1; i >= 1; i-- {
		if Gain(pts[i-1], pts[i]) >= gainPer10 {
			last = i
			break
		}
	}
	switch {
	case last < 0:
		return pts[0], true // flat across the whole grid
	case last == len(pts)-1:
		return Point{}, false // still climbing at the top of the grid
	default:
		return pts[last], true
	}
}

// Gain is the quality bought by one step of the curve, expressed as VMAF
// points per +10% bitrate. Relative rather than absolute, because +500 kbps
// means something entirely different at 800 kbps and at 6000 kbps.
func Gain(from, to Point) float64 {
	if from.Kbps <= 0 || to.Kbps <= from.Kbps {
		return 0
	}
	pct := (to.Kbps - from.Kbps) / from.Kbps * 100
	return (to.VMAF - from.VMAF) / pct * 10
}

// Recommend picks the ladder off the hull, best rung first.
//
// Rungs are spaced by perceived quality (step VMAF points, ~6 being roughly
// one just-noticeable difference) instead of by the usual bitrate halving:
// equal bitrate steps put several indistinguishable rungs at the top and leave
// a cliff at the bottom. Every rung is a point that was actually measured — no
// interpolated bitrate ever reaches the ladder.
func Recommend(hull []Point, step, target float64) []Point {
	if len(hull) == 0 {
		return nil
	}
	top := len(hull) - 1
	for i, p := range hull {
		if p.VMAF >= target {
			top = i
			break
		}
	}
	out := []Point{hull[top]}
	last := hull[top].VMAF
	for i := top - 1; i >= 0; i-- {
		if last-hull[i].VMAF >= step {
			out = append(out, hull[i])
			last = hull[i].VMAF
		}
	}
	return out
}

// Compare measures each rung of an existing ladder against the frontier: what
// quality it delivers today, and the cheapest way the frontier reaches it.
func Compare(current []Rendition, points []Point, hull []Point) []Saving {
	out := make([]Saving, 0, len(current))
	for _, r := range current {
		s := Saving{Current: r}
		at := sortByKbps(filterHeight(points, r.Height))
		kbps, ok := snap(at, float64(r.Kbps))
		if !ok {
			s.Note = "outside the measured grid at this resolution"
			out = append(out, s)
			continue
		}
		vmaf, ok := InterpolateVMAF(at, kbps)
		if !ok {
			s.Note = "outside the measured grid at this resolution"
			out = append(out, s)
			continue
		}
		s.CurrentVMAF = vmaf
		kbps, height, ok := InterpolateBitrate(hull, vmaf)
		if !ok {
			s.Note = "no point on the frontier reaches this quality"
			out = append(out, s)
			continue
		}
		s.EfficientKbps, s.EfficientHeight = kbps, height
		if r.Kbps > 0 {
			s.SavedPct = (float64(r.Kbps) - kbps) / float64(r.Kbps) * 100
		}
		out = append(out, s)
	}
	return out
}

// SnapTolerance is how far outside the measured range a configured rung may
// sit and still be compared against it.
//
// Rate control never lands exactly on the requested bitrate: a grid point
// asked for 3000k and measured 2971k. Without this, a current rung configured
// at exactly 3000k — the very bitrate the grid was built around — would be
// reported as "outside the measured grid", which is true to the arithmetic and
// wrong to the reader.
const SnapTolerance = 0.05

func snap(points []Point, kbps float64) (float64, bool) {
	if len(points) == 0 {
		return 0, false
	}
	lo, hi := points[0].Kbps, points[len(points)-1].Kbps
	switch {
	case kbps < lo:
		if kbps >= lo*(1-SnapTolerance) {
			return lo, true
		}
	case kbps > hi:
		if kbps <= hi*(1+SnapTolerance) {
			return hi, true
		}
	default:
		return kbps, true
	}
	return 0, false
}

// InterpolateVMAF reads the quality of a curve at an arbitrary bitrate.
// It refuses to extrapolate: a rate-quality curve outside the measured range
// is a guess, and a guess is exactly what this tool exists to replace.
func InterpolateVMAF(points []Point, kbps float64) (float64, bool) {
	pts := sortByKbps(points)
	if len(pts) == 0 || kbps < pts[0].Kbps || kbps > pts[len(pts)-1].Kbps {
		return 0, false
	}
	for i := 1; i < len(pts); i++ {
		if kbps <= pts[i].Kbps {
			return lerp(pts[i-1].Kbps, pts[i-1].VMAF, pts[i].Kbps, pts[i].VMAF, kbps), true
		}
	}
	return pts[len(pts)-1].VMAF, true
}

// InterpolateBitrate finds the cheapest place on the hull reaching a quality,
// and the resolution that delivers it there.
func InterpolateBitrate(hull []Point, vmaf float64) (float64, int, bool) {
	if len(hull) == 0 || vmaf > hull[len(hull)-1].VMAF {
		return 0, 0, false
	}
	if vmaf <= hull[0].VMAF {
		return hull[0].Kbps, hull[0].Height, true
	}
	for i := 1; i < len(hull); i++ {
		if vmaf <= hull[i].VMAF {
			kbps := lerp(hull[i-1].VMAF, hull[i-1].Kbps, hull[i].VMAF, hull[i].Kbps, vmaf)
			// The resolution is the one at the top of the segment: it is the
			// rendition that actually delivers this quality here.
			return kbps, hull[i].Height, true
		}
	}
	return hull[len(hull)-1].Kbps, hull[len(hull)-1].Height, true
}

func lerp(x0, y0, x1, y1, x float64) float64 {
	if x1 == x0 {
		return y1
	}
	return y0 + (y1-y0)*(x-x0)/(x1-x0)
}

func sortByKbps(points []Point) []Point {
	out := append([]Point(nil), points...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kbps != out[j].Kbps {
			return out[i].Kbps < out[j].Kbps
		}
		return out[i].VMAF < out[j].VMAF
	})
	return out
}

func filterHeight(points []Point, height int) []Point {
	var out []Point
	for _, p := range points {
		if p.Height == height {
			out = append(out, p)
		}
	}
	return out
}

// heights lists the resolutions present, tallest first.
func heights(points []Point) []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range points {
		if !seen[p.Height] {
			seen[p.Height] = true
			out = append(out, p.Height)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}
