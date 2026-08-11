<template>
  <div class="backup-restore-panel">
    <v-row>
      <v-col cols="auto">
        <v-checkbox v-model="exclude" :label="$t('main.backup.exclStats')" value="stats" hide-details />
      </v-col>
      <v-col cols="auto">
        <v-checkbox v-model="exclude" :label="$t('main.backup.exclChanges')" value="changes" hide-details />
      </v-col>
    </v-row>

    <v-row>
      <v-col cols="auto" align-self="center">
        <v-btn color="primary" @click="backup()" hide-details>{{ $t('main.backup.backup') }}</v-btn>
      </v-col>
      <ComponentSlot name="backup-restore:backup-options" :ctx="backupSlotContext" />
      <v-spacer />
      <v-col cols="auto" align-self="center">
        <v-btn color="primary" to="/operations#restore" hide-details>{{ $t('main.backup.restore') }}</v-btn>
      </v-col>
      <v-col cols="12">
        <div class="text-caption text-medium-emphasis">{{ $t('main.backup.restoreHint') }}</div>
      </v-col>
    </v-row>

    <v-row>
      <v-col cols="12">
        <v-text-field
          v-model="securityCredential"
          autocomplete="current-password"
          :hint="$t('security.stepUpCredentialHint')"
          :label="$t('security.stepUpCredential')"
          persistent-hint
          type="password"
        />
      </v-col>
    </v-row>

    <v-row class="backup-restore-panel__section">
      <v-divider />
      <v-col cols="auto" align-self="center">
        <v-btn color="primary" @click="config()" hide-details>{{ $t('main.backup.sbConfig') }}</v-btn>
      </v-col>
    </v-row>

    <ComponentSlot name="backup-restore:actions" :ctx="actionSlotContext" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import ComponentSlot from '@/componentSystem/ComponentSlot.vue'
import { acquireStepUpToken } from '@/shared/composables/useSecurityOperations'

const props = withDefaults(defineProps<{
  visible?: boolean
}>(), {
  visible: true,
})

const emit = defineEmits<{
  close: []
}>()

const exclude = ref<string[]>(['stats', 'changes'])
const securityCredential = ref('')
const backupParams = ref<Record<string, string>>({})

const setBackupParam = (key: string, value?: string) => {
  const next = { ...backupParams.value }
  if (value === undefined || value === '') {
    delete next[key]
  } else {
    next[key] = value
  }
  backupParams.value = next
}

const closePanel = () => emit('close')
const backupSlotContext = computed(() => ({
  visible: props.visible,
  setBackupParam,
}))
const actionSlotContext = computed(() => ({
  visible: props.visible,
  close: closePanel,
  acquireDatabaseImportStepUp: async () => {
    const { token } = await acquireStepUpToken(
      'backup.restore',
      'database:compatible-import',
      securityCredential.value,
    )
    securityCredential.value = ''
    return token
  },
}))

const backup = () => {
  const params = new URLSearchParams()
  if (exclude.value.length > 0) {
    params.set('exclude', exclude.value.join(','))
  }
  for (const [key, value] of Object.entries(backupParams.value)) {
    if (value) {
      params.set(key, value)
    }
  }
  const query = params.toString()
  window.location.href = 'api/getdb' + (query ? '?' + query : '')
}

const config = () => {
  window.location.href = 'api/singbox-config'
}

const reset = () => {
  exclude.value = ['stats', 'changes']
  backupParams.value = {}
  securityCredential.value = ''
}

watch(() => props.visible, visible => {
  if (visible) reset()
}, { immediate: true })
</script>

<style scoped>
.backup-restore-panel {
  min-width: 0;
}

.backup-restore-panel__section {
  margin-top: var(--nexus-gap-2, 8px);
}

.backup-restore-panel__report {
  white-space: pre-wrap;
}
</style>
