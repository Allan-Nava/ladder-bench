package output

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
)

// Text renders the terminal report.
func Text(out io.Writer, r Report) error {
	w := &errWriter{w: out}
	fmt.Fprintf(w, "%s %s — %s\n", r.Tool, r.Version, r.Generated)
	fmt.Fprintf(w, "source     %s  %s  %s  %s  %.1fs\n", r.Input,
		geometry(r.Reference.Source.Width, r.Reference.Source.Height),
		fps(r.Reference.Source.FrameRate), r.Reference.Source.Codec, r.Reference.Source.Duration)
	fmt.Fprintf(w, "reference  %s  %s  %.1fs%s\n", r.Reference.Path,
		geometry(r.Reference.Media.Width, r.Reference.Media.Height),
		r.Reference.Media.Duration, reusedNote(r.Reference.Reused))
	fmt.Fprintf(w, "measured   %d points across %d encoder(s)\n", len(r.Results), len(r.Analyses))

	for _, a := range r.Analyses {
		fmt.Fprintln(w)
		if sample, ok := encoderOf(r.Results, a.Encoder); ok {
			fmt.Fprintf(w, "encoder %s (%s%s)\n", a.Encoder, sample.Codec, presetNote(sample.Preset))
		} else {
			fmt.Fprintf(w, "encoder %s\n", a.Encoder)
		}
		writeCurves(w, a)
		writeSaturation(w, a)
		writeFrontier(w, a, r.Options)
		writeSavings(w, a)
	}
	return w.err
}

func writeCurves(w io.Writer, a analysis.Result) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  RES\tTARGET\tACTUAL\tVMAF\tMIN\tGAIN/+10%")
	for _, c := range a.Curves {
		g := gains(c.Points)
		for _, p := range c.Points {
			gain := "—"
			if v, ok := g[p.Target]; ok {
				gain = fmt.Sprintf("%.2f", v)
			}
			fmt.Fprintf(tw, "  %s\t%dk\t%s\t%.2f\t%.2f\t%s\n",
				res(p.Height), p.Target, kbps(p.Kbps), p.VMAF, p.VMAFMin, gain)
		}
	}
	_ = tw.Flush()
}

func writeSaturation(w io.Writer, a analysis.Result) {
	fmt.Fprintln(w, "\n  saturation")
	for _, c := range a.Curves {
		switch {
		case c.StillClimbing:
			fmt.Fprintf(w, "    %-6s still climbing at the top of the grid — extend it upward\n", res(c.Height))
		case c.Knee == nil:
			fmt.Fprintf(w, "    %-6s not enough points to tell\n", res(c.Height))
		case c.FlatFromStart:
			fmt.Fprintf(w, "    %-6s already flat at the cheapest point measured (%s) — extend the grid downward\n",
				res(c.Height), kbps(c.Knee.Kbps))
		default:
			fmt.Fprintf(w, "    %-6s flattens at %s (VMAF %.1f) — the top %.0f%% of this grid's bitrate buys nothing\n",
				res(c.Height), kbps(c.Knee.Kbps), c.Knee.VMAF, c.WastedPct)
		}
	}
}

func writeFrontier(w io.Writer, a analysis.Result, opt analysis.Options) {
	fmt.Fprintln(w, "\n  efficient frontier")
	fmt.Fprintf(w, "    %s\n", frontier(a.Hull))
	fmt.Fprintf(w, "\n  recommended ladder (steps of %.1f VMAF, target %.1f)\n", opt.LadderStep, opt.TargetVMAF)
	if !a.TargetReached {
		fmt.Fprintf(w, "    ! no measured point reached VMAF %.1f — the top rung is the best the grid could do\n", opt.TargetVMAF)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range a.Ladder {
		fmt.Fprintf(tw, "    %s\t%s\tVMAF %.2f\n", res(p.Height), kbps(p.Kbps), p.VMAF)
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "    total %s\n", kbps(a.LadderTotalKbps))
}

func writeSavings(w io.Writer, a analysis.Result) {
	if len(a.Savings) == 0 {
		return
	}
	// Like for like: the same quality those rungs deliver today, bought on the
	// frontier. Comparing against the recommended ladder's total instead would
	// compare two different quality targets and two different rung counts.
	fmt.Fprintf(w, "\n  vs current ladder — same quality, %d of %d rungs comparable\n",
		a.ComparedRungs, len(a.Savings))
	fmt.Fprintf(w, "    %dk → %s  (%s)\n",
		a.CurrentTotalKbps, kbps(a.EfficientTotalKbps), pct(-a.TotalSavedPct))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, s := range a.Savings {
		if s.Note != "" {
			fmt.Fprintf(tw, "    %s %dk\t—\t%s\n", res(s.Current.Height), s.Current.Kbps, s.Note)
			continue
		}
		fmt.Fprintf(tw, "    %s %dk\t→ %s %s\tsame quality (VMAF %.2f)\t%s\n",
			res(s.Current.Height), s.Current.Kbps,
			res(s.EfficientHeight), kbps(s.EfficientKbps), s.CurrentVMAF, pct(-s.SavedPct))
	}
	_ = tw.Flush()
}

func reusedNote(reused bool) string {
	if reused {
		return "  (reused)"
	}
	return ""
}

func presetNote(preset string) string {
	if preset == "" {
		return ""
	}
	return ", preset " + preset
}
