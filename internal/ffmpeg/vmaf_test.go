package ffmpeg

import "testing"

const vmafJSON = `{
  "version": "3.0.0",
  "frames": [
    {"frameNum": 0, "metrics": {"vmaf": 91.2}},
    {"frameNum": 1, "metrics": {"vmaf": 88.4}}
  ],
  "pooled_metrics": {
    "vmaf": {"min": 71.35, "max": 97.02, "mean": 90.88, "harmonic_mean": 89.91}
  }
}`

func TestParseVMAFLog(t *testing.T) {
	s, err := ParseVMAFLog([]byte(vmafJSON))
	if err != nil {
		t.Fatalf("ParseVMAFLog: %v", err)
	}
	if s.Mean != 90.88 || s.Min != 71.35 || s.Harmonic != 89.91 {
		t.Errorf("score = %+v", s)
	}
	if s.Frames != 2 {
		t.Errorf("frames = %d, want 2", s.Frames)
	}
}

// ffmpeg exits 0 and writes a log with no pooled metrics when the two inputs
// never overlapped. Reading that as a zero score would put a fake point on the
// curve instead of an error.
func TestParseVMAFLogRejectsEmptyComparison(t *testing.T) {
	if _, err := ParseVMAFLog([]byte(`{"version":"3.0.0","frames":[],"pooled_metrics":{}}`)); err == nil {
		t.Fatal("ParseVMAFLog accepted a log with no pooled metrics")
	}
	if _, err := ParseVMAFLog([]byte(`not json`)); err == nil {
		t.Fatal("ParseVMAFLog accepted invalid JSON")
	}
}
