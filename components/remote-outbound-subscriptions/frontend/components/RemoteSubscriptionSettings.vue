<template>
  <v-col v-if="settings" cols="12" sm="6" md="4">
    <v-select
      v-model="groupAdaptation"
      class="setting-info-field"
      density="compact"
      hide-details
      :items="groupAdaptationItems"
      :label="$t('remoteOutbound.setting.groupAdaptation')"
      variant="outlined"
    >
      <template #append-inner>
        <SettingInfo :text="$t('remoteOutbound.setting.hint.groupAdaptation')" />
      </template>
    </v-select>
  </v-col>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import SettingInfo from '@/components/settings/SettingInfo.vue'

type SettingsMap = Record<string, string>

const props = defineProps<{
  ctx?: {
    settings?: SettingsMap
  }
}>()

const groupAdaptationItems = ['urltest', 'selector', 'failover']

const settings = computed(() => props.ctx?.settings)

const groupAdaptation = computed({
  get: () => settings.value?.subRemoteGroupAdaptation ?? 'urltest',
  set: (value: string) => {
    if (!settings.value) return
    settings.value.subRemoteGroupAdaptation = value || 'urltest'
  },
})
</script>
