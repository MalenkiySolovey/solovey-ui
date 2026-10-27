<template>
  <v-card>
    <v-card-text>
      <p class="mb-4">{{ $t('nativeOutbound.groupHint') }}</p>
      <template v-if="data.type === 'core-failover'">
        <StrictSelect v-model="data.outbounds" :items="members" :label="$t('pages.outbounds')" multiple chips />
        <v-row>
          <v-col cols="12" sm="6"><StrictSelect v-model="data.strategy" :items="['sequential', 'cycle']" :label="$t('nativeOutbound.strategy')" clearable /></v-col>
          <v-col cols="12" sm="6"><v-text-field v-model="data.delay" :label="$t('nativeOutbound.delay')" placeholder="100ms" /></v-col>
        </v-row>
      </template>
      <template v-else>
        <p class="mb-4">{{ $t('nativeOutbound.ratioHint') }}</p>
        <v-row v-for="(member, index) in data.outbounds" :key="index" align="center">
          <v-col cols="12" sm="4"><StrictSelect v-model="member.outbound" :items="members" :label="$t('pages.outbounds')" /></v-col>
          <v-col cols="4" sm="2"><v-text-field v-model.number="member.download_ratio" type="number" min="0" max="100" :label="$t('nativeOutbound.downloadRatio')" /></v-col>
          <v-col cols="4" sm="2"><v-text-field v-model.number="member.upload_ratio" type="number" min="0" max="100" :label="$t('nativeOutbound.uploadRatio')" /></v-col>
          <v-col cols="4" sm="2"><v-text-field v-model.number="member.count" type="number" min="1" max="255" :label="$t('nativeOutbound.count')" /></v-col>
          <v-col cols="12" sm="2"><v-btn variant="text" color="error" @click="data.outbounds.splice(index, 1)">{{ $t('actions.del') }}</v-btn></v-col>
        </v-row>
        <v-btn variant="tonal" @click="data.outbounds.push({ outbound: '', download_ratio: 100, upload_ratio: 100, count: 1 })">{{ $t('actions.add') }}</v-btn>
      </template>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Outbound } from '@/types/outbounds'
import Data from '@/store/modules/data'
import StrictSelect from '@/shared/ui/StrictSelect.vue'

const props = defineProps<{ data: Outbound }>()
// Native groups borrow canonical outbound instances. Endpoints retain their
// separate owner and are not offered as outbound member definitions.
const members = computed(() => Data().outbounds.filter((row: Outbound) => row.id !== props.data.id && row.tag !== props.data.tag).map((row: Outbound) => row.tag))
</script>
