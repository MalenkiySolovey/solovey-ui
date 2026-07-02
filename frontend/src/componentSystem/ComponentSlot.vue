<template>
  <component
    :is="entryComponent(entry)"
    v-for="(entry, index) in entries"
    :key="`${name}:${index}`"
    :ctx="ctx"
  />
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { slotEntries } from './registry'
import type { SlotContribution, SlotName } from './types'

const props = defineProps<{
  name: SlotName
  ctx?: Record<string, unknown>
}>()

const entries = computed(() => slotEntries(props.name))
const entryComponent = (entry: SlotContribution) => defineAsyncComponent(entry.component)
</script>
