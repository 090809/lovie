export function fmtTime(ms: number): string {
  return new Date(ms).toISOString().replace('T', ' ').replace('Z', '')
}

export function fmtDur(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`
  if (ms < 1000) return `${ms.toFixed(1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function fmtVal(v: number): string {
  if (v === 0) return '0'
  if (Math.abs(v) >= 1e9) return (v / 1e9).toFixed(2) + 'G'
  if (Math.abs(v) >= 1e6) return (v / 1e6).toFixed(2) + 'M'
  if (Math.abs(v) >= 1e3) return (v / 1e3).toFixed(2) + 'K'
  return v.toFixed(4).replace(/\.?0+$/, '')
}

export function kindLabel(k: number): string {
  return ['UNSPECIFIED', 'INTERNAL', 'SERVER', 'CLIENT', 'PRODUCER', 'CONSUMER'][k] ?? 'UNKNOWN'
}

export function statusLabel(s: number): string {
  return ['UNSET', 'OK', 'ERROR'][s] ?? 'UNKNOWN'
}

const SEV_NAMES: Record<string, string> = {
  TRACE: 'TRACE', TRACE2: 'TRACE', TRACE3: 'TRACE', TRACE4: 'TRACE',
  DEBUG: 'DEBUG', DEBUG2: 'DEBUG', DEBUG3: 'DEBUG', DEBUG4: 'DEBUG',
  INFO: 'INFO', INFO2: 'INFO', INFO3: 'INFO', INFO4: 'INFO',
  WARN: 'WARN', WARNING: 'WARN', WARN2: 'WARN', WARN3: 'WARN', WARN4: 'WARN',
  ERROR: 'ERROR', ERROR2: 'ERROR', ERROR3: 'ERROR', ERROR4: 'ERROR',
  FATAL: 'FATAL', FATAL2: 'FATAL', FATAL3: 'FATAL', FATAL4: 'FATAL',
}

export function normSev(s: string): string {
  return SEV_NAMES[s.toUpperCase()] ?? (s || 'UNKNOWN')
}
