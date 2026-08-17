package analysis

import (
	"fmt"
	"math"
	"sort"
)

// MinOverlapVMAF is the narrowest quality overlap two curves may have and still
// produce a BD-rate.
//
// BD-rate is an average over the quality range both encoders actually reached.
// When that range shrinks to a sliver, the average is dominated by the noise of
// two or three frames' worth of VMAF, and the resulting percentage looks like a
// verdict while being a rounding error. Below this width the comparison is
// declined instead.
const MinOverlapVMAF = 1.0

// bdCubicPoints is how many measurements a least-squares cubic fit needs before
// it stops being an exercise in drawing a curve through noise.
const bdCubicPoints = 4

// Methods a BD-rate can be computed with, reported alongside the number so a
// reader can tell a fitted result from an interpolated one.
const (
	BDMethodCubic  = "cubic fit"
	BDMethodLinear = "piecewise linear"
)

// BD is one Bjøntegaard delta rate: how much bitrate the test encoder needs
// against the anchor, at equal measured quality.
//
// It is the number that settles a "should we move to AV1?" discussion, because
// it collapses two curves into one percentage without picking a favourite
// operating point. It is also only as honest as its interval: RatePct is an
// average over [LowVMAF, HighVMAF], the quality range *both* encoders reached,
// and says nothing about quality outside it.
type BD struct {
	// Height is the resolution this figure covers, or 0 for the comparison
	// made on the two efficient frontiers.
	Height int `json:"height,omitempty"`
	// LowVMAF and HighVMAF bound the overlapping quality interval the average
	// was taken over.
	LowVMAF  float64 `json:"low_vmaf,omitempty"`
	HighVMAF float64 `json:"high_vmaf,omitempty"`
	// RatePct is the bitrate difference at equal quality, test against anchor:
	// -18.4 means the test encoder reaches the same VMAF with 18.4% fewer bits.
	RatePct float64 `json:"rate_pct"`
	// Method names how each curve was integrated, cubic or linear.
	Method string `json:"method,omitempty"`
	// AnchorPoints and TestPoints are how many measurements backed each curve.
	AnchorPoints int `json:"anchor_points,omitempty"`
	TestPoints   int `json:"test_points,omitempty"`
	// Note explains why no figure could be produced. When it is set, RatePct is
	// zero because nothing was computed — not because the encoders tied.
	Note string `json:"note,omitempty"`
}

// OK reports whether this BD-rate carries a number.
func (b BD) OK() bool { return b.Note == "" }

// Comparison is every BD-rate between one encoder and the anchor.
type Comparison struct {
	Anchor string `json:"anchor"`
	Test   string `json:"test"`
	// Frontier compares the two efficient frontiers: the ladder-level answer,
	// where each encoder is free to pick its best resolution per bitrate.
	Frontier BD `json:"frontier"`
	// ByHeight compares resolution against resolution, tallest first.
	ByHeight []BD `json:"by_height,omitempty"`
}

// BDRates compares every other encoder against the anchor.
//
// The anchor is the encoder already in production, so the sign of every figure
// reads the same way: negative means the challenger is cheaper.
func BDRates(anchor string, results []Result) []Comparison {
	var base *Result
	for i := range results {
		if results[i].Encoder == anchor {
			base = &results[i]
			break
		}
	}
	if base == nil || len(results) < 2 {
		return nil
	}
	var out []Comparison
	for i := range results {
		test := &results[i]
		if test.Encoder == anchor {
			continue
		}
		c := Comparison{Anchor: anchor, Test: test.Encoder}
		c.Frontier = BDRate(base.Hull, test.Hull)
		for _, ac := range base.Curves {
			// Every encoder walks the same grid, so a height missing from the
			// challenger means the run never finished — and a run that did not
			// finish never reaches this far.
			tc, ok := curveAt(test.Curves, ac.Height)
			if !ok {
				continue
			}
			bd := BDRate(ac.Points, tc.Points)
			bd.Height = ac.Height
			c.ByHeight = append(c.ByHeight, bd)
		}
		out = append(out, c)
	}
	return out
}

// BDRate is the Bjøntegaard delta rate of test against anchor: the average
// bitrate difference at equal quality over the quality range both curves cover.
//
// Both curves are integrated as log10(bitrate) over VMAF — bitrate differences
// are multiplicative, so averaging them in the linear domain would let the top
// of the grid outvote the bottom. The result is the ratio of those two averages
// back in the linear domain.
//
// It never leaves the measured range: the interval is the *intersection* of
// what the two encoders reached, and a pair of curves that barely overlap is
// declined rather than stretched to meet.
func BDRate(anchor, test []Point) BD {
	a, b := logCurve(anchor), logCurve(test)
	bd := BD{AnchorPoints: len(a), TestPoints: len(b)}
	if len(a) < 2 || len(b) < 2 {
		bd.Note = "not enough measured points on both curves"
		return bd
	}
	low := math.Max(a[0].vmaf, b[0].vmaf)
	high := math.Min(a[len(a)-1].vmaf, b[len(b)-1].vmaf)
	if high-low < MinOverlapVMAF {
		bd.Note = fmt.Sprintf("the two curves share less than %.1f VMAF of quality — nothing to average over", MinOverlapVMAF)
		return bd
	}
	bd.LowVMAF, bd.HighVMAF = low, high

	anchorArea, anchorMethod, ok := integrateLogRate(a, low, high)
	if !ok {
		bd.Note = "the anchor curve could not be integrated (two points at the same quality?)"
		return bd
	}
	testArea, testMethod, ok := integrateLogRate(b, low, high)
	if !ok {
		bd.Note = "the test curve could not be integrated (two points at the same quality?)"
		return bd
	}
	bd.Method = BDMethodCubic
	if anchorMethod == BDMethodLinear || testMethod == BDMethodLinear {
		// Report the weaker of the two: the pair is only as fitted as its
		// coarsest curve, and the reader should know which one they are getting.
		bd.Method = BDMethodLinear
	}
	bd.RatePct = (math.Pow(10, (testArea-anchorArea)/(high-low)) - 1) * 100
	return bd
}

// logPoint is one measurement in the domain BD-rate works in: quality on the x
// axis, log10 of the bitrate on the y axis.
type logPoint struct {
	vmaf float64
	rate float64
}

// logCurve turns measurements into a curve of log10(bitrate) over VMAF, sorted
// by quality. Points without a bitrate are dropped: log10(0) is not a number,
// and a point with no measured size was never really measured.
func logCurve(points []Point) []logPoint {
	out := make([]logPoint, 0, len(points))
	for _, p := range points {
		if p.Kbps <= 0 {
			continue
		}
		out = append(out, logPoint{vmaf: p.VMAF, rate: math.Log10(p.Kbps)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].vmaf < out[j].vmaf })
	return out
}

// integrateLogRate is the area under one curve's log10(bitrate) between two
// qualities, plus the method it used to get there.
//
// With enough points it fits the classic third-order polynomial and integrates
// it exactly; with fewer it interpolates between the measurements instead. A
// cubic through three points is not a fit, it is a curve drawn through noise.
func integrateLogRate(c []logPoint, low, high float64) (float64, string, bool) {
	if len(c) >= bdCubicPoints {
		if area, ok := integrateCubic(c, low, high); ok {
			return area, BDMethodCubic, true
		}
		// A degenerate fit (every point at the same quality, a singular normal
		// matrix) falls through to interpolation rather than reporting a number
		// from a system that had no solution.
	}
	area, ok := integrateLinear(c, low, high)
	return area, BDMethodLinear, ok
}

// integrateCubic fits log10(bitrate) = f(VMAF) as a cubic by least squares and
// integrates it over [low, high].
//
// The quality axis is centred and scaled to [-1, 1] before fitting. VMAF values
// live around 90, so an uncentred cubic would build normal equations spanning
// 90^6 and lose most of its precision to conditioning alone.
func integrateCubic(c []logPoint, low, high float64) (float64, bool) {
	mid, half := axis(c)
	if half == 0 {
		return 0, false
	}
	var m [4][5]float64
	for _, pt := range c {
		u := (pt.vmaf - mid) / half
		pow := [4]float64{1, u, u * u, u * u * u}
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				m[i][j] += pow[i] * pow[j]
			}
			m[i][4] += pow[i] * pt.rate
		}
	}
	coef, ok := solve4(m)
	if !ok {
		return 0, false
	}
	// dVMAF = half·du, so the integral in u is scaled back by half.
	primitive := func(u float64) float64 {
		return coef[0]*u + coef[1]*u*u/2 + coef[2]*u*u*u/3 + coef[3]*u*u*u*u/4
	}
	u0, u1 := (low-mid)/half, (high-mid)/half
	return (primitive(u1) - primitive(u0)) * half, true
}

// axis returns the centre and half-width of a curve's quality range.
func axis(c []logPoint) (mid, half float64) {
	lo, hi := c[0].vmaf, c[0].vmaf
	for _, pt := range c {
		lo, hi = math.Min(lo, pt.vmaf), math.Max(hi, pt.vmaf)
	}
	return (lo + hi) / 2, (hi - lo) / 2
}

// solve4 solves a 4×4 system by Gaussian elimination with partial pivoting.
// The augmented column is index 4.
func solve4(m [4][5]float64) ([4]float64, bool) {
	var out [4]float64
	for col := 0; col < 4; col++ {
		pivot := col
		for r := col + 1; r < 4; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(m[pivot][col]) < 1e-12 {
			return out, false
		}
		m[col], m[pivot] = m[pivot], m[col]
		for r := col + 1; r < 4; r++ {
			f := m[r][col] / m[col][col]
			for k := col; k <= 4; k++ {
				m[r][k] -= f * m[col][k]
			}
		}
	}
	for r := 3; r >= 0; r-- {
		sum := m[r][4]
		for k := r + 1; k < 4; k++ {
			sum -= m[r][k] * out[k]
		}
		out[r] = sum / m[r][r]
	}
	return out, true
}

// integrateLinear is the trapezoid area under the piecewise-linear curve
// through the measured points, between two qualities.
func integrateLinear(c []logPoint, low, high float64) (float64, bool) {
	for i := 1; i < len(c); i++ {
		if c[i].vmaf <= c[i-1].vmaf {
			// Two encodes that scored the same is not a function of quality:
			// there is no single bitrate to read off at that VMAF.
			return 0, false
		}
	}
	// Break at every measured point inside the interval, so the trapezoids
	// follow the curve's own corners instead of cutting across them.
	xs := []float64{low}
	for _, pt := range c {
		if pt.vmaf > low && pt.vmaf < high {
			xs = append(xs, pt.vmaf)
		}
	}
	xs = append(xs, high)
	total := 0.0
	for i := 1; i < len(xs); i++ {
		y0, ok0 := interpLogRate(c, xs[i-1])
		y1, ok1 := interpLogRate(c, xs[i])
		if !ok0 || !ok1 {
			return 0, false
		}
		total += (y0 + y1) / 2 * (xs[i] - xs[i-1])
	}
	return total, true
}

// interpLogRate reads log10(bitrate) off a curve at one quality. It refuses to
// extrapolate, for the same reason the rest of the package does.
func interpLogRate(c []logPoint, vmaf float64) (float64, bool) {
	if len(c) == 0 || vmaf < c[0].vmaf || vmaf > c[len(c)-1].vmaf {
		return 0, false
	}
	for i := 1; i < len(c); i++ {
		if vmaf <= c[i].vmaf {
			return lerp(c[i-1].vmaf, c[i-1].rate, c[i].vmaf, c[i].rate, vmaf), true
		}
	}
	return c[len(c)-1].rate, true
}

func curveAt(curves []Curve, height int) (Curve, bool) {
	for _, c := range curves {
		if c.Height == height {
			return c, true
		}
	}
	return Curve{}, false
}
