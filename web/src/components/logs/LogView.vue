<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { api } from '../../api/client'
import type { DisplayLog } from '../../types'
import { fmtTime, normSev } from '../../utils/format'
import { sevColors } from '../../utils/colors'

const emit = defineEmits<{
  jumpToTrace: [traceId: string, spanId: string]
}>()

const props = defineProps<{
  jumpSpanId?: string | null
  jumpKey?: number
}>()

const LIMIT = 200
const SEV_OPTIONS = ['ALL', 'TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']

const logs = ref<DisplayLog[]>([])
const total = ref(0)
const offset = ref(0)
const loading = ref(false)
const q = ref('')
const sev = ref('ALL')
const jumpedSpanId = ref<string | null>(null)
let debounce: ReturnType<typeof setTimeout>

async function load(reset = false) {
  if (reset) {
    offset.value = 0
    logs.value = []
  }
  loading.value = true
  try {
    const page = await api.logs({
      offset: offset.value,
      limit: LIMIT,
      q: q.value || undefined,
      sev: sev.value !== 'ALL' ? sev.value : undefined,
    })
    if (reset) logs.value = page.items
    else logs.value.push(...page.items)
    total.value = page.total
  } finally {
    loading.value = false
  }
}

function loadMore() {
  offset.value += LIMIT
  load()
}

onMounted(() => load(true))

watch([q, sev], () => {
  clearTimeout(debounce)
  debounce = setTimeout(() => load(true), 300)
})

// Jump: filter by spanId, highlight rows
watch(() => props.jumpKey, async () => {
  if (!props.jumpSpanId) return
  jumpedSpanId.value = props.jumpSpanId
  // Load logs filtered to this span via search
  offset.value = 0
  logs.value = []
  loading.value = true
  sev.value = 'ALL'
  q.value = ''
  try {
    const page = await api.logs({ spanId: props.jumpSpanId, limit: LIMIT })
    logs.value = page.items
    total.value = page.total
  } finally {
    loading.value = false
  }
})

function clearJump() {
  jumpedSpanId.value = null
  load(true)
}
</script>

<template>
  <div>
    <!-- Active span filter banner -->
    <div v-if="jumpedSpanId" class="span-filter-banner">
      <span>Showing logs for span <span class="mono">{{ jumpedSpanId }}</span></span>
      <button @click="clearJump" class="clear-filter-btn">✕ Clear filter</button>
    </div>

    <div class="log-toolbar">
      <input v-model="q" placeholder="Search logs…" style="width: 280px;" :disabled="!!jumpedSpanId" />
      <select v-model="sev" :disabled="!!jumpedSpanId">
        <option v-for="s in SEV_OPTIONS" :key="s" :value="s">{{ s }}</option>
      </select>
      <span style="color: var(--text2); font-size: 12px; margin-left: auto;">
        {{ total }} total{{ loading ? ' · loading…' : '' }}
      </span>
    </div>

    <table class="log-table">
      <thead>
        <tr>
          <th>Severity</th>
          <th>Timestamp</th>
          <th>Service</th>
          <th>Message</th>
          <th>Trace</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="(log, i) in logs"
          :key="i"
          class="log-row"
          :class="{ highlighted: jumpedSpanId && log.spanId === jumpedSpanId }"
        >
          <td>
            <span
              class="tag"
              :style="{ background: sevColors(normSev(log.severityText)).bg, color: sevColors(normSev(log.severityText)).fg }"
            >{{ normSev(log.severityText) }}</span>
          </td>
          <td class="mono">{{ fmtTime(log.timeMs) }}</td>
          <td>{{ log.service }}</td>
          <td class="log-body-cell">{{ log.body }}</td>
          <td>
            <a
              v-if="log.traceId"
              href="#"
              @click.prevent="emit('jumpToTrace', log.traceId, log.spanId ?? '')"
              class="jump-link mono"
              :title="`Go to span ${log.spanId ?? ''} in trace ${log.traceId}`"
            >
              <span>{{ log.traceId.slice(0, 8) }}…</span>
              <span class="jump-icon">↗</span>
            </a>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-if="logs.length < total" style="text-align: center; padding: 16px;">
      <button @click="loadMore" :disabled="loading">
        {{ loading ? 'Loading…' : `Load more (${total - logs.length} remaining)` }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.log-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0 12px;
}
.log-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.log-table th {
  text-align: left;
  color: var(--text2);
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  font-weight: 500;
  position: sticky;
  top: 48px;
  background: var(--bg);
}
.log-row td {
  padding: 5px 8px;
  border-bottom: 1px solid var(--border);
  vertical-align: top;
}
.log-row:hover td { background: var(--bg2); }
.log-body-cell { word-break: break-word; max-width: 600px; }
.jump-link { display: inline-flex; align-items: center; gap: 3px; font-size: 11px; color: var(--accent); text-decoration: none; }
.jump-link:hover .jump-icon { opacity: 1; }
.jump-icon { opacity: 0.5; font-size: 12px; }

.span-filter-banner {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  background: #0d1f38;
  border: 1px solid var(--accent);
  border-radius: var(--radius);
  margin-bottom: 8px;
  font-size: 12px;
  color: var(--text);
}
.clear-filter-btn {
  margin-left: auto;
  padding: 3px 10px;
  border-radius: var(--radius);
  border: 1px solid var(--border);
  background: var(--bg3);
  color: var(--text2);
  cursor: pointer;
  font-size: 11px;
}
.clear-filter-btn:hover { border-color: var(--accent); color: var(--accent); }
.log-row.highlighted td { background: #0d1f38; }
</style>
