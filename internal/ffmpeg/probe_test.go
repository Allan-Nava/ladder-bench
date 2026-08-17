package ffmpeg

import "testing"

const probeJSON = `{
  "programs": [],
  "streams": [
    {"codec_name": "h264", "width": 1920, "height": 1080, "r_frame_rate": "30000/1001", "duration": "12.000000"}
  ],
  "format": {"duration": "30.040000"}
}`

func TestParseProbe(t *testing.T) {
	m, err := parseProbe([]byte(probeJSON))
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	if m.Width != 1920 || m.Height != 1080 || m.Codec != "h264" {
		t.Errorf("geometry = %dx%d %s", m.Width, m.Height, m.Codec)
	}
	if got := m.FrameRate; got < 29.9 || got > 30.0 {
		t.Errorf("frame rate = %f, want ~29.97", got)
	}
	// The container duration wins: the stream one is missing or wrong often
	// enough that trusting it silently shortens every bitrate calculation.
	if m.Duration != 30.04 {
		t.Errorf("duration = %f, want the container's 30.04", m.Duration)
	}
}

func TestParseProbeFallsBackToStreamDuration(t *testing.T) {
	m, err := parseProbe([]byte(`{"streams":[{"width":640,"height":360,"r_frame_rate":"25/1","duration":"9.5"}],"format":{}}`))
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	if m.Duration != 9.5 {
		t.Errorf("duration = %f, want 9.5", m.Duration)
	}
}

func TestParseProbeRejectsUnusableInput(t *testing.T) {
	for name, data := range map[string]string{
		"no stream":     `{"streams":[],"format":{}}`,
		"no dimensions": `{"streams":[{"codec_name":"aac"}],"format":{}}`,
		"not json":      `oops`,
	} {
		if _, err := parseProbe([]byte(data)); err == nil {
			t.Errorf("%s: parseProbe accepted unusable output", name)
		}
	}
}

func TestParseRate(t *testing.T) {
	cases := map[string]float64{
		"25/1":         25,
		"30000/1001":   29.97002997002997,
		"0/0":          0,
		"50":           50,
		"":             0,
		"garbage":      0,
		"  24000/1001": 23.976023976023978,
	}
	for in, want := range cases {
		if got := parseRate(in); got != want {
			t.Errorf("parseRate(%q) = %v, want %v", in, got, want)
		}
	}
}

const filterList = `Filters:
  T.. = Timeline support
 ... libvmaf           VV->V      Calculate the VMAF between two video streams.
 ... vmafmotion        V->V       Calculate the VMAF Motion score.
`

const encoderList = `Encoders:
 V....D libx264              libx264 H.264 / AVC
 V....D libsvtav1            SVT-AV1 encoder
`

func TestListHasMatchesTheNameColumn(t *testing.T) {
	if !listHas([]byte(filterList), "libvmaf") {
		t.Error("libvmaf not found in the filter list")
	}
	if !listHas([]byte(encoderList), "libsvtav1") {
		t.Error("libsvtav1 not found in the encoder list")
	}
	// "VMAF" appears in descriptions; matching the whole output as a substring
	// would report filters this build does not have.
	if listHas([]byte(filterList), "VMAF") {
		t.Error("a word from a description must not count as a filter name")
	}
	if listHas([]byte(encoderList), "libx265") {
		t.Error("libx265 is not in this list")
	}
}
