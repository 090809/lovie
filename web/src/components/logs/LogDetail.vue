<script setup lang="ts">
import type { DisplayLog } from '../../types'
import { fmtTime, normSev } from '../../utils/format'
import { sevColors } from '../../utils/colors'

const props = defineProps<{
  log: DisplayLog
}>()

const emit = defineEmits<{
  jumpToTrace: [traceId: string, spanId?: string]
}>()

function attrStr(v: unknown): string {
  if (typeof v === 'string') return v
  return JSON.stringify(v) ?? ''
}

function attrsEntries(a: Record<string, unknown>): [string, string][] {
  return Object.entries(a).map(([k, v]) => [k, attrStr(v)])
}
</script>

<template>
  <tr class="log-detail-row">
    <td colspan="4">
      <div class="log-detail">
        <div class="detail-row">
          <div class="detail-col">
            <div class="detail-label">Severity</div>
            <div class="detail-value">
              <span
                class="tag"
                :style="{ background: sevColors(normSev(log.severityText)).bg, color: sevColors(normSev(log.severityText)).fg }"
              >{{ normSev(log.severityText) }}</span>
            </div>
          </div>
          <div class="detail-col">
            <div class="detail-label">Timestamp</div>
            <div class="detail-value mono">{{ fmtTime(log.timeMs) }}</div>
          </div>
          <div class="detail-col">
            <div class="detail-label">Service</div>
            <div class="detail-value">{{ log.service }}</div>
          </div>
        </div>

        <details class="body-section" open>
          <summary>Message</summary>
          <div class="body-value mono">{{ log.body }}</div>
        </details>

        <details v-if="log.traceId || log.spanId" class="ids-section" open>
          <summary>IDs</summary>
          <div class="ids-grid">
            <template v-if="log.traceId">
              <span class="id-label">Trace ID</span>
              <span class="id-val mono">
                {{ log.traceId }}
                <a
                  href="#"
                  class="jump-link"
                  @click.prevent="emit('jumpToTrace', log.traceId)"
                >Open trace ↗</a>
              </span>
            </template>
            <template v-if="log.spanId">
              <span class="id-label">Span ID</span>
              <span class="id-val mono">
                {{ log.spanId }}
                <a
                  v-if="log.traceId"
                  href="#"
                  class="jump-link"
                  @click.prevent="emit('jumpToTrace', log.traceId, log.spanId)"
                >Open span ↗</a>
              </span>
            </template>
          </div>
        </details>

        <details v-if="Object.keys(log.attributes).length" class="attrs-section" open>
          <summary>Attributes ({{ Object.keys(log.attributes).length }})</summary>
          <table class="attrs-table">
            <tr v-for="[k, v] in attrsEntries(log.attributes)" :key="k">
              <td class="attr-key mono">{{ k }}</td>
              <td class="attr-val mono">{{ v }}</td>
            </tr>
          </table>
        </details>

        <details v-if="log.resource && Object.keys(log.resource).length" class="attrs-section" open>
          <summary>Resource ({{ Object.keys(log.resource).length }})</summary>
          <table class="attrs-table">
            <tr v-for="[k, v] in attrsEntries(log.resource)" :key="k">
              <td class="attr-key mono">{{ k }}</td>
              <td class="attr-val mono">{{ v }}</td>
            </tr>
          </table>
        </details>
      </div>
    </td>
  </tr>
</template>

<style scoped>
.log-detail-row td {
  padding: 0;
  border-bottom: 1px solid var(--border);
}

.log-detail {
  background: var(--bg3);
  padding: 12px 16px;
  font-size: 12px;
}

.detail-row { display: flex; flex-wrap: wrap; gap: 16px; margin-bottom: 10px; }
.detail-label { color: var(--text2); font-size: 11px; text-transform: uppercase; letter-spacing: .05em; }
.detail-value { color: var(--text); }

.body-section, .ids-section, .attrs-section {
  margin-top: 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 6px 10px;
}

.body-value { margin-top: 6px; color: var(--text); white-space: pre-wrap; word-break: break-word; }
.ids-grid { display: grid; grid-template-columns: auto 1fr; gap: 2px 12px; margin-top: 6px; }
.id-label { color: var(--text2); }
.id-val { color: var(--text); word-break: break-all; }
.jump-link {
  margin-left: 10px;
  color: var(--accent);
  text-decoration: none;
  font-size: 11px;
}
.jump-link:hover { text-decoration: underline; }

.attrs-table { border-collapse: collapse; width: 100%; margin-top: 6px; }
.attr-key { color: var(--accent); padding: 1px 8px 1px 0; vertical-align: top; white-space: nowrap; }
.attr-val { color: var(--text); padding: 1px 0; word-break: break-all; }
</style>
