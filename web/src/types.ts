// Types match the Go JSON output from parser.go + server.go

export interface Meta {
  file: string
  traceCount: number
  logCount: number
  metricCount: number
}

export interface TraceSummary {
  traceId: string
  rootService: string
  rootName: string
  startMs: number
  durationMs: number
  spanCount: number
  errorCount: number
}

export interface DisplayEvent {
  timeMs: number
  name: string
  attributes: Record<string, unknown>
}

export interface DisplaySpan {
  spanId: string
  parentSpanId?: string
  traceId: string
  name: string
  service: string
  kind: number
  statusCode: number
  statusMsg?: string
  startMs: number
  endMs: number
  durationMs: number
  attributes: Record<string, unknown>
  resource?: Record<string, unknown>
  events?: DisplayEvent[]
  hasError: boolean
  depth: number
}

export interface TraceDetail {
  traceId: string
  rootService: string
  rootName: string
  startMs: number
  durationMs: number
  spanCount: number
  errorCount: number
  spans: DisplaySpan[]
}

export interface DisplayLog {
  timeMs: number
  severityText: string
  severityNumber: number
  body: string
  traceId?: string
  spanId?: string
  service: string
  attributes: Record<string, unknown>
  resource?: Record<string, unknown>
}

export interface LogPage {
  total: number
  offset: number
  limit: number
  items: DisplayLog[]
}

export interface HistogramValue {
  count: number
  sum: number
  bounds?: number[]
  counts?: number[]
}

export type DataPointValue = number | HistogramValue

export interface DataPoint {
  timeMs: number
  value: DataPointValue
  attributes?: Record<string, unknown>
}

export interface DisplayMetric {
  name: string
  description?: string
  unit?: string
  type: string
  service: string
  dataPoints: DataPoint[]
}
