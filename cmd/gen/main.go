// gen — generates a large OTLP file-exporter JSONL for lovie testing.
//
// Uses the official OTel Go SDK with semconv v1.26.0 and official proto types.
// Each span/log/metric batch is written as one JSONL line in OTLP JSON format,
// matching what the OTLP file exporter produces.
//
// Usage: go run ./cmd/gen [-o output.jsonl] [-logs N] [-depth N] [-children N]
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	collectorlogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func randInt(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}
func randFloat(min, max float64) float64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<30))
	return min + (float64(n.Int64())/float64(1<<30))*(max-min)
}
func randDur(minMs, maxMs int) time.Duration {
	return time.Duration(minMs+randInt(maxMs-minMs)) * time.Millisecond
}
func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// ── JSON line writer ──────────────────────────────────────────────────────────

type lineWriter struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func (lw *lineWriter) write(msg proto.Message) error {
	b, err := protojson.Marshal(msg)
	if err != nil {
		return err
	}
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.w.Write(b)
	lw.w.WriteByte('\n')
	return nil
}

// ── proto conversion helpers ──────────────────────────────────────────────────

func resourceToProto(res *sdkresource.Resource) *resourcepb.Resource {
	return &resourcepb.Resource{Attributes: attrsToProto(res.Attributes())}
}

func attrsToProto(attrs []attribute.KeyValue) []*commonpb.KeyValue {
	out := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		out = append(out, kvToProto(kv))
	}
	return out
}

func kvToProto(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: string(kv.Key), Value: anyValueToProto(kv.Value)}
}

func anyValueToProto(v attribute.Value) *commonpb.AnyValue {
	switch v.Type() {
	case attribute.STRING:
		s := v.AsString()
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	case attribute.INT64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: v.AsInt64()}}
	case attribute.FLOAT64:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v.AsFloat64()}}
	case attribute.BOOL:
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: v.AsBool()}}
	default:
		s := v.Emit()
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	}
}

func sdkSpanToProto(sp sdktrace.ReadOnlySpan) *tracepb.Span {
	tid := sp.SpanContext().TraceID()
	sid := sp.SpanContext().SpanID()
	psid := sp.Parent().SpanID()

	p := &tracepb.Span{
		TraceId:           tid[:],
		SpanId:            sid[:],
		Name:              sp.Name(),
		Kind:              tracepb.Span_SpanKind(sp.SpanKind()),
		StartTimeUnixNano: uint64(sp.StartTime().UnixNano()),
		EndTimeUnixNano:   uint64(sp.EndTime().UnixNano()),
		Attributes:        attrsToProto(sp.Attributes()),
	}
	if psid.IsValid() {
		p.ParentSpanId = psid[:]
	}
	st := sp.Status()
	switch st.Code {
	case codes.Ok:
		p.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK, Message: st.Description}
	case codes.Error:
		p.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: st.Description}
	default:
		p.Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_UNSET}
	}
	for _, ev := range sp.Events() {
		p.Events = append(p.Events, &tracepb.Span_Event{
			Name:         ev.Name,
			TimeUnixNano: uint64(ev.Time.UnixNano()),
			Attributes:   attrsToProto(ev.Attributes),
		})
	}
	return p
}

func sdkLogToProto(r sdklog.Record) *logspb.LogRecord {
	tid := r.TraceID()
	sid := r.SpanID()
	body := r.Body()

	var bodyAny *commonpb.AnyValue
	if body.Kind() == otellog.KindString {
		s := body.AsString()
		bodyAny = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	} else {
		s := body.String()
		bodyAny = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	}

	rec := &logspb.LogRecord{
		TimeUnixNano:         uint64(r.Timestamp().UnixNano()),
		ObservedTimeUnixNano: uint64(r.ObservedTimestamp().UnixNano()),
		SeverityNumber:       logspb.SeverityNumber(r.Severity()),
		SeverityText:         r.SeverityText(),
		Body:                 bodyAny,
	}
	var zeroTid [16]byte
	if tid != zeroTid {
		rec.TraceId = tid[:]
		rec.SpanId = sid[:]
	}
	r.WalkAttributes(func(kv otellog.KeyValue) bool {
		// Convert otellog.KeyValue → commonpb.KeyValue
		var av *commonpb.AnyValue
		switch kv.Value.Kind() {
		case otellog.KindString:
			s := kv.Value.AsString()
			av = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
		case otellog.KindInt64:
			av = &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: kv.Value.AsInt64()}}
		case otellog.KindFloat64:
			av = &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: kv.Value.AsFloat64()}}
		case otellog.KindBool:
			av = &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: kv.Value.AsBool()}}
		default:
			s := fmt.Sprintf("%v", kv.Value.AsString())
			av = &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
		}
		rec.Attributes = append(rec.Attributes, &commonpb.KeyValue{Key: kv.Key, Value: av})
		return true
	})
	return rec
}

func sdkMetricsToProto(rm *metricdata.ResourceMetrics) *metricspb.ResourceMetrics {
	out := &metricspb.ResourceMetrics{Resource: resourceToProto(rm.Resource)}
	for _, sm := range rm.ScopeMetrics {
		smProto := &metricspb.ScopeMetrics{
			Scope: &commonpb.InstrumentationScope{Name: sm.Scope.Name, Version: sm.Scope.Version},
		}
		for _, m := range sm.Metrics {
			smProto.Metrics = append(smProto.Metrics, metricToProto(m))
		}
		out.ScopeMetrics = append(out.ScopeMetrics, smProto)
	}
	return out
}

func metricToProto(m metricdata.Metrics) *metricspb.Metric {
	pm := &metricspb.Metric{Name: m.Name, Description: m.Description, Unit: m.Unit}
	const cumulative = metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE
	switch d := m.Data.(type) {
	case metricdata.Gauge[int64]:
		dps := make([]*metricspb.NumberDataPoint, 0, len(d.DataPoints))
		for _, dp := range d.DataPoints {
			dps = append(dps, &metricspb.NumberDataPoint{
				Attributes:   attrsToProto(dp.Attributes.ToSlice()),
				TimeUnixNano: uint64(dp.Time.UnixNano()),
				Value:        &metricspb.NumberDataPoint_AsInt{AsInt: dp.Value},
			})
		}
		pm.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: dps}}
	case metricdata.Gauge[float64]:
		dps := make([]*metricspb.NumberDataPoint, 0, len(d.DataPoints))
		for _, dp := range d.DataPoints {
			dps = append(dps, &metricspb.NumberDataPoint{
				Attributes:   attrsToProto(dp.Attributes.ToSlice()),
				TimeUnixNano: uint64(dp.Time.UnixNano()),
				Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: dp.Value},
			})
		}
		pm.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: dps}}
	case metricdata.Sum[int64]:
		dps := make([]*metricspb.NumberDataPoint, 0, len(d.DataPoints))
		for _, dp := range d.DataPoints {
			dps = append(dps, &metricspb.NumberDataPoint{
				Attributes:        attrsToProto(dp.Attributes.ToSlice()),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Value:             &metricspb.NumberDataPoint_AsInt{AsInt: dp.Value},
			})
		}
		pm.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{
			DataPoints: dps, AggregationTemporality: cumulative, IsMonotonic: d.IsMonotonic,
		}}
	case metricdata.Sum[float64]:
		dps := make([]*metricspb.NumberDataPoint, 0, len(d.DataPoints))
		for _, dp := range d.DataPoints {
			dps = append(dps, &metricspb.NumberDataPoint{
				Attributes:        attrsToProto(dp.Attributes.ToSlice()),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: dp.Value},
			})
		}
		pm.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{
			DataPoints: dps, AggregationTemporality: cumulative, IsMonotonic: d.IsMonotonic,
		}}
	case metricdata.Histogram[float64]:
		dps := make([]*metricspb.HistogramDataPoint, 0, len(d.DataPoints))
		for _, dp := range d.DataPoints {
			dps = append(dps, &metricspb.HistogramDataPoint{
				Attributes:        attrsToProto(dp.Attributes.ToSlice()),
				StartTimeUnixNano: uint64(dp.StartTime.UnixNano()),
				TimeUnixNano:      uint64(dp.Time.UnixNano()),
				Count:             dp.Count,
				Sum:               &dp.Sum,
				ExplicitBounds:    dp.Bounds,
				BucketCounts:      dp.BucketCounts,
			})
		}
		pm.Data = &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
			DataPoints: dps, AggregationTemporality: cumulative,
		}}
	}
	return pm
}

// ── SpanExporter ─────────────────────────────────────────────────────────────

type traceExporter struct {
	lw  *lineWriter
	res *sdkresource.Resource
}

func (e *traceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if len(spans) == 0 {
		return nil
	}
	protoSpans := make([]*tracepb.Span, 0, len(spans))
	for _, sp := range spans {
		protoSpans = append(protoSpans, sdkSpanToProto(sp))
	}
	return e.lw.write(&colltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: resourceToProto(e.res),
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: &commonpb.InstrumentationScope{Name: "order-service/tracer", Version: "1.0.0"},
				Spans: protoSpans,
			}},
		}},
	})
}

func (e *traceExporter) Shutdown(context.Context) error { return nil }

// ── LogExporter ───────────────────────────────────────────────────────────────

type logExporter struct {
	lw  *lineWriter
	res *sdkresource.Resource
}

func (e *logExporter) Export(ctx context.Context, records []sdklog.Record) error {
	if len(records) == 0 {
		return nil
	}
	protoLogs := make([]*logspb.LogRecord, 0, len(records))
	for _, r := range records {
		protoLogs = append(protoLogs, sdkLogToProto(r))
	}
	return e.lw.write(&collectorlogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: resourceToProto(e.res),
			ScopeLogs: []*logspb.ScopeLogs{{
				Scope:      &commonpb.InstrumentationScope{Name: "order-service/logger"},
				LogRecords: protoLogs,
			}},
		}},
	})
}

func (e *logExporter) Shutdown(context.Context) error   { return nil }
func (e *logExporter) ForceFlush(context.Context) error { return nil }

// ── MetricExporter ────────────────────────────────────────────────────────────

type metricExporter struct {
	lw  *lineWriter
	res *sdkresource.Resource
}

func (e *metricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	return e.lw.write(&collectormetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{sdkMetricsToProto(rm)},
	})
}

func (e *metricExporter) Temporality(_ sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}

func (e *metricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	if kind == sdkmetric.InstrumentKindHistogram {
		return sdkmetric.AggregationExplicitBucketHistogram{
			Boundaries: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		}
	}
	return sdkmetric.AggregationSum{}
}

func (e *metricExporter) Shutdown(context.Context) error { return nil }

// ── Span-tree generator ───────────────────────────────────────────────────────

var layerNames = [][]string{
	{}, // layer 0: root handled by main
	// 1 — middleware / auth / dispatch
	{"auth.validateToken", "ratelimit.check", "order.service/create", "cart.service/update", "session.load"},
	// 2 — DB / cache
	{"db.query users", "cache.get session", "db.query orders", "redis.get cart", "db.insert order", "db.query products"},
	// 3 — business logic
	{"pricing.compute", "inventory.reserve", "payment.authorize", "shipping.estimate", "fraud.check"},
	// 4 — messaging / async
	{"kafka.produce order_created", "s3.put receipt", "grpc.call fulfillment", "sns.publish notification"},
	// 5 — fulfillment sub-service
	{"fulfillment.rpc/ship", "warehouse.pick", "label.generate", "carrier.lookup"},
	// 6 — carrier API
	{"carrier.api POST /shipments", "barcode.encode", "pdf.render label", "address.validate"},
	// 7 — leaf I/O
	{"http.client POST carrier.api", "zlib.compress", "base64.encode", "cache.set label"},
}

var layerKinds = []trace.SpanKind{
	trace.SpanKindServer,   // 0 root
	trace.SpanKindClient,   // 1
	trace.SpanKindClient,   // 2
	trace.SpanKindInternal, // 3
	trace.SpanKindProducer, // 4
	trace.SpanKindServer,   // 5
	trace.SpanKindClient,   // 6
	trace.SpanKindClient,   // 7
}

var spanEventNames = []string{
	"cache.miss", "db.retry", "lock.acquired", "lock.released",
	"checkpoint", "exception", "circuit_breaker.open", "rate_limit.exceeded",
}

// logTemplate is a structured log template for a severity level.
type logTemplate struct {
	sev     otellog.Severity
	sevText string
	tmpl    string
}

var logTemplates = []logTemplate{
	{otellog.SeverityInfo, "INFO", "Starting %s"},
	{otellog.SeverityInfo, "INFO", "Completed %s successfully in %dms"},
	{otellog.SeverityDebug, "DEBUG", "Cache miss for %s, falling back to DB"},
	{otellog.SeverityDebug, "DEBUG", "Span context propagated to %s"},
	{otellog.SeverityInfo, "INFO", "DB query executed in %dms for %s"},
	{otellog.SeverityWarn, "WARN", "Slow query in %s: %dms exceeds 200ms threshold"},
	{otellog.SeverityInfo, "INFO", "Token validated, continuing %s"},
	{otellog.SeverityInfo, "INFO", "Rate limit passed (remaining: %d) for %s"},
	{otellog.SeverityWarn, "WARN", "Retrying %s (attempt %d/3)"},
	{otellog.SeverityInfo, "INFO", "Kafka offset=%d produced by %s"},
	{otellog.SeverityDebug, "DEBUG", "gRPC call via %s took %dms"},
	{otellog.SeverityInfo, "INFO", "Notification queued for %s"},
	{otellog.SeverityError, "ERROR", "Exception in %s: nil pointer dereference at line %d"},
	{otellog.SeverityWarn, "WARN", "High memory %.1f%% during %s"},
	{otellog.SeverityInfo, "INFO", "%s completed in %dms"},
	{otellog.SeverityDebug, "DEBUG", "DB connection released by %s (pool_size=%d)"},
	{otellog.SeverityInfo, "INFO", "Order id=%s created by %s"},
	{otellog.SeverityDebug, "DEBUG", "Pricing: base=%.2f discount=%.2f final=%.2f in %s"},
}

type generator struct {
	tracer      trace.Tracer
	logger      otellog.Logger
	spanCount   int
	logCount    int
	targetLogs  int
	logsPerSpan int
}

func (g *generator) emitLogs(ctx context.Context, spName string, n int) {
	for i := 0; i < n; i++ {
		t := logTemplates[randInt(len(logTemplates))]
		body := fmt.Sprintf(t.tmpl, spName, randInt(500)+1, randFloat(10, 100))

		now := time.Now()
		var rec otellog.Record
		rec.SetSeverity(t.sev)
		rec.SetSeverityText(t.sevText)
		rec.SetBody(otellog.StringValue(body))
		rec.SetTimestamp(now)
		rec.SetObservedTimestamp(now)
		rec.AddAttributes(
			otellog.String(string(semconv.CodeFunctionKey), spName),
			otellog.Int(string(semconv.ThreadIDKey), 100+randInt(64)),
			otellog.String("request.id", "req_"+randHex(8)),
		)
		g.logger.Emit(ctx, rec)
		g.logCount++
	}
}

func (g *generator) buildTree(ctx context.Context, depth, maxDepth, children int, start time.Time) time.Time {
	// terminate at leaf depth
	if depth > maxDepth {
		return start
	}

	layer := depth
	if layer >= len(layerNames) {
		layer = len(layerNames) - 1
	}
	names := layerNames[layer]
	if len(names) == 0 {
		return start
	}
	name := names[randInt(len(names))]

	kind := trace.SpanKindInternal
	if layer < len(layerKinds) {
		kind = layerKinds[layer]
	}

	spanStart := start.Add(time.Duration(1+randInt(5)) * time.Millisecond)
	ctx, sp := g.tracer.Start(ctx, name,
		trace.WithTimestamp(spanStart),
		trace.WithSpanKind(kind),
		trace.WithAttributes(layerAttrs(layer, name)...),
	)

	// Span events
	for i := 0; i < randInt(4); i++ {
		evOff := time.Duration(1+randInt(50)) * time.Millisecond
		sp.AddEvent(spanEventNames[randInt(len(spanEventNames))],
			trace.WithTimestamp(spanStart.Add(evOff)),
			trace.WithAttributes(attribute.Int("attempt", 1+randInt(3))),
		)
	}

	// Logs during this span — scale to hit targetLogs
	g.emitLogs(ctx, name, g.logsPerSpan+randInt(g.logsPerSpan/2+1))

	// Recurse into children (only non-leaf levels)
	end := spanStart.Add(randDur(2, 8))
	childStart := spanStart.Add(500 * time.Microsecond)
	if depth < maxDepth {
		for i := 0; i < children; i++ {
			ce := g.buildTree(ctx, depth+1, maxDepth, children, childStart)
			if ce.After(end) {
				end = ce
			}
			childStart = ce
		}
	}
	end = end.Add(randDur(1, 3))

	hasError := randInt(12) == 0
	if hasError {
		sp.RecordError(fmt.Errorf("internal error in %s", name),
			trace.WithAttributes(semconv.ExceptionTypeKey.String("RuntimeException")))
		sp.SetStatus(codes.Error, "internal error")
	} else {
		sp.SetStatus(codes.Ok, "")
	}
	sp.End(trace.WithTimestamp(end))
	g.spanCount++
	return end
}

// layerAttrs returns semconv-based attributes for each span layer.
func layerAttrs(layer int, name string) []attribute.KeyValue {
	switch layer {
	case 0, 5:
		return []attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String("POST"),
			semconv.URLPath("/api/v2/orders"),
			semconv.ServerAddress("api.example.com"),
			semconv.NetworkProtocolVersion("1.1"),
			semconv.HTTPResponseStatusCode(200),
		}
	case 1:
		return []attribute.KeyValue{
			semconv.RPCSystemKey.String("grpc"),
			semconv.RPCService(name),
			semconv.RPCMethod("Execute"),
			semconv.ServerAddress("internal.svc.cluster.local"),
			semconv.ServerPort(50051),
		}
	case 2:
		return []attribute.KeyValue{
			semconv.DBSystemKey.String("postgresql"),
			semconv.DBNamespace("orders_db"),
			semconv.DBQueryText("SELECT * FROM orders WHERE id = $1"),
			semconv.ServerAddress("db.internal"),
			semconv.ServerPort(5432),
			attribute.Int("db.rows_affected", randInt(1000)),
		}
	case 3:
		return []attribute.KeyValue{
			attribute.String("component", name),
			attribute.Float64("result.value", randFloat(0, 9999)),
		}
	case 4:
		return []attribute.KeyValue{
			semconv.MessagingSystemKey.String("kafka"),
			semconv.MessagingDestinationName("order_created"),
			semconv.MessagingOperationTypePublish,
			semconv.NetworkPeerAddress("kafka.internal:9092"),
			attribute.Int("messaging.kafka.partition", randInt(8)),
		}
	case 6, 7:
		return []attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String("POST"),
			semconv.ServerAddress("carrier.api.example.com"),
			semconv.URLFull("https://carrier.api.example.com/shipments"),
			semconv.HTTPResponseStatusCode(200),
		}
	default:
		return []attribute.KeyValue{attribute.String("component", name)}
	}
}

// ── metrics recording ─────────────────────────────────────────────────────────

func recordMetrics(ctx context.Context, mp *sdkmetric.MeterProvider, reader *sdkmetric.ManualReader, exp *metricExporter) error {
	meter := mp.Meter("order-service/meter",
		otelmetric.WithInstrumentationVersion("1.0.0"),
	)

	// HTTP server request duration
	httpDur, _ := meter.Float64Histogram(string(semconv.HTTPServerRequestDurationName),
		otelmetric.WithDescription("Duration of HTTP server requests"),
		otelmetric.WithUnit("s"),
		otelmetric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0),
	)
	for i := 0; i < 500+randInt(1000); i++ {
		httpDur.Record(ctx, randFloat(0.001, 1.5),
			otelmetric.WithAttributes(
				semconv.HTTPRequestMethodKey.String("POST"),
				semconv.HTTPRoute("/api/v2/orders"),
				semconv.HTTPResponseStatusCode(200),
			))
	}

	// DB client operation duration
	dbDur, _ := meter.Float64Histogram(string(semconv.DBClientOperationDurationName),
		otelmetric.WithDescription("Duration of DB client operations"),
		otelmetric.WithUnit("s"),
		otelmetric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5),
	)
	for _, op := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		for i := 0; i < 100+randInt(400); i++ {
			dbDur.Record(ctx, randFloat(0.0005, 0.3),
				otelmetric.WithAttributes(
					semconv.DBOperationName(op),
					semconv.DBSystemKey.String("postgresql"),
				))
		}
	}

	// Process CPU utilisation (observable gauge, one reading per CPU)
	cpuUtil, _ := meter.Float64ObservableGauge(string(semconv.ProcessCPUUtilizationName),
		otelmetric.WithDescription("CPU utilisation"),
		otelmetric.WithUnit("1"),
	)
	meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		for cpu := 0; cpu < 4; cpu++ {
			obs.ObserveFloat64(cpuUtil, randFloat(0.05, 0.95),
				otelmetric.WithAttributes(attribute.Int("cpu.id", cpu)))
		}
		return nil
	}, cpuUtil)

	// Process memory usage
	memUsage, _ := meter.Int64ObservableGauge(string(semconv.ProcessMemoryUsageName),
		otelmetric.WithDescription("Memory usage in bytes"),
		otelmetric.WithUnit("By"),
	)
	meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		obs.ObserveInt64(memUsage, int64(64e6+randFloat(0, 448e6)))
		return nil
	}, memUsage)

	// Orders processed (monotonic sum)
	ordersTotal, _ := meter.Int64Counter("orders.processed.total",
		otelmetric.WithDescription("Total orders processed"),
		otelmetric.WithUnit("{order}"),
	)
	ordersTotal.Add(ctx, int64(10000+randInt(50000)),
		otelmetric.WithAttributes(attribute.String("status", "success")))
	ordersTotal.Add(ctx, int64(randInt(500)),
		otelmetric.WithAttributes(attribute.String("status", "failed")))

	// Active HTTP server requests
	activeReq, _ := meter.Int64UpDownCounter(string(semconv.HTTPServerActiveRequestsName),
		otelmetric.WithDescription("Active HTTP server requests"),
		otelmetric.WithUnit("{request}"),
	)
	activeReq.Add(ctx, int64(randInt(200)))

	// Kafka consumer lag per partition
	kafkaLag, _ := meter.Int64ObservableGauge("messaging.kafka.consumer.lag",
		otelmetric.WithDescription("Consumer lag per partition"),
		otelmetric.WithUnit("{message}"),
	)
	meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		for p := 0; p < 8; p++ {
			obs.ObserveInt64(kafkaLag, int64(randInt(500)),
				otelmetric.WithAttributes(
					semconv.MessagingDestinationName("order_created"),
					attribute.Int("messaging.kafka.partition", p),
				))
		}
		return nil
	}, kafkaLag)

	// Cache hit ratio
	cacheHit, _ := meter.Float64ObservableGauge("cache.hit_ratio",
		otelmetric.WithDescription("Cache hit ratio"),
		otelmetric.WithUnit("1"),
	)
	meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		obs.ObserveFloat64(cacheHit, math.Round(randFloat(0.6, 0.98)*100)/100,
			otelmetric.WithAttributes(attribute.String("cache.backend", "redis")))
		return nil
	}, cacheHit)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		return err
	}
	return exp.Export(ctx, &rm)
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	outFile := flag.String("o", "test.jsonl", "output file")
	logTarget := flag.Int("logs", 5000, "approximate number of log records")
	maxDepth := flag.Int("depth", 4, "max span tree depth")
	maxChildren := flag.Int("children", 3, "children per node")
	flag.Parse()

	// Estimate total spans: sum of children^d for d=0..maxDepth
	// = (children^(maxDepth+1) - 1) / (children - 1)   (for children > 1)
	estimatedSpans := 1
	power := 1
	for d := 0; d < *maxDepth; d++ {
		power *= *maxChildren
		estimatedSpans += power
	}
	logsPerSpan := *logTarget / estimatedSpans
	if logsPerSpan < 1 {
		logsPerSpan = 1
	}

	f, err := os.Create(*outFile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	bw := bufio.NewWriterSize(f, 4<<20)
	defer bw.Flush()

	lw := &lineWriter{w: bw}
	ctx := context.Background()

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName("order-service"),
			semconv.ServiceVersion("2.4.1"),
			semconv.DeploymentEnvironment("production"),
			semconv.HostName("prod-worker-07"),
			attribute.String(string(semconv.K8SPodNameKey), "order-service-7d9f8b-xk2p4"),
			attribute.String(string(semconv.K8SNamespaceNameKey), "prod"),
			attribute.String(string(semconv.TelemetrySDKNameKey), "opentelemetry"),
			attribute.String(string(semconv.TelemetrySDKLanguageKey), "go"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	tExp := &traceExporter{lw: lw, res: res}
	lExp := &logExporter{lw: lw, res: res}
	mExp := &metricExporter{lw: lw, res: res}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(tExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	defer tp.Shutdown(ctx)

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(lExp)),
		sdklog.WithResource(res),
	)
	defer lp.Shutdown(ctx)

	tracer := tp.Tracer("order-service/tracer", trace.WithInstrumentationVersion("1.0.0"))
	logger := lp.Logger("order-service/logger")

	g := &generator{tracer: tracer, logger: logger, targetLogs: *logTarget, logsPerSpan: logsPerSpan}

	log.Printf("Generating trace (depth=%d, children=%d, target logs≈%d)…",
		*maxDepth, *maxChildren, *logTarget)

	start := time.Now().Add(-5 * time.Minute)

	ctx, rootSp := tracer.Start(ctx, "POST /api/v2/orders",
		trace.WithTimestamp(start),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			semconv.HTTPRequestMethodKey.String("POST"),
			semconv.URLPath("/api/v2/orders"),
			semconv.URLScheme("https"),
			semconv.ServerAddress("api.example.com"),
			semconv.HTTPResponseStatusCode(200),
			semconv.ClientAddress("203.0.113.42"),
			semconv.UserAgentOriginal("OrderBot/1.0 (lovie-test-generator)"),
			attribute.String("http.request_id", "req_"+randHex(8)),
			semconv.NetworkProtocolVersion("1.1"),
		),
	)
	rootSp.AddEvent("request.received", trace.WithTimestamp(start.Add(100*time.Microsecond)))
	rootSp.AddEvent("auth.started", trace.WithTimestamp(start.Add(500*time.Microsecond)))
	g.emitLogs(ctx, "POST /api/v2/orders", 3)

	endTime := start.Add(2 * time.Millisecond)
	childTime := start.Add(500 * time.Microsecond)
	for i := 0; i < *maxChildren; i++ {
		ce := g.buildTree(ctx, 1, *maxDepth, *maxChildren, childTime)
		if ce.After(endTime) {
			endTime = ce
		}
		childTime = ce
	}
	endTime = endTime.Add(5 * time.Millisecond)

	rootSp.AddEvent("response.sent", trace.WithTimestamp(endTime.Add(-200*time.Microsecond)))
	rootSp.SetStatus(codes.Ok, "")
	rootSp.End(trace.WithTimestamp(endTime))
	g.spanCount++

	log.Printf("Spans: %d  Logs: %d  Writing metrics…", g.spanCount, g.logCount)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(reader))
	defer mp.Shutdown(ctx)
	if err := recordMetrics(ctx, mp, reader, mExp); err != nil {
		log.Printf("metrics error: %v", err)
	}

	if err := bw.Flush(); err != nil {
		log.Fatal(err)
	}

	fi, _ := f.Stat()
	log.Printf("Done → %s (%.1f MB, %d spans, %d logs)",
		*outFile, float64(fi.Size())/1e6, g.spanCount, g.logCount)
}
