package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Allan-Nava/ladder-bench/internal/output"
)

// runExport writes the recommended ladder as something another system accepts.
//
// It reads a report rather than measuring anything, so exporting is free and can
// be done as many times as there are packagers to feed. A ladder retyped by hand
// into a playlist is where a measured result quietly becomes a wrong one.
func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	format := fs.String("format", "json", "export format: "+output.ExportFormats())
	encoder := fs.String("encoder", "", "which encoder's ladder to export (required when the report has several)")
	outPath := fs.String("out", "", "write to this file instead of stdout")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(files) != 1 {
		return fmt.Errorf("usage: ladder-bench export [flags] REPORT.json")
	}
	report, err := loadReport(files[0])
	if err != nil {
		return err
	}
	ladder, err := output.BuildLadder(report, *encoder)
	if err != nil {
		return err
	}
	w, closeOut, err := writer(*outPath)
	if err != nil {
		return err
	}
	defer closeOut()
	return output.ExportLadder(w, ladder, *format)
}

// runChart writes the rate-quality curves as an SVG.
func runChart(args []string) error {
	fs := flag.NewFlagSet("chart", flag.ContinueOnError)
	encoder := fs.String("encoder", "", "which encoder to chart (required when the report has several)")
	outPath := fs.String("out", "", "write to this file instead of stdout")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(files) != 1 {
		return fmt.Errorf("usage: ladder-bench chart [flags] REPORT.json")
	}
	report, err := loadReport(files[0])
	if err != nil {
		return err
	}
	w, closeOut, err := writer(*outPath)
	if err != nil {
		return err
	}
	defer closeOut()
	return output.Chart(w, report, *encoder)
}

// writer opens the output, or hands back stdout and a no-op close.
func writer(path string) (*os.File, func(), error) {
	if path == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}
