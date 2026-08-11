<template>
  <v-container fluid class="server-protection-page">
    <div class="d-flex flex-wrap align-center justify-space-between ga-3 mb-4">
      <div>
        <h2>{{ t('serverProtection.title') }}</h2>
        <p class="text-medium-emphasis mb-0">{{ t('serverProtection.subtitle') }}</p>
      </div>
      <v-btn prepend-icon="mdi-refresh" variant="tonal" :loading="loading" @click="refreshResources">
        {{ t('serverProtection.refresh') }}
      </v-btn>
    </div>

    <v-alert type="info" variant="tonal" class="mb-3" icon="mdi-eye-outline">
      <strong>{{ t('serverProtection.previewOnly') }}</strong>
      <div>{{ t('serverProtection.previewOnlyHelp') }}</div>
    </v-alert>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
		<v-alert v-if="operations.recoveryRequired" type="error" variant="tonal" class="mb-3" icon="mdi-backup-restore">
			{{ t('serverProtection.recoveryRequired', { count: operations.recoveryRequired }) }}
		</v-alert>

    <v-tabs :model-value="tab" show-arrows @update:model-value="activateTab(String($event))">
      <v-tab value="overview">{{ t('serverProtection.tabs.overview') }}</v-tab>
      <v-tab value="resources">{{ t('serverProtection.tabs.resources') }}</v-tab>
			<v-tab value="surfaces">{{ t('serverProtection.tabs.surfaces') }}</v-tab>
      <v-tab value="profiles">{{ t('serverProtection.tabs.profiles') }}</v-tab>
      <v-tab value="observations">{{ t('serverProtection.tabs.observations') }}</v-tab>
      <v-tab value="firewall">{{ t('serverProtection.tabs.firewall') }}</v-tab>
			<v-tab value="udp">{{ t('serverProtection.tabs.udp') }}</v-tab>
      <v-tab value="localProxy">{{ t('serverProtection.tabs.localProxy') }}</v-tab>
      <v-tab value="interception">{{ t('serverProtection.tabs.interception') }}</v-tab>
      <v-tab value="fronting">{{ t('serverProtection.tabs.fronting') }}</v-tab>
      <v-tab value="diagnostics">{{ t('serverProtection.tabs.diagnostics') }}</v-tab>
			<v-tab value="recovery">{{ t('serverProtection.tabs.recovery') }}</v-tab>
      <v-tab value="settings">{{ t('serverProtection.tabs.settings') }}</v-tab>
    </v-tabs>

    <v-window :model-value="tab" class="mt-4">
      <v-window-item value="overview">
        <div class="protection-metric-grid">
          <v-card variant="outlined"><v-card-text><div class="text-caption">{{ t('serverProtection.support') }}</div><v-chip :color="stateColor(status?.supportState)" size="small">{{ status?.supportState || 'unknown' }}</v-chip></v-card-text></v-card>
          <v-card variant="outlined"><v-card-text><div class="text-caption">{{ t('serverProtection.resources') }}</div><div class="text-h5">{{ status?.counters.resources ?? 0 }}</div></v-card-text></v-card>
          <v-card variant="outlined"><v-card-text><div class="text-caption">{{ t('serverProtection.profiles') }}</div><div class="text-h5">{{ status?.counters.profiles ?? 0 }}</div></v-card-text></v-card>
          <v-card variant="outlined"><v-card-text><div class="text-caption">{{ t('serverProtection.collisions') }}</div><div class="text-h5">{{ status?.counters.collisions ?? 0 }}</div></v-card-text></v-card>
        </div>
        <v-card variant="outlined" class="mt-4">
          <v-card-title>{{ t('serverProtection.readinessBlockers') }}</v-card-title>
          <v-card-text>
            <div v-for="blocker in status?.blockers || []" :key="blocker" class="d-flex ga-2 mb-2">
              <v-icon color="info" icon="mdi-information-outline" size="small" />
              <span>{{ blocker }}</span>
            </div>
          </v-card-text>
        </v-card>
      </v-window-item>

      <v-window-item value="resources">
        <v-alert v-if="!inventory.resources.length" type="warning" variant="tonal">{{ t('serverProtection.emptyResources') }}</v-alert>
        <v-alert v-for="collision in inventory.collisions || []" :key="`${collision.leftResourceId}:${collision.rightResourceId}`" :type="collision.severity === 'error' ? 'error' : 'warning'" variant="tonal" class="mb-2">
          {{ collision.message }}: {{ collision.leftResourceId }} / {{ collision.rightResourceId }} ({{ collision.protocol }}:{{ collision.port }})
        </v-alert>
        <div class="protection-resource-list">
          <v-card v-for="resource in inventory.resources" :key="resource.id" variant="outlined">
            <v-card-title class="d-flex flex-wrap align-center ga-2">
              <span>{{ resource.name || resource.id }}</span>
              <v-chip size="x-small">{{ resource.kind }}</v-chip>
              <v-chip size="x-small" :color="resource.capabilities.known ? 'success' : 'warning'">{{ resource.capabilities.known ? 'known' : 'unsupported' }}</v-chip>
            </v-card-title>
            <v-card-text>
              <div class="text-body-2"><code>{{ resource.protocol }}://{{ resource.listen }}:{{ resource.port }}</code></div>
              <div class="text-caption text-medium-emphasis">{{ resource.owner }} / {{ resource.source }} / TLS: {{ resource.tls }}</div>
              <div v-for="warning in resource.warnings || []" :key="warning" class="text-caption text-warning mt-1">{{ warning }}</div>
            </v-card-text>
            <v-card-actions v-if="!profileByResource.has(resource.id)">
              <v-btn size="small" variant="tonal" prepend-icon="mdi-database-eye-outline" @click="createProfile(resource, 'metadata_only')">{{ t('serverProtection.metadataOnly') }}</v-btn>
              <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-record-circle-outline" :disabled="!resource.capabilities.known" @click="createProfile(resource, 'record_only')">{{ t('serverProtection.recordOnly') }}</v-btn>
            </v-card-actions>
            <v-card-actions v-else><v-chip color="success" size="small">{{ t('serverProtection.profileAttached') }}</v-chip></v-card-actions>
          </v-card>
        </div>
      </v-window-item>

      <v-window-item value="profiles">
        <v-alert v-if="!profiles.length" type="info" variant="tonal">{{ t('serverProtection.emptyProfiles') }}</v-alert>
        <v-card v-for="profile in profiles" :key="profile.id" variant="outlined" class="mb-2">
          <v-card-text class="d-flex flex-wrap align-center ga-3">
            <div class="flex-grow-1">
              <div class="font-weight-medium">{{ profile.resourceId }}</div>
              <div class="text-caption">{{ profile.mode }} / {{ profile.resourceKind }}</div>
            </div>
            <v-chip :color="stateColor(profile.status)" size="small">{{ profile.status }}</v-chip>
            <v-switch :model-value="profile.enabled" hide-details color="primary" density="compact" @update:model-value="setProfileEnabled(profile, Boolean($event))" />
            <v-btn icon="mdi-delete-outline" color="error" variant="text" @click="removeProfile(profile)" />
          </v-card-text>
        </v-card>
      </v-window-item>

			<v-window-item value="surfaces">
				<ContractInspection />
			</v-window-item>

      <v-window-item value="observations">
        <div class="d-flex flex-wrap ga-2 mb-3">
          <v-btn size="small" variant="tonal" prepend-icon="mdi-delete-sweep-outline" @click="clearEvents">{{ t('serverProtection.clearEvents') }}</v-btn>
          <v-btn size="small" variant="tonal" prepend-icon="mdi-format-list-bulleted-square" @click="clearGraylist">{{ t('serverProtection.clearGraylist') }}</v-btn>
        </div>
        <v-alert v-if="!events.length" type="info" variant="tonal" class="mb-3">{{ t('serverProtection.noEvents') }}</v-alert>
        <v-table v-else density="compact">
          <thead><tr><th>{{ t('serverProtection.time') }}</th><th>{{ t('serverProtection.resource') }}</th><th>{{ t('serverProtection.signal') }}</th><th>{{ t('serverProtection.score') }}</th></tr></thead>
          <tbody><tr v-for="event in events" :key="event.id"><td>{{ new Date(event.observedAt * 1000).toLocaleString() }}</td><td>{{ event.resourceId }}</td><td>{{ event.signalKind }}</td><td>{{ event.scoreDelta }}</td></tr></tbody>
        </v-table>
        <v-alert v-if="graylist.length" type="warning" variant="tonal" class="mt-3">{{ t('serverProtection.graylistCount', { count: graylist.length }) }}</v-alert>
      </v-window-item>

      <v-window-item value="firewall">
        <v-card variant="outlined">
          <v-card-title>{{ t('serverProtection.firewallPreview') }}</v-card-title>
          <v-card-text>
            <p>{{ t('serverProtection.firewallPreviewHelp') }}</p>
            <v-alert type="info" variant="tonal" class="mb-3">{{ t('serverProtection.noApply') }}</v-alert>
            <v-expansion-panels class="mb-4">
              <v-expansion-panel>
                <v-expansion-panel-title>{{ t('serverProtection.portAllowlist') }}</v-expansion-panel-title>
                <v-expansion-panel-text>
                  <v-row align="center">
                    <v-col cols="12" sm="2"><v-select v-model="newPort.protocol" :items="['tcp', 'udp']" label="Protocol" /></v-col>
                    <v-col cols="12" sm="2"><v-text-field v-model="newPort.listen" label="Listen" /></v-col>
                    <v-col cols="12" sm="2"><v-text-field v-model.number="newPort.portStart" type="number" min="1" max="65535" :label="t('serverProtection.portStart')" /></v-col>
                    <v-col cols="12" sm="2"><v-text-field v-model.number="newPort.portEnd" type="number" min="1" max="65535" :label="t('serverProtection.portEnd')" /></v-col>
                    <v-col cols="12" sm="3"><v-text-field v-model="newPort.reason" :label="t('serverProtection.reason')" /></v-col>
                    <v-col cols="12" sm="1"><v-btn icon="mdi-plus" color="primary" :title="t('serverProtection.addKeep')" @click="addPortAllowlist" /></v-col>
                  </v-row>
                  <v-chip v-for="item in portAllowlist" :key="item.id" class="me-2 mb-2" closable @click:close="removePortAllowlist(item.id)">
                    {{ item.protocol }} {{ item.listen }}:{{ item.portStart }}<template v-if="item.portEnd !== item.portStart">-{{ item.portEnd }}</template> - {{ item.reason }}
                  </v-chip>
                </v-expansion-panel-text>
              </v-expansion-panel>
              <v-expansion-panel>
                <v-expansion-panel-title>{{ t('serverProtection.ipAllowlist') }}</v-expansion-panel-title>
                <v-expansion-panel-text>
                  <v-row align="center">
                    <v-col cols="12" sm="5"><v-text-field v-model="newIP.ipCidr" :label="t('serverProtection.ipCidr')" /></v-col>
                    <v-col cols="12" sm="5"><v-text-field v-model="newIP.reason" :label="t('serverProtection.reason')" /></v-col>
                    <v-col cols="12" sm="2"><v-btn prepend-icon="mdi-plus" color="primary" variant="tonal" @click="addIPAllowlist">{{ t('serverProtection.addKeep') }}</v-btn></v-col>
                  </v-row>
                  <v-chip v-for="item in ipAllowlist" :key="item.id" class="me-2 mb-2" closable @click:close="removeIPAllowlist(item.id)">{{ item.ipCidr }} - {{ item.reason }}</v-chip>
                </v-expansion-panel-text>
              </v-expansion-panel>
            </v-expansion-panels>
            <v-alert v-if="firewallMessage" type="warning" variant="tonal" class="mb-3">{{ firewallMessage }}</v-alert>
            <v-btn prepend-icon="mdi-file-eye-outline" color="primary" variant="tonal" @click="requestFirewallPreview">{{ t('serverProtection.generatePreview') }}</v-btn>
				<v-btn v-if="firewallPreview" class="ms-2" prepend-icon="mdi-shield-check-outline" color="warning" variant="tonal" :disabled="status?.readiness !== 'apply_beta'" @click="runMockFirewallWorkflow">{{ t('serverProtection.runMockWorkflow') }}</v-btn>
            <div v-if="firewallPreview" class="mt-4">
              <div class="d-flex flex-wrap align-center ga-2 mb-3">
                <v-chip :color="stateColor(firewallPreview.backend)">{{ firewallPreview.backend }}</v-chip>
                <code>{{ firewallPreview.revision }}</code>
              </div>
              <v-alert v-for="warning in firewallPreview.wouldWarn" :key="warning" type="warning" variant="tonal" class="mb-2">{{ warning }}</v-alert>
              <v-list density="compact" lines="two">
                <v-list-subheader>{{ t('serverProtection.wouldKeep') }}</v-list-subheader>
                <v-list-item v-for="item in firewallPreview.wouldKeep" :key="item" prepend-icon="mdi-lock-open-check-outline" :title="item" />
                <v-list-subheader v-if="firewallPreview.wouldOpen.length">{{ t('serverProtection.wouldOpen') }}</v-list-subheader>
                <v-list-item v-for="item in firewallPreview.wouldOpen" :key="item" prepend-icon="mdi-door-open" :title="item" />
              </v-list>
              <v-expansion-panels v-if="firewallPreview.generatedNft" class="mt-3">
                <v-expansion-panel>
                  <v-expansion-panel-title>{{ t('serverProtection.generatedScript') }}</v-expansion-panel-title>
                  <v-expansion-panel-text><pre class="protection-script">{{ firewallPreview.generatedNft }}</pre></v-expansion-panel-text>
                </v-expansion-panel>
              </v-expansion-panels>
            </div>
          </v-card-text>
        </v-card>
      </v-window-item>

      <v-window-item value="diagnostics">
        <v-btn class="mb-3" prepend-icon="mdi-refresh" variant="tonal" @click="refreshDiagnostics">{{ t('serverProtection.refreshDiagnostics') }}</v-btn>
        <v-alert v-for="warning in diagnostics?.warnings || []" :key="warning" type="warning" variant="tonal" class="mb-2">{{ warning }}</v-alert>
        <v-card v-for="check in diagnostics?.checks || []" :key="check.id" variant="outlined" class="mb-2">
          <v-card-text class="d-flex align-center justify-space-between ga-3"><span>{{ check.id }}</span><v-chip :color="stateColor(check.status)" size="small">{{ check.status }}</v-chip><code>{{ check.details }}</code></v-card-text>
        </v-card>
      </v-window-item>

			<v-window-item value="udp"><UDPDirectGuard /></v-window-item>
      <v-window-item value="localProxy"><LocalProxyGuard /></v-window-item>
      <v-window-item value="interception"><InterceptionGuard /></v-window-item>

      <v-window-item value="fronting">
		<FrontingControl />
      </v-window-item>

			<v-window-item value="recovery">
				<v-alert type="info" variant="tonal" class="mb-3">{{ t('serverProtection.recoveryHelp') }}</v-alert>
				<v-alert v-if="!operations.items.length" type="success" variant="tonal">{{ t('serverProtection.noRecoveryOperations') }}</v-alert>
				<v-card v-for="operation in operations.items" :key="operation.operationId" variant="outlined" class="mb-2">
					<v-card-text class="d-flex flex-wrap align-center ga-3">
						<div class="flex-grow-1">
							<div class="font-weight-medium"><code>{{ operation.operationId }}</code></div>
							<div class="text-caption">{{ operation.kind }} · {{ operation.protocol || 'n/a' }}://{{ operation.listen || 'n/a' }}<template v-if="operation.port">:{{ operation.port }}</template></div>
						</div>
						<v-chip :color="stateColor(operation.state)" size="small">{{ operation.state }}</v-chip>
						<v-chip v-if="operation.recoveryBundleAvailable" color="info" size="small">{{ t('serverProtection.bundlePreserved') }}</v-chip>
					<span v-if="operation.recoveryAttempts" class="text-caption">{{ t('serverProtection.recoveryAttempts', { count: operation.recoveryAttempts }) }}</span>
					<v-btn v-if="operation.kind === 'firewall' && operation.state === 'applied'" size="small" variant="tonal" color="warning" @click="rollbackMockOperation(operation.operationId)">{{ t('serverProtection.rollbackMock') }}</v-btn>
					</v-card-text>
				</v-card>
				<v-expansion-panels class="mt-3">
					<v-expansion-panel>
						<v-expansion-panel-title>{{ t('serverProtection.confirmationPhrases') }}</v-expansion-panel-title>
						<v-expansion-panel-text><div v-for="(phrase, name) in operations.confirmationTemplates" :key="name" class="mb-2"><strong>{{ name }}</strong>: <code>{{ phrase }}</code></div></v-expansion-panel-text>
					</v-expansion-panel>
				</v-expansion-panels>
			</v-window-item>

      <v-window-item value="settings">
        <v-card v-if="settings" variant="outlined">
          <v-card-title>{{ t('serverProtection.settings') }}</v-card-title>
          <v-card-text>
            <v-switch v-model="settings.enabled" color="primary" :label="t('serverProtection.enabled')" />
			<v-switch v-model="settings.featureFlags.enable_apply_beta" color="warning" :label="t('serverProtection.enableApplyBeta')" @update:model-value="value => settings && (settings.advancedAcknowledgedAt = value ? Math.floor(Date.now() / 1000) : undefined)" />
			<v-switch v-model="settings.featureFlags.enable_fronting_beta" color="warning" :label="t('serverProtection.enableFrontingBeta')" @update:model-value="value => settings && (settings.advancedAcknowledgedAt = value ? Math.floor(Date.now() / 1000) : undefined)" />
			<v-alert type="warning" variant="tonal" class="mb-3">{{ t('serverProtection.applyBetaWarning') }}</v-alert>
            <v-row>
              <v-col cols="12" md="4"><v-text-field v-model.number="settings.defaultScoreThreshold" type="number" :min="1" :max="settings.maxScore" :label="t('serverProtection.scoreThreshold')" /></v-col>
              <v-col cols="12" md="4"><v-text-field v-model.number="settings.defaultGraylistTtlSeconds" type="number" :min="60" :max="604800" :label="t('serverProtection.graylistTtl')" /></v-col>
              <v-col cols="12" md="4"><v-text-field v-model.number="settings.observationBufferSize" type="number" :min="0" :max="65536" :label="t('serverProtection.bufferSize')" /></v-col>
            </v-row>
            <v-alert type="info" variant="tonal" class="mb-3">{{ t('serverProtection.advancedDisabled') }}</v-alert>
            <v-btn color="primary" prepend-icon="mdi-content-save" @click="saveSettings">{{ t('serverProtection.save') }}</v-btn>
          </v-card-text>
        </v-card>
      </v-window-item>
    </v-window>
  </v-container>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { stateColor, useServerProtection } from '../useServerProtection'
import ContractInspection from './ContractInspection.vue'
import FrontingControl from './FrontingControl.vue'
import LocalProxyGuard from './LocalProxyGuard.vue'
import InterceptionGuard from './InterceptionGuard.vue'
import UDPDirectGuard from './UDPDirectGuard.vue'
import './ServerProtection.scss'

const { t } = useI18n()
const {
  tab, loading, error, status, inventory, profiles, events, graylist, diagnostics, settings, operations,
  firewallMessage, firewallPreview, portAllowlist, ipAllowlist, newPort, newIP, profileByResource, activateTab, refreshResources, createProfile, setProfileEnabled,
  removeProfile, clearEvents, clearGraylist, requestFirewallPreview, saveSettings, refreshDiagnostics,
  addPortAllowlist, removePortAllowlist, addIPAllowlist, removeIPAllowlist,
		runMockFirewallWorkflow, rollbackMockOperation,
} = useServerProtection()
</script>
