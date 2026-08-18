package analysis

import "fmt"

// PointDelta is one grid coordinate seen in two runs.
type PointDelta struct {
	Height int `json:"height"`
	Target int `json:"target_kbps"`
	// BaseKbps and Kbps are the real bitrates, BaseVMAF and VMAF the scores.
	BaseKbps float64 `json:"baseline_kbps,omitempty"`
	Kbps     float64 `json:"kbps,omitempty"`
	BaseVMAF float64 `json:"baseline_vmaf,omitempty"`
	VMAF     float64 `json:"vmaf,omitempty"`
	// Note explains a coordinate only one of the runs measured. When it is set
	// the deltas are meaningless and the renderers show the note instead.
	Note string `json:"note,omitempty"`
}

// DeltaVMAF is what the quality did, and DeltaKbps what it cost.
func (d PointDelta) DeltaVMAF() float64 { return d.VMAF - d.BaseVMAF }

// DeltaKbpsPct is the bitrate change as a percentage of the baseline, which is
// how a bitrate difference is worth reading: +200 kbps means something different
// at 800 kbps and at 6000 kbps.
func (d PointDelta) DeltaKbpsPct() float64 {
	if d.BaseKbps == 0 {
		return 0
	}
	return (d.Kbps - d.BaseKbps) / d.BaseKbps * 100
}

// Comparable reports whether both runs measured this coordinate.
func (d PointDelta) Comparable() bool { return d.Note == "" }

// RungDelta is one rung of the recommended ladder in two runs.
type RungDelta struct {
	Height   int     `json:"height"`
	BaseKbps float64 `json:"baseline_kbps"`
	Kbps     float64 `json:"kbps"`
	BaseVMAF float64 `json:"baseline_vmaf"`
	VMAF     float64 `json:"vmaf"`
}

// DeltaKbpsPct is the rung's bitrate change against the baseline.
func (r RungDelta) DeltaKbpsPct() float64 {
	if r.BaseKbps == 0 {
		return 0
	}
	return (r.Kbps - r.BaseKbps) / r.BaseKbps * 100
}

// EncoderDiff is everything two runs say about one encoder.
type EncoderDiff struct {
	Encoder string `json:"encoder"`
	// BDRate is the headline: the bitrate this run needs for the quality the
	// baseline delivered. Positive means it got more expensive. It is the same
	// arithmetic as the cross-encoder BD-rate, pointed at time instead of at a
	// competitor — which is exactly the question "did the upgrade cost us?".
	BDRate   BD   `json:"bd_rate"`
	ByHeight []BD `json:"bd_rate_by_height,omitempty"`
	// Points is every grid coordinate either run measured, in the baseline's
	// order, with the ones only one run has flagged rather than dropped.
	Points []PointDelta `json:"points"`
	// Ladder is the rung-by-rung comparison, and is empty when the two ladders
	// have different shapes — see LadderChanged.
	Ladder []RungDelta `json:"ladder,omitempty"`
	// LadderChanged means the ladders do not have the same resolutions in the
	// same order, so there is no honest pairing of their rungs.
	LadderChanged   bool    `json:"ladder_changed"`
	BaseLadder      []Point `json:"baseline_ladder,omitempty"`
	CurrentLadder   []Point `json:"current_ladder,omitempty"`
	BaseTargetMet   bool    `json:"baseline_target_reached"`
	TargetMet       bool    `json:"target_reached"`
	BaseTotalKbps   float64 `json:"baseline_ladder_total_kbps"`
	LadderTotalKbps float64 `json:"ladder_total_kbps"`
}

// TargetLost reports a run that no longer reaches the quality the baseline did.
// It is the one regression that a percentage cannot express: the grid stopped
// being able to answer the question, rather than answering it more expensively.
func (d EncoderDiff) TargetLost() bool { return d.BaseTargetMet && !d.TargetMet }

// Diff compares one encoder's analysis across two runs.
//
// The BD-rate is computed with the baseline as the anchor, so the sign reads the
// way every other percentage in this tool reads: negative is cheaper, positive
// costs more. Nothing here interpolates between the two runs — a coordinate only
// one of them measured is reported as such.
func Diff(base, current Result) EncoderDiff {
	d := EncoderDiff{
		Encoder:         current.Encoder,
		BDRate:          BDRate(base.Hull, current.Hull),
		BaseTargetMet:   base.TargetReached,
		TargetMet:       current.TargetReached,
		BaseTotalKbps:   base.LadderTotalKbps,
		LadderTotalKbps: current.LadderTotalKbps,
	}
	for _, bc := range base.Curves {
		cc, ok := curveAt(current.Curves, bc.Height)
		if !ok {
			continue
		}
		bd := BDRate(bc.Points, cc.Points)
		bd.Height = bc.Height
		d.ByHeight = append(d.ByHeight, bd)
	}
	d.Points = diffPoints(base, current)
	d.Ladder, d.LadderChanged = diffLadder(base.Ladder, current.Ladder)
	if d.LadderChanged {
		d.BaseLadder, d.CurrentLadder = base.Ladder, current.Ladder
	}
	return d
}

// diffPoints pairs the grid coordinates of two runs, keeping the baseline's order
// and appending whatever the current run added.
func diffPoints(base, current Result) []PointDelta {
	type key struct{ height, target int }
	cur := map[key]Point{}
	for _, c := range current.Curves {
		for _, p := range c.Points {
			cur[key{p.Height, p.Target}] = p
		}
	}
	var out []PointDelta
	seen := map[key]bool{}
	for _, c := range base.Curves {
		for _, p := range c.Points {
			k := key{p.Height, p.Target}
			seen[k] = true
			delta := PointDelta{Height: p.Height, Target: p.Target, BaseKbps: p.Kbps, BaseVMAF: p.VMAF}
			q, ok := cur[k]
			if !ok {
				delta.Note = "not measured in this run"
				out = append(out, delta)
				continue
			}
			delta.Kbps, delta.VMAF = q.Kbps, q.VMAF
			out = append(out, delta)
		}
	}
	for _, c := range current.Curves {
		for _, p := range c.Points {
			if k := (key{p.Height, p.Target}); !seen[k] {
				out = append(out, PointDelta{
					Height: p.Height, Target: p.Target,
					Kbps: p.Kbps, VMAF: p.VMAF,
					Note: "new in this run",
				})
			}
		}
	}
	return out
}

// diffLadder pairs two ladders rung by rung, and refuses when they are not the
// same resolutions in the same order.
//
// A ladder with different rungs is not a set of deltas: pairing a 1080p rung
// against a 720p one because they happen to sit at the same index would invent a
// comparison. When the shape changed, that *is* the finding.
func diffLadder(base, current []Point) ([]RungDelta, bool) {
	if len(base) != len(current) {
		return nil, true
	}
	for i := range base {
		if base[i].Height != current[i].Height {
			return nil, true
		}
	}
	out := make([]RungDelta, 0, len(base))
	for i := range base {
		out = append(out, RungDelta{
			Height:   base[i].Height,
			BaseKbps: base[i].Kbps, Kbps: current[i].Kbps,
			BaseVMAF: base[i].VMAF, VMAF: current[i].VMAF,
		})
	}
	return out, false
}

// DefaultRegressionThreshold is the BD-rate a run may drift by before it counts
// as a regression.
//
// Not zero: two runs of the same grid on the same binaries differ by a fraction
// of a percent, because encoders are not bit-exact across runs and rate control
// lands in a slightly different place each time. A gate at zero fails on that
// noise and gets switched off, which is worse than no gate.
const DefaultRegressionThreshold = 2.0

// Regression is one reason a comparison should fail a pipeline.
type Regression struct {
	Encoder string `json:"encoder"`
	Reason  string `json:"reason"`
	// Value and Threshold are the measured drift and the limit it passed, in
	// percent. Both zero for a regression that is not a matter of degree.
	Value     float64 `json:"value,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
}

// Regressions lists why a comparison should fail, or nothing when it should not.
//
// Two things count, and deliberately only two:
//
//   - the ladder got more expensive at equal measured quality, by more than the
//     threshold. Quality-normalised on purpose: a run that spends more bits and
//     shows more for them has not regressed;
//   - the grid stopped reaching target_vmaf when it used to. That is not a
//     matter of degree — the run can no longer answer the question it was asked.
//
// A curve that moved without moving the BD-rate is not a regression. Neither is
// a wider or narrower grid: those change what was measured, and this function
// judges results rather than experiments.
func Regressions(diffs []EncoderDiff, threshold float64) []Regression {
	if threshold <= 0 {
		threshold = DefaultRegressionThreshold
	}
	var out []Regression
	for _, d := range diffs {
		if d.TargetLost() {
			out = append(out, Regression{
				Encoder: d.Encoder,
				Reason:  "no measured point reaches the target any more, and the baseline reached it",
			})
		}
		if d.BDRate.OK() && d.BDRate.RatePct > threshold {
			out = append(out, Regression{
				Encoder:   d.Encoder,
				Reason:    fmt.Sprintf("needs %.2f%% more bitrate for the quality the baseline delivered", d.BDRate.RatePct),
				Value:     d.BDRate.RatePct,
				Threshold: threshold,
			})
		}
	}
	return out
}
