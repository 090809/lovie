package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"lovie/oteljsonl"
)

func TestParseOTLPOtelJSONL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		spanName      string
		configOptions []oteljsonl.ConfigOption
		decryptConfig oteljsonl.DecryptConfig
	}{
		{
			name:     "plain",
			spanName: "plain-span",
		},
		{
			name:     "encrypted",
			spanName: "encrypted-span",
			configOptions: []oteljsonl.ConfigOption{
				oteljsonl.WithCompression(oteljsonl.CompressionGzip),
				oteljsonl.WithSymmetricEncryption(bytes.Repeat([]byte{7}, 32), []byte("parser-test")),
			},
			decryptConfig: oteljsonl.DecryptConfig{
				Key: bytes.Repeat([]byte{7}, 32),
				AAD: []byte("parser-test"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeTraceLine(t, tc.spanName, tc.configOptions...)

			//nolint:gosec // Test opens a file created under t.TempDir().
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer file.Close()

			data, err := parseOTLP(file, tc.decryptConfig)
			if err != nil {
				t.Fatalf("parseOTLP() error = %v", err)
			}

			if len(data.Traces) != 1 {
				t.Fatalf("expected 1 trace, got %d", len(data.Traces))
			}

			traceData := data.Traces[0]
			if traceData.RootName != tc.spanName {
				t.Fatalf("expected root span %q, got %q", tc.spanName, traceData.RootName)
			}

			if len(traceData.Spans) != 1 {
				t.Fatalf("expected 1 span, got %d", len(traceData.Spans))
			}
		})
	}
}

func writeTraceLine(t *testing.T, spanName string, configOptions ...oteljsonl.ConfigOption) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "trace.jsonl")
	cfgOptions := append([]oteljsonl.ConfigOption{
		oteljsonl.WithPath(path),
		oteljsonl.WithCreateDirs(true),
	}, configOptions...)

	cfg, err := oteljsonl.NewConfig(cfgOptions...)
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}

	exporter, err := oteljsonl.NewTraceExporter(cfg)
	if err != nil {
		t.Fatalf("NewTraceExporter() error = %v", err)
	}

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		SpanID:  trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2},
	})

	if err := exporter.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{tracetest.SpanStub{
		Name:        spanName,
		SpanContext: spanContext,
		StartTime:   time.Unix(10, 0),
		EndTime:     time.Unix(11, 0),
	}.Snapshot()}); err != nil {
		t.Fatalf("ExportSpans() error = %v", err)
	}

	if err := exporter.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	return path
}
