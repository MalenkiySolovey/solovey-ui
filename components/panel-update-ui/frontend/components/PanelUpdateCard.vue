<template>
  <section class="panel-update">
    <div class="panel-update__heading">
      <v-icon color="primary" icon="lucide:cloud-download" />
      <div>
        <h3>{{ $t('update.title') }}</h3>
        <p>{{ $t('update.subtitle') }}</p>
      </div>
    </div>

    <section class="panel-update__components">
      <div class="panel-update__section-title">{{ $t('panelUpdateComponent.panelVersion') }}</div>
      <div class="panel-update__panel-card">
        <div class="panel-update__row">
          <v-btn-toggle v-model="channel" color="primary" density="comfortable" divided mandatory variant="outlined">
            <v-btn value="main">{{ $t('update.channelMain') }}</v-btn>
            <v-btn value="beta">{{ $t('update.channelBeta') }}</v-btn>
          </v-btn-toggle>
          <v-btn color="primary" prepend-icon="lucide:refresh-cw" :loading="checking" variant="tonal" @click="checkUpdates">
            {{ $t('update.check') }}
          </v-btn>
        </div>

        <div class="panel-update__versions">
          <span>{{ $t('update.current') }}: <strong>{{ status?.current || '-' }}</strong></span>
          <v-icon icon="lucide:arrow-right" size="small" />
          <span>{{ $t('update.available') }}: <strong>{{ status?.latest || '-' }}</strong></span>
          <v-chip v-if="status?.latest" :color="status.prerelease ? 'warning' : 'success'" density="compact" label size="small">
            {{ status.prerelease ? $t('update.beta') : $t('update.stable') }}
          </v-chip>
        </div>
      </div>
    </section>

    <v-alert v-if="status?.checkError" density="compact" type="warning" variant="tonal">{{ $t('update.checkFailed') }}</v-alert>
    <v-alert v-else-if="status && !status.updateAvailable && status.latest" density="compact" type="success" variant="tonal">
      {{ $t('update.upToDate') }}
    </v-alert>
    <v-alert v-else-if="status?.updateAvailable && !status.assetAvailable" density="compact" type="warning" variant="tonal">
      {{ $t('update.assetUnavailable') }}
    </v-alert>


    <div v-if="jobActive" class="panel-update__progress">
      <v-progress-linear color="primary" indeterminate rounded />
      <span>{{ $t(`update.stage.${status?.job?.stage || 'idle'}`) }}</span>
    </div>
    <v-alert v-else-if="status?.job?.stage === 'failed'" density="compact" type="error" variant="tonal">
      {{ $t('update.failed') }}<span v-if="status.job.error"> - {{ status.job.error }}</span>
    </v-alert>

    <div class="panel-update__actions">
      <v-btn color="primary" :disabled="!canUpdate" prepend-icon="lucide:arrow-up-circle" @click="openConfirm">
        {{ $t('update.update') }}
      </v-btn>
    </div>

    <v-divider />

    <section class="panel-update__components">
      <div class="panel-update__section-title">{{ $t('panelUpdateComponent.componentCatalog') }}</div>
      <div class="panel-update__catalog-card">
        <span>{{ $t('panelUpdateComponent.binaryProfile') }}: <strong>{{ componentInventory?.binaryProfile || $t('panelUpdateComponent.unknown') }}</strong></span>
        <span>{{ $t('panelUpdateComponent.authority') }}: <strong>{{ $t('panelUpdateComponent.runningBinary') }}</strong></span>
      </div>
      <div class="panel-update__section-title">{{ $t('panelUpdateComponent.installedComponents') }}</div>
      <div v-if="installedComponents.length === 0" class="panel-update__empty">{{ $t('panelUpdateComponent.noneInstalled') }}</div>
      <div
        v-for="component in installedComponents"
        :key="component.id"
        class="panel-update__component-row"
      >
        <div>
          <strong>{{ component.name }}</strong>
          <div class="panel-update__component-meta">{{ componentMeta(component) }}</div>
        </div>
        <div class="panel-update__component-actions">
          <v-chip :color="component.active ? 'success' : 'warning'" density="compact" label size="small">
            {{ component.active ? $t('panelUpdateComponent.enabled') : $t('panelUpdateComponent.disabled') }}
          </v-chip>
          <v-chip v-if="component.locked" color="info" density="compact" label size="small">
            {{ $t('panelUpdateComponent.locked') }}
          </v-chip>
          <v-btn
            v-if="component.active && !component.locked"
            size="small"
            variant="tonal"
            :disabled="!!componentAction"
            :loading="isComponentAction(component, 'disable')"
            @click="setComponentEnabled(component, false)"
          >
            {{ $t('panelUpdateComponent.disable') }}
          </v-btn>
          <v-btn
            v-else-if="!component.locked"
            size="small"
            variant="tonal"
            color="primary"
            :disabled="!!componentAction"
            :loading="isComponentAction(component, 'enable')"
            @click="setComponentEnabled(component, true)"
          >
            {{ $t('panelUpdateComponent.enable') }}
          </v-btn>
          <v-btn
            v-if="component.removable && !component.locked"
            size="small"
            variant="tonal"
            color="error"
            :disabled="!!componentAction"
            :loading="isComponentAction(component, 'remove')"
            @click="openComponentRemoveConfirm(component)"
          >
            {{ $t('panelUpdateComponent.remove') }}
          </v-btn>
        </div>
      </div>

      <div class="panel-update__section-title">{{ $t('panelUpdateComponent.availableForBuild') }}</div>
      <div v-if="availableComponents.length === 0" class="panel-update__empty">{{ $t('panelUpdateComponent.noneAvailable') }}</div>
      <div
        v-for="component in availableComponents"
        :key="component.id"
        class="panel-update__component-row"
      >
        <div>
          <strong>{{ component.name }}</strong>
          <div class="panel-update__component-meta">{{ componentMeta(component) }}</div>
        </div>
        <v-btn
          size="small"
          variant="tonal"
          color="primary"
          :disabled="!!componentAction || !component.installable"
          :loading="isComponentAction(component, 'install')"
          @click="setComponentInstalled(component, true)"
        >
          {{ component.installable ? $t('panelUpdateComponent.install') : $t('panelUpdateComponent.unavailable') }}
        </v-btn>
      </div>

      <div class="panel-update__section-title">{{ $t('panelUpdateComponent.unavailableForProfile') }}</div>
      <div v-if="unavailableComponents.length === 0" class="panel-update__empty">{{ $t('panelUpdateComponent.noneUnavailable') }}</div>
      <div
        v-for="component in unavailableComponents"
        :key="component.id"
        class="panel-update__component-row panel-update__component-row--muted"
      >
        <div>
          <strong>{{ component.name }}</strong>
          <div class="panel-update__component-meta">
            {{ componentMeta(component) }}
            <span v-if="component.unavailableReason"> / {{ component.unavailableReason }}</span>
          </div>
        </div>
        <v-btn disabled size="small" variant="tonal">
          {{ component.compatible ? $t('panelUpdateComponent.notBundled') : $t('panelUpdateComponent.requiresNewerPanel') }}
        </v-btn>
      </div>
    </section>

    <v-dialog v-model="confirm" max-width="460">
      <v-card>
        <v-card-title>{{ $t('update.confirmTitle') }}</v-card-title>
        <v-card-text>
          <v-alert class="mb-3" density="compact" type="warning" variant="tonal">{{ $t('update.restartWarning') }}</v-alert>
          <p class="mb-3">{{ $t('update.confirmTo', { version: status?.latest }) }}</p>
          <v-text-field v-model="password" autocomplete="current-password" density="comfortable" :label="$t('update.password')" type="password" variant="outlined" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirm = false">{{ $t('cancel') }}</v-btn>
          <v-btn color="primary" :disabled="!password" :loading="applying" @click="runUpdate">{{ $t('update.update') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="componentRemoveConfirm" max-width="460">
      <v-card>
        <v-card-title>{{ $t('panelUpdateComponent.removeTitle') }}</v-card-title>
        <v-card-text>
          <v-alert class="mb-3" density="compact" type="warning" variant="tonal">
            {{ $t('panelUpdateComponent.removeWarning') }}
          </v-alert>
          <p class="mb-3">{{ $t('panelUpdateComponent.removeConfirm', { name: componentRemoveTarget?.name }) }}</p>
          <v-text-field
            v-model="componentRemovePassword"
            autocomplete="current-password"
            density="comfortable"
            :label="$t('panelUpdateComponent.password')"
            type="password"
            variant="outlined"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="componentRemoveConfirm = false">{{ $t('cancel') }}</v-btn>
          <v-btn color="error" :disabled="!componentRemovePassword" :loading="!!componentAction" @click="removeComponent">{{ $t('panelUpdateComponent.remove') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>

<script lang="ts" setup>
import { usePanelUpdate } from '../composables/usePanelUpdate'
import type { ComponentCatalogStatus } from '../types'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const {
  applying,
  availableComponents,
  canUpdate,
  channel,
  checkUpdates,
  checking,
  componentAction,
  componentInventory,
  componentRemoveConfirm,
  componentRemovePassword,
  componentRemoveTarget,
  confirm,
  jobActive,
  openConfirm,
  openComponentRemoveConfirm,
  password,
  removeComponent,
  runUpdate,
  installedComponents,
  setComponentEnabled,
  setComponentInstalled,
  status,
  unavailableComponents,
} = usePanelUpdate()

const isComponentAction = (component: ComponentCatalogStatus, action: string) => componentAction.value === `${component.id}:${action}`
const componentMeta = (component: ComponentCatalogStatus) => {
  const parts = [component.id]
  if (component.version) parts.push(`${t('panelUpdateComponent.current')} ${component.version}`)
  if (component.latestVersion && component.latestVersion !== component.version) parts.push(`${t('panelUpdateComponent.latest')} ${component.latestVersion}`)
  if (component.requiredPanelVersion || component.since) parts.push(`${t('panelUpdateComponent.requiresPanel')} ${component.requiredPanelVersion || component.since}`)
  parts.push(component.delivery)
  if (!component.compatible) parts.push(t('panelUpdateComponent.incompatible'))
  if (component.locked && component.lockedReason) parts.push(component.lockedReason)
  else if (!component.installable && !component.removable && !component.availableInBinary) parts.push(t('panelUpdateComponent.releaseOnly'))
  return parts.join(' / ')
}
</script>

<style scoped>
.panel-update { border: 1px solid rgba(var(--v-theme-on-surface), 0.12); border-radius: 8px; display: grid; gap: 14px; min-width: 0; padding: 16px; }
.panel-update__heading { align-items: flex-start; display: flex; gap: 12px; min-width: 0; }
.panel-update__heading h3 { font-size: 1rem; font-weight: 600; margin: 0; }
.panel-update__heading p { color: rgba(var(--v-theme-on-surface), 0.72); font-size: 0.875rem; margin: 2px 0 0; }
.panel-update__row, .panel-update__versions { align-items: center; display: flex; flex-wrap: wrap; gap: 12px; }
.panel-update__row { justify-content: space-between; }
.panel-update__versions { font-size: 0.9rem; }
.panel-update__progress { display: grid; gap: 6px; }
.panel-update__actions { display: flex; justify-content: flex-end; }
.panel-update__components { display: grid; gap: 10px; min-width: 0; }
.panel-update__panel-card { border: 1px solid rgba(var(--v-theme-on-surface), 0.10); border-radius: 8px; display: grid; gap: 12px; padding: 10px; }
.panel-update__catalog-card { align-items: center; border: 1px solid rgba(var(--v-theme-on-surface), 0.10); border-radius: 8px; display: flex; flex-wrap: wrap; gap: 10px 14px; padding: 10px; }
.panel-update__section-title { color: var(--nexus-text-primary); font-size: 0.88rem; font-weight: 700; margin-top: 4px; }
.panel-update__empty { color: rgba(var(--v-theme-on-surface), 0.62); font-size: 0.82rem; }
.panel-update__component-row { align-items: center; border: 1px solid rgba(var(--v-theme-on-surface), 0.10); border-radius: 8px; display: flex; gap: 12px; justify-content: space-between; min-width: 0; padding: 10px; }
.panel-update__component-row--muted { opacity: 0.72; }
.panel-update__component-meta { color: rgba(var(--v-theme-on-surface), 0.62); font-size: 0.78rem; margin-top: 2px; word-break: break-word; }
.panel-update__component-actions { align-items: center; display: flex; flex-wrap: wrap; gap: 8px; justify-content: flex-end; }
</style>
