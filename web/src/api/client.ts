import type { Meta, TraceSummary, TraceDetail, LogPage, DisplayMetric } from '../types'

const BASE = '/api'

async function get<T>(path: string): Promise<T> {
  const res = await fetch(BASE + path)
  if (!res.ok) throw new Error(`API error ${res.status}: ${await res.text()}`)
  return res.json() as Promise<T>
}

export const api = {
  meta: () => get<Meta>('/meta'),
  traces: () => get<TraceSummary[]>('/traces'),
  traceDetail: (id: string) => get<TraceDetail>(`/traces/${id}`),
  logs: (params: { offset?: number; limit?: number; traceId?: string; spanId?: string; q?: string; sev?: string }) => {
    const p = new URLSearchParams()
    if (params.offset !== undefined) p.set('offset', String(params.offset))
    if (params.limit !== undefined) p.set('limit', String(params.limit))
    if (params.traceId) p.set('traceId', params.traceId)
    if (params.spanId) p.set('spanId', params.spanId)
    if (params.q) p.set('q', params.q)
    if (params.sev) p.set('sev', params.sev)
    const qs = p.toString()
    return get<LogPage>(`/logs${qs ? '?' + qs : ''}`)
  },
  metrics: () => get<DisplayMetric[]>('/metrics'),
}
