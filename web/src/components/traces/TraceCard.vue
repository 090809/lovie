<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import SpanDetail from './SpanDetail.vue'
import { api } from '../../api/client'
import type { TraceSummary, TraceDetail, DisplaySpan } from '../../types'
import { fmtDur, fmtTime } from '../../utils/format'
import { svcColor } from '../../utils/colors'

const COLLAPSE_DEPTH = 5

const props = defineProps<{
  summary: TraceSummary
  autoExpand?: boolean
  jumpSpan?: string | null
  jumpKey?: number
}>()

const emit = defineEmits<{
  jumpToLogs: [spanId: string]
}>()

const expanded = ref(false)
const detail = ref<TraceDetail | null>(null)
const loading = ref(false)
const selectedSpan = ref<DisplaySpan | null>(null)

// spanIds whose direct children are hidden
const collapsedSpans = ref(new Set<string>())

async function toggle() {
  expanded.value = !expanded.value
  if (expanded.value && !detail.value) {
    loading.value = true
    try {
      detail.value = await api.traceDetail(props.summary.traceId)
    } finally {
      loading.value = false
    }
  }
}

watch(() => props.autoExpand, async (v) => {
  if (v && !expanded.value) {
    loading.value = true
    expanded.value = true
    try {
      detail.value = await api.traceDetail(props.summary.traceId)
    } finally {
      loading.value = false
    }
  }
}, { immediate: true })

// Fires on every jump (including re-jumps to already-expanded trace)
watch(() => props.jumpKey, async () => {
  if (!props.jumpSpan) return
  // Ensure trace is expanded and loaded
  if (!expanded.value || !detail.value) {
    expanded.value = true
    loading.value = true
    try {
      detail.value = await api.traceDetail(props.summary.traceId)
    } finally {
      loading.value = false
    }
  }
  if (!detail.value) return
  const sp = detail.value.spans.find(s => s.spanId === props.jumpSpan)
  if (!sp) return

  // Un-collapse all ancestors so the span becomes visible
  const po = parentOf.value
  const next = new Set(collapsedSpans.value)
  let cur = sp.parentSpanId
  while (cur) {
    next.delete(cur)
    cur = po.get(cur) ?? ''
  }
  collapsedSpans.value = next

  selectedSpan.value = sp

  // Scroll span row into view
  await nextTick()
  const el = document.querySelector(`[data-span-id="${sp.spanId}"]`)
  el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
})

// When trace data loads, auto-collapse spans at depth >= COLLAPSE_DEPTH-1 that have children
watch(detail, (d) => {
  if (!d) { collapsedSpans.value = new Set(); return }
  const childCount = new Map<string, number>()
  for (const sp of d.spans) {
    if (sp.parentSpanId) childCount.set(sp.parentSpanId, (childCount.get(sp.parentSpanId) ?? 0) + 1)
  }
  const initial = new Set<string>()
  for (const sp of d.spans) {
    if (sp.depth >= COLLAPSE_DEPTH - 1 && (childCount.get(sp.spanId) ?? 0) > 0) {
      initial.add(sp.spanId)
    }
  }
  collapsedSpans.value = initial
})

const traceStart = computed(() => detail.value ? detail.value.startMs : 0)
const traceDur = computed(() => detail.value ? (detail.value.durationMs || 1) : 1)

// spanId → parentSpanId
const parentOf = computed(() => {
  const m = new Map<string, string>()
  for (const sp of detail.value?.spans ?? []) {
    if (sp.parentSpanId) m.set(sp.spanId, sp.parentSpanId)
  }
  return m
})

// spanId → direct child count
const childrenOf = computed(() => {
  const m = new Map<string, number>()
  for (const sp of detail.value?.spans ?? []) {
    if (sp.parentSpanId) m.set(sp.parentSpanId, (m.get(sp.parentSpanId) ?? 0) + 1)
  }
  return m
})

// Visible spans: hidden if any ancestor is in collapsedSpans
const visibleSpans = computed<DisplaySpan[]>(() => {
  if (!detail.value) return []
  const po = parentOf.value
  const cs = collapsedSpans.value
  return detail.value.spans.filter(sp => {
    let cur = sp.parentSpanId
    while (cur) {
      if (cs.has(cur)) return false
      cur = po.get(cur) ?? ''
    }
    return true
  })
})

function hasChildren(sp: DisplaySpan): boolean {
  return (childrenOf.value.get(sp.spanId) ?? 0) > 0
}

function isCollapsed(sp: DisplaySpan): boolean {
  return collapsedSpans.value.has(sp.spanId)
}

function toggleCollapse(sp: DisplaySpan, e: Event) {
  e.stopPropagation()
  const next = new Set(collapsedSpans.value)
  if (next.has(sp.spanId)) next.delete(sp.spanId)
  else next.add(sp.spanId)
  collapsedSpans.value = next
}

function spanLabelStyle(sp: DisplaySpan): object {
  const left = (sp.startMs - traceStart.value) / traceDur.value * 100
  const width = Math.max(sp.durationMs / traceDur.value * 100, 0.3)
  const rightEdge = left + width
  if (width >= 12) {
    // Wide enough — label inside bar
    return { left: left.toFixed(2) + '%', width: width.toFixed(2) + '%', textAlign: 'center', color: 'rgba(255,255,255,0.9)', overflow: 'hidden' }
  }
  if (rightEdge > 85) {
    // Near right edge — label to the LEFT of bar
    return { right: (100 - left).toFixed(2) + '%', paddingRight: '4px' }
  }
  // Default — label to the RIGHT of bar
  return { left: rightEdge.toFixed(2) + '%', paddingLeft: '4px' }
}

function spanLeft(sp: DisplaySpan): string {
  return ((sp.startMs - traceStart.value) / traceDur.value * 100).toFixed(2) + '%'
}

function spanWidth(sp: DisplaySpan): string {
  const w = Math.max(sp.durationMs / traceDur.value * 100, 0.3)
  return w.toFixed(2) + '%'
}

function eventLeft(sp: DisplaySpan, evTimeMs: number): string {
  if (sp.durationMs < 0.001) return '0%'
  const pct = (evTimeMs - sp.startMs) / sp.durationMs * 100
  return Math.min(Math.max(pct, 0), 100).toFixed(2) + '%'
}

function selectSpan(sp: DisplaySpan) {
  selectedSpan.value = selectedSpan.value?.spanId === sp.spanId ? null : sp
}
</script>

<template>
  <div class="trace-card card">
    <!-- Header row -->
    <div class="trace-header" @click="toggle">
      <span class="expand-icon">{{ expanded ? '▼' : '▶' }}</span>
      <span class="svc-dot" :style="{ background: svcColor(summary.rootService) }"></span>
      <span class="root-name">{{ summary.rootName }}</span>
      <span class="root-svc">{{ summary.rootService }}</span>
      <span style="flex:1"></span>
      <span v-if="summary.errorCount" class="tag" style="background:#2b0d0d;color:#f85149;margin-right:8px">
        {{ summary.errorCount }} error{{ summary.errorCount > 1 ? 's' : '' }}
      </span>
      <span class="dur tag" style="background:var(--bg3);color:var(--text2)">{{ fmtDur(summary.durationMs) }}</span>
      <span class="span-count" style="color:var(--text2);font-size:12px;margin-left:8px">{{ summary.spanCount }} spans</span>
    </div>

    <!-- Loading -->
    <div v-if="expanded && loading" style="padding:12px;color:var(--text2)">
      <span class="spinner"></span> Loading…
    </div>

    <!-- Waterfall -->
    <div v-if="expanded && detail" class="waterfall">
      <template
        v-for="sp in visibleSpans"
        :key="sp.spanId"
      >
        <div
          class="span-row"
          :data-span-id="sp.spanId"
          :class="{ selected: selectedSpan?.spanId === sp.spanId, error: sp.hasError }"
          @click="selectSpan(sp)"
        >
          <!-- Name column -->
          <div class="span-name" :style="{ paddingLeft: (sp.depth * 16 + 4) + 'px' }">
            <button
              v-if="hasChildren(sp)"
              class="collapse-btn"
              :title="isCollapsed(sp) ? 'Expand children' : 'Collapse children'"
              @click="toggleCollapse(sp, $event)"
            >{{ isCollapsed(sp) ? '▶' : '▼' }}</button>
            <span v-else class="collapse-placeholder"></span>
            <span class="svc-pip" :style="{ background: svcColor(sp.service) }"></span>
            <span class="truncate">{{ sp.name }}</span>
          </div>
          <!-- Bar column -->
          <div class="span-bar-col">
            <div class="span-bar-track">
              <div
                class="span-bar"
                :style="{
                  left: spanLeft(sp),
                  width: spanWidth(sp),
                  background: svcColor(sp.service),
                  opacity: sp.hasError ? 1 : 0.8,
                  outline: sp.hasError ? '1px solid #f85149' : 'none',
                }"
                :title="sp.name + ' · ' + fmtDur(sp.durationMs)"
              ></div>
              <span
                class="span-bar-label"
                :style="spanLabelStyle(sp)"
              >{{ fmtDur(sp.durationMs) }}</span>
              <!-- Event markers -->
              <div
                v-for="(ev, i) in (sp.events ?? [])"
                :key="i"
                class="event-marker"
                :style="{ left: eventLeft(sp, ev.timeMs) }"
                :title="ev.name + ' @ ' + fmtTime(ev.timeMs)"
              >◆</div>
            </div>
          </div>
        </div>
        <!-- Inline span detail — renders immediately below the clicked span row -->
        <SpanDetail
          v-if="selectedSpan?.spanId === sp.spanId"
          :span="sp"
          :traceId="summary.traceId"
          @jumpToLogs="emit('jumpToLogs', $event)"
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
.trace-card { padding: 0; overflow: hidden; }

.trace-header {
  display: flex;
  align-items: center;
  padding: 10px 14px;
  cursor: pointer;
  gap: 8px;
  user-select: none;
}
.trace-header:hover { background: var(--bg3); }
.expand-icon { color: var(--text2); font-size: 10px; }
.svc-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
.root-name { font-weight: 600; }
.root-svc { color: var(--text2); font-size: 12px; }

.waterfall { border-top: 1px solid var(--border); }

.span-row {
  display: flex;
  align-items: center;
  height: 28px;
  cursor: pointer;
  border-bottom: 1px solid var(--border);
}
.span-row:hover { background: var(--bg3); }
.span-row.selected { background: #1a2540; }
.span-row.error { box-shadow: inset 3px 0 0 #f85149; }

.span-name {
  width: 280px;
  min-width: 280px;
  display: flex;
  align-items: center;
  gap: 6px;
  overflow: hidden;
  border-right: 1px solid var(--border);
  height: 100%;
  padding-right: 8px;
  font-size: 12px;
}
.svc-pip { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }

.collapse-btn {
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  padding: 0;
  margin-right: 2px;
  font-size: 9px;
  border-radius: 3px;
  border: 1px solid var(--border);
  background: var(--bg3);
  color: var(--text2);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
}
.collapse-btn:hover { border-color: var(--accent); color: var(--accent); }
.collapse-placeholder { flex-shrink: 0; width: 18px; }

.span-bar-col { flex: 1; height: 100%; padding: 4px 8px; }
.span-bar-track { position: relative; height: 100%; }

.span-bar {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  height: 14px;
  border-radius: 3px;
  min-width: 3px;
}
.span-bar-label {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  font-size: 10px;
  color: var(--text2);
  white-space: nowrap;
  pointer-events: none;
}

.event-marker {
  position: absolute;
  top: 50%;
  transform: translate(-50%, -50%);
  font-size: 9px;
  color: #f0883e;
  cursor: default;
  z-index: 2;
  pointer-events: auto;
}
</style>
