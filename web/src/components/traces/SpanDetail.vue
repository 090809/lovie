<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { api } from '../../api/client'
import type { DisplaySpan, DisplayLog } from '../../types'
import { fmtTime, fmtDur, kindLabel, statusLabel, normSev } from '../../utils/format'
import { sevColors } from '../../utils/colors'

const props = defineProps<{
  span: DisplaySpan
  traceId: string
}>()

const emit = defineEmits<{
  jumpToLogs: [spanId: string]
}>()

const logs = ref<DisplayLog[]>([])
const logsLoading = ref(false)

async function loadLogs() {
  logsLoading.value = true
  try {
    const page = await api.logs({ spanId: props.span.spanId, limit: 200 })
    logs.value = page.items
  } finally {
    logsLoading.value = false
  }
}

onMounted(loadLogs)
watch(() => props.span.spanId, loadLogs)

function attrStr(v: unknown): string {
  if (typeof v === 'string') return v
  return JSON.stringify(v) ?? ''
}

function attrsEntries(a: Record<string, unknown>): [string, string][] {
  return Object.entries(a).map(([k, v]) => [k, attrStr(v)])
}
</script>

<template>
  <div class="span-detail">
    <div class="detail-row">
      <div class="detail-col">
        <div class="detail-label">Name</div>
        <div class="detail-value">{{ span.name }}</div>
      </div>
      <div class="detail-col">
        <div class="detail-label">Service</div>
        <div class="detail-value">{{ span.service }}</div>
      </div>
      <div class="detail-col">
        <div class="detail-label">Kind</div>
        <div class="detail-value">{{ kindLabel(span.kind) }}</div>
      </div>
      <div class="detail-col">
        <div class="detail-label">Status</div>
        <div class="detail-value" :style="{ color: span.statusCode === 2 ? 'var(--red)' : span.statusCode === 1 ? 'var(--green)' : 'inherit' }">
          {{ statusLabel(span.statusCode) }}
          <span v-if="span.statusMsg" style="color:var(--text2);font-size:12px;margin-left:6px">{{ span.statusMsg }}</span>
        </div>
      </div>
      <div class="detail-col">
        <div class="detail-label">Duration</div>
        <div class="detail-value">{{ fmtDur(span.durationMs) }}</div>
      </div>
      <div class="detail-col">
        <div class="detail-label">Start</div>
        <div class="detail-value mono">{{ fmtTime(span.startMs) }}</div>
      </div>
    </div>

    <!-- Collapsible IDs -->
    <details class="ids-section">
      <summary>IDs</summary>
      <div class="ids-grid">
        <span class="id-label">Trace ID</span><span class="id-val mono">{{ span.traceId }}</span>
        <span class="id-label">Span ID</span><span class="id-val mono">{{ span.spanId }}</span>
        <template v-if="span.parentSpanId">
          <span class="id-label">Parent Span ID</span>
          <span class="id-val mono">{{ span.parentSpanId }}</span>
        </template>
      </div>
    </details>

    <!-- Attributes -->
    <details v-if="Object.keys(span.attributes).length" open class="attrs-section">
      <summary>Attributes ({{ Object.keys(span.attributes).length }})</summary>
      <table class="attrs-table">
        <tr v-for="[k, v] in attrsEntries(span.attributes)" :key="k">
          <td class="attr-key mono">{{ k }}</td>
          <td class="attr-val mono">{{ v }}</td>
        </tr>
      </table>
    </details>

    <!-- Events -->
    <details v-if="span.events?.length" open class="events-section">
      <summary>Events ({{ span.events.length }})</summary>
      <div class="event-list">
        <div v-for="(ev, i) in span.events" :key="i" class="event-item">
          <div class="event-header">
            <span class="event-diamond">◆</span>
            <span class="event-name">{{ ev.name }}</span>
            <span class="event-time mono">{{ fmtTime(ev.timeMs) }}</span>
          </div>
          <table v-if="Object.keys(ev.attributes).length" class="event-attrs-table">
            <tr v-for="[k, v] in attrsEntries(ev.attributes)" :key="k">
              <td class="ev-attr-key mono">{{ k }}</td>
              <td class="ev-attr-val mono">{{ v }}</td>
            </tr>
          </table>
        </div>
      </div>
    </details>

    <!-- Span logs -->
    <details v-if="logs.length || logsLoading" open class="logs-section">
      <summary>
        Logs{{ logs.length ? ` (${logs.length})` : '' }}
        <span v-if="logsLoading" class="spinner" style="width:10px;height:10px;margin-left:8px;"></span>
        <a
          v-if="logs.length"
          href="#"
          class="logs-jump-link"
          @click.prevent.stop="emit('jumpToLogs', span.spanId)"
          title="View all logs for this span"
        >View in Logs ↗</a>
      </summary>
      <div class="log-list">
        <div v-for="(log, i) in logs" :key="i" class="log-item">
          <span
            class="log-sev tag"
            :style="{ background: sevColors(normSev(log.severityText)).bg, color: sevColors(normSev(log.severityText)).fg }"
          >{{ normSev(log.severityText) }}</span>
          <span class="log-time mono">{{ fmtTime(log.timeMs) }}</span>
          <span class="log-body">{{ log.body }}</span>
        </div>
      </div>
    </details>
  </div>
</template>

<style scoped>
.span-detail {
  background: var(--bg3);
  border-top: 1px solid var(--border);
  padding: 12px 16px;
  font-size: 12px;
}
.detail-row { display: flex; flex-wrap: wrap; gap: 16px; margin-bottom: 10px; }
.detail-label { color: var(--text2); font-size: 11px; text-transform: uppercase; letter-spacing: .05em; }
.detail-value { color: var(--text); }

.ids-section, .attrs-section, .events-section, .logs-section {
  margin-top: 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 6px 10px;
}
.ids-grid { display: grid; grid-template-columns: auto 1fr; gap: 2px 12px; margin-top: 6px; }
.id-label { color: var(--text2); }
.id-val { color: var(--text); word-break: break-all; }

.attrs-table { border-collapse: collapse; width: 100%; margin-top: 6px; }
.attr-key { color: var(--accent); padding: 1px 8px 1px 0; vertical-align: top; white-space: nowrap; }
.attr-val { color: var(--text); padding: 1px 0; word-break: break-all; }

.event-list { margin-top: 6px; }
.event-item { padding: 6px 0; border-bottom: 1px solid var(--border); }
.event-header { display: flex; align-items: center; gap: 8px; }
.event-diamond { color: var(--orange); flex-shrink: 0; }
.event-name { font-weight: 600; }
.event-time { color: var(--text2); margin-left: auto; flex-shrink: 0; }

.event-attrs-table { border-collapse: collapse; margin-top: 4px; margin-left: 16px; }
.ev-attr-key { color: var(--accent); padding: 1px 12px 1px 0; vertical-align: top; white-space: nowrap; }
.ev-attr-val { color: var(--text); padding: 1px 0; word-break: break-all; }

.log-list { margin-top: 6px; }
.log-item { display: flex; align-items: baseline; gap: 8px; padding: 3px 0; border-bottom: 1px solid var(--border); }
.log-sev { flex-shrink: 0; font-size: 10px; }
.log-time { color: var(--text2); flex-shrink: 0; }
.log-body { word-break: break-word; }
.logs-jump-link {
  margin-left: auto;
  float: right;
  font-size: 11px;
  color: var(--accent);
  text-decoration: none;
}
.logs-jump-link:hover { text-decoration: underline; }
</style>
