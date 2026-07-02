<template>
  <v-row class="settings-maintenance" density="comfortable">
    <v-col cols="12">
      <v-card class="settings-maintenance__card" variant="outlined">
        <ConfigDoctor />
      </v-card>
    </v-col>

    <v-col v-if="maintenanceSlots.length > 0" cols="12" md="6">
      <v-card class="settings-maintenance__card" variant="outlined">
        <div class="settings-maintenance__slot">
          <ComponentSlot name="settings:maintenance" />
        </div>
      </v-card>
    </v-col>

    <v-col cols="12" :md="maintenanceSlots.length > 0 ? 6 : 12">
      <v-card class="settings-maintenance__card settings-maintenance__backup" variant="outlined">
        <div class="settings-maintenance__heading">
          <v-icon color="primary" icon="mdi-backup-restore" />
          <div>
            <h3>{{ $t('main.backup.title') }}</h3>
          </div>
        </div>
        <BackupRestorePanel />
      </v-card>
    </v-col>
  </v-row>
</template>

<script setup lang="ts">
import BackupRestorePanel from '@/components/settings/BackupRestorePanel.vue'
import ConfigDoctor from '@/components/settings/ConfigDoctor.vue'
import ComponentSlot from '@/componentSystem/ComponentSlot.vue'
import { useSlot } from '@/componentSystem/slots'

const maintenanceSlots = useSlot('settings:maintenance')
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

.settings-maintenance__card :deep(.settings-config-doctor) {
  border: 0;
  height: 100%;
}

.settings-maintenance__slot {
  display: grid;
  gap: var(--nexus-gap-3);
  height: 100%;
  min-width: 0;
}

.settings-maintenance__slot :deep(> *) {
  border: 0;
  min-width: 0;
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
