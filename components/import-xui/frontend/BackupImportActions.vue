<template>
  <v-row class="backup-import-actions">
    <v-divider class="mb-2" />
    <v-col cols="12">
      <div class="text-subtitle-1">{{ $t('migrateXui.backupAction.title') }}</div>
      <div class="text-caption text-medium-emphasis mb-2">{{ $t('migrateXui.backupAction.hint') }}</div>
    </v-col>
    <v-col cols="auto">
      <v-checkbox v-model="dryRun" :label="$t('migrateXui.backupAction.dryRun')" hide-details />
    </v-col>
    <v-col cols="12" sm="auto">
      <v-select
        v-model="strategy"
        :items="['merge', 'replace', 'skip']"
        :label="$t('migrateXui.backupAction.strategy')"
        density="compact"
        hide-details
      />
    </v-col>
    <v-col cols="12" sm="auto" align-self="center">
      <v-btn color="primary" @click="migrate()" hide-details>{{ $t('migrateXui.backupAction.button') }}</v-btn>
    </v-col>
    <v-col cols="12" sm="auto" align-self="center">
      <v-btn variant="tonal" prepend-icon="mdi-open-in-new" @click="openFullMigration()" hide-details>
        {{ $t('migrateXui.backupAction.openFull') }}
      </v-btn>
    </v-col>
    <v-col v-if="report" cols="12">
      <v-card variant="outlined" class="pa-3">
        <div class="text-subtitle-2 mb-1">{{ $t('migrateXui.backupAction.summary') }}</div>
        <pre class="text-caption backup-import-actions__report">{{ JSON.stringify(report.summary, null, 2) }}</pre>
        <template v-if="report.warnings && report.warnings.length">
          <div class="text-subtitle-2 mt-2">{{ $t('migrateXui.backupAction.warnings') }}</div>
          <ul>
            <li v-for="(warning, index) in report.warnings" :key="index">{{ warning }}</li>
          </ul>
        </template>
      </v-card>
    </v-col>
  </v-row>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { importXuiDatabase } from './composables/useXuiMigrationOperations'

type BackupActionContext = {
  visible?: boolean
  close?: () => void
  acquireDatabaseImportStepUp?: () => Promise<string | null>
}

const props = defineProps<{
  ctx?: BackupActionContext
}>()

const router = useRouter()
const dryRun = ref(true)
const strategy = ref('merge')
const report = ref<null | Record<string, any>>(null)

const migrate = () => {
  const fileInput = document.createElement('input')
  fileInput.type = 'file'
  fileInput.accept = '.db'

  fileInput.addEventListener('change', async (event: Event) => {
    const inputElement = event.target as HTMLInputElement
    const dbFile = inputElement.files ? inputElement.files[0] : null

    if (!dbFile) return
    const formData = new FormData()
    formData.append('db', dbFile)
    formData.append('dryRun', dryRun.value ? '1' : '0')
    formData.append('strategy', strategy.value)

    let stepUpToken = ''
    if (!dryRun.value) {
      stepUpToken = await props.ctx?.acquireDatabaseImportStepUp?.() ?? ''
      if (!stepUpToken) return
    }
    const uploadMsg = await importXuiDatabase(formData, stepUpToken)
    if (uploadMsg.success) {
      report.value = uploadMsg.obj
      if (!dryRun.value) {
        props.ctx?.close?.()
        await new Promise(resolve => setTimeout(resolve, 1000))
        location.reload()
      }
    }
  })

  fileInput.click()
}

const openFullMigration = () => {
  props.ctx?.close?.()
  void router.push('/migrate-xui')
}

watch(() => props.ctx?.visible, (visible: boolean | undefined) => {
  if (visible) {
    dryRun.value = true
    strategy.value = 'merge'
    report.value = null
  }
}, { immediate: true })
</script>

<style scoped>
.backup-import-actions {
  margin-top: var(--nexus-gap-2, 8px);
}

.backup-import-actions__report {
  white-space: pre-wrap;
}
</style>
