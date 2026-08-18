// Package ffmpeg wraps the two binaries ladder-bench drives: it finds them,
// asks what they can do, builds the exact argument lists for the reference cut,
// the encodes and the VMAF comparison, and reads the results back.
//
// Every argument list is built by a pure function so `ladder-bench plan` can
// print the same commands the run would execute, and so the tests can assert
// on them without ffmpeg installed.
package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Tools are the resolved paths of the two binaries.
type Tools struct {
	FFmpeg  string
	FFprobe string
}

// Find resolves both binaries, preferring the explicit paths from the config
// and falling back to PATH.
func Find(ffmpegPath, ffprobePath string) (Tools, error) {
	ff, err := resolve(ffmpegPath, "ffmpeg")
	if err != nil {
		return Tools{}, err
	}
	fp, err := resolve(ffprobePath, "ffprobe")
	if err != nil {
		return Tools{}, err
	}
	return Tools{FFmpeg: ff, FFprobe: fp}, nil
}

func resolve(explicit, name string) (string, error) {
	if explicit != "" {
		info, err := os.Stat(explicit)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s: %s is a directory", name, explicit)
		}
		return explicit, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found in PATH (install ffmpeg, or set the %q key in the config)", name, name)
	}
	return path, nil
}

// HasFilter reports whether this ffmpeg build exposes the named filter.
// libvmaf is a build-time option: a stock distro ffmpeg often lacks it, and
// finding that out after two hours of encodes is the worst way to learn it.
func (t Tools) HasFilter(ctx context.Context, name string) (bool, error) {
	out, err := capture(ctx, t.FFmpeg, "-hide_banner", "-filters")
	if err != nil {
		return false, err
	}
	return listHas(out, name), nil
}

// HasEncoder reports whether this ffmpeg build exposes the named encoder.
func (t Tools) HasEncoder(ctx context.Context, name string) (bool, error) {
	out, err := capture(ctx, t.FFmpeg, "-hide_banner", "-encoders")
	if err != nil {
		return false, err
	}
	return listHas(out, name), nil
}

// listHas scans the `-filters` / `-encoders` tables, whose rows are
// "<flags> <name> <description>", and matches the name column exactly.
// Matching the whole output as a substring would accept "libvmaf" inside the
// description of an unrelated filter.
func listHas(out []byte, name string) bool {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[1] == name {
			return true
		}
	}
	return false
}

// Executor runs a command. The indirection exists so the benchmark loop can be
// tested end to end without ffmpeg: a fake executor writes the files a real run
// would have produced.
type Executor interface {
	Run(ctx context.Context, bin string, args []string) error
}

// Version is the first line of `ffmpeg -version`, which carries the version and
// the build it came from.
//
// The whole line rather than a parsed version number: distributions and static
// builds spell it differently ("7.1", "6.1.1-3ubuntu5", "N-119145-g4d0d6e1"), and
// a report is better off quoting what the binary said than storing this tool's
// interpretation of it.
func (t Tools) Version(ctx context.Context) (string, error) {
	out, err := capture(ctx, t.FFmpeg, "-hide_banner", "-version")
	if err != nil {
		return "", err
	}
	lines := nonEmptyLines(string(out))
	if len(lines) == 0 {
		return "", fmt.Errorf("%s -version printed nothing", t.FFmpeg)
	}
	return lines[0], nil
}

// ExecRunner runs commands for real.
type ExecRunner struct {
	// Verbose echoes each command to stderr before running it.
	Verbose bool
}

// Run executes bin with args, surfacing the tail of stderr on failure.
func (r ExecRunner) Run(ctx context.Context, bin string, args []string) error {
	if r.Verbose {
		fmt.Fprintln(os.Stderr, "+", Quote(bin, args))
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", filepath.Base(bin), err, errorContext(stderr.String()))
	}
	return nil
}

func capture(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", filepath.Base(bin), strings.Join(args, " "), err, errorContext(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// stderrTailLines is how many trailing lines of ffmpeg's output a failure
// carries. ffmpeg answers a broken encode with a cascade of its own messages —
// one per thread that noticed — so the tail has to be deep enough to clear the
// cascade and still land on something.
const stderrTailLines = 12

// libraryError matches a line where an encoder library reports on itself rather
// than ffmpeg reporting on the library. These are where the actual cause lives:
//
//	Svt[error]: Instance 1: Max Bitrate only supported with CRF mode
//	x265 [error]: ...
//	[libsvtav1 @ 0x12062e730] Error setting encoder parameters
//
// ffmpeg then prints eight or more lines of consequence underneath, which is
// exactly how the one useful line falls out of a fixed-size tail.
var libraryError = regexp.MustCompile(`(?i)^\s*(\[[a-z0-9_.-]+ @ 0x[0-9a-f]+\].*error|[a-z0-9]+ ?\[error\]|[a-z0-9]+ ?\[fatal\])`)

// errorContext is what a failed command reports: the tail of its stderr, plus
// any earlier line where a library said what went wrong.
//
// Lifting those lines out of order is deliberate. A `…` marks the gap, because a
// reader who sees two blocks knows one was pulled forward, and a reader who sees
// only a truncated cascade knows nothing at all.
func errorContext(s string) string {
	lines := nonEmptyLines(s)
	if len(lines) <= stderrTailLines {
		return strings.Join(lines, "\n")
	}
	cut := len(lines) - stderrTailLines
	var lifted []string
	for _, l := range lines[:cut] {
		if libraryError.MatchString(l) {
			lifted = append(lifted, l)
		}
	}
	if len(lifted) == 0 {
		return strings.Join(lines[cut:], "\n")
	}
	return strings.Join(append(append(lifted, "…"), lines[cut:]...), "\n")
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimRight(l, "\r"))
		}
	}
	return lines
}

// Quote renders a command the way a shell would accept it, for `plan` output
// and verbose logging.
func Quote(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(bin))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'\\$`*?[]();&|<>#~=") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
