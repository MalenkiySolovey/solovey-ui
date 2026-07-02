<template>
  <v-dialog transition="dialog-bottom-transition" width="90%" max-width="500">
    <v-card class="rounded-lg">
      <v-card-title>
        <v-row>
          <v-col>{{ $t('main.backup.title') }}</v-col>
          <v-spacer />
          <v-col cols="auto">
            <v-icon icon="mdi-close" @click="control.visible = false" />
          </v-col>
        </v-row>
      </v-card-title>
      <v-divider />
      <v-card-text>
        <BackupRestorePanel :visible="visible" @close="control.visible = false" />
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
import BackupRestorePanel from '@/components/settings/BackupRestorePanel.vue'

defineProps<{
  control: { visible: boolean }
  visible: boolean
}>()
</script>
