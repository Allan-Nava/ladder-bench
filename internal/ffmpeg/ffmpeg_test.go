package ffmpeg

import (
	"strings"
	"testing"
)

// ffmpeg answers a broken encode with a cascade of its own consequences, and the
// line that says why sits above the cascade. A fixed-size tail drops exactly
// that line, which is how a failure ends up with nothing to debug.
func TestErrorContextKeepsTheLibraryLineAboveTheCascade(t *testing.T) {
	stderr := strings.Join([]string{
		"Svt[info]: -------------------------------------------",
		"Svt[info]: SVT [version]:\tSVT-AV1 Encoder Lib v2.2.0",
		"Svt[error]: Instance 1: Max Bitrate only supported with CRF mode",
		"[libsvtav1 @ 0x12062e730] Error setting encoder parameters: bad parameter (0x80001005)",
		"[vost#0:0/libsvtav1 @ 0x120637cd0] Error while opening encoder",
		"[vf#0:0 @ 0x12062f1f0] Error sending frames to consumers: Invalid argument",
		"[vf#0:0 @ 0x12062f1f0] Task finished with error code: -22 (Invalid argument)",
		"[vf#0:0 @ 0x12062f1f0] Terminating thread with return code -22 (Invalid argument)",
		"[vost#0:0/libsvtav1 @ 0x120637cd0] Could not open encoder before EOF",
		"[vost#0:0/libsvtav1 @ 0x120637cd0] Task finished with error code: -22 (Invalid argument)",
		"[vost#0:0/libsvtav1 @ 0x120637cd0] Terminating thread with return code -22",
		"[out#0/mp4 @ 0x120637380] Nothing was written into output file",
		"[out#0/mp4 @ 0x120637380] video:0KiB audio:0KiB subtitle:0KiB",
		// Enough consequence underneath to push the cause out of any fixed tail.
		"[aost#0:1 @ 0x120638000] Terminating thread with return code -22",
		"[aost#0:1 @ 0x120638000] Task finished with error code: -22",
		"[in#0 @ 0x120639000] Terminating thread with return code -22",
		"[in#0 @ 0x120639000] Task finished with error code: -22",
		"[fc#0 @ 0x12063a000] Terminating thread with return code -22",
		"[fc#0 @ 0x12063a000] Task finished with error code: -22",
		"Conversion failed!",
	}, "\n")
	got := errorContext(stderr)
	if !strings.Contains(got, "Max Bitrate only supported with CRF mode") {
		t.Errorf("the cause was dropped:\n%s", got)
	}
	if !strings.Contains(got, "Conversion failed!") {
		t.Errorf("the tail is missing:\n%s", got)
	}
	// The lifted lines come first, then a marker for the gap: a reader who sees
	// two blocks knows one was pulled forward.
	lines := strings.Split(got, "\n")
	gap := -1
	for i, l := range lines {
		if l == "…" {
			if gap >= 0 {
				t.Fatalf("more than one gap marker:\n%s", got)
			}
			gap = i
		}
	}
	if gap < 1 {
		t.Fatalf("expected a gap marker after the lifted block:\n%s", got)
	}
	if lines[0] != "Svt[error]: Instance 1: Max Bitrate only supported with CRF mode" {
		t.Errorf("the cause should lead, got %q", lines[0])
	}
	// Only library lines are lifted — the tail speaks for itself.
	for _, l := range lines[:gap] {
		if !libraryError.MatchString(l) {
			t.Errorf("lifted a line that is not a library error: %q", l)
		}
	}
	// Chatter from before the failure stays out of it.
	if strings.Contains(got, "SVT [version]") {
		t.Errorf("info lines must not be lifted:\n%s", got)
	}
}

func TestErrorContextOnShortAndCleanOutput(t *testing.T) {
	// Short output is passed through whole, with no marker to explain.
	short := "Unrecognized option 'nope'.\nError splitting the argument list."
	if got := errorContext(short); got != short {
		t.Errorf("errorContext = %q, want the whole thing", got)
	}
	// A long cascade with no library line gets the plain tail, and no marker
	// promising a line that was never found.
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, "frame=  100 fps=0.0 q=-1.0 size=     1kB")
	}
	got := errorContext(strings.Join(many, "\n"))
	if strings.Contains(got, "…") {
		t.Errorf("no library line was found, so nothing should be marked as lifted:\n%s", got)
	}
	if n := len(strings.Split(got, "\n")); n != stderrTailLines {
		t.Errorf("kept %d lines, want %d", n, stderrTailLines)
	}
	if errorContext("") != "" {
		t.Error("empty stderr should stay empty")
	}
}
