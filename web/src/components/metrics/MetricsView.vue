<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import type { DisplayMetric, DataPoint, HistogramValue } from '../../types'
import { fmtVal, fmtTime } from '../../utils/format'
import { svcColor } from '../../utils/colors'

const metrics = ref<DisplayMetric[]>([])
const loading = ref(true)

onMounted(async () => {
  try {
    metrics.value = await api.metrics()
  } finally {
    loading.value = false
  }
})

function latestValue(dp: DataPoint, type: string): string {
  if (type === 'histogram' && typeof dp.value === 'object' && dp.value !== null) {
    const hv = dp.value as HistogramValue
    return `count=${hv.count} sum=${fmtVal(hv.sum)}`
  }
  return fmtVal(dp.value as number)
}

function attrsLabel(a: Record<string, unknown>): string {
  return Object.entries(a).map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`).join(' ')
}
</script>

<template>
  <div>
    <div v-if="loading" style="padding: 40px; text-align: center;">
      <span class="spinner"></span>
    </div>
    <div v-else-if="metrics.length === 0" style="color: var(--text2); padding: 20px;">No metrics found.</div>
    <div v-else class="metrics-grid">
      <div v-for="m in metrics" :key="m.service + '/' + m.name" class="metric-card card">
        <div class="metric-header">
          <span class="metric-type tag" :style="{ background: 'var(--bg3)', color: 'var(--text2)' }">{{ m.type }}</span>
          <span class="metric-name">{{ m.name }}</span>
          <span class="metric-svc" :style="{ color: svcColor(m.service) }">{{ m.service }}</span>
        </div>
        <div v-if="m.description" class="metric-desc">{{ m.description }}</div>
        <div v-if="m.unit" class="metric-unit">unit: {{ m.unit }}</div>
        <div class="datapoints">
          <div v-for="(dp, i) in m.dataPoints" :key="i" class="dp-row">
            <span class="dp-val">{{ latestValue(dp, m.type) }}</span>
            <span v-if="dp.attributes && Object.keys(dp.attributes).length" class="dp-attrs mono">{{ attrsLabel(dp.attributes) }}</span>
            <span class="dp-time mono">{{ fmtTime(dp.timeMs) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 12px;
}
.metric-card { padding: 12px 14px; }
.metric-header { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.metric-name { font-weight: 600; }
.metric-svc { font-size: 12px; margin-left: auto; }
.metric-desc { color: var(--text2); font-size: 12px; margin-bottom: 4px; }
.metric-unit { color: var(--text2); font-size: 11px; margin-bottom: 8px; }
.datapoints { border-top: 1px solid var(--border); padding-top: 8px; }
.dp-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 3px 0;
  font-size: 12px;
  border-bottom: 1px solid var(--border);
}
.dp-val { font-weight: 600; color: var(--accent); }
.dp-attrs { color: var(--text2); font-size: 11px; flex: 1; }
.dp-time { color: var(--text2); font-size: 11px; margin-left: auto; }
</style>
