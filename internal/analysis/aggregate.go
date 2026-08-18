package analysis

// Aggregate collapses points measured on several clips into one point per grid
// coordinate, and records how far apart the clips were.
//
// The whole analysis downstream — the knee, the frontier, the ladder, the
// BD-rate — then runs on the aggregated curve, which is the point: a ladder
// chosen across three cuts of the source is a different, better answer than a
// ladder chosen on whichever thirty seconds happened to be measured first.
//
// It is idempotent for a single-clip run: one point per coordinate, no spread,
// and Clips left at zero so those reports look exactly as they did before.
func Aggregate(points []Point) []Point {
	type key struct {
		encoder string
		height  int
		target  int
	}
	order := make([]key, 0, len(points))
	groups := map[key][]Point{}
	for _, p := range points {
		k := key{p.Encoder, p.Height, p.Target}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], p)
	}

	out := make([]Point, 0, len(order))
	for _, k := range order {
		out = append(out, fold(groups[k]))
	}
	return out
}

// fold reduces the measurements of one grid coordinate across clips.
//
// Averages for the things a curve is drawn from, minima for the things that
// describe a tail. Averaging the tails would hide the clip that fell apart,
// which is the one the extra clips were measured to find.
func fold(group []Point) Point {
	p := group[0]
	if len(group) == 1 {
		return p
	}
	var sumKbps, sumVMAF, sumHarmonic float64
	lo, hi := group[0].VMAF, group[0].VMAF
	for _, q := range group {
		sumKbps += q.Kbps
		sumVMAF += q.VMAF
		sumHarmonic += q.VMAFHarmonic
		lo, hi = min(lo, q.VMAF), max(hi, q.VMAF)
		p.VMAFMin = min(p.VMAFMin, q.VMAFMin)
	}
	n := float64(len(group))
	p.Kbps = sumKbps / n
	p.VMAF = sumVMAF / n
	p.VMAFHarmonic = sumHarmonic / n
	p.P1 = worst(group, func(q Point) *float64 { return q.P1 })
	p.P5 = worst(group, func(q Point) *float64 { return q.P5 })
	p.PSNR = mean(group, func(q Point) *float64 { return q.PSNR })
	p.SSIM = mean(group, func(q Point) *float64 { return q.SSIM })
	p.Clips = len(group)
	p.VMAFSpread = hi - lo
	return p
}

// worst takes the lowest value across the clips, ignoring the ones that carry
// none. It returns nil when no clip measured it.
func worst(group []Point, pick func(Point) *float64) *float64 {
	var out *float64
	for _, q := range group {
		v := pick(q)
		if v == nil {
			continue
		}
		if out == nil || *v < *out {
			w := *v
			out = &w
		}
	}
	return out
}

// mean averages across the clips that carry a value, and returns nil when none
// do. Averaging over the clips that have it — rather than over all of them —
// keeps a partially measured grid from reporting a number that is quietly a
// fraction of the truth.
func mean(group []Point, pick func(Point) *float64) *float64 {
	var sum float64
	n := 0
	for _, q := range group {
		if v := pick(q); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	avg := sum / float64(n)
	return &avg
}

// ClipCount is how many clips backed the points of a result, or 1 when the run
// measured a single cut. It decides whether the report has a dispersion to talk
// about at all.
func ClipCount(res Result) int {
	n := 1
	for _, c := range res.Curves {
		for _, p := range c.Points {
			if p.Clips > n {
				n = p.Clips
			}
		}
	}
	return n
}

// WidestSpread is the largest VMAF distance between clips at any one grid point,
// and the rung it happened at.
//
// It is the number that says whether the clip choice mattered more than the rung
// choice: a spread wider than the ladder step means two cuts of the same source
// disagree about this rung by more than a whole rung.
func WidestSpread(res Result) (Point, bool) {
	var worst Point
	found := false
	for _, c := range res.Curves {
		for _, p := range c.Points {
			if p.Clips > 1 && (!found || p.VMAFSpread > worst.VMAFSpread) {
				worst, found = p, true
			}
		}
	}
	return worst, found
}
