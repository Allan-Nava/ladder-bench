package ffmpeg

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Score is the pooled VMAF result of one comparison.
type Score struct {
	Mean float64 `json:"mean"`
	Min  float64 `json:"min"`
	// Harmonic is libvmaf's harmonic mean: it weighs the worst frames more
	// heavily than the arithmetic mean, so a clip with a few badly broken
	// seconds does not hide behind a good average.
	Harmonic float64 `json:"harmonic_mean"`
	Frames   int     `json:"frames"`
}

type vmafLog struct {
	Frames []struct {
		FrameNum int `json:"frameNum"`
	} `json:"frames"`
	Pooled struct {
		VMAF *struct {
			Min      float64 `json:"min"`
			Max      float64 `json:"max"`
			Mean     float64 `json:"mean"`
			Harmonic float64 `json:"harmonic_mean"`
		} `json:"vmaf"`
	} `json:"pooled_metrics"`
}

// ParseVMAFLog reads the JSON log libvmaf writes.
func ParseVMAFLog(data []byte) (Score, error) {
	var l vmafLog
	if err := json.Unmarshal(data, &l); err != nil {
		return Score{}, fmt.Errorf("parsing VMAF log: %w", err)
	}
	if l.Pooled.VMAF == nil {
		// This is what an empty comparison looks like: ffmpeg exits 0 and
		// writes a log with no pooled metrics when the two inputs never
		// overlapped in time.
		return Score{}, errors.New("VMAF log has no pooled metrics (did the two inputs share any frames?)")
	}
	return Score{
		Mean:     l.Pooled.VMAF.Mean,
		Min:      l.Pooled.VMAF.Min,
		Harmonic: l.Pooled.VMAF.Harmonic,
		Frames:   len(l.Frames),
	}, nil
}
