# lovie — Local OpenTelemetry Viewer

**lovie** (**L**ocal **O**penTelemetry **Vie**wer) is a zero-dependency CLI tool for exploring [OTLP file-exporter](https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/) JSONL files. Point it at a `.jsonl` file and it spins up a local web server with a rich interactive SPA — no cloud, no config, no state.

---

## Features

- **Traces** — waterfall view with span bars, duration labels, colour-coded services, error highlighting, collapsible subtrees (auto-collapsed beyond depth 5), inline span details
- **Events on spans** — ◆ markers on waterfall bars; full event attributes shown in the detail panel
- **Logs in spans** — logs associated with a span are embedded in its detail panel; "View in Logs ↗" jumps to the Logs tab pre-filtered to that span
- **Cross-references** — jump from any log row to the exact span in the Traces tab (and back); ancestors are auto-expanded and the span is scrolled into view
- **Metrics** — gauge, sum, histogram data points with per-service colouring
- **Lazy loading** — data is fetched on demand via REST API; handles 50 MB+ files without embedding JSON in the page
- **Parallel parsing** — JSONL lines are parsed concurrently using a worker pool (`min(NumCPU, 8)`)
- **Browser auto-launch** — opens the viewer in the default browser on startup (or prints the URL if it can't)
- **Single binary** — frontend is embedded in the Go binary via `go:embed`

---

## Installation

```bash
go install lovie@latest
```

Or build from source:

```bash
git clone <repo>
cd lovie
go build -o lovie .
```

Requires **Go 1.22+**.

---

## Usage

```
lovie <file.jsonl>
```

**Example:**

```bash
lovie trace_output.jsonl
# Serving lovie on http://127.0.0.1:7788
# Opening browser…
```

The server runs until you press `Ctrl+C`.

---

## Input format

lovie reads JSONL produced by the [OTLP file exporter](https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/). Each line is one of:

| Line type | Top-level key |
|-----------|--------------|
| Spans | `resourceSpans` |
| Logs | `resourceLogs` |
| Metrics | `resourceMetrics` |

Lines may arrive in any order — lovie handles out-of-order data correctly.

---

## REST API

The embedded server exposes a lightweight JSON API used by the SPA:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/meta` | File metadata (counts) |
| `GET` | `/api/traces` | List of trace summaries |
| `GET` | `/api/traces/{id}` | Full trace detail with sorted waterfall spans |
| `GET` | `/api/logs` | Paginated logs with optional filters |
| `GET` | `/api/metrics` | All metric data points |

**Log query parameters:** `offset`, `limit`, `q` (substring search), `sev` (severity), `traceId`, `spanId`

---

## Test data generator

A separate generator lives in `cmd/gen/` and produces realistic OTLP JSONL using the official OTel Go SDK with semconv v1.26.0.

```bash
cd cmd/gen
go run . -o ../../test.jsonl -logs 5000 -depth 4 -children 3
```

| Flag | Default | Description |
|------|---------|-------------|
| `-o` | `test.jsonl` | Output file path |
| `-logs` | `5000` | Approximate number of log records |
| `-depth` | `4` | Max span tree depth |
| `-children` | `3` | Children per span node |

A `-depth 5 -children 3` run produces ~364 spans and ~24 000 logs (~29 MB).

---

## Frontend development

The SPA is built with **Vue 3 + TypeScript + Vite** and lives in `web/`.

```bash
cd web
npm install
npm run dev      # dev server with HMR (proxies /api to :7788)
npm run build    # build into web/dist (embedded in binary)
```

After `npm run build`, rebuild the Go binary to pick up the new frontend:

```bash
go build -o lovie .
```

---

## Architecture

```
lovie <file.jsonl>
  │
  ├── parser.go      — parallel JSONL parsing via proto/otlp + protojson
  ├── server.go      — HTTP server, REST API, browser launch, go:embed
  ├── main.go        — CLI entry point
  │
  └── web/           — Vue 3 SPA (Vite + TypeScript)
      ├── src/
      │   ├── components/
      │   │   ├── traces/   TraceListView, TraceCard, SpanDetail
      │   │   ├── logs/     LogView
      │   │   └── metrics/  MetricsView
      │   ├── api/client.ts
      │   └── utils/        format, colors
      └── dist/             (embedded in binary)

cmd/gen/           — standalone test data generator (separate Go module)
```

---

## License

MIT
