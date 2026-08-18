package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
)

// Markdown renders the report for a PR comment, a wiki page or a CI job
// summary: same content as the text output, in tables.
func Markdown(out io.Writer, r Report) error {
	w := &errWriter{w: out}
	fmt.Fprintf(w, "# ladder-bench report\n\n")
	fmt.Fprintf(w, "- **Source**: `%s` — %s, %s, %s, %.1fs\n", r.Input,
		geometry(r.Reference.Source.Width, r.Reference.Source.Height),
		fps(r.Reference.Source.FrameRate), r.Reference.Source.Codec, r.Reference.Source.Duration)
	fmt.Fprintf(w, "- **Reference clip**: `%s` — %s, %.1fs\n", r.Reference.Path,
		geometry(r.Reference.Media.Width, r.Reference.Media.Height), r.Reference.Media.Duration)
	fmt.Fprintf(w, "- **Measured**: %d points · target VMAF %.1f · ladder step %.1f\n", len(r.Results), r.Options.TargetVMAF, r.Options.LadderStep)
	fmt.Fprintf(w, "- **Tool**: %s %s, %s\n", r.Tool, r.Version, r.Generated)
	if r.Env.FFmpeg != "" {
		fmt.Fprintf(w, "- **ffmpeg**: `%s`\n", r.Env.FFmpeg)
	}
	if len(r.Env.LibVMAF) > 0 {
		fmt.Fprintf(w, "- **libvmaf**: %s\n", strings.Join(r.Env.LibVMAF, ", "))
	}
	if r.Env.ConfigSHA256 != "" {
		fmt.Fprintf(w, "- **Config fingerprint**: `%s`\n", r.Env.ConfigShort())
	}
	if r.Env.MixedLibVMAF() {
		fmt.Fprintf(w, "\n> These points were not all measured by the same libvmaf. Re-run with `--force` before comparing them.\n")
	}

	for _, a := range r.Analyses {
		fmt.Fprintf(w, "\n## Encoder `%s`\n", a.Encoder)
		psnr, ssim := measured(a, pointPSNR), measured(a, pointSSIM)
		tails := measured(a, pointP1) || measured(a, pointP5)
		fmt.Fprintf(w, "\n### Measurements\n\n")
		head := "| Resolution | Target | Actual | VMAF | VMAF harmonic |"
		rule := "|---|---:|---:|---:|---:|"
		if tails {
			head, rule = head+" P5 | P1 |", rule+"---:|---:|"
		}
		head += " VMAF min | Gain per +10% |"
		rule += "---:|---:|"
		if psnr {
			head, rule = head+" PSNR-Y |", rule+"---:|"
		}
		if ssim {
			head, rule = head+" SSIM |", rule+"---:|"
		}
		fmt.Fprintln(w, head)
		fmt.Fprintln(w, rule)
		for _, c := range a.Curves {
			g := gains(c.Points)
			for _, p := range c.Points {
				gain := "—"
				if v, ok := g[p.Target]; ok {
					gain = fmt.Sprintf("%.2f", v)
				}
				row := fmt.Sprintf("| %s | %dk | %s | %.2f | %s |",
					res(p.Height), p.Target, kbps(p.Kbps), p.VMAF, harmonic(p.VMAFHarmonic))
				if tails {
					row += " " + optional(p.P5, "%.2f") + " | " + optional(p.P1, "%.2f") + " |"
				}
				row += fmt.Sprintf(" %.2f | %s |", p.VMAFMin, gain)
				if psnr {
					row += " " + optional(p.PSNR, "%.2f") + " |"
				}
				if ssim {
					row += " " + optional(p.SSIM, "%.4f") + " |"
				}
				fmt.Fprintln(w, row)
			}
		}

		fmt.Fprintf(w, "\n### Saturation\n\n")
		for _, c := range a.Curves {
			switch {
			case c.StillClimbing:
				fmt.Fprintf(w, "- **%s** — still climbing at the top of the grid; extend it upward.\n", res(c.Height))
			case c.Knee == nil:
				fmt.Fprintf(w, "- **%s** — not enough points to tell.\n", res(c.Height))
			case c.FlatFromStart:
				fmt.Fprintf(w, "- **%s** — already flat at %s, the cheapest point measured; extend the grid downward.\n",
					res(c.Height), kbps(c.Knee.Kbps))
			default:
				fmt.Fprintf(w, "- **%s** — flattens at **%s** (VMAF %.1f): the top %.0f%% of this grid's bitrate buys nothing.\n",
					res(c.Height), kbps(c.Knee.Kbps), c.Knee.VMAF, c.WastedPct)
			}
		}

		fmt.Fprintf(w, "\n### Efficient frontier\n\n%s\n", frontier(a.Hull))

		fmt.Fprintf(w, "\n### Recommended ladder\n\n")
		if !a.TargetReached {
			fmt.Fprintf(w, "> No measured point reached VMAF %.1f — the top rung is the best the grid could do.\n\n", r.Options.TargetVMAF)
		}
		fmt.Fprintln(w, "| Resolution | Bitrate | VMAF |")
		fmt.Fprintln(w, "|---|---:|---:|")
		for _, p := range a.Ladder {
			fmt.Fprintf(w, "| %s | %s | %.2f |\n", res(p.Height), kbps(p.Kbps), p.VMAF)
		}
		fmt.Fprintf(w, "| **total** | **%s** | |\n", kbps(a.LadderTotalKbps))

		writeMarkdownSavings(w, a)
	}
	writeMarkdownBDRates(w, r.BDRates)
	return w.err
}

func writeMarkdownBDRates(w io.Writer, cmps []analysis.Comparison) {
	if len(cmps) == 0 {
		return
	}
	fmt.Fprintf(w, "\n## BD-rate versus `%s`\n\n", cmps[0].Anchor)
	fmt.Fprintf(w, "Bitrate needed for the same measured quality, averaged over the range both encoders reached. Negative means fewer bits.\n")
	for _, c := range cmps {
		fmt.Fprintf(w, "\n### `%s`\n\n", c.Test)
		fmt.Fprintln(w, "| Scope | BD-rate | Over | Method |")
		fmt.Fprintln(w, "|---|---:|---|---|")
		writeMarkdownBDRow(w, "Efficient frontier", c.Frontier)
		for _, bd := range c.ByHeight {
			writeMarkdownBDRow(w, res(bd.Height), bd)
		}
	}
}

func writeMarkdownBDRow(w io.Writer, label string, bd analysis.BD) {
	if !bd.OK() {
		fmt.Fprintf(w, "| %s | — | %s | — |\n", label, bd.Note)
		return
	}
	fmt.Fprintf(w, "| %s | %s | VMAF %.1f–%.1f | %s |\n",
		label, pct(bd.RatePct), bd.LowVMAF, bd.HighVMAF, bd.Method)
}

func writeMarkdownSavings(w io.Writer, a analysis.Result) {
	if len(a.Savings) == 0 {
		return
	}
	fmt.Fprintf(w, "\n### Versus the current ladder\n\n")
	fmt.Fprintf(w, "Same quality as today, bought on the frontier — %d of %d rungs comparable:\n\n",
		a.ComparedRungs, len(a.Savings))
	fmt.Fprintf(w, "**%dk → %s (%s)**\n\n", a.CurrentTotalKbps, kbps(a.EfficientTotalKbps), pct(-a.TotalSavedPct))
	fmt.Fprintln(w, "| Current rung | Delivers | Same quality costs | Change |")
	fmt.Fprintln(w, "|---|---:|---|---:|")
	for _, s := range a.Savings {
		if s.Note != "" {
			fmt.Fprintf(w, "| %s %dk | — | %s | — |\n", res(s.Current.Height), s.Current.Kbps, s.Note)
			continue
		}
		fmt.Fprintf(w, "| %s %dk | VMAF %.2f | %s %s | %s |\n",
			res(s.Current.Height), s.Current.Kbps, s.CurrentVMAF,
			res(s.EfficientHeight), kbps(s.EfficientKbps), pct(-s.SavedPct))
	}
}
