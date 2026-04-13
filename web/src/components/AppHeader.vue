<script setup lang="ts">
import type { Meta } from '../types'

defineProps<{
  meta: Meta | null
  activeTab: string
}>()

const emit = defineEmits<{
  switchTab: [tab: string]
}>()

const TABS = [
  { id: 'traces', label: '🔍 Traces' },
  { id: 'logs',   label: '📋 Logs' },
  { id: 'metrics', label: '📊 Metrics' },
]
</script>

<template>
  <header class="header">
    <div class="header-left">
      <span class="logo">lovie</span>
      <span v-if="meta" class="file-info">
        {{ meta.file }} &mdash;
        {{ meta.traceCount }} traces,
        {{ meta.logCount }} logs,
        {{ meta.metricCount }} metrics
      </span>
    </div>
    <nav class="tabs">
      <button
        v-for="t in TABS"
        :key="t.id"
        :class="['tab-btn', activeTab === t.id ? 'active' : '']"
        @click="emit('switchTab', t.id)"
      >{{ t.label }}</button>
    </nav>
  </header>
</template>

<style scoped>
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 20px;
  background: var(--bg2);
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  z-index: 100;
}
.logo { font-weight: 700; font-size: 18px; color: var(--accent); margin-right: 12px; }
.file-info { color: var(--text2); font-size: 12px; }
.tabs { display: flex; gap: 4px; }
.tab-btn { background: transparent; border: 1px solid transparent; padding: 6px 14px; border-radius: var(--radius); }
.tab-btn:hover { border-color: var(--border); }
.tab-btn.active { background: var(--bg3); border-color: var(--accent); color: var(--accent); }
</style>
