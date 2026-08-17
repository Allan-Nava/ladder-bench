package ffmpeg

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ReferenceArgs cuts the reference clip out of the source.
//
// The clip is re-encoded losslessly (x264 crf 0) instead of stream-copied on
// purpose. A copy can only cut on a keyframe, so the clip would start at a
// different frame than asked and carry the source's timestamps; every encode
// and every VMAF comparison downstream would then be measuring a slightly
// different set of frames. Lossless costs disk, not quality: the reference
// stays frame-exact and cheap to decode over and over.
//
// The pixel format is normalised to yuv420p here, once, because the VMAF
// models are trained on 8-bit 4:2:0 and a mixed-format comparison makes
// libvmaf convert implicitly — silently, and differently per input.
func ReferenceArgs(input, out string, start, duration time.Duration) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if start > 0 {
		// -ss before -i seeks by index instead of decoding to the cut point.
		args = append(args, "-ss", formatSeconds(start))
	}
	args = append(args, "-i", input)
	if duration > 0 {
		args = append(args, "-t", formatSeconds(duration))
	}
	args = append(args,
		"-map", "0:v:0",
		"-an", "-sn", "-dn",
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "0",
		"-pix_fmt", "yuv420p",
		out,
	)
	return args
}

// EncodeSpec is one grid point: a resolution and a bitrate for one encoder.
type EncodeSpec struct {
	Reference string
	Out       string
	Height    int
	Kbps      int
	Codec     string
	Preset    string
	Extra     []string
	// GOP is the keyframe interval in frames. Zero leaves the encoder default.
	GOP int
}

// EncodeArgs builds the encode of one grid point.
//
// The rate control is deliberately capped (maxrate/bufsize), not plain average
// bitrate: an ABR rendition that overshoots its declared bandwidth is a
// rendition the player will abandon, so measuring an uncapped encode would
// score quality nobody can actually deliver at that rung.
func EncodeArgs(s EncodeSpec) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", s.Reference}
	// scale=-2 keeps the aspect ratio and rounds the width to an even number,
	// which yuv420p requires.
	args = append(args, "-vf", fmt.Sprintf("scale=-2:%d:flags=lanczos", s.Height))
	args = append(args, "-c:v", s.Codec)
	if s.Preset != "" {
		args = append(args, "-preset", s.Preset)
	}
	args = append(args,
		"-b:v", kbps(s.Kbps),
		"-maxrate", kbps(s.Kbps*110/100),
		"-bufsize", kbps(s.Kbps*2),
	)
	if s.GOP > 0 {
		args = append(args, "-g", strconv.Itoa(s.GOP), "-keyint_min", strconv.Itoa(s.GOP))
	}
	args = append(args, "-pix_fmt", "yuv420p", "-an", "-sn", "-dn")
	// User arguments last so they can override anything above.
	args = append(args, s.Extra...)
	return append(args, s.Out)
}

// VMAFSpec compares one encode against the reference.
type VMAFSpec struct {
	Distorted string
	Reference string
	LogPath   string
	// Width and Height are the reference geometry.
	Width, Height int
	Model         string
	Threads       int
	Subsample     int
}

// VMAFArgs builds the comparison of one encode against the reference.
//
// Two details decide whether the numbers mean anything:
//
//   - the distorted input is scaled back UP to the reference resolution. VMAF
//     models a viewer watching on a full-size screen, so a 360p encode has to
//     be judged as the upscaled picture the viewer sees. Comparing at native
//     resolution instead makes low rungs look excellent and the whole
//     cross-resolution comparison meaningless.
//   - libvmaf takes the distorted stream as its FIRST input and the reference
//     as its second. Swapping them does not error; it just reports a different,
//     wrong number.
func VMAFArgs(s VMAFSpec) []string {
	opts := []string{
		"log_fmt=json",
		"log_path=" + escapeFilter(s.LogPath),
	}
	if s.Model != "" {
		opts = append(opts, "model="+escapeFilter(s.Model))
	}
	if s.Threads > 0 {
		opts = append(opts, "n_threads="+strconv.Itoa(s.Threads))
	}
	if s.Subsample > 1 {
		opts = append(opts, "n_subsample="+strconv.Itoa(s.Subsample))
	}
	filter := fmt.Sprintf(
		"[0:v]scale=%d:%d:flags=bicubic,setpts=PTS-STARTPTS[dist];[1:v]setpts=PTS-STARTPTS[ref];[dist][ref]libvmaf=%s",
		s.Width, s.Height, strings.Join(opts, ":"),
	)
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-i", s.Distorted,
		"-i", s.Reference,
		"-lavfi", filter,
		"-f", "null", "-",
	}
}

// escapeFilter protects a value inside a filtergraph. A path containing ':'
// would otherwise be read as the start of the next libvmaf option, and one
// containing ',' as the start of the next filter.
func escapeFilter(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`:`, `\:`,
		`'`, `\'`,
		`,`, `\,`,
		`;`, `\;`,
		`[`, `\[`,
		`]`, `\]`,
	)
	return r.Replace(s)
}

func kbps(v int) string { return strconv.Itoa(v) + "k" }

func formatSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64)
}
