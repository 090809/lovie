package main

import (
	"fmt"
	"os"
	"path/filepath"
)

var version = "dev"

const usageFmt = `lovie %s — OpenTelemetry JSONL viewer

Usage:
  lovie <file.jsonl>

Arguments:
  file.jsonl  Path to an OTLP file-exporter JSONL file
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usageFmt, version)
		os.Exit(1)
	}

	filePath := os.Args[1]

	f, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	absPath, _ := filepath.Abs(filePath)
	fmt.Fprintf(os.Stderr, "📂 Parsing %s …\n", absPath)

	data, err := parseOTLP(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing: %v\n", err)
		os.Exit(1)
	}
	data.Meta.File = filepath.Base(filePath)

	fmt.Fprintf(os.Stderr, "✔  %d traces · %d logs · %d metrics\n",
		len(data.Traces), len(data.Logs), len(data.Metrics))

	if err := serve(data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
