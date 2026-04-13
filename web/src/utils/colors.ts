const PALETTE = [
  '#58a6ff', '#3fb950', '#f0883e', '#bc8cff', '#f85149',
  '#d29922', '#39c5cf', '#ff7b72', '#79c0ff', '#ffa657',
]

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  return Math.abs(h)
}

export function svcColor(service: string): string {
  return PALETTE[hashStr(service) % PALETTE.length]
}

const SEV_COLORS: Record<string, { fg: string; bg: string }> = {
  TRACE: { fg: '#8b949e', bg: '#1c2230' },
  DEBUG: { fg: '#79c0ff', bg: '#0d2335' },
  INFO:  { fg: '#3fb950', bg: '#0d2a12' },
  WARN:  { fg: '#d29922', bg: '#2b2006' },
  ERROR: { fg: '#f85149', bg: '#2b0d0d' },
  FATAL: { fg: '#ff7b72', bg: '#3b1212' },
}

export function sevColors(sev: string): { fg: string; bg: string } {
  return SEV_COLORS[sev] ?? { fg: '#8b949e', bg: '#1c2230' }
}
