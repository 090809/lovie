<script setup lang="ts">
import { ref, onMounted } from 'vue'
import TraceCard from './TraceCard.vue'
import { api } from '../../api/client'
import type { TraceSummary } from '../../types'

const props = defineProps<{
  jumpTrace: string | null
  jumpSpan: string | null
  jumpKey: number
}>()

const emit = defineEmits<{
  jumpToLogs: [spanId: string]
}>()

const traces = ref<TraceSummary[]>([])
const loading = ref(true)
const error = ref<string | null>(null)

onMounted(async () => {
  try {
    traces.value = await api.traces()
  } catch (e: unknown) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div>
    <div v-if="loading" style="padding: 40px; text-align: center;">
      <span class="spinner"></span>
      <span style="margin-left: 8px; color: var(--text2);">Loading traces…</span>
    </div>
    <div v-else-if="error" style="color: var(--red); padding: 20px;">{{ error }}</div>
    <div v-else-if="traces.length === 0" style="color: var(--text2); padding: 20px;">No traces found.</div>
    <TraceCard
      v-for="t in traces"
      :key="t.traceId"
      :summary="t"
      :autoExpand="jumpTrace === t.traceId"
      :jumpSpan="jumpTrace === t.traceId ? jumpSpan : null"
      :jumpKey="jumpTrace === t.traceId ? jumpKey : 0"
      @jumpToLogs="emit('jumpToLogs', $event)"
    />
  </div>
</template>
