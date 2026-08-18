package main

import (
	"flag"
	"testing"
)

// Nobody types the file names last, and the standard library's flag package
// stops at the first one — so a gate flag written after them would be read as a
// third file and never fire.
func TestParseInterspersedAcceptsFlagsAnywhere(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		files []string
		gate  bool
		limit float64
	}{
		{"flags last", []string{"a.json", "b.json", "--exit-on-regression", "--threshold", "5"}, []string{"a.json", "b.json"}, true, 5},
		{"flags first", []string{"--exit-on-regression", "--threshold=5", "a.json", "b.json"}, []string{"a.json", "b.json"}, true, 5},
		{"flags between", []string{"a.json", "--threshold", "5", "b.json", "--exit-on-regression"}, []string{"a.json", "b.json"}, true, 5},
		{"no flags", []string{"a.json", "b.json"}, []string{"a.json", "b.json"}, false, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("compare", flag.ContinueOnError)
			gate := fs.Bool("exit-on-regression", false, "")
			limit := fs.Float64("threshold", 2, "")
			files, err := parseInterspersed(fs, tc.args)
			if err != nil {
				t.Fatalf("parseInterspersed: %v", err)
			}
			if len(files) != len(tc.files) || files[0] != tc.files[0] || files[1] != tc.files[1] {
				t.Errorf("files = %v, want %v", files, tc.files)
			}
			if *gate != tc.gate {
				t.Errorf("gate = %v, want %v", *gate, tc.gate)
			}
			if *limit != tc.limit {
				t.Errorf("threshold = %v, want %v", *limit, tc.limit)
			}
		})
	}
}

func TestParseInterspersedRejectsAnUnknownFlag(t *testing.T) {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(discard{})
	fs.Bool("exit-on-regression", false, "")
	if _, err := parseInterspersed(fs, []string{"a.json", "--nope", "b.json"}); err == nil {
		t.Error("an unknown flag should be an error, not a file name")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
