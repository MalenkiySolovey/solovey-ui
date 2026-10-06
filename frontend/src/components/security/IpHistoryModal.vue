<template>
  <v-dialog :model-value="visible" width="680" @update:model-value="setVisible">
    <v-card :loading="loading" class="rounded-lg">
      <v-card-title>{{ $t('client.ipHistory') }} - {{ client }}</v-card-title>
      <v-divider></v-divider>
      <v-card-text>
        <v-row align="center" class="mb-2">
          <v-col cols="12" sm="6">
            <v-switch
              v-if="isAdmin && hasRawRows"
              :model-value="showRaw"
              color="warning"
              density="compact"
              hide-details
              :label="$t('client.showRawIp')"
              @update:model-value="requestShowRaw"
            />
          </v-col>
        </v-row>
        <v-table density="compact">
          <thead>
            <tr>
              <th>IP</th>
              <th>{{ $t('client.firstSeen') }}</th>
              <th>{{ $t('client.lastSeen') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, index) in rows" :key="row.id ?? `${row.ip}-${index}`">
              <td>{{ displayIP(row.ip, showRaw) }}</td>
              <td>{{ formatTime(row.firstSeen) }}</td>
              <td>{{ formatTime(row.lastSeen) }}</td>
            </tr>
            <tr v-if="rows.length === 0 && !loading">
              <td colspan="3">{{ $t('noData') }}</td>
            </tr>
          </tbody>
        </v-table>
      </v-card-text>
      <v-card-actions>
        <v-btn color="error" variant="outlined" :disabled="loading || rows.length === 0" @click="clearHistory">{{ $t('reset') }}</v-btn>
        <v-spacer></v-spacer>
        <v-btn variant="outlined" @click="setVisible(false)">{{ $t('actions.close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-model="confirmRaw" max-width="440">
    <v-card>
      <v-card-title>{{ $t('client.showRawIp') }}</v-card-title>
      <v-divider></v-divider>
      <v-card-text>{{ $t('client.showRawIpConfirm') }}</v-card-text>
      <v-card-actions>
        <v-btn color="warning" variant="outlined" @click="confirmShowRaw">{{ $t('yes') }}</v-btn>
        <v-btn color="primary" variant="outlined" @click="cancelShowRaw">{{ $t('no') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import { dateLocale } from '@/locales'
import { displayIP, hasRawIPRows } from '@/components/security/ipHistory'
import { useIpHistory } from '@/features/useIpHistory'

const props = withDefaults(defineProps<{
  visible: boolean
  client: string
  isAdmin?: boolean
}>(), {
  isAdmin: true,
})

const emit = defineEmits<{
  'update:visible': [value: boolean]
  cleared: []
}>()

const showRaw = ref(false)
const confirmRaw = ref(false)
const history = useIpHistory(() => [props.visible, props.client] as const, () => {
  showRaw.value = false
  emit('cleared')
})
const { rows, loading, clearHistory } = history

const hasRawRows = computed(() => hasRawIPRows(rows.value))

watch(() => [props.visible, props.client] as const, () => {
  showRaw.value = false
  confirmRaw.value = false
}, { flush: 'sync' })

const setVisible = (value: boolean) => {
  if (!value) {
    history.cancel()
    showRaw.value = false
    confirmRaw.value = false
  }
  emit('update:visible', value)
}

const requestShowRaw = (value: boolean | null) => {
  if (!value) {
    showRaw.value = false
    return
  }
  if (!props.isAdmin) return
  confirmRaw.value = true
}

const confirmShowRaw = () => {
  showRaw.value = true
  confirmRaw.value = false
}

const cancelShowRaw = () => {
  showRaw.value = false
  confirmRaw.value = false
}

const formatTime = (value: number) => {
  if (!value) return '-'
  return new Date(value * 1000).toLocaleString(dateLocale())
}
</script>
