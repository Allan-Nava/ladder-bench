package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func joined(args []string) string { return strings.Join(args, " ") }

// indexOf returns the position of an argument, or -1.
func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestReferenceArgsSeeksBeforeInput(t *testing.T) {
	args := ReferenceArgs("in.mxf", "out.mkv", 90*time.Second, 30*time.Second)
	ss, in := indexOf(args, "-ss"), indexOf(args, "-i")
	if ss < 0 || in < 0 || ss > in {
		t.Fatalf("-ss must come before -i for a fast seek, got %v", args)
	}
	if args[ss+1] != "90.000" {
		t.Errorf("-ss = %q, want 90.000", args[ss+1])
	}
	if got := args[indexOf(args, "-t")+1]; got != "30.000" {
		t.Errorf("-t = %q, want 30.000", got)
	}
	if !strings.Contains(joined(args), "-crf 0") {
		t.Errorf("the reference must be lossless, got %v", args)
	}
	if args[len(args)-1] != "out.mkv" {
		t.Errorf("output must be last, got %v", args)
	}
}

func TestReferenceArgsOmitsUnsetCut(t *testing.T) {
	args := ReferenceArgs("in.mxf", "out.mkv", 0, 0)
	if indexOf(args, "-ss") >= 0 || indexOf(args, "-t") >= 0 {
		t.Errorf("a zero cut must not emit -ss/-t (it would truncate to nothing), got %v", args)
	}
}

func TestEncodeArgsCapsTheRate(t *testing.T) {
	args := EncodeArgs(EncodeSpec{
		Reference: "ref.mkv", Out: "out.mp4", Height: 720, Kbps: 2000,
		Codec: "libx264", Preset: "slow", GOP: 50,
		Extra: []string{"-tune", "film"},
	})
	want := map[string]string{
		"-b:v":        "2000k",
		"-maxrate":    "2200k",
		"-bufsize":    "4000k",
		"-g":          "50",
		"-keyint_min": "50",
		"-preset":     "slow",
		"-c:v":        "libx264",
		"-pix_fmt":    "yuv420p",
	}
	for flag, value := range want {
		i := indexOf(args, flag)
		if i < 0 {
			t.Errorf("missing %s in %v", flag, args)
			continue
		}
		if args[i+1] != value {
			t.Errorf("%s = %q, want %q", flag, args[i+1], value)
		}
	}
	if !strings.Contains(joined(args), "scale=-2:720") {
		t.Errorf("must scale to the rung height keeping the aspect ratio, got %v", args)
	}
	// User arguments must win over ours, which only works if they come after.
	if indexOf(args, "-tune") < indexOf(args, "-b:v") {
		t.Errorf("extra_args must come after the built-in flags, got %v", args)
	}
	if args[len(args)-1] != "out.mp4" {
		t.Errorf("output must be last, got %v", args)
	}
}

func TestEncodeArgsSkipsEmptyPresetAndGOP(t *testing.T) {
	args := EncodeArgs(EncodeSpec{Reference: "ref.mkv", Out: "o.mp4", Height: 480, Kbps: 800, Codec: "libaom-av1"})
	if indexOf(args, "-preset") >= 0 {
		t.Errorf("an unset preset must not be passed (not every encoder has one), got %v", args)
	}
	if indexOf(args, "-g") >= 0 {
		t.Errorf("an unset GOP must not be passed, got %v", args)
	}
}

// The distorted encode goes in first and is scaled back up to the reference
// size. Both are easy to get backwards, and neither fails loudly — you just
// get numbers that mean something else.
func TestVMAFArgsOrdersInputsAndUpscales(t *testing.T) {
	args := VMAFArgs(VMAFSpec{
		Distorted: "enc.mp4", Reference: "ref.mkv", LogPath: "enc.vmaf.json",
		Width: 1920, Height: 1080, Model: "version=vmaf_v0.6.1", Subsample: 5, Threads: 4,
	})
	first, second := indexOf(args, "enc.mp4"), indexOf(args, "ref.mkv")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("distorted must be input 0 and reference input 1, got %v", args)
	}
	filter := args[indexOf(args, "-lavfi")+1]
	if !strings.HasPrefix(filter, "[0:v]scale=1920:1080") {
		t.Errorf("the distorted input must be scaled to the reference geometry, got %q", filter)
	}
	if !strings.Contains(filter, "[dist][ref]libvmaf=") {
		t.Errorf("libvmaf must take dist then ref, got %q", filter)
	}
	for _, opt := range []string{"log_fmt=json", "model=version=vmaf_v0.6.1", "n_threads=4", "n_subsample=5"} {
		if !strings.Contains(filter, opt) {
			t.Errorf("filter is missing %q: %q", opt, filter)
		}
	}
	if !strings.HasSuffix(joined(args), "-f null -") {
		t.Errorf("the comparison must not write a video, got %v", args)
	}
}

func TestVMAFArgsSkipsDefaultOptions(t *testing.T) {
	args := VMAFArgs(VMAFSpec{Distorted: "a.mp4", Reference: "b.mkv", LogPath: "l.json", Width: 640, Height: 360, Subsample: 1})
	filter := args[indexOf(args, "-lavfi")+1]
	if strings.Contains(filter, "n_subsample") {
		t.Errorf("n_subsample=1 is the default and must not be emitted: %q", filter)
	}
	if strings.Contains(filter, "n_threads") {
		t.Errorf("n_threads=0 means auto and must not be emitted: %q", filter)
	}
}

// A work dir with a colon in it (a date, a Windows drive) would otherwise end
// the log_path option early and take the rest of the path as a libvmaf flag.
func TestVMAFArgsEscapesTheLogPath(t *testing.T) {
	args := VMAFArgs(VMAFSpec{
		Distorted: "a.mp4", Reference: "b.mkv",
		LogPath: "runs/2026-08-17T10:00/a.json", Width: 640, Height: 360,
	})
	filter := args[indexOf(args, "-lavfi")+1]
	if !strings.Contains(filter, `log_path=runs/2026-08-17T10\:00/a.json`) {
		t.Errorf("colon not escaped in the log path: %q", filter)
	}
}

func TestEscapeFilter(t *testing.T) {
	got := escapeFilter(`a:b,c;d[e]f\g'h`)
	want := `a\:b\,c\;d\[e\]f\\g\'h`
	if got != want {
		t.Errorf("escapeFilter = %q, want %q", got, want)
	}
}

func TestQuoteIsShellSafe(t *testing.T) {
	got := Quote("/usr/bin/ffmpeg", []string{"-i", "my clip.mp4", "-lavfi", "[0:v]scale=2:2"})
	if !strings.Contains(got, `'my clip.mp4'`) {
		t.Errorf("a path with a space must be quoted: %s", got)
	}
	if !strings.Contains(got, `'[0:v]scale=2:2'`) {
		t.Errorf("a filtergraph must be quoted: %s", got)
	}
}
