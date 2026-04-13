<script setup lang="ts">
import { ref, onMounted, shallowRef } from 'vue'
import AppHeader from './components/AppHeader.vue'
import TraceListView from './components/traces/TraceListView.vue'
import LogView from './components/logs/LogView.vue'
import MetricsView from './components/metrics/MetricsView.vue'
import { api } from './api/client'
import type { Meta } from './types'

type Tab = 'traces' | 'logs' | 'metrics'

const meta = shallowRef<Meta | null>(null)
const activeTab = ref<Tab>('traces')
const mounted = ref(new Set<Tab>(['traces']))

const jumpTrace = ref<string | null>(null)
const jumpSpan = ref<string | null>(null)
const jumpKey = ref(0)

const jumpLogSpan = ref<string | null>(null)
const jumpLogKey = ref(0)

function switchTab(t: Tab) {
  activeTab.value = t
  mounted.value.add(t)
}

function jumpToTrace(traceId: string, spanId?: string) {
  jumpTrace.value = traceId
  jumpSpan.value = spanId ?? null
  jumpKey.value++
  switchTab('traces')
}

function jumpToLogs(spanId: string) {
  jumpLogSpan.value = spanId
  jumpLogKey.value++
  switchTab('logs')
}

onMounted(async () => {
  meta.value = await api.meta()
})
</script>

<template>
  <AppHeader :meta="meta" :activeTab="activeTab" @switchTab="switchTab" />
  <main style="padding: 16px; max-width: 1800px; margin: 0 auto;">
    <TraceListView
      v-if="mounted.has('traces')"
      v-show="activeTab === 'traces'"
      :jumpTrace="jumpTrace"
      :jumpSpan="jumpSpan"
      :jumpKey="jumpKey"
      @jumpToLogs="jumpToLogs"
    />
    <LogView
      v-if="mounted.has('logs')"
      v-show="activeTab === 'logs'"
      :jumpSpanId="jumpLogSpan"
      :jumpKey="jumpLogKey"
      @jumpToTrace="jumpToTrace"
    />
    <MetricsView
      v-if="mounted.has('metrics')"
      v-show="activeTab === 'metrics'"
    />
  </main>
</template>
