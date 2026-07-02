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
        <v-btn color="primary" @click="restore()" hide-details>{{ $t('main.backup.restore') }}</v-btn>
      </v-col>
      <v-col cols="12">
        <div class="text-caption text-medium-emphasis">{{ $t('main.backup.restoreHint') }}</div>
      </v-col>
    </v-row>

    <v-row v-if="restoreIsEncryptedEnvelope">
      <v-col cols="12">
        <v-text-field
          v-model="restorePassphrase"
          type="password"
          autocomplete="current-password"
          :label="$t('main.backup.restorePassphrase')"
          hide-details
          @keyup.enter="restore()"
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
import { restoreDatabase } from '@/shared/composables/useBackupOperations'

const props = withDefaults(defineProps<{
  visible?: boolean
}>(), {
  visible: true,
})

const emit = defineEmits<{
  close: []
}>()

const exclude = ref<string[]>(['stats', 'changes'])
const restoreFile = ref<File | null>(null)
const restoreIsEncryptedEnvelope = ref(false)
const restorePassphrase = ref('')
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

const restore = () => {
  if (restoreFile.value && restoreIsEncryptedEnvelope.value) {
    if (!restorePassphrase.value.trim()) return
    void uploadRestore(restoreFile.value, restorePassphrase.value)
    return
  }
  const fileInput = document.createElement('input')
  fileInput.type = 'file'
  fileInput.accept = '.db,.aes'

  fileInput.addEventListener('change', async (event: Event) => {
    const inputElement = event.target as HTMLInputElement
    const dbFile = inputElement.files ? inputElement.files[0] : null

    if (!dbFile) return
    if (await isEncryptedBackupEnvelope(dbFile)) {
      restoreFile.value = dbFile
      restoreIsEncryptedEnvelope.value = true
      restorePassphrase.value = ''
      return
    }
    await uploadRestore(dbFile, '')
  })

  fileInput.click()
}

const uploadRestore = async (dbFile: File, passphrase: string) => {
  const formData = new FormData()
  formData.append('db', dbFile)
  if (passphrase) {
    formData.append('backupPassphrase', passphrase)
  }

  emit('close')
  const uploadMsg = await restoreDatabase(formData)

  if (uploadMsg.success) {
    await new Promise(resolve => setTimeout(resolve, 1000))
    location.reload()
  }
}

const isEncryptedBackupEnvelope = async (file: File) => {
  const magic = new Uint8Array(await file.slice(0, 10).arrayBuffer())
  const expected = [83, 85, 73, 45, 84, 71, 66, 75, 80, 0]
  return expected.every((value, index) => magic[index] === value)
}

const reset = () => {
  exclude.value = ['stats', 'changes']
  backupParams.value = {}
  restoreFile.value = null
  restoreIsEncryptedEnvelope.value = false
  restorePassphrase.value = ''
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
