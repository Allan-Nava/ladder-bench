package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Allan-Nava/ladder-bench/internal/analysis"
	"github.com/Allan-Nava/ladder-bench/internal/ffmpeg"
)

// ExportFormats lists what a ladder can be written as, for help text.
func ExportFormats() string { return "hls, dash, json" }

// Rung is one rung of an exported ladder.
//
// It carries the measured bitrate and the declared peak separately, because they
// answer different questions: a packager needs the peak to advertise, and a
// capacity plan needs the average that will actually be shipped.
type Rung struct {
	Height int `json:"height"`
	// Width is derived from the reference geometry the way `scale=-2:H` derives
	// it — the nearest even width that preserves the aspect ratio. Zero when the
	// report has no geometry to derive it from.
	Width int `json:"width,omitempty"`
	// TargetKbps is what the grid asked for, PeakKbps the cap the encode was
	// given, and Kbps what the file measured.
	TargetKbps int     `json:"target_kbps"`
	PeakKbps   int     `json:"peak_kbps"`
	Kbps       float64 `json:"kbps"`
	VMAF       float64 `json:"vmaf"`
}

// Ladder is a recommended ladder ready to be handed to something else.
type Ladder struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Encoder string `json:"encoder"`
	Codec   string `json:"codec,omitempty"`
	Preset  string `json:"preset,omitempty"`
	// ConfigSHA256 and Generated tie the ladder back to the run that produced it,
	// so a playlist in production can be traced to the measurement behind it.
	ConfigSHA256 string `json:"config_sha256,omitempty"`
	Generated    string `json:"generated,omitempty"`
	// TargetVMAF is the quality the top rung aimed at, and TargetReached whether
	// the grid got there. A ladder exported from a run that never reached its
	// target is still a ladder; it is just not the one that was asked for.
	TargetVMAF    float64 `json:"target_vmaf"`
	TargetReached bool    `json:"target_reached"`
	// Rungs run best first, the order the report shows and the order a master
	// playlist is usually written in.
	Rungs []Rung `json:"rungs"`
}

// BuildLadder turns one encoder's recommended ladder into an exportable form.
//
// The encoder must be named when a report has more than one: which codec you ship
// is not something to guess at, and guessing wrong produces a playlist that looks
// right and describes a different experiment.
func BuildLadder(r Report, encoder string) (Ladder, error) {
	if len(r.Analyses) == 0 {
		return Ladder{}, fmt.Errorf("this report has no analysis in it")
	}
	var a analysis.Result
	switch {
	case encoder != "":
		found := false
		for _, x := range r.Analyses {
			if x.Encoder == encoder {
				a, found = x, true
				break
			}
		}
		if !found {
			return Ladder{}, fmt.Errorf("no encoder %q in this report (it has %s)", encoder, strings.Join(encoderNames(r), ", "))
		}
	case len(r.Analyses) == 1:
		a = r.Analyses[0]
	default:
		return Ladder{}, fmt.Errorf("this report measured %s — name one with --encoder", strings.Join(encoderNames(r), ", "))
	}

	l := Ladder{
		Tool: "ladder-bench", Version: r.Version, Encoder: a.Encoder,
		ConfigSHA256: r.Env.ConfigSHA256, Generated: r.Generated,
		TargetVMAF: r.Options.TargetVMAF, TargetReached: a.TargetReached,
	}
	if sample, ok := encoderOf(r.Results, a.Encoder); ok {
		l.Codec, l.Preset = sample.Codec, sample.Preset
	}
	ref := primary(r.References).Media
	for _, p := range a.Ladder {
		l.Rungs = append(l.Rungs, Rung{
			Height:     p.Height,
			Width:      evenWidth(p.Height, ref.Width, ref.Height),
			TargetKbps: p.Target,
			PeakKbps:   p.Target * ffmpeg.MaxratePct / 100,
			Kbps:       p.Kbps,
			VMAF:       p.VMAF,
		})
	}
	return l, nil
}

func encoderNames(r Report) []string {
	out := make([]string, 0, len(r.Analyses))
	for _, a := range r.Analyses {
		out = append(out, a.Encoder)
	}
	sort.Strings(out)
	return out
}

// evenWidth derives the width `scale=-2:height` would have produced: the nearest
// even width that keeps the reference's aspect ratio.
//
// Derived rather than recorded, because the grid is expressed in heights and the
// encodes never stored their width. It is the same arithmetic ffmpeg does, and it
// is left at zero when there is no reference geometry to derive it from — an
// invented RESOLUTION is worse than none.
func evenWidth(height, refW, refH int) int {
	if height <= 0 || refW <= 0 || refH <= 0 {
		return 0
	}
	w := float64(height) * float64(refW) / float64(refH)
	return int((w+1)/2) * 2
}

// rungName identifies a rung uniquely, the way the work dir names its encodes.
//
// The height alone is not enough: a recommended ladder can hold two rungs at the
// same resolution — a well-encoded 1080p at 6000k and a starved one at 1500k are
// different rungs — and two playlist entries pointing at one URI is a playlist
// players cannot use.
func rungName(r Rung) string {
	return fmt.Sprintf("%dp_%dk", r.Height, r.TargetKbps)
}

// ExportLadder writes a ladder in the named format.
func ExportLadder(w io.Writer, l Ladder, format string) error {
	switch format {
	case "", "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(l)
	case "hls", "m3u8":
		return exportHLS(w, l)
	case "dash", "mpd":
		return exportDASH(w, l)
	default:
		return fmt.Errorf("unknown export format %q (%s)", format, ExportFormats())
	}
}

// codecsNote is printed by every playlist export.
//
// The CODECS attribute is an RFC 6381 string carrying the profile and level the
// encoder actually chose, and this tool does not know them: it knows the encoder
// name and the bitrate. Guessing produces a playlist that players reject in ways
// that look like content problems, so the attribute is left out and named as the
// one thing still to fill in.
const codecsNote = "CODECS is deliberately absent: it carries the profile and level the encoder chose,\n" +
	"which this tool does not measure. Read it off a real encode before shipping, e.g.\n" +
	"  ffprobe -v error -select_streams v:0 -show_entries stream=codec_tag_string,profile,level -of default=nw=1 rung.mp4"

func exportHLS(out io.Writer, l Ladder) error {
	w := &errWriter{w: out}
	fmt.Fprintln(w, "#EXTM3U")
	fmt.Fprintln(w, "#EXT-X-VERSION:7")
	fmt.Fprintf(w, "\n## %s %s — measured ladder for %s", l.Tool, l.Version, l.Encoder)
	if l.Codec != "" {
		fmt.Fprintf(w, " (%s%s)", l.Codec, presetNote(l.Preset))
	}
	fmt.Fprintln(w)
	if l.ConfigSHA256 != "" {
		fmt.Fprintf(w, "## config %s, generated %s\n", l.ConfigSHA256[:12], l.Generated)
	}
	if !l.TargetReached {
		fmt.Fprintf(w, "## WARNING: no measured point reached VMAF %.1f — the top rung is the best the grid could do\n", l.TargetVMAF)
	}
	for _, line := range strings.Split(codecsNote, "\n") {
		fmt.Fprintf(w, "## %s\n", line)
	}
	// BANDWIDTH is the peak a player must be able to sustain, which for these
	// encodes is the cap they were given. AVERAGE-BANDWIDTH is what they measured.
	for _, r := range l.Rungs {
		fmt.Fprintf(w, "\n## VMAF %.2f\n", r.VMAF)
		fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d", r.PeakKbps*1000, int(r.Kbps*1000))
		if r.Width > 0 {
			fmt.Fprintf(w, ",RESOLUTION=%dx%d", r.Width, r.Height)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%s.m3u8\n", rungName(r))
	}
	return w.err
}

func exportDASH(out io.Writer, l Ladder) error {
	w := &errWriter{w: out}
	fmt.Fprintf(w, "<!-- %s %s — measured ladder for %s", l.Tool, l.Version, l.Encoder)
	if l.Codec != "" {
		fmt.Fprintf(w, " (%s%s)", l.Codec, presetNote(l.Preset))
	}
	fmt.Fprintln(w)
	if l.ConfigSHA256 != "" {
		fmt.Fprintf(w, "     config %s, generated %s\n", l.ConfigSHA256[:12], l.Generated)
	}
	if !l.TargetReached {
		fmt.Fprintf(w, "     WARNING: no measured point reached VMAF %.1f\n", l.TargetVMAF)
	}
	fmt.Fprintf(w, "     %s -->\n", strings.ReplaceAll(codecsNote, "\n", "\n     "))
	fmt.Fprintln(w, `<AdaptationSet contentType="video" segmentAlignment="true" startWithSAP="1">`)
	// Representations are written cheapest first, which is the order a manifest
	// is conventionally read in — the opposite of the report's.
	for i := len(l.Rungs) - 1; i >= 0; i-- {
		r := l.Rungs[i]
		fmt.Fprintf(w, "  <!-- VMAF %.2f, measured %.0f kbps -->\n", r.VMAF, r.Kbps)
		fmt.Fprintf(w, `  <Representation id="%s" bandwidth="%d"`, rungName(r), r.PeakKbps*1000)
		if r.Width > 0 {
			fmt.Fprintf(w, ` width="%d" height="%d"`, r.Width, r.Height)
		} else {
			fmt.Fprintf(w, ` height="%d"`, r.Height)
		}
		fmt.Fprintln(w, ` codecs="TODO" />`)
	}
	fmt.Fprintln(w, "</AdaptationSet>")
	return w.err
}
