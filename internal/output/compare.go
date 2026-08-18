package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
)

// Comparison is two runs put side by side.
type Comparison struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	// Baseline and Current are the two reports, trimmed to what a comparison
	// needs to identify them. The full reports stay in their own files.
	Baseline Provenance `json:"baseline"`
	Current  Provenance `json:"current"`
	// Comparable is false when the two runs did not measure the same experiment.
	// Everything below is still shown — a reader may well want to see it — but no
	// number in it answers "did this get worse".
	Comparable bool                   `json:"comparable"`
	Encoders   []analysis.EncoderDiff `json:"encoders"`
	// OnlyInBaseline and OnlyInCurrent are encoders one run measured and the
	// other did not. Named rather than dropped: a missing encoder is a changed
	// experiment, not an absent finding.
	OnlyInBaseline []string              `json:"only_in_baseline,omitempty"`
	OnlyInCurrent  []string              `json:"only_in_current,omitempty"`
	Regressions    []analysis.Regression `json:"regressions,omitempty"`
	Threshold      float64               `json:"threshold"`
}

// Provenance is how a run is identified in a comparison.
type Provenance struct {
	Path      string      `json:"path"`
	Version   string      `json:"version,omitempty"`
	Generated string      `json:"generated,omitempty"`
	Input     string      `json:"input,omitempty"`
	Env       Environment `json:"environment"`
}

// provenance reduces a report to what identifies it.
func provenance(path string, r Report) Provenance {
	return Provenance{Path: path, Version: r.Version, Generated: r.Generated, Input: r.Input, Env: r.Env}
}

// Compare pairs two reports encoder by encoder.
//
// The config fingerprint decides whether the comparison means anything. Two runs
// of different experiments produce differences that are real and answer nothing:
// a wider grid, another target, one more clip all move every number without
// anything having got better or worse. So the mismatch is reported as the finding
// rather than buried under the tables it invalidates.
func Compare(basePath string, base Report, curPath string, cur Report, threshold float64) Comparison {
	c := Comparison{
		Tool:      "ladder-bench",
		Baseline:  provenance(basePath, base),
		Current:   provenance(curPath, cur),
		Threshold: threshold,
	}
	if threshold <= 0 {
		c.Threshold = analysis.DefaultRegressionThreshold
	}
	c.Comparable = base.Env.ConfigSHA256 != "" &&
		base.Env.ConfigSHA256 == cur.Env.ConfigSHA256

	byName := map[string]analysis.Result{}
	for _, a := range cur.Analyses {
		byName[a.Encoder] = a
	}
	seen := map[string]bool{}
	for _, b := range base.Analyses {
		seen[b.Encoder] = true
		a, ok := byName[b.Encoder]
		if !ok {
			c.OnlyInBaseline = append(c.OnlyInBaseline, b.Encoder)
			continue
		}
		c.Encoders = append(c.Encoders, analysis.Diff(b, a))
	}
	for _, a := range cur.Analyses {
		if !seen[a.Encoder] {
			c.OnlyInCurrent = append(c.OnlyInCurrent, a.Encoder)
		}
	}
	// Only a comparable pair can regress. Gating on two different experiments
	// would fail a pipeline for a config edit, which teaches people to switch
	// the gate off.
	if c.Comparable {
		c.Regressions = analysis.Regressions(c.Encoders, c.Threshold)
	}
	return c
}

// Regressed reports whether this comparison should fail a pipeline.
func (c Comparison) Regressed() bool { return len(c.Regressions) > 0 }

// RenderComparison writes a comparison in the named format.
func RenderComparison(w io.Writer, c Comparison, format string) error {
	switch format {
	case "", "text":
		return comparisonText(w, c)
	case "markdown", "md":
		return comparisonMarkdown(w, c)
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(c)
	default:
		return fmt.Errorf("unknown output format %q (%s)", format, Formats())
	}
}

func comparisonText(out io.Writer, c Comparison) error {
	w := &errWriter{w: out}
	fmt.Fprintf(w, "%s %s — comparing two runs\n", c.Tool, c.Version)
	writeProvenance(w, "baseline", c.Baseline)
	writeProvenance(w, "current ", c.Current)
	if !c.Comparable {
		fmt.Fprintf(w, "\n! these runs did not measure the same experiment — the config fingerprints differ,\n")
		fmt.Fprintf(w, "  so the differences below are real and answer nothing about better or worse.\n")
		fmt.Fprintf(w, "  Re-measure the baseline with the current config before comparing.\n")
	}
	for _, name := range c.OnlyInBaseline {
		fmt.Fprintf(w, "\n! encoder %s is in the baseline and not in this run\n", name)
	}
	for _, name := range c.OnlyInCurrent {
		fmt.Fprintf(w, "\n! encoder %s is new in this run, so it has nothing to be compared against\n", name)
	}

	for _, d := range c.Encoders {
		fmt.Fprintf(w, "\nencoder %s\n", d.Encoder)
		fmt.Fprintln(w, "\n  bitrate for the quality the baseline delivered — negative is cheaper now")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		writeBDLine(tw, "frontier", d.BDRate)
		for _, bd := range d.ByHeight {
			writeBDLine(tw, res(bd.Height), bd)
		}
		_ = tw.Flush()

		writeComparedPoints(w, d)
		writeComparedLadder(w, d)
	}

	if len(c.Regressions) > 0 {
		fmt.Fprintf(w, "\nREGRESSION (threshold %.2f%%)\n", c.Threshold)
		for _, r := range c.Regressions {
			fmt.Fprintf(w, "  %s: %s\n", r.Encoder, r.Reason)
		}
	}
	return w.err
}

func writeProvenance(w io.Writer, label string, p Provenance) {
	fmt.Fprintf(w, "%s   %s", label, p.Path)
	if p.Generated != "" {
		fmt.Fprintf(w, "  %s", p.Generated)
	}
	if p.Version != "" {
		fmt.Fprintf(w, "  (%s)", p.Version)
	}
	fmt.Fprintln(w)
	if p.Env.ConfigSHA256 != "" {
		fmt.Fprintf(w, "           config %s", p.Env.ConfigShort())
		if len(p.Env.LibVMAF) > 0 {
			fmt.Fprintf(w, ", libvmaf %s", strings.Join(p.Env.LibVMAF, "/"))
		}
		fmt.Fprintln(w)
	}
}

func writeComparedPoints(w io.Writer, d analysis.EncoderDiff) {
	fmt.Fprintln(w, "\n  per point")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "    RES\tTARGET\tVMAF\tΔVMAF\tACTUAL\tΔBITRATE")
	for _, p := range d.Points {
		if !p.Comparable() {
			fmt.Fprintf(tw, "    %s\t%dk\t—\t—\t—\t%s\n", res(p.Height), p.Target, p.Note)
			continue
		}
		fmt.Fprintf(tw, "    %s\t%dk\t%.2f\t%+.2f\t%s\t%s\n",
			res(p.Height), p.Target, p.VMAF, p.DeltaVMAF(), kbps(p.Kbps), pct(p.DeltaKbpsPct()))
	}
	_ = tw.Flush()
}

func writeComparedLadder(w io.Writer, d analysis.EncoderDiff) {
	if d.TargetLost() {
		fmt.Fprintln(w, "\n  ! the baseline reached the target and this run does not")
	}
	if d.LadderChanged {
		fmt.Fprintln(w, "\n  the recommended ladder changed shape, so its rungs cannot be paired")
		fmt.Fprintf(w, "    baseline  %s\n", frontier(d.BaseLadder))
		fmt.Fprintf(w, "    current   %s\n", frontier(d.CurrentLadder))
		return
	}
	if len(d.Ladder) == 0 {
		return
	}
	fmt.Fprintln(w, "\n  recommended ladder")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range d.Ladder {
		fmt.Fprintf(tw, "    %s\t%s → %s\t%s\tVMAF %.2f → %.2f\n",
			res(r.Height), kbps(r.BaseKbps), kbps(r.Kbps), pct(r.DeltaKbpsPct()), r.BaseVMAF, r.VMAF)
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "    total %s → %s\n", kbps(d.BaseTotalKbps), kbps(d.LadderTotalKbps))
}

func comparisonMarkdown(out io.Writer, c Comparison) error {
	w := &errWriter{w: out}
	fmt.Fprintf(w, "# ladder-bench comparison\n\n")
	fmt.Fprintf(w, "| | Baseline | Current |\n|---|---|---|\n")
	fmt.Fprintf(w, "| Report | `%s` | `%s` |\n", c.Baseline.Path, c.Current.Path)
	fmt.Fprintf(w, "| Generated | %s | %s |\n", c.Baseline.Generated, c.Current.Generated)
	fmt.Fprintf(w, "| Tool | %s | %s |\n", c.Baseline.Version, c.Current.Version)
	fmt.Fprintf(w, "| libvmaf | %s | %s |\n",
		strings.Join(c.Baseline.Env.LibVMAF, "/"), strings.Join(c.Current.Env.LibVMAF, "/"))
	fmt.Fprintf(w, "| Config | `%s` | `%s` |\n", c.Baseline.Env.ConfigShort(), c.Current.Env.ConfigShort())

	if !c.Comparable {
		fmt.Fprintf(w, "\n> **These runs did not measure the same experiment.** The config fingerprints differ, so the differences below are real and answer nothing about better or worse. Re-measure the baseline with the current config before comparing.\n")
	}
	for _, name := range c.OnlyInBaseline {
		fmt.Fprintf(w, "\n> Encoder `%s` is in the baseline and not in this run.\n", name)
	}
	for _, name := range c.OnlyInCurrent {
		fmt.Fprintf(w, "\n> Encoder `%s` is new in this run, so it has nothing to be compared against.\n", name)
	}

	for _, d := range c.Encoders {
		fmt.Fprintf(w, "\n## Encoder `%s`\n", d.Encoder)
		fmt.Fprintf(w, "\nBitrate for the quality the baseline delivered — negative is cheaper now.\n\n")
		fmt.Fprintln(w, "| Scope | Change | Over | Method |")
		fmt.Fprintln(w, "|---|---:|---|---|")
		writeMarkdownBDRow(w, "Efficient frontier", d.BDRate)
		for _, bd := range d.ByHeight {
			writeMarkdownBDRow(w, res(bd.Height), bd)
		}

		fmt.Fprintf(w, "\n### Per point\n\n")
		fmt.Fprintln(w, "| Resolution | Target | VMAF | ΔVMAF | Actual | ΔBitrate |")
		fmt.Fprintln(w, "|---|---:|---:|---:|---:|---:|")
		for _, p := range d.Points {
			if !p.Comparable() {
				fmt.Fprintf(w, "| %s | %dk | — | — | — | %s |\n", res(p.Height), p.Target, p.Note)
				continue
			}
			fmt.Fprintf(w, "| %s | %dk | %.2f | %+.2f | %s | %s |\n",
				res(p.Height), p.Target, p.VMAF, p.DeltaVMAF(), kbps(p.Kbps), pct(p.DeltaKbpsPct()))
		}

		if d.TargetLost() {
			fmt.Fprintf(w, "\n> The baseline reached the target and this run does not.\n")
		}
		fmt.Fprintf(w, "\n### Recommended ladder\n\n")
		if d.LadderChanged {
			fmt.Fprintf(w, "The ladder changed shape, so its rungs cannot be paired.\n\n")
			fmt.Fprintf(w, "- **Baseline**: %s\n", frontier(d.BaseLadder))
			fmt.Fprintf(w, "- **Current**: %s\n", frontier(d.CurrentLadder))
		} else {
			fmt.Fprintln(w, "| Resolution | Bitrate | Change | VMAF |")
			fmt.Fprintln(w, "|---|---|---:|---|")
			for _, r := range d.Ladder {
				fmt.Fprintf(w, "| %s | %s → %s | %s | %.2f → %.2f |\n",
					res(r.Height), kbps(r.BaseKbps), kbps(r.Kbps), pct(r.DeltaKbpsPct()), r.BaseVMAF, r.VMAF)
			}
			fmt.Fprintf(w, "| **total** | **%s → %s** | | |\n", kbps(d.BaseTotalKbps), kbps(d.LadderTotalKbps))
		}
	}

	if len(c.Regressions) > 0 {
		fmt.Fprintf(w, "\n## Regression (threshold %.2f%%)\n\n", c.Threshold)
		for _, r := range c.Regressions {
			fmt.Fprintf(w, "- **`%s`** — %s\n", r.Encoder, r.Reason)
		}
	}
	return w.err
}
