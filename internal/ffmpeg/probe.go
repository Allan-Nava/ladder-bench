package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Media describes the first video stream of a file.
type Media struct {
	Codec     string  `json:"codec"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	FrameRate float64 `json:"frame_rate"`
	Duration  float64 `json:"duration_s"`
}

// Probe reads the video properties ladder-bench needs: the reference geometry
// every distorted encode is scaled back to, and the frame rate the GOP length
// is derived from.
func (t Tools) Probe(ctx context.Context, path string) (Media, error) {
	out, err := capture(ctx, t.FFprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height,r_frame_rate,duration",
		"-show_entries", "format=duration",
		"-of", "json",
		path,
	)
	if err != nil {
		return Media{}, err
	}
	return parseProbe(out)
}

type probeOutput struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		RFrame    string `json:"r_frame_rate"`
		Duration  string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func parseProbe(data []byte) (Media, error) {
	var p probeOutput
	if err := json.Unmarshal(data, &p); err != nil {
		return Media{}, fmt.Errorf("parsing ffprobe output: %w", err)
	}
	if len(p.Streams) == 0 {
		return Media{}, fmt.Errorf("no video stream found")
	}
	s := p.Streams[0]
	m := Media{
		Codec:     s.CodecName,
		Width:     s.Width,
		Height:    s.Height,
		FrameRate: parseRate(s.RFrame),
	}
	// Container duration first: a stream-level duration is missing in
	// Matroska and wrong in a few MPEG-TS captures.
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		m.Duration = d
	} else if d, err := strconv.ParseFloat(s.Duration, 64); err == nil {
		m.Duration = d
	}
	if m.Width <= 0 || m.Height <= 0 {
		return Media{}, fmt.Errorf("video stream has no usable dimensions (%dx%d)", m.Width, m.Height)
	}
	return m, nil
}

// parseRate reads ffprobe's rational frame rate ("30000/1001"). It returns 0
// when the rate is unknown ("0/0"), which callers treat as "fall back to a
// default GOP" rather than dividing by zero.
func parseRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	n, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
	if err != nil {
		return 0
	}
	if !ok {
		return n
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(den), 64)
	if err != nil || d == 0 {
		return 0
	}
	return n / d
}
