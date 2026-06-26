<template>
  <v-row class="settings-maintenance" density="comfortable">
    <v-col cols="12">
      <v-card class="settings-maintenance__card" variant="outlined">
        <ConfigDoctor />
      </v-card>
    </v-col>

    <v-col cols="12" md="6">
      <v-card class="settings-maintenance__card" variant="outlined">
        <PanelUpdateCard />
      </v-card>
    </v-col>

    <v-col cols="12" md="6">
      <v-card class="settings-maintenance__card settings-maintenance__backup" variant="outlined">
        <div class="settings-maintenance__heading">
          <v-icon color="primary" icon="mdi-backup-restore" />
          <div>
            <h3>{{ $t('main.backup.title') }}</h3>
          </div>
        </div>
        <v-btn
          variant="tonal"
          prepend-icon="mdi-backup-restore"
          @click="backupModal.visible = true"
        >
          {{ $t('main.backup.title') }}
        </v-btn>
      </v-card>
    </v-col>
  </v-row>

  <Backup
    v-model="backupModal.visible"
    :control="backupModal"
    :visible="backupModal.visible"
  />
</template>

<script setup lang="ts">
import ConfigDoctor from '@/components/settings/ConfigDoctor.vue'
import PanelUpdateCard from '@/components/settings/PanelUpdateCard.vue'
import Backup from '@/layouts/modals/Backup.vue'
import { ref } from 'vue'

interface ModalControl {
  visible: boolean
}

const backupModal = ref<ModalControl>({ visible: false })
</script>

<style scoped>
.settings-maintenance {
  align-items: stretch;
}

.settings-maintenance__card {
  background: var(--nexus-surface-2);
  border-color: var(--nexus-border);
  display: flex;
  flex-direction: column;
  height: 100%;
  min-width: 0;
}

.settings-maintenance__card :deep(.settings-config-doctor),
.settings-maintenance__card :deep(.panel-update) {
  border: 0;
  height: 100%;
}

.settings-maintenance__backup {
  gap: var(--nexus-gap-4);
  padding: var(--nexus-gap-4);
}

.settings-maintenance__heading {
  align-items: flex-start;
  display: flex;
  gap: var(--nexus-gap-3);
  min-width: 0;
}

.settings-maintenance__heading h3,
.settings-maintenance__heading p {
  letter-spacing: 0;
  margin: 0;
  min-width: 0;
  overflow-wrap: anywhere;
}

.settings-maintenance__heading h3 {
  color: var(--nexus-text-primary);
  font-size: 1rem;
  font-weight: 600;
  line-height: 1.4;
}

.settings-maintenance__heading p {
  color: rgba(var(--v-theme-on-surface), 0.72);
  font-size: 0.875rem;
  line-height: 1.4;
  margin-top: 2px;
}
</style>
