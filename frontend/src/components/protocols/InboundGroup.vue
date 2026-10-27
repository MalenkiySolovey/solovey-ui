<template>
  <v-card :subtitle="data.type === InTypes.Bond ? 'Bond' : 'Core failover'">
    <v-card-text>
      <v-select
        v-model="data.inbounds"
        :items="members"
        :label="$t('nativeInbound.bondMembers')"
        multiple chips closable-chips
      />
      <div class="text-caption mb-2">{{ $t('nativeInbound.bondMembersHint') }}</div>
      <div class="text-caption">{{ $t(data.type === InTypes.Bond ? 'nativeInbound.bondIdentityHint' : 'nativeInbound.coreFailoverHint') }}</div>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { InTypes, isInboundGroup, type InboundGroup } from '@/types/inbounds'
import Data from '@/store/modules/data'

const props = defineProps<{ data: InboundGroup }>()
const members = computed(() => {
  const rows = Data().inbounds
  const claimed = new Set<string>()
  for (const row of rows) {
    if (row.tag !== props.data.tag && isInboundGroup(row.type) && Array.isArray(row.inbounds)) {
      for (const tag of row.inbounds) claimed.add(tag)
    }
  }
  const choices = rows
    .filter(row => row.id !== props.data.id && !isInboundGroup(row.type) && !claimed.has(row.tag))
    .map(row => ({ title: `${row.tag} (${row.type})`, value: row.tag }))
  // Retain a captured reference even if its member disappeared; the canonical
  // save validator explains it instead of the editor silently dropping it.
  for (const tag of props.data.inbounds ?? []) {
    if (!choices.some(row => row.value === tag)) choices.push({ title: tag, value: tag })
  }
  return choices
})
</script>
