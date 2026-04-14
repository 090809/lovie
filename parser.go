package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"

	colllogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"lovie/oteljsonl"
)

// ── proto helpers ─────────────────────────────────────────────────────────────

func hexID(b []byte) string { return hex.EncodeToString(b) }

func nanoToMs(ns uint64) float64 { return float64(ns) / 1e6 }

func attrToMap(attrs []*commonpb.KeyValue) map[string]interface{} {
	m := make(map[string]interface{}, len(attrs))

	for _, kv := range attrs {
		m[kv.Key] = anyVal(kv.Value)
	}

	return m
}

func anyVal(v *commonpb.AnyValue) interface{} {
	if v == nil {
		return nil
	}

	switch x := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_IntValue:
		return x.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return x.DoubleValue
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue
	case *commonpb.AnyValue_ArrayValue:
		if x.ArrayValue == nil {
			return nil
		}

		arr := make([]interface{}, len(x.ArrayValue.Values))

		for i, av := range x.ArrayValue.Values {
			arr[i] = anyVal(av)
		}

		return arr
	case *commonpb.AnyValue_KvlistValue:
		if x.KvlistValue == nil {
			return nil
		}

		return attrToMap(x.KvlistValue.Values)
	case *commonpb.AnyValue_BytesValue:
		return hex.EncodeToString(x.BytesValue)
	}

	return nil
}

func resourceService(r *resourcepb.Resource) string {
	if r == nil {
		return "unknown"
	}

	for _, kv := range r.Attributes {
		if kv.Key == "service.name" {
			if sv, ok := kv.Value.GetValue().(*commonpb.AnyValue_StringValue); ok {
				return sv.StringValue
			}
		}
	}

	return "unknown"
}

// ── display types ────────────────────────────────────────────────────────────

type DisplayData struct {
	Traces  []DisplayTrace  `json:"traces"`
	Logs    []DisplayLog    `json:"logs"`
	Metrics []DisplayMetric `json:"metrics"`
	Meta    DisplayMeta     `json:"meta"`
}
type DisplayMeta struct {
	File string `json:"file"`
}

type DisplayTrace struct {
	TraceID    string        `json:"traceId"`
	Spans      []DisplaySpan `json:"spans"`
	StartMs    float64       `json:"startMs"`
	EndMs      float64       `json:"endMs"`
	DurationMs float64       `json:"durationMs"`
	Services   []string      `json:"services"`
	RootName   string        `json:"rootName"`
	HasError   bool          `json:"hasError"`
}
type DisplaySpan struct {
	TraceID      string                 `json:"traceId"`
	SpanID       string                 `json:"spanId"`
	ParentSpanID string                 `json:"parentSpanId,omitempty"`
	Name         string                 `json:"name"`
	Service      string                 `json:"service"`
	Kind         int                    `json:"kind"`
	StartMs      float64                `json:"startMs"`
	EndMs        float64                `json:"endMs"`
	DurationMs   float64                `json:"durationMs"`
	StatusCode   int                    `json:"statusCode"`
	StatusMsg    string                 `json:"statusMsg,omitempty"`
	Attributes   map[string]interface{} `json:"attributes"`
	Resource     map[string]interface{} `json:"resource"`
	Events       []DisplayEvent         `json:"events,omitempty"`
	HasError     bool                   `json:"hasError"`
	Depth        int                    `json:"depth"`
}
type DisplayEvent struct {
	TimeMs     float64                `json:"timeMs"`
	Name       string                 `json:"name"`
	Attributes map[string]interface{} `json:"attributes"`
}
type DisplayLog struct {
	TimeMs         float64                `json:"timeMs"`
	SeverityText   string                 `json:"severityText"`
	SeverityNumber int                    `json:"severityNumber"`
	Body           string                 `json:"body"`
	TraceID        string                 `json:"traceId,omitempty"`
	SpanID         string                 `json:"spanId,omitempty"`
	Service        string                 `json:"service"`
	Attributes     map[string]interface{} `json:"attributes"`
	Resource       map[string]interface{} `json:"resource"`
}
type DisplayMetric struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Unit        string             `json:"unit,omitempty"`
	Type        string             `json:"type"`
	Service     string             `json:"service"`
	DataPoints  []DisplayDataPoint `json:"dataPoints"`
}
type DisplayDataPoint struct {
	TimeMs     float64                `json:"timeMs"`
	Value      interface{}            `json:"value"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}
type HistogramValue struct {
	Count  uint64    `json:"count"`
	Sum    float64   `json:"sum"`
	Bounds []float64 `json:"bounds,omitempty"`
	Counts []uint64  `json:"counts,omitempty"`
}

// ── parsed line result (from workers) ────────────────────────────────────────

type parsedLine struct {
	spans   []*tracepb.ResourceSpans
	logs    []*logspb.ResourceLogs
	metrics []*metricspb.ResourceMetrics
}

var pjson = protojson.UnmarshalOptions{DiscardUnknown: true}

func parseLine(line []byte) parsedLine {
	var result parsedLine
	// Detect type by first JSON key
	key := firstKey(line)
	switch key {
	case "resourceSpans":
		var req colltracepb.ExportTraceServiceRequest
		if pjson.Unmarshal(line, &req) == nil {
			result.spans = req.ResourceSpans
		}
	case "resourceLogs":
		var req colllogspb.ExportLogsServiceRequest
		if pjson.Unmarshal(line, &req) == nil {
			result.logs = req.ResourceLogs
		}
	case "resourceMetrics":
		var req collmetricspb.ExportMetricsServiceRequest
		if pjson.Unmarshal(line, &req) == nil {
			result.metrics = req.ResourceMetrics
		}
	}

	return result
}

// firstKey returns the first JSON object key without full parsing.
func firstKey(data []byte) string {
	i := bytes.IndexByte(data, '"')
	if i < 0 {
		return ""
	}

	j := bytes.IndexByte(data[i+1:], '"')
	if j < 0 {
		return ""
	}

	return string(data[i+1 : i+1+j])
}

// ── parser ────────────────────────────────────────────────────────────────────

func parseOTLP(r io.Reader, decryptConfig oteljsonl.DecryptConfig) (*DisplayData, error) {
	// Read all lines first (IO-bound, single goroutine)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)

	var lines [][]byte

	lineNumber := 0
	for scanner.Scan() {
		lineNumber++

		b := scanner.Bytes()
		if len(b) == 0 {
			continue
		}

		cp := make([]byte, len(b))
		copy(cp, b)

		decoded, err := oteljsonl.DecodeLine(cp, decryptConfig)
		if err != nil {
			return nil, fmt.Errorf("decode line %d: %w", lineNumber, err)
		}

		lines = append(lines, decoded)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Parse lines in parallel using a worker pool
	nWorkers := runtime.NumCPU()
	if nWorkers > 8 {
		nWorkers = 8
	}

	results := make([]parsedLine, len(lines))
	work := make(chan int, len(lines))

	for i := range lines {
		work <- i
	}

	close(work)

	var wg sync.WaitGroup

	for w := 0; w < nWorkers; w++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range work {
				results[i] = parseLine(lines[i])
			}
		}()
	}

	wg.Wait()

	// Merge results (preserving order for determinism)
	var (
		allSpans   []*tracepb.ResourceSpans
		allLogs    []*logspb.ResourceLogs
		allMetrics []*metricspb.ResourceMetrics
	)

	for _, r := range results {
		allSpans = append(allSpans, r.spans...)
		allLogs = append(allLogs, r.logs...)
		allMetrics = append(allMetrics, r.metrics...)
	}

	data := &DisplayData{}

	data.Traces = buildTraces(allSpans)
	data.Logs = buildLogs(allLogs)
	data.Metrics = buildMetrics(allMetrics)

	sort.Slice(data.Traces, func(i, j int) bool {
		return data.Traces[i].StartMs > data.Traces[j].StartMs
	})

	sort.Slice(data.Logs, func(i, j int) bool {
		return data.Logs[i].TimeMs < data.Logs[j].TimeMs
	})

	return data, nil
}

// ── traces ────────────────────────────────────────────────────────────────────

type flatSpan struct {
	sp  *tracepb.Span
	svc string
	res *resourcepb.Resource
}

func buildTraces(rss []*tracepb.ResourceSpans) []DisplayTrace {
	byTrace := map[string][]flatSpan{}

	for _, rs := range rss {
		svc := resourceService(rs.Resource)

		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				tid := hexID(sp.TraceId)
				byTrace[tid] = append(byTrace[tid], flatSpan{sp, svc, rs.Resource})
			}
		}
	}

	traces := make([]DisplayTrace, 0, len(byTrace))

	for tid, flat := range byTrace {
		traces = append(traces, buildSingleTrace(tid, flat))
	}

	sort.Slice(traces, func(i, j int) bool {
		return traces[i].StartMs > traces[j].StartMs
	})

	return traces
}

func buildSingleTrace(traceID string, flat []flatSpan) DisplayTrace {
	dspans := buildDisplaySpans(traceID, flat)
	assignSpanDepths(dspans)

	return summarizeTrace(traceID, dspans)
}

func buildDisplaySpans(traceID string, flat []flatSpan) []DisplaySpan {
	dspans := make([]DisplaySpan, 0, len(flat))

	for _, f := range flat {
		sp := f.sp
		isError := sp.Status != nil && sp.Status.Code == tracepb.Status_STATUS_CODE_ERROR

		ds := DisplaySpan{
			TraceID:      traceID,
			SpanID:       hexID(sp.SpanId),
			ParentSpanID: hexID(sp.ParentSpanId),
			Name:         sp.Name,
			Service:      f.svc,
			Kind:         int(sp.Kind),
			StartMs:      nanoToMs(sp.StartTimeUnixNano),
			EndMs:        nanoToMs(sp.EndTimeUnixNano),
			DurationMs:   nanoToMs(sp.EndTimeUnixNano - sp.StartTimeUnixNano),
			StatusCode:   int(sp.GetStatus().GetCode()),
			StatusMsg:    sp.GetStatus().GetMessage(),
			Attributes:   attrToMap(sp.Attributes),
			Resource:     attrToMap(f.res.GetAttributes()),
			HasError:     isError,
		}
		if ds.ParentSpanID == "0000000000000000" || ds.ParentSpanID == "" {
			ds.ParentSpanID = ""
		}

		for _, ev := range sp.Events {
			ds.Events = append(ds.Events, DisplayEvent{
				TimeMs:     nanoToMs(ev.TimeUnixNano),
				Name:       ev.Name,
				Attributes: attrToMap(ev.Attributes),
			})
		}

		dspans = append(dspans, ds)
	}

	sort.Slice(dspans, func(i, j int) bool {
		return dspans[i].StartMs < dspans[j].StartMs
	})

	return dspans
}

func assignSpanDepths(dspans []DisplaySpan) {
	spanByID := indexSpans(dspans)

	for i := range dspans {
		ds := &dspans[i]

		if isRootSpan(*ds, spanByID) {
			ds.Depth = 0
			assignDepths(ds, dspans)
		}
	}
}

func summarizeTrace(traceID string, dspans []DisplaySpan) DisplayTrace {
	spanByID := indexSpans(dspans)

	var (
		startMs, endMs float64
		svcSet         = map[string]struct{}{}
		hasError       bool
		rootName       = traceID
	)

	for i, ds := range dspans {
		svcSet[ds.Service] = struct{}{}

		if ds.HasError {
			hasError = true
		}

		if i == 0 || ds.StartMs < startMs {
			startMs = ds.StartMs
		}

		if ds.EndMs > endMs {
			endMs = ds.EndMs
		}

		if isRootSpan(ds, spanByID) {
			rootName = ds.Name
		}
	}

	svcs := make([]string, 0, len(svcSet))
	for s := range svcSet {
		svcs = append(svcs, s)
	}

	sort.Strings(svcs)

	return DisplayTrace{
		TraceID:    traceID,
		Spans:      dspans,
		StartMs:    startMs,
		EndMs:      endMs,
		DurationMs: endMs - startMs,
		Services:   svcs,
		RootName:   rootName,
		HasError:   hasError,
	}
}

func indexSpans(dspans []DisplaySpan) map[string]*DisplaySpan {
	spanByID := make(map[string]*DisplaySpan, len(dspans))

	for i := range dspans {
		spanByID[dspans[i].SpanID] = &dspans[i]
	}

	return spanByID
}

func isRootSpan(ds DisplaySpan, spanByID map[string]*DisplaySpan) bool {
	return ds.ParentSpanID == "" || spanByID[ds.ParentSpanID] == nil
}

func assignDepths(parent *DisplaySpan, all []DisplaySpan) {
	for i := range all {
		child := &all[i]

		if child.ParentSpanID == parent.SpanID && child.SpanID != parent.SpanID {
			if child.Depth == 0 && child != parent {
				child.Depth = parent.Depth + 1
				assignDepths(child, all)
			}
		}
	}
}

// ── logs ──────────────────────────────────────────────────────────────────────

func buildLogs(rls []*logspb.ResourceLogs) []DisplayLog {
	var out []DisplayLog

	for _, rl := range rls {
		svc := resourceService(rl.Resource)
		res := attrToMap(rl.Resource.GetAttributes())

		for _, sl := range rl.ScopeLogs {
			for _, lr := range sl.LogRecords {
				sev := lr.SeverityText
				if sev == "" {
					sev = sevNumberToText(int(lr.SeverityNumber))
				}

				out = append(out, DisplayLog{
					TimeMs:         nanoToMs(lr.TimeUnixNano),
					SeverityText:   sev,
					SeverityNumber: int(lr.SeverityNumber),
					Body:           bodyToString(lr.Body),
					TraceID:        hexID(lr.TraceId),
					SpanID:         hexID(lr.SpanId),
					Service:        svc,
					Attributes:     attrToMap(lr.Attributes),
					Resource:       res,
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].TimeMs < out[j].TimeMs
	})

	return out
}

func bodyToString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}

	switch x := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	default:
		s := anyVal(v)
		if s == nil {
			return ""
		}

		switch sv := s.(type) {
		case string:
			return sv
		default:
			return ""
		}
	}
}

func sevNumberToText(n int) string {
	switch {
	case n <= 0:
		return "UNSPECIFIED"
	case n <= 4:
		return "TRACE"
	case n <= 8:
		return "DEBUG"
	case n <= 12:
		return "INFO"
	case n <= 16:
		return "WARN"
	case n <= 20:
		return "ERROR"
	default:
		return "FATAL"
	}
}

// ── metrics ───────────────────────────────────────────────────────────────────

func buildMetrics(rms []*metricspb.ResourceMetrics) []DisplayMetric {
	var out []DisplayMetric

	for _, rm := range rms {
		svc := resourceService(rm.Resource)

		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				out = append(out, convertMetric(m, svc))
			}
		}
	}

	return out
}

func convertMetric(m *metricspb.Metric, svc string) DisplayMetric {
	dm := DisplayMetric{
		Name:        m.Name,
		Description: m.Description,
		Unit:        m.Unit,
		Service:     svc,
	}
	switch d := m.Data.(type) {
	case *metricspb.Metric_Gauge:
		dm.Type = "gauge"
		for _, dp := range d.Gauge.DataPoints {
			dm.DataPoints = append(dm.DataPoints, gaugeDP(dp))
		}
	case *metricspb.Metric_Sum:
		dm.Type = "sum"
		for _, dp := range d.Sum.DataPoints {
			dm.DataPoints = append(dm.DataPoints, gaugeDP(dp))
		}
	case *metricspb.Metric_Histogram:
		dm.Type = "histogram"
		for _, dp := range d.Histogram.DataPoints {
			dm.DataPoints = append(dm.DataPoints, DisplayDataPoint{
				TimeMs: nanoToMs(dp.TimeUnixNano),
				Value: HistogramValue{
					Count:  dp.Count,
					Sum:    dp.GetSum(),
					Bounds: dp.ExplicitBounds,
					Counts: dp.BucketCounts,
				},
				Attributes: attrToMap(dp.Attributes),
			})
		}
	case *metricspb.Metric_ExponentialHistogram:
		dm.Type = "exponential_histogram"
	case *metricspb.Metric_Summary:
		dm.Type = "summary"
	default:
		dm.Type = "unknown"
	}

	return dm
}

func gaugeDP(dp *metricspb.NumberDataPoint) DisplayDataPoint {
	var val interface{}

	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		val = v.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		val = v.AsInt
	}

	return DisplayDataPoint{
		TimeMs:     nanoToMs(dp.TimeUnixNano),
		Value:      val,
		Attributes: attrToMap(dp.Attributes),
	}
}
