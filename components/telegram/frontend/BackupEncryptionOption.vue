<template>
  <v-col cols="12" sm="auto" align-self="center">
    <v-tooltip :text="$t('telegram.backup.encryptDisabledHint')" :disabled="passphraseConfigured">
      <template #activator="{ props: tooltipProps }">
        <span v-bind="tooltipProps">
          <v-checkbox
            v-model="enabled"
            :label="$t('telegram.backup.encryptManual')"
            :disabled="!passphraseConfigured"
            hide-details
          />
        </span>
      </template>
    </v-tooltip>
  </v-col>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { loadBackupSettings } from '@/shared/composables/useBackupOperations'

type BackupOptionContext = {
  visible?: boolean
  setBackupParam?: (key: string, value?: string) => void
}

const props = defineProps<{
  ctx?: BackupOptionContext
}>()

const enabled = ref(false)
const passphraseConfigured = ref(false)

const setParam = (value?: string) => {
  props.ctx?.setBackupParam?.('backupEncryption', value ? 'telegram' : undefined)
}

const loadState = async () => {
  const msg = await loadBackupSettings()
  passphraseConfigured.value = msg.success && msg.obj?.telegramBackupPassphraseHasSecret === 'true'
  if (!passphraseConfigured.value) {
    enabled.value = false
  }
}

watch(enabled, (value: boolean) => {
  setParam(value && passphraseConfigured.value ? 'true' : undefined)
})

watch(() => props.ctx?.visible, (visible: boolean | undefined) => {
  if (visible) {
    enabled.value = false
    setParam(undefined)
    void loadState()
  }
}, { immediate: true })

onBeforeUnmount(() => setParam(undefined))
</script>
