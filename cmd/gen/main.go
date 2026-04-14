// gen — generates a large JSONL telemetry file for lovie testing.
//
// Usage: go run ./cmd/gen [-o output.jsonl] [-logs N] [-depth N] [-children N]
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"time"

	"lovie/oteljsonl"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

type generatorOptions struct {
	outFile               string
	logTarget             int
	maxDepth              int
	maxChildren           int
	compression           string
	aad                   string
	keyHex                string
	recipientKeyID        string
	recipientPublicKeyHex string
}

// ── helpers ───────────────────────────────────────────────────────────────────

func randInt(max int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(fmt.Errorf("crypto/rand int: %w", err))
	}

	return int(n.Int64())
}

func randFloat(min, max float64) float64 {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<30))
	if err != nil {
		panic(fmt.Errorf("crypto/rand float: %w", err))
	}

	return min + (float64(n.Int64())/float64(1<<30))*(max-min)
}

func randDur(minMs, maxMs int) time.Duration {
	return time.Duration(minMs+randInt(maxMs-minMs)) * time.Millisecond
}

func randHex(n int) string {
	b := make([]byte, n)

	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("crypto/rand bytes: %w", err))
	}

	return fmt.Sprintf("%x", b)
}

func parseFlags() generatorOptions {
	opts := generatorOptions{}

	flag.StringVar(&opts.outFile, "o", "test.jsonl", "output file")
	flag.IntVar(&opts.logTarget, "logs", 5000, "approximate number of log records")
	flag.IntVar(&opts.maxDepth, "depth", 4, "max span tree depth")
	flag.IntVar(&opts.maxChildren, "children", 3, "children per node")
	flag.StringVar(&opts.compression, "compression", "none", "output compression: none|gzip")
	flag.StringVar(&opts.aad, "aad", "", "optional oteljsonl AAD for compression/encryption")
	flag.StringVar(&opts.keyHex, "key-hex", "", "32-byte symmetric AES-256-GCM key in hex")
	flag.StringVar(&opts.recipientKeyID, "recipient-key-id", "", "optional recipient key id")
	flag.StringVar(&opts.recipientPublicKeyHex, "recipient-public-key-hex", "", "X25519 recipient public key in hex")
	flag.Parse()

	return opts
}

func buildExporterConfig(opts generatorOptions) (oteljsonl.Config, error) {
	compression, err := parseCompression(opts.compression)
	if err != nil {
		return oteljsonl.Config{}, err
	}

	cfgOpts := []oteljsonl.ConfigOption{
		oteljsonl.WithPath(opts.outFile),
		oteljsonl.WithCreateDirs(true),
		oteljsonl.WithCompression(compression),
	}

	if opts.keyHex != "" && opts.recipientPublicKeyHex != "" {
		return oteljsonl.Config{}, fmt.Errorf("use either -key-hex or -recipient-public-key-hex, not both")
	}

	switch {
	case opts.keyHex != "":
		key, err := decodeHexArg(opts.keyHex, "symmetric key")
		if err != nil {
			return oteljsonl.Config{}, err
		}

		cfgOpts = append(cfgOpts, oteljsonl.WithSymmetricEncryption(key, []byte(opts.aad)))
	case opts.recipientPublicKeyHex != "":
		publicKey, err := decodeHexArg(opts.recipientPublicKeyHex, "recipient public key")
		if err != nil {
			return oteljsonl.Config{}, err
		}

		cfgOpts = append(cfgOpts, oteljsonl.WithAsymmetricRecipients(
			[]byte(opts.aad),
			oteljsonl.RecipientPublicKey{
				KeyID:     opts.recipientKeyID,
				PublicKey: publicKey,
			},
		))
	case opts.aad != "":
		cfgOpts = append(cfgOpts, oteljsonl.WithAAD([]byte(opts.aad)))
	}

	return oteljsonl.NewConfig(cfgOpts...)
}

func parseCompression(value string) (oteljsonl.Compression, error) {
	switch value {
	case "", "none":
		return oteljsonl.CompressionNone, nil
	case "gzip":
		return oteljsonl.CompressionGzip, nil
	default:
		return "", fmt.Errorf("unsupported compression %q", value)
	}
}

func decodeHexArg(value string, label string) ([]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode %s hex: %w", label, err)
	}

	return decoded, nil
}

// ── Span-tree generator ───────────────────────────────────────────────────────

var layerNames = [][]string{
	{}, // layer 0: root handled by main
	{"auth.validateToken", "ratelimit.check", "order.service/create", "cart.service/update", "session.load"},
	{"db.query users", "cache.get session", "db.query orders", "redis.get cart", "db.insert order", "db.query products"},
	{"pricing.compute", "inventory.reserve", "payment.authorize", "shipping.estimate", "fraud.check"},
	{"kafka.produce order_created", "s3.put receipt", "grpc.call fulfillment", "sns.publish notification"},
	{"fulfillment.rpc/ship", "warehouse.pick", "label.generate", "carrier.lookup"},
	{"carrier.api POST /shipments", "barcode.encode", "pdf.render label", "address.validate"},
	{"http.client POST carrier.api", "zlib.compress", "base64.encode", "cache.set label"},
}

var layerKinds = []trace.SpanKind{
	trace.SpanKindServer,
	trace.SpanKindClient,
	trace.SpanKindClient,
	trace.SpanKindInternal,
	trace.SpanKindProducer,
	trace.SpanKindServer,
	trace.SpanKindClient,
	trace.SpanKindClient,
}

var spanEventNames = []string{
	"cache.miss", "db.retry", "lock.acquired", "lock.released",
	"checkpoint", "exception", "circuit_breaker.open", "rate_limit.exceeded",
}

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
		tmpl := logTemplates[randInt(len(logTemplates))]
		body := fmt.Sprintf(tmpl.tmpl, spName, randInt(500)+1, randFloat(10, 100))
		now := time.Now()

		var rec otellog.Record

		rec.SetSeverity(tmpl.sev)
		rec.SetSeverityText(tmpl.sevText)
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

	for i := 0; i < randInt(4); i++ {
		evOff := time.Duration(1+randInt(50)) * time.Millisecond
		sp.AddEvent(spanEventNames[randInt(len(spanEventNames))],
			trace.WithTimestamp(spanStart.Add(evOff)),
			trace.WithAttributes(attribute.Int("attempt", 1+randInt(3))),
		)
	}

	g.emitLogs(ctx, name, g.logsPerSpan+randInt(g.logsPerSpan/2+1))

	end := spanStart.Add(randDur(2, 8))
	childStart := spanStart.Add(500 * time.Microsecond)

	if depth < maxDepth {
		for i := 0; i < children; i++ {
			childEnd := g.buildTree(ctx, depth+1, maxDepth, children, childStart)
			if childEnd.After(end) {
				end = childEnd
			}

			childStart = childEnd
		}
	}

	end = end.Add(randDur(1, 3))

	if randInt(12) == 0 {
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

func recordMetrics(ctx context.Context, mp *sdkmetric.MeterProvider) error {
	meter := mp.Meter("order-service/meter",
		otelmetric.WithInstrumentationVersion("1.0.0"),
	)

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

	cpuUtil, _ := meter.Float64ObservableGauge(string(semconv.ProcessCPUUtilizationName),
		otelmetric.WithDescription("CPU utilisation"),
		otelmetric.WithUnit("1"),
	)
	if _, err := meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		for cpu := 0; cpu < 4; cpu++ {
			obs.ObserveFloat64(cpuUtil, randFloat(0.05, 0.95),
				otelmetric.WithAttributes(attribute.Int("cpu.id", cpu)))
		}

		return nil
	}, cpuUtil); err != nil {
		return err
	}

	memUsage, _ := meter.Int64ObservableGauge(string(semconv.ProcessMemoryUsageName),
		otelmetric.WithDescription("Memory usage in bytes"),
		otelmetric.WithUnit("By"),
	)
	if _, err := meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		obs.ObserveInt64(memUsage, int64(64e6+randFloat(0, 448e6)))

		return nil
	}, memUsage); err != nil {
		return err
	}

	ordersTotal, _ := meter.Int64Counter("orders.processed.total",
		otelmetric.WithDescription("Total orders processed"),
		otelmetric.WithUnit("{order}"),
	)
	ordersTotal.Add(ctx, int64(10000+randInt(50000)),
		otelmetric.WithAttributes(attribute.String("status", "success")))
	ordersTotal.Add(ctx, int64(randInt(500)),
		otelmetric.WithAttributes(attribute.String("status", "failed")))

	activeReq, _ := meter.Int64UpDownCounter(string(semconv.HTTPServerActiveRequestsName),
		otelmetric.WithDescription("Active HTTP server requests"),
		otelmetric.WithUnit("{request}"),
	)
	activeReq.Add(ctx, int64(randInt(200)))

	kafkaLag, _ := meter.Int64ObservableGauge("messaging.kafka.consumer.lag",
		otelmetric.WithDescription("Consumer lag per partition"),
		otelmetric.WithUnit("{message}"),
	)
	if _, err := meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		for p := 0; p < 8; p++ {
			obs.ObserveInt64(kafkaLag, int64(randInt(500)),
				otelmetric.WithAttributes(
					semconv.MessagingDestinationName("order_created"),
					attribute.Int("messaging.kafka.partition", p),
				))
		}

		return nil
	}, kafkaLag); err != nil {
		return err
	}

	cacheHit, _ := meter.Float64ObservableGauge("cache.hit_ratio",
		otelmetric.WithDescription("Cache hit ratio"),
		otelmetric.WithUnit("1"),
	)
	if _, err := meter.RegisterCallback(func(_ context.Context, obs otelmetric.Observer) error {
		obs.ObserveFloat64(cacheHit, math.Round(randFloat(0.6, 0.98)*100)/100,
			otelmetric.WithAttributes(attribute.String("cache.backend", "redis")))

		return nil
	}, cacheHit); err != nil {
		return err
	}

	return nil
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

//nolint:funlen // CLI orchestration is clearer as a single top-level flow.
func run() error {
	opts := parseFlags()

	cfg, err := buildExporterConfig(opts)
	if err != nil {
		return err
	}

	exporters, err := oteljsonl.NewExporters(cfg)
	if err != nil {
		return err
	}

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
		return err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporters.Trace),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	defer tp.Shutdown(ctx)

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporters.Log)),
		sdklog.WithResource(res),
	)
	defer lp.Shutdown(ctx)

	reader := sdkmetric.NewPeriodicReader(exporters.Metric, sdkmetric.WithInterval(time.Hour))

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)

	estimatedSpans := 1
	power := 1

	for d := 0; d < opts.maxDepth; d++ {
		power *= opts.maxChildren
		estimatedSpans += power
	}

	logsPerSpan := opts.logTarget / estimatedSpans
	if logsPerSpan < 1 {
		logsPerSpan = 1
	}

	tracer := tp.Tracer("order-service/tracer", trace.WithInstrumentationVersion("1.0.0"))
	logger := lp.Logger("order-service/logger")
	gen := &generator{
		tracer:      tracer,
		logger:      logger,
		targetLogs:  opts.logTarget,
		logsPerSpan: logsPerSpan,
	}

	log.Printf("Generating telemetry (depth=%d, children=%d, target logs≈%d)…",
		opts.maxDepth, opts.maxChildren, opts.logTarget)

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
	gen.emitLogs(ctx, "POST /api/v2/orders", 3)

	endTime := start.Add(2 * time.Millisecond)
	childTime := start.Add(500 * time.Microsecond)

	for i := 0; i < opts.maxChildren; i++ {
		childEnd := gen.buildTree(ctx, 1, opts.maxDepth, opts.maxChildren, childTime)
		if childEnd.After(endTime) {
			endTime = childEnd
		}

		childTime = childEnd
	}

	endTime = endTime.Add(5 * time.Millisecond)
	rootSp.AddEvent("response.sent", trace.WithTimestamp(endTime.Add(-200*time.Microsecond)))
	rootSp.SetStatus(codes.Ok, "")
	rootSp.End(trace.WithTimestamp(endTime))

	gen.spanCount++

	log.Printf("Spans: %d  Logs: %d  Writing metrics…", gen.spanCount, gen.logCount)

	if err := recordMetrics(ctx, mp); err != nil {
		return err
	}

	if err := mp.Shutdown(ctx); err != nil {
		return err
	}

	info, err := os.Stat(opts.outFile)
	if err != nil {
		return err
	}

	log.Printf("Done → %s (%.1f MB, %d spans, %d logs)",
		opts.outFile, float64(info.Size())/1e6, gen.spanCount, gen.logCount)

	return nil
}
