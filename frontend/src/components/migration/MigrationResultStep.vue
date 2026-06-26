<template>
  <v-window-item :value="4">
    <v-card class="pa-4" rounded="lg">
      <v-row>
        <v-col cols="12" md="6">
          <div class="text-subtitle-1 mb-2">{{ $t('migrateXui.resultTitle') }}</div>
          <pre class="preview-json">{{ summaryText }}</pre>
        </v-col>
        <v-col cols="12" md="6">
          <div class="text-subtitle-2">{{ $t('migrateXui.backupPath') }}</div>
          <div class="text-body-2 backup-path">{{ report?.backupPath || '-' }}</div>
          <v-row class="mt-3">
            <v-col cols="12" sm="auto">
              <v-btn prepend-icon="mdi-code-json" variant="tonal" :disabled="!report" @click="$emit('download-json')">
                {{ $t('migrateXui.downloadJson') }}
              </v-btn>
            </v-col>
            <v-col cols="12" sm="auto">
              <v-btn prepend-icon="mdi-language-markdown" variant="tonal" :disabled="!report" @click="$emit('download-markdown')">
                {{ $t('migrateXui.downloadMarkdown') }}
              </v-btn>
            </v-col>
            <v-col cols="12" sm="auto">
              <v-btn color="warning" prepend-icon="mdi-database-refresh" :loading="rollbackLoading" :disabled="!report?.backupPath" @click="$emit('rollback')">
                {{ $t('migrateXui.restore') }}
              </v-btn>
            </v-col>
          </v-row>
        </v-col>
        <v-col v-if="rollbackError" cols="12" md="6" offset-md="6">
          <v-alert type="error" variant="tonal" data-testid="migrate-xui-rollback-error" :title="$t('migrateXui.rollbackFailed')">
            {{ rollbackError }}
          </v-alert>
        </v-col>
        <v-col v-if="report?.warnings?.length" cols="12">
          <v-alert type="warning" variant="tonal" :title="$t('migrateXui.warnings')">
            <ul><li v-for="warning in report.warnings" :key="warning">{{ warning }}</li></ul>
          </v-alert>
        </v-col>
        <v-col v-if="hasGeneratedAdmins" cols="12">
          <v-alert type="info" variant="tonal" :title="$t('migrateXui.generatedAdmins')" data-testid="migrate-xui-generated-admins">
            <div class="mb-2">{{ $t('migrateXui.passwordShownOnce') }}</div>
            <div v-if="!generatedAdminsRevealed" class="text-body-2 mb-2" data-testid="migrate-xui-generated-admins-hidden">
              {{ $t('migrateXui.passwordsHidden') }}
            </div>
            <v-row class="mb-2" density="compact">
              <v-col cols="auto">
                <v-btn variant="tonal" :prepend-icon="generatedAdminsRevealed ? 'mdi-eye-off' : 'mdi-eye'" @click="$emit('update:generatedAdminsRevealed', !generatedAdminsRevealed)">
                  {{ generatedAdminsRevealed ? $t('migrateXui.hideGeneratedAdmins') : $t('migrateXui.revealGeneratedAdmins') }}
                </v-btn>
              </v-col>
              <v-col cols="auto">
                <v-btn variant="tonal" prepend-icon="mdi-delete-outline" @click="$emit('clear-generated-admins')">
                  {{ $t('migrateXui.clearGeneratedAdmins') }}
                </v-btn>
              </v-col>
            </v-row>
            <pre v-if="generatedAdminsRevealed" class="preview-json" data-testid="migrate-xui-generated-admins-json">{{ generatedAdminsText }}</pre>
          </v-alert>
        </v-col>
      </v-row>
    </v-card>
  </v-window-item>
</template>

<script setup lang="ts">
defineProps<{
  summaryText: string
  report: any
  rollbackLoading: boolean
  rollbackError: string
  hasGeneratedAdmins: boolean
  generatedAdminsRevealed: boolean
  generatedAdminsText: string
}>()

defineEmits<{
  'download-json': []
  'download-markdown': []
  rollback: []
  'clear-generated-admins': []
  'update:generatedAdminsRevealed': [value: boolean]
}>()
</script>

<style scoped>
.preview-json { margin: 0; max-height: 360px; overflow: auto; white-space: pre-wrap; }
.backup-path { overflow-wrap: anywhere; }
</style>
