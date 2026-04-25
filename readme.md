# lovie — Local OpenTelemetry Viewer

**lovie** (**L**ocal **O**penTelemetry **Vie**wer) is a zero-dependency CLI tool for exploring [OTLP file-exporter](https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/) JSONL, including files wrapped by `oteljsonl` compression/encryption envelopes. Point it at a `.jsonl` file and it spins up a local web server with a rich interactive SPA — no cloud, no config, no state.

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

```text
lovie [options] <file.jsonl>
```

**Example:**

```bash
lovie trace_output.jsonl
# Serving lovie on http://127.0.0.1:7788
# Opening browser…
```

Encrypted `oteljsonl` input can be opened directly:

```bash
lovie -aad demo -recipient-key-id primary \
  -recipient-private-key-hex "$PRIVATE_KEY_HEX" \
  trace_output.jsonl
```

| Flag | Description |
|------|-------------|
| `-aad` | Additional authenticated data for `oteljsonl` decode |
| `-key-hex` | 32-byte symmetric AES-256-GCM key in hex |
| `-recipient-key-id` | Optional recipient key ID for recipient-based decode |
| `-recipient-private-key-hex` | X25519 recipient private key in hex |

The server runs until you press `Ctrl+C`.

---

## Input format

lovie reads:

1. [OTLP file exporter](https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/) JSONL
2. Compressed and/or encrypted `oteljsonl` envelope lines whose decoded payload is OTLP JSONL

OTLP lines use one of these top-level keys:

| Line type | Top-level key |
|-----------|--------------|
| Spans | `resourceSpans` |
| Logs | `resourceLogs` |
| Metrics | `resourceMetrics` |

Plain OTLP lines are parsed directly. `oteljsonl` envelope lines are decoded first, then parsed by the same OTLP path. Lines may arrive in any order — lovie handles out-of-order data correctly.

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

A separate generator lives in `cmd/gen/` and produces realistic OTLP-compatible JSONL using the official OTel Go SDK with semconv v1.26.0. When compression or encryption is enabled, the payload is wrapped by `oteljsonl`.

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
| `-compression` | `none` | `none` or `gzip` |
| `-aad` | `""` | Optional `oteljsonl` AAD |
| `-key-hex` | `""` | Symmetric AES-256-GCM key in hex |
| `-recipient-key-id` | `""` | Optional recipient key ID |
| `-recipient-public-key-hex` | `""` | X25519 recipient public key in hex |

A `-depth 5 -children 3` run produces ~364 spans and ~24 000 logs (~29 MB).

Example with recipient-based encryption:

```bash
cd cmd/gen
go run . -o ../../test.jsonl -compression gzip -aad demo \
  -recipient-key-id primary \
  -recipient-public-key-hex "$PUBLIC_KEY_HEX"
```

## Key helper

Use the small helper in `cmd/oteljsonl-keygen` to generate an X25519 keypair and copy-paste-friendly flags:

```bash
go run ./cmd/oteljsonl-keygen
```

Sample output:

```text
key_id=ab9083fe1023c775
public_key_hex=...
private_key_hex=...
cmd_gen_flags=-recipient-key-id ab9083fe1023c775 -recipient-public-key-hex ...
lovie_flags=-recipient-key-id ab9083fe1023c775 -recipient-private-key-hex ...
```

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
  ├── parser.go      — OTLP parsing with optional oteljsonl envelope decode
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

cmd/gen/                 — standalone oteljsonl test data generator
cmd/oteljsonl-keygen/    — X25519 key generation helper
```

---

## License

Apache-2.0
