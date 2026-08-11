<template>
  <v-container class="operations-page">
    <header class="operations-header mb-5">
      <div>
        <h1 class="text-h4">{{ t('operations.title') }}</h1>
        <p class="mb-0">{{ t('operations.subtitle') }}</p>
      </div>
      <v-btn variant="outlined" :loading="loading" @click="loadAll">{{ t('operations.refresh') }}</v-btn>
    </header>

    <nav class="section-nav mb-5" :aria-label="t('operations.sectionNavigation')">
      <a href="#summary">{{ t('operations.summary') }}</a>
      <a href="#updates">{{ t('operations.updates') }}</a>
      <a href="#resources">{{ t('operations.resources') }}</a>
      <a href="#data">{{ t('operations.dataLifecycle') }}</a>
    </nav>

    <v-alert type="warning" variant="tonal" class="mb-4" role="status">
      <strong>{{ t('operations.evidenceBoundary') }}</strong>
      <div>{{ t('operations.evidenceBoundaryDetail') }}</div>
    </v-alert>
    <v-alert v-if="errorMessage" type="error" variant="tonal" class="mb-4" role="alert">
      {{ errorMessage }}
    </v-alert>
    <v-progress-linear v-if="loading" indeterminate :aria-label="t('operations.loading')" />

    <template v-else>
      <section id="summary" aria-labelledby="summary-heading">
        <h2 id="summary-heading" class="text-h5 mb-3">{{ t('operations.summary') }}</h2>
        <v-row>
          <v-col cols="12" md="6" xl="3">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.security') }}</v-card-title>
              <v-card-text>
                <dl class="facts-grid one-column">
                  <div><dt>{{ t('operations.authState') }}</dt><dd>{{ safe(security?.authState) }}</dd></div>
                  <div><dt>{{ t('operations.assurance') }}</dt><dd>{{ safe(security?.assurance) }}</dd></div>
                  <div><dt>{{ t('operations.mfa') }}</dt><dd>{{ yesNo(security?.mfa.enabled ?? false) }}</dd></div>
                  <div><dt>{{ t('operations.sessions') }}</dt><dd>{{ sessions?.items.length ?? 0 }}</dd></div>
                  <div><dt>{{ t('operations.clientIdentity') }}</dt><dd>{{ safe(security?.clientIdentity.provenance) }}</dd></div>
                </dl>
                <v-btn class="mt-3" variant="text" to="/security">{{ t('operations.openSecurity') }}</v-btn>
              </v-card-text>
            </v-card>
          </v-col>
          <v-col cols="12" md="6" xl="3">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.sshRecovery') }}</v-card-title>
              <v-card-text>
                <dl class="facts-grid one-column">
                  <div><dt>{{ t('operations.state') }}</dt><dd>{{ safe(ssh?.state) }}</dd></div>
                  <div><dt>{{ t('operations.freshness') }}</dt><dd>{{ ssh?.fresh ? t('operations.fresh') : t('operations.staleUnknown') }}</dd></div>
                  <div><dt>{{ t('operations.serviceState') }}</dt><dd>{{ safe(ssh?.posture?.service.state) }}</dd></div>
                  <div><dt>{{ t('operations.revision') }}</dt><dd class="digest">{{ safe(ssh?.posture?.semanticRevision) }}</dd></div>
                </dl>
                <v-btn class="mt-3" variant="text" to="/ssh-management">{{ t('operations.openSSH') }}</v-btn>
              </v-card-text>
            </v-card>
          </v-col>
          <v-col cols="12" md="6" xl="3">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.deployment') }}</v-card-title>
              <v-card-text>
                <dl class="facts-grid one-column">
                  <div><dt>{{ t('operations.state') }}</dt><dd>{{ safe(deployment?.state) }}</dd></div>
                  <div><dt>{{ t('operations.desired') }}</dt><dd>{{ safe(deployment?.desiredProfile) }}</dd></div>
                  <div><dt>{{ t('operations.installed') }}</dt><dd>{{ safe(deployment?.installedProfile) }}</dd></div>
                  <div><dt>{{ t('operations.active') }}</dt><dd>{{ safe(deployment?.activeProfile) }}</dd></div>
                  <div><dt>{{ t('operations.evidence') }}</dt><dd>{{ safe(deployment?.evidenceStatus) }}</dd></div>
                </dl>
                <v-btn class="mt-3" variant="text" to="/deployment">{{ t('operations.openDeployment') }}</v-btn>
              </v-card-text>
            </v-card>
          </v-col>
          <v-col cols="12" md="6" xl="3">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.acceptance') }}</v-card-title>
              <v-card-text>
                <dl class="facts-grid one-column">
                  <div><dt>{{ t('operations.normalCI') }}</dt><dd>{{ safe(summary?.evidence.normalCI) }}</dd></div>
                  <div><dt>{{ t('operations.liveTested') }}</dt><dd>{{ safe(summary?.evidence.live) }}</dd></div>
                  <div><dt>{{ t('operations.accepted') }}</dt><dd>{{ yesNo(summary?.evidence.accepted ?? false) }}</dd></div>
                  <div><dt>{{ t('operations.generatedAt') }}</dt><dd>{{ formatTime(summary?.generatedAt) }}</dd></div>
                </dl>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>
      </section>

      <section id="updates" class="section-block" aria-labelledby="updates-heading">
        <h2 id="updates-heading" class="text-h5 mb-3">{{ t('operations.updates') }}</h2>
        <v-row>
          <v-col cols="12" lg="7">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.signedReleasePosture') }}</v-card-title>
              <v-card-text>
                <v-alert :type="updateFresh ? 'info' : 'warning'" variant="tonal" class="mb-4">
                  {{ updateFresh ? t('operations.metadataFresh') : t('operations.metadataNotFresh') }}
                </v-alert>
                <dl class="facts-grid">
                  <div><dt>{{ t('operations.state') }}</dt><dd>{{ safe(update?.state) }}</dd></div>
                  <div><dt>{{ t('operations.signing') }}</dt><dd>{{ safe(update?.signingStatus) }}</dd></div>
                  <div><dt>{{ t('operations.desiredChannel') }}</dt><dd>{{ safe(update?.desired.channel) }}</dd></div>
                  <div><dt>{{ t('operations.releaseId') }}</dt><dd>{{ safe(update?.selected?.releaseId) }}</dd></div>
                  <div><dt>{{ t('operations.selectedVersion') }}</dt><dd>{{ safe(update?.selected?.version) }}</dd></div>
                  <div><dt>{{ t('operations.selectedSequence') }}</dt><dd>{{ update?.selected?.sequence ?? t('operations.unknown') }}</dd></div>
                  <div><dt>{{ t('operations.actualVersion') }}</dt><dd>{{ safe(update?.actual.version) }}</dd></div>
                  <div><dt>{{ t('operations.runtimeOwnership') }}</dt><dd>{{ safe(update?.actual.mode) }}</dd></div>
                  <div><dt>{{ t('operations.reboot') }}</dt><dd>{{ safe(update?.capabilities.reboot) }}</dd></div>
                  <div class="wide"><dt>{{ t('operations.manifestDigest') }}</dt><dd class="digest">{{ safe(update?.selected?.manifestDigest) }}</dd></div>
                  <div class="wide"><dt>{{ t('operations.signingKey') }}</dt><dd class="digest">{{ safe(update?.selected?.signingKeyId) }}</dd></div>
                  <div class="wide"><dt>{{ t('operations.capabilityRevision') }}</dt><dd class="digest">{{ safe(update?.capabilities.revision) }}</dd></div>
                  <div class="wide"><dt>{{ t('operations.reasons') }}</dt><dd>{{ reasons(update?.reasonCodes) }}</dd></div>
                </dl>
                <div class="d-flex flex-wrap ga-2 mt-4">
                  <v-select
                    v-model="updateChannel"
                    :items="updateChannels"
                    :label="t('operations.channel')"
                    class="channel-select"
                    hide-details
                  />
                  <v-btn color="primary" :loading="updateWorking" @click="runUpdateCheck">
                    {{ t('operations.checkSignedRelease') }}
                  </v-btn>
                </div>
              </v-card-text>
            </v-card>
          </v-col>
          <v-col cols="12" lg="5">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.updateCapabilities') }}</v-card-title>
              <v-card-text>
                <dl class="facts-grid one-column">
                  <div><dt>{{ t('operations.download') }}</dt><dd>{{ safe(update?.capabilities.download) }}</dd></div>
                  <div><dt>{{ t('operations.preflight') }}</dt><dd>{{ safe(update?.capabilities.prepare) }}</dd></div>
                  <div><dt>{{ t('operations.activation') }}</dt><dd>{{ safe(update?.capabilities.activate) }}</dd></div>
                  <div><dt>{{ t('operations.rollback') }}</dt><dd>{{ safe(update?.capabilities.rollback) }}</dd></div>
                  <div><dt>{{ t('operations.osUpdates') }}</dt><dd>{{ safe(update?.capabilities.osUpdates) }}</dd></div>
                </dl>
                <v-alert v-if="update?.actual.mode === 'docker-operator-managed'" type="info" variant="tonal" class="mt-4">
                  {{ t('operations.dockerOperatorManaged') }}
                </v-alert>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <v-card v-if="updateCheck" class="mt-4">
          <v-card-title>{{ t('operations.updateReview') }}</v-card-title>
          <v-card-text>
            <dl class="facts-grid">
              <div><dt>{{ t('operations.state') }}</dt><dd>{{ safe(updateCheck.state) }}</dd></div>
              <div><dt>{{ t('operations.releaseId') }}</dt><dd>{{ safe(updateCheck.releaseId) }}</dd></div>
              <div><dt>{{ t('operations.availableVersion') }}</dt><dd>{{ safe(updateCheck.version) }}</dd></div>
              <div><dt>{{ t('operations.selectedSequence') }}</dt><dd>{{ updateCheck.sequence ?? t('operations.unknown') }}</dd></div>
              <div><dt>{{ t('operations.restart') }}</dt><dd>{{ safe(updateCheck.restartClass) }}</dd></div>
              <div><dt>{{ t('operations.reboot') }}</dt><dd>{{ safe(updateCheck.rebootClass) }}</dd></div>
              <div><dt>{{ t('operations.rollback') }}</dt><dd>{{ safe(updateCheck.rollbackClass) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.manifestDigest') }}</dt><dd class="digest">{{ safe(updateCheck.manifestDigest) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.artifactSetDigest') }}</dt><dd class="digest">{{ safe(updateCheck.artifactSetDigest) }}</dd></div>
            </dl>
            <template v-if="updateCheck.updateAvailable && updateCheck.sequence && updateCheck.manifestDigest">
              <v-alert type="warning" variant="tonal" class="my-4">{{ t('operations.preflightWarning') }}</v-alert>
              <v-checkbox v-model="updateAcknowledged" :label="t('operations.acknowledgeUpdate')" />
              <v-text-field
                v-model="updateTypedConfirmation"
                :label="t('operations.typedConfirmation')"
                :hint="prepareConfirmation"
                persistent-hint
                autocomplete="off"
              />
              <v-text-field
                v-model="updateCredential"
                :label="t('operations.securityCredential')"
                type="password"
                autocomplete="current-password"
              />
              <v-btn color="warning" :disabled="!canPrepareUpdate" :loading="updateWorking" @click="runUpdatePrepare">
                {{ t('operations.prepareAndRehearse') }}
              </v-btn>
            </template>
            <v-alert v-else type="info" variant="tonal" class="mt-4">{{ reasons(updateCheck.reasonCodes) }}</v-alert>
          </v-card-text>
        </v-card>

        <v-card v-if="updateOperation" class="mt-4">
          <v-card-title>
            <h3 ref="updateOperationHeading" class="text-h6" tabindex="-1">{{ t('operations.updateOperation') }}</h3>
          </v-card-title>
          <v-card-text>
            <dl class="facts-grid">
              <div><dt>ID</dt><dd class="digest">{{ safe(updateOperation.operationId) }}</dd></div>
              <div><dt>{{ t('operations.releaseId') }}</dt><dd>{{ safe(updateOperation.releaseId) }}</dd></div>
              <div><dt>{{ t('operations.state') }}</dt><dd>{{ safe(updateOperation.state) }}</dd></div>
              <div><dt>{{ t('operations.revision') }}</dt><dd>{{ updateOperation.revision }}</dd></div>
              <div><dt>{{ t('operations.rollbackReadiness') }}</dt><dd>{{ yesNo(updateOperation.rollbackAvailable) }}</dd></div>
              <div><dt>{{ t('operations.progress') }}</dt><dd>{{ formatBytes(updateOperation.bytesCompleted) }} / {{ formatBytes(updateOperation.bytesTotal) }}</dd></div>
              <div><dt>{{ t('operations.backupReference') }}</dt><dd class="digest">{{ safe(updateOperation.backupRef) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.artifactSetDigest') }}</dt><dd class="digest">{{ safe(updateOperation.artifactSetDigest) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.reason') }}</dt><dd>{{ safe(updateOperation.reasonCode) }}</dd></div>
            </dl>
            <v-alert v-if="updateOperation.restoredUntrusted || updateOperation.state === 'RECOVERY_REQUIRED'" type="error" variant="tonal" class="my-4">
              {{ t('operations.recoveryRequired') }}
            </v-alert>
            <template v-if="canShowUpdateActions">
              <v-text-field
                v-model="operationConfirmation"
                :label="t('operations.typedConfirmation')"
                :hint="operationConfirmationHint"
                persistent-hint
                autocomplete="off"
                class="mt-4"
              />
              <v-text-field
                v-model="operationCredential"
                :label="t('operations.securityCredential')"
                type="password"
                autocomplete="current-password"
              />
              <div class="d-flex flex-wrap ga-2">
                <v-btn
                  v-if="updateOperation.state === 'PREPARED'"
                  color="warning"
                  :disabled="operationConfirmation !== activateConfirmation || !operationCredential"
                  :loading="updateWorking"
                  @click="runUpdateOperation('ACTIVATE')"
                >
                  {{ t('operations.activate') }}
                </v-btn>
                <v-btn
                  v-if="updateOperation.rollbackAvailable"
                  color="error"
                  variant="outlined"
                  :disabled="operationConfirmation !== rollbackConfirmation || !operationCredential"
                  :loading="updateWorking"
                  @click="runUpdateOperation('ROLLBACK')"
                >
                  {{ t('operations.rollback') }}
                </v-btn>
              </div>
            </template>
            <h4 class="text-subtitle-1 mt-5 mb-2">{{ t('operations.timeline') }}</h4>
            <div class="table-scroll">
              <table class="status-table">
                <thead><tr><th>#</th><th>{{ t('operations.state') }}</th><th>{{ t('operations.event') }}</th><th>{{ t('operations.reason') }}</th><th>{{ t('operations.observed') }}</th></tr></thead>
                <tbody>
                  <tr v-for="entry in updateTimeline" :key="entry.sequence">
                    <td>{{ entry.sequence }}</td><td>{{ safe(entry.state) }}</td><td>{{ safe(entry.event) }}</td>
                    <td>{{ safe(entry.reasonCode) }}</td><td>{{ formatTime(entry.createdAt) }}</td>
                  </tr>
                  <tr v-if="updateTimeline.length === 0"><td colspan="5">{{ t('operations.noTimeline') }}</td></tr>
                </tbody>
              </table>
            </div>
          </v-card-text>
        </v-card>
      </section>

      <section id="resources" class="section-block" aria-labelledby="resources-heading">
        <h2 id="resources-heading" class="text-h5 mb-3">{{ t('operations.resources') }}</h2>
        <v-card>
          <v-card-title>{{ t('operations.resourcePressure') }}</v-card-title>
          <v-card-text>
            <v-alert :type="pressure?.state === 'NORMAL' ? 'info' : 'warning'" variant="tonal" class="mb-4">
              {{ t('operations.pressureStateSummary', { state: safe(pressure?.state), previous: safe(pressure?.previousState) }) }}
            </v-alert>
            <dl class="facts-grid mb-4">
              <div><dt>{{ t('operations.observed') }}</dt><dd>{{ formatTime(pressure?.observedAt) }}</dd></div>
              <div><dt>{{ t('operations.freshUntil') }}</dt><dd>{{ formatTime(pressure?.freshUntil) }}</dd></div>
              <div><dt>{{ t('operations.sampleInterval') }}</dt><dd>{{ pressure?.desired.sampleIntervalSeconds ?? 0 }}s</dd></div>
              <div><dt>{{ t('operations.recoveryWindow') }}</dt><dd>{{ pressure?.desired.recoveryWindowSeconds ?? 0 }}s</dd></div>
              <div><dt>{{ t('operations.revision') }}</dt><dd>{{ pressure?.revision ?? 0 }}</dd></div>
              <div class="wide"><dt>{{ t('operations.observationDigest') }}</dt><dd class="digest">{{ safe(pressure?.actual.observationDigest) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.reasons') }}</dt><dd>{{ reasons(pressure?.reasonCodes) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.limitations') }}</dt><dd>{{ reasons(pressure?.limitations) }}</dd></div>
            </dl>
            <h3 class="text-subtitle-1 mb-2">{{ t('operations.selectedSignals') }}</h3>
            <div class="table-scroll mb-5">
              <table class="status-table">
                <thead><tr><th>{{ t('operations.source') }}</th><th>{{ t('operations.state') }}</th><th>{{ t('operations.value') }}</th><th>{{ t('operations.observed') }}</th><th>{{ t('operations.freshUntil') }}</th><th>{{ t('operations.reason') }}</th></tr></thead>
                <tbody>
                  <tr v-for="signal in pressure?.selected.signals ?? []" :key="signal.id">
                    <td>{{ signal.id }}</td><td>{{ safe(signal.status) }}</td><td>{{ formatSignal(signal.value, signal.unit) }}</td>
                    <td>{{ formatTime(signal.observedAt) }}</td><td>{{ formatTime(signal.expiresAt) }}</td><td>{{ safe(signal.reasonCode) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <h3 class="text-subtitle-1 mb-2">{{ t('operations.thresholds') }}</h3>
            <div class="table-scroll mb-5">
              <table class="status-table">
                <thead><tr><th>{{ t('operations.source') }}</th><th>{{ t('operations.direction') }}</th><th>{{ t('operations.warningLevel') }}</th><th>{{ t('operations.constrainedLevel') }}</th><th>{{ t('operations.criticalLevel') }}</th><th>{{ t('operations.required') }}</th></tr></thead>
                <tbody>
                  <tr v-for="threshold in pressure?.desired.thresholds ?? []" :key="threshold.id">
                    <td>{{ threshold.id }}</td><td>{{ threshold.direction }}</td><td>{{ threshold.warning }}</td>
                    <td>{{ threshold.constrained }}</td><td>{{ threshold.critical }}</td><td>{{ yesNo(threshold.required) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <h3 class="text-subtitle-1 mb-2">{{ t('operations.admissionEffects') }}</h3>
            <div class="table-scroll">
              <table class="status-table">
                <thead><tr><th>{{ t('operations.operationClass') }}</th><th>{{ t('operations.allowed') }}</th><th>{{ t('operations.reason') }}</th><th>{{ t('operations.retryAfter') }}</th></tr></thead>
                <tbody>
                  <tr v-for="[name, admission] in admissionRows" :key="name">
                    <td>{{ name }}</td><td>{{ yesNo(admission.allowed) }}</td><td>{{ safe(admission.reasonCode) }}</td><td>{{ admission.retryAfterSeconds ?? 0 }}s</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </v-card-text>
        </v-card>
      </section>

      <section id="data" class="section-block" aria-labelledby="data-heading">
        <h2 id="data-heading" class="text-h5 mb-3">{{ t('operations.dataLifecycle') }}</h2>
        <v-row>
          <v-col cols="12" lg="6">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.sqliteSafety') }}</v-card-title>
              <v-card-text>
                <dl class="facts-grid">
                  <div><dt>{{ t('operations.provider') }}</dt><dd>{{ safe(sqlite?.provider) }}</dd></div>
                  <div><dt>{{ t('operations.moduleVersion') }}</dt><dd>{{ safe(sqlite?.moduleVersion) }}</dd></div>
                  <div><dt>{{ t('operations.runtimeVersion') }}</dt><dd>{{ safe(sqlite?.runtimeVersion) }}</dd></div>
                  <div><dt>{{ t('operations.journalMode') }}</dt><dd>{{ safe(sqlite?.journalMode) }}</dd></div>
                  <div><dt>{{ t('operations.walCapable') }}</dt><dd>{{ yesNo(sqlite?.walCapable ?? false) }}</dd></div>
                  <div><dt>{{ t('operations.walResetSafe') }}</dt><dd>{{ yesNo(sqlite?.walResetSafe ?? false) }}</dd></div>
                  <div class="wide"><dt>{{ t('operations.sourceId') }}</dt><dd class="digest">{{ safe(sqlite?.sourceId) }}</dd></div>
                  <div class="wide"><dt>{{ t('operations.revision') }}</dt><dd class="digest">{{ safe(sqlite?.revision) }}</dd></div>
                </dl>
              </v-card-text>
            </v-card>
          </v-col>
          <v-col cols="12" lg="6">
            <v-card class="h-100">
              <v-card-title>{{ t('operations.migrations') }}</v-card-title>
              <v-card-text>
                <dl class="facts-grid one-column">
                  <div><dt>{{ t('operations.state') }}</dt><dd>{{ safe(migrations?.state) }}</dd></div>
                  <div><dt>{{ t('operations.targetCoreSchema') }}</dt><dd>{{ safe(summary?.migrations.targetCoreSchema) }}</dd></div>
                  <div><dt>{{ t('operations.journalRows') }}</dt><dd>{{ summary?.migrations.journalRows ?? 0 }}</dd></div>
                  <div><dt>{{ t('operations.truncated') }}</dt><dd>{{ yesNo(migrations?.truncated ?? false) }}</dd></div>
                </dl>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <v-card class="mt-4">
          <v-card-title>{{ t('operations.installedOwners') }}</v-card-title>
          <v-card-text class="table-scroll">
            <table class="status-table">
              <thead><tr><th>{{ t('operations.owner') }}</th><th>{{ t('operations.installed') }}</th><th>{{ t('operations.available') }}</th><th>{{ t('operations.enabled') }}</th><th>{{ t('operations.backup') }}</th><th>{{ t('operations.restore') }}</th><th>{{ t('operations.dropData') }}</th><th>{{ t('operations.review') }}</th></tr></thead>
              <tbody>
                <tr v-for="owner in owners?.items ?? []" :key="owner.id">
                  <td>{{ owner.id }}</td><td>{{ yesNo(owner.installed) }}</td><td>{{ yesNo(owner.available) }}</td>
                  <td>{{ safe(owner.enabled) }}</td><td>{{ safe(owner.backup) }}</td><td>{{ safe(owner.restore) }}</td><td>{{ safe(owner.dropData) }}</td>
                  <td><v-btn v-if="owner.id !== 'core'" size="small" variant="text" @click="runDropPreview(owner.id)">{{ t('operations.preview') }}</v-btn></td>
                </tr>
              </tbody>
            </table>
          </v-card-text>
        </v-card>

        <v-card v-if="dropPreview" class="mt-4">
          <v-card-title>
            <h3 ref="dropPreviewHeading" class="text-h6" tabindex="-1">{{ t('operations.dropReview', { owner: dropPreview.ownerId }) }}</h3>
          </v-card-title>
          <v-card-text>
            <v-alert :type="dropPreview.blockers.length === 0 ? 'warning' : 'error'" variant="tonal" class="mb-4">
              {{ dropPreview.blockers.length === 0 ? t('operations.dropIrreversible') : reasons(dropPreview.blockers) }}
            </v-alert>
            <dl class="facts-grid mb-4">
              <div><dt>{{ t('operations.enabled') }}</dt><dd>{{ yesNo(dropPreview.enabled) }}</dd></div>
              <div><dt>{{ t('operations.externalAuthority') }}</dt><dd>{{ safe(dropPreview.externalAuthority) }}</dd></div>
              <div><dt>{{ t('operations.activeLeases') }}</dt><dd>{{ dropPreview.leaseCount }}</dd></div>
              <div><dt>{{ t('operations.postcondition') }}</dt><dd>{{ safe(dropPreview.postcondition) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.previewRevision') }}</dt><dd class="digest">{{ dropPreview.revision }}</dd></div>
            </dl>
            <div class="table-scroll mb-4">
              <table class="status-table">
                <thead><tr><th>{{ t('operations.resource') }}</th><th>{{ t('operations.kind') }}</th><th>{{ t('operations.rows') }}</th><th>{{ t('operations.currentState') }}</th><th>{{ t('operations.classification') }}</th></tr></thead>
                <tbody>
                  <tr v-for="resource in dropPreview.resources" :key="resource.id">
                    <td>{{ resource.id }}</td><td>{{ resource.kind }}</td><td>{{ resource.rows ?? 0 }}</td>
                    <td>{{ safe(resource.terminalState) }}</td><td>{{ safe(resource.class) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <template v-if="dropPreview.blockers.length === 0">
              <v-checkbox v-model="dropBackupAcknowledged" :label="t('operations.acknowledgeBackup')" />
              <v-checkbox v-model="dropAcknowledged" :label="t('operations.acknowledgeDrop')" />
              <v-text-field v-model="dropTypedConfirmation" :label="t('operations.typedConfirmation')" :hint="requiredDropConfirmation" persistent-hint autocomplete="off" />
              <v-text-field v-model="dropCredential" :label="t('operations.securityCredential')" type="password" autocomplete="current-password" />
              <v-btn color="error" :disabled="!canExecuteDrop" :loading="dataWorking" @click="runDropExecute">{{ t('operations.executeDrop') }}</v-btn>
            </template>
          </v-card-text>
        </v-card>

        <v-row class="mt-1">
          <v-col cols="12" lg="5">
            <v-card id="backup" class="h-100">
              <v-card-title>{{ t('operations.backup') }}</v-card-title>
              <v-card-text>
                <p>{{ t('operations.backupDescription') }}</p>
                <dl class="facts-grid one-column mb-4">
                  <div><dt>{{ t('operations.state') }}</dt><dd>{{ safe(summary?.backup.state) }}</dd></div>
                  <div><dt>{{ t('operations.contents') }}</dt><dd>{{ t('operations.installedOwnerContents') }}</dd></div>
                  <div><dt>{{ t('operations.bounds') }}</dt><dd>512 MiB</dd></div>
                </dl>
                <v-btn color="primary" href="api/getdb?exclude=stats%2Cchanges">{{ t('operations.downloadBackup') }}</v-btn>
              </v-card-text>
            </v-card>
          </v-col>
          <v-col cols="12" lg="7">
            <v-card id="restore" class="h-100">
              <v-card-title>{{ t('operations.restoreRehearsal') }}</v-card-title>
              <v-card-text>
                <p>{{ t('operations.restoreDescription') }}</p>
                <label class="file-label" for="restore-file">{{ t('operations.restoreFile') }}</label>
                <input id="restore-file" class="file-input" type="file" accept=".db,.aes" @change="selectRestoreFile">
                <div class="text-caption mt-1">{{ restoreFile?.name || t('operations.noFileSelected') }}</div>
                <v-text-field
                  v-model="restorePassphrase"
                  :label="t('operations.restorePassphrase')"
                  type="password"
                  autocomplete="current-password"
                  class="mt-3"
                />
                <v-btn color="primary" :disabled="!canRehearseRestore" :loading="dataWorking" @click="runRestoreRehearsal">
                  {{ t('operations.runRehearsal') }}
                </v-btn>
              </v-card-text>
            </v-card>
          </v-col>
        </v-row>

        <v-card v-if="restoreRehearsal" class="mt-4">
          <v-card-title>
            <h3 ref="restoreHeading" class="text-h6" tabindex="-1">{{ t('operations.restoreReview') }}</h3>
          </v-card-title>
          <v-card-text>
            <v-alert :type="restoreRehearsal.possible ? 'warning' : 'error'" variant="tonal" class="mb-4">
              {{ restoreRehearsal.possible ? t('operations.restorePossibleWarning') : reasons(restoreRehearsal.reasonCodes) }}
            </v-alert>
            <dl class="facts-grid mb-4">
              <div><dt>{{ t('operations.integrity') }}</dt><dd>{{ safe(restoreRehearsal.integrity) }}</dd></div>
              <div><dt>{{ t('operations.manifest') }}</dt><dd>{{ safe(restoreRehearsal.manifestStatus) }}</dd></div>
              <div><dt>{{ t('operations.schemaCompatibility') }}</dt><dd>{{ safe(restoreRehearsal.schemaCompatibility) }}</dd></div>
              <div><dt>{{ t('operations.releaseCompatibility') }}</dt><dd>{{ safe(restoreRehearsal.releaseCompatibility) }}</dd></div>
              <div><dt>{{ t('operations.migrationPlan') }}</dt><dd>{{ safe(restoreRehearsal.migrationPlan) }}</dd></div>
              <div><dt>{{ t('operations.spaceStatus') }}</dt><dd>{{ safe(restoreRehearsal.spaceStatus) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.backupDigest') }}</dt><dd class="digest">{{ safe(restoreRehearsal.backupDigest) }}</dd></div>
              <div class="wide"><dt>{{ t('operations.previewRevision') }}</dt><dd class="digest">{{ safe(restoreRehearsal.revision) }}</dd></div>
            </dl>
            <div class="table-scroll mb-4">
              <table class="status-table">
                <thead><tr><th>{{ t('operations.owner') }}</th><th>{{ t('operations.included') }}</th><th>{{ t('operations.available') }}</th><th>{{ t('operations.compatibility') }}</th><th>{{ t('operations.hookStatus') }}</th></tr></thead>
                <tbody>
                  <tr v-for="owner in restoreRehearsal.owners" :key="owner.id">
                    <td>{{ owner.id }}</td><td>{{ yesNo(owner.included) }}</td><td>{{ yesNo(owner.available) }}</td>
                    <td>{{ safe(owner.compatibility) }}</td><td>{{ safe(owner.hookStatus) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <template v-if="restoreRehearsal.possible">
              <v-checkbox v-model="restoreAcknowledged" :label="t('operations.acknowledgeRestore')" />
              <v-text-field v-model="restoreTypedConfirmation" :label="t('operations.typedConfirmation')" :hint="requiredRestoreConfirmation" persistent-hint autocomplete="off" />
              <v-text-field v-model="restoreCredential" :label="t('operations.securityCredential')" type="password" autocomplete="current-password" />
              <v-btn color="error" :disabled="!canExecuteRestore" :loading="dataWorking" @click="runRestoreExecute">{{ t('operations.executeRestore') }}</v-btn>
            </template>
          </v-card-text>
        </v-card>
      </section>
    </template>

    <div class="sr-only" aria-live="polite" aria-atomic="true">{{ liveStatus }}</div>
  </v-container>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSecurityPosture, getSecuritySessions, acquireStepUpToken, type SecurityPosture, type SessionInventory } from '@/shared/composables/useSecurityOperations'
import { getSSHPosture, type SSHPostureEnvelope } from '@/shared/composables/useSSHManagement'
import { getDeploymentStatus, deploymentMessage, type DeploymentStatus } from '@/shared/composables/useDeployment'
import {
  activateSignedUpdate, checkSignedUpdate, dropConfirmation, executeDropData, executeRestore, getDataOwners,
  getMigrationPosture, getOperationsStatus, getResourcePressure, getSQLitePosture, getUpdatePosture,
  getUpdateTimeline, isFresh, operationsMessage, prepareSignedUpdate, previewDropData, rehearseRestore,
  restoreConfirmation, rollbackSignedUpdate, safeOperationalValue, safeReasonCodes, updateConfirmation,
  type DataOwnersPosture, type DropPreview, type MigrationPosture, type OperationsStatus,
  type ResourcePressurePosture, type RestoreRehearsal, type SQLitePosture, type UpdateCheck,
  type UpdateOperation, type UpdatePosture, type UpdateTimelineEntry,
} from '@/shared/composables/useOperations'

const { t, locale } = useI18n()
const loading = ref(true), updateWorking = ref(false), dataWorking = ref(false)
const errorMessage = ref(''), liveStatus = ref('')
const summary = ref<OperationsStatus | null>(null)
const security = ref<SecurityPosture | null>(null), sessions = ref<SessionInventory | null>(null)
const ssh = ref<SSHPostureEnvelope | null>(null), deployment = ref<DeploymentStatus | null>(null)
const update = ref<UpdatePosture | null>(null), updateCheck = ref<UpdateCheck | null>(null)
const updateOperation = ref<UpdateOperation | null>(null), updateTimeline = ref<UpdateTimelineEntry[]>([])
const pressure = ref<ResourcePressurePosture | null>(null), sqlite = ref<SQLitePosture | null>(null)
const migrations = ref<MigrationPosture | null>(null), owners = ref<DataOwnersPosture | null>(null)

const updateChannel = ref<'main' | 'beta'>('main')
const updateChannels: Array<'main' | 'beta'> = ['main', 'beta']
const updateAcknowledged = ref(false), updateTypedConfirmation = ref(''), updateCredential = ref('')
const operationConfirmation = ref(''), operationCredential = ref('')
const updateOperationHeading = ref<HTMLElement | null>(null)

const dropPreview = ref<DropPreview | null>(null), dropPreviewHeading = ref<HTMLElement | null>(null)
const dropBackupAcknowledged = ref(false), dropAcknowledged = ref(false)
const dropTypedConfirmation = ref(''), dropCredential = ref('')

const restoreFile = ref<File | null>(null), restorePassphrase = ref(''), restoreEncrypted = ref(false)
const restoreRehearsal = ref<RestoreRehearsal | null>(null), restoreHeading = ref<HTMLElement | null>(null)
const restoreAcknowledged = ref(false), restoreTypedConfirmation = ref(''), restoreCredential = ref('')

const updateFresh = computed(() => isFresh(update.value?.freshUntil))
const prepareConfirmation = computed(() => updateCheck.value?.sequence ? updateConfirmation('PREPARE', updateCheck.value.sequence) : '')
const activateConfirmation = computed(() => updateOperation.value ? updateConfirmation('ACTIVATE', updateOperation.value.sequence) : '')
const rollbackConfirmation = computed(() => updateOperation.value ? updateConfirmation('ROLLBACK', updateOperation.value.sequence) : '')
const operationConfirmationHint = computed(() =>
  updateOperation.value?.state === 'PREPARED'
    ? `${activateConfirmation.value} / ${rollbackConfirmation.value}`
    : rollbackConfirmation.value,
)
const canPrepareUpdate = computed(() => Boolean(
  updateCheck.value?.updateAvailable && updateCheck.value.sequence && updateCheck.value.manifestDigest &&
  updateAcknowledged.value && updateTypedConfirmation.value === prepareConfirmation.value && updateCredential.value,
))
const canShowUpdateActions = computed(() => Boolean(
  updateOperation.value && (updateOperation.value.state === 'PREPARED' || updateOperation.value.rollbackAvailable),
))
const admissionRows = computed(() => Object.entries(pressure.value?.admissionEffects ?? {}).sort(([a], [b]) => a.localeCompare(b)))
const requiredDropConfirmation = computed(() => dropPreview.value ? dropConfirmation(dropPreview.value.ownerId) : '')
const canExecuteDrop = computed(() => Boolean(
  dropPreview.value && dropPreview.value.blockers.length === 0 && dropBackupAcknowledged.value && dropAcknowledged.value &&
  dropTypedConfirmation.value === requiredDropConfirmation.value && dropCredential.value,
))
const requiredRestoreConfirmation = computed(() => restoreRehearsal.value ? restoreConfirmation(restoreRehearsal.value.revision) : '')
const canRehearseRestore = computed(() => Boolean(restoreFile.value && (!restoreEncrypted.value || restorePassphrase.value)))
const canExecuteRestore = computed(() => Boolean(
  restoreFile.value && restoreRehearsal.value?.possible && restoreAcknowledged.value &&
  restoreTypedConfirmation.value === requiredRestoreConfirmation.value && restoreCredential.value &&
  (!restoreEncrypted.value || restorePassphrase.value),
))

const loadAll = async () => {
  loading.value = true
  errorMessage.value = ''
  const responses = await Promise.all([
    getOperationsStatus(), getSecurityPosture(), getSecuritySessions(), getSSHPosture(), getDeploymentStatus(),
    getUpdatePosture(updateChannel.value), getResourcePressure(), getSQLitePosture(), getMigrationPosture(), getDataOwners(),
  ])
  summary.value = operationsMessage<OperationsStatus>(responses[0])
  security.value = operationsMessage<SecurityPosture>(responses[1])
  sessions.value = operationsMessage<SessionInventory>(responses[2])
  ssh.value = operationsMessage<SSHPostureEnvelope>(responses[3])
  deployment.value = deploymentMessage<DeploymentStatus>(responses[4])
  update.value = operationsMessage<UpdatePosture>(responses[5])
  pressure.value = operationsMessage<ResourcePressurePosture>(responses[6])
  sqlite.value = operationsMessage<SQLitePosture>(responses[7])
  migrations.value = operationsMessage<MigrationPosture>(responses[8])
  owners.value = operationsMessage<DataOwnersPosture>(responses[9])
  if (update.value?.operation) updateOperation.value = update.value.operation
  if (updateOperation.value) await refreshUpdateTimeline()
  errorMessage.value = responses.find(response => !response.success)?.msg ?? ''
  loading.value = false
  liveStatus.value = errorMessage.value || t('operations.statusRefreshed')
}

const runUpdateCheck = async () => {
  updateWorking.value = true
  updateCheck.value = null
  resetUpdateInputs()
  const response = await checkSignedUpdate(updateChannel.value)
  updateCheck.value = operationsMessage<UpdateCheck>(response)
  errorMessage.value = response.success ? '' : response.msg
  updateWorking.value = false
  liveStatus.value = errorMessage.value || t('operations.updateCheckComplete')
}

const runUpdatePrepare = async () => {
  const candidate = updateCheck.value
  if (!candidate?.sequence || !candidate.manifestDigest || !canPrepareUpdate.value) return
  updateWorking.value = true
  const target = `release:${candidate.manifestDigest}:${candidate.sequence}`
  const grant = await acquireStepUpToken('update.prepare', target, updateCredential.value)
  if (!grant.token) {
    errorMessage.value = grant.response.msg
    updateCredential.value = ''
    updateWorking.value = false
    return
  }
  const response = await prepareSignedUpdate({
    channel: candidate.channel,
    expectedSequence: candidate.sequence,
    expectedManifestDigest: candidate.manifestDigest,
    idempotencyKey: `update-ui:${crypto.randomUUID()}`,
    confirmation: prepareConfirmation.value,
    acknowledged: true,
  }, grant.token)
  updateOperation.value = updateOperationFromResponse(response)
  errorMessage.value = response.success ? '' : response.msg
  resetUpdateInputs()
  updateWorking.value = false
  if (updateOperation.value) {
    await refreshUpdateTimeline()
    await nextTick()
    updateOperationHeading.value?.focus()
  }
  liveStatus.value = errorMessage.value || t('operations.preflightComplete')
}

const runUpdateOperation = async (action: 'ACTIVATE' | 'ROLLBACK') => {
  const operation = updateOperation.value
  if (!operation || !operationCredential.value) return
  const expected = action === 'ACTIVATE' ? activateConfirmation.value : rollbackConfirmation.value
  if (operationConfirmation.value !== expected) return
  updateWorking.value = true
  const operationKind = action === 'ACTIVATE' ? 'update.activate' : 'update.rollback'
  const grant = await acquireStepUpToken(operationKind, `${operation.operationId}:${operation.revision}`, operationCredential.value)
  operationCredential.value = ''
  if (!grant.token) {
    errorMessage.value = grant.response.msg
    updateWorking.value = false
    return
  }
  const payload = { operationId: operation.operationId, expectedRevision: operation.revision, confirmation: expected }
  const response = action === 'ACTIVATE'
    ? await activateSignedUpdate(payload, grant.token)
    : await rollbackSignedUpdate(payload, grant.token)
  updateOperation.value = updateOperationFromResponse(response) ?? operation
  errorMessage.value = response.success ? '' : response.msg
  operationConfirmation.value = ''
  updateWorking.value = false
  await refreshUpdateTimeline()
  liveStatus.value = errorMessage.value || t('operations.operationUpdated')
}

const updateOperationFromResponse = (response: { success: boolean; obj: unknown }): UpdateOperation | null => {
  const direct = response.success && response.obj ? response.obj as UpdateOperation : null
  if (direct?.operationId) return direct
  if (response.obj && typeof response.obj === 'object') {
    const result = (response.obj as { result?: UpdateOperation }).result
    if (result?.operationId) return result
  }
  return null
}

const refreshUpdateTimeline = async () => {
  if (!updateOperation.value) return
  const response = await getUpdateTimeline(updateOperation.value.operationId, 0, 100)
  updateTimeline.value = operationsMessage<{ items: UpdateTimelineEntry[] }>(response)?.items ?? []
}

const resetUpdateInputs = () => {
  updateAcknowledged.value = false
  updateTypedConfirmation.value = ''
  updateCredential.value = ''
}

const runDropPreview = async (ownerId: string) => {
  dataWorking.value = true
  dropPreview.value = null
  dropBackupAcknowledged.value = false
  dropAcknowledged.value = false
  dropTypedConfirmation.value = ''
  dropCredential.value = ''
  const response = await previewDropData(ownerId)
  dropPreview.value = operationsMessage<DropPreview>(response)
  errorMessage.value = response.success ? '' : response.msg
  dataWorking.value = false
  await nextTick()
  dropPreviewHeading.value?.focus()
  liveStatus.value = errorMessage.value || t('operations.dropPreviewComplete')
}

const runDropExecute = async () => {
  const preview = dropPreview.value
  if (!preview || !canExecuteDrop.value) return
  dataWorking.value = true
  const grant = await acquireStepUpToken('data.drop', `durable-owner:${preview.ownerId}:${preview.revision}`, dropCredential.value)
  dropCredential.value = ''
  if (!grant.token) {
    errorMessage.value = grant.response.msg
    dataWorking.value = false
    return
  }
  const response = await executeDropData({
    ownerId: preview.ownerId,
    expectedPreviewRevision: preview.revision,
    idempotencyKey: `drop-ui:${crypto.randomUUID()}`,
    confirmation: requiredDropConfirmation.value,
    backupAcknowledged: true,
    acknowledged: true,
  }, grant.token)
  errorMessage.value = response.success ? '' : response.msg
  dropTypedConfirmation.value = ''
  dropBackupAcknowledged.value = false
  dropAcknowledged.value = false
  dataWorking.value = false
  liveStatus.value = errorMessage.value || t('operations.dropComplete')
  if (response.success) await loadAll()
}

const selectRestoreFile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  restoreFile.value = input.files?.[0] ?? null
  restoreEncrypted.value = restoreFile.value ? await isEncryptedBackupEnvelope(restoreFile.value) : false
  restoreRehearsal.value = null
  restoreAcknowledged.value = false
  restoreTypedConfirmation.value = ''
  restoreCredential.value = ''
}

const isEncryptedBackupEnvelope = async (file: File) => {
  const magic = new Uint8Array(await file.slice(0, 10).arrayBuffer())
  const expected = [83, 85, 73, 45, 84, 71, 66, 75, 80, 0]
  return expected.every((value, index) => magic[index] === value)
}

const restoreForm = (execution = false) => {
  const form = new FormData()
  if (!restoreFile.value) return form
  form.append('db', restoreFile.value)
  if (restorePassphrase.value) form.append('backupPassphrase', restorePassphrase.value)
  if (execution && restoreRehearsal.value) {
    form.append('expectedRehearsalRevision', restoreRehearsal.value.revision)
    form.append('idempotencyKey', `restore-ui:${crypto.randomUUID()}`)
    form.append('confirmation', requiredRestoreConfirmation.value)
    form.append('acknowledged', 'true')
  }
  return form
}

const runRestoreRehearsal = async () => {
  if (!restoreFile.value) return
  dataWorking.value = true
  restoreRehearsal.value = null
  const response = await rehearseRestore(restoreForm())
  restoreRehearsal.value = operationsMessage<RestoreRehearsal>(response)
  errorMessage.value = response.success ? '' : response.msg
  restorePassphrase.value = ''
  dataWorking.value = false
  await nextTick()
  restoreHeading.value?.focus()
  liveStatus.value = errorMessage.value || t('operations.rehearsalComplete')
}

const runRestoreExecute = async () => {
  const rehearsal = restoreRehearsal.value
  if (!rehearsal || !canExecuteRestore.value) return
  dataWorking.value = true
  const grant = await acquireStepUpToken('backup.restore', `database:restore:${rehearsal.revision}`, restoreCredential.value)
  restoreCredential.value = ''
  if (!grant.token) {
    errorMessage.value = grant.response.msg
    dataWorking.value = false
    return
  }
  const response = await executeRestore(restoreForm(true), grant.token)
  restorePassphrase.value = ''
  restoreTypedConfirmation.value = ''
  restoreAcknowledged.value = false
  errorMessage.value = response.success ? '' : response.msg
  dataWorking.value = false
  liveStatus.value = errorMessage.value || t('operations.restoreComplete')
}

const safe = (value: unknown) => safeOperationalValue(value)
const reasons = (values?: string[]) => {
  const safeValues = safeReasonCodes(values)
  return safeValues.length ? safeValues.join(', ') : t('operations.none')
}
const yesNo = (value: boolean) => value ? t('yes') : t('no')
const formatTime = (unix?: number) => unix && unix > 0
  ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(unix * 1000))
  : t('operations.unknown')
const formatBytes = (bytes?: number) => typeof bytes === 'number' && bytes >= 0
  ? new Intl.NumberFormat(locale.value, { style: 'unit', unit: 'byte', unitDisplay: 'short', maximumFractionDigits: 1 }).format(bytes)
  : t('operations.unknown')
const formatSignal = (value?: number, unit?: string) => typeof value === 'number'
  ? [new Intl.NumberFormat(locale.value, { maximumFractionDigits: 4 }).format(value), unit].filter(Boolean).join(' ')
  : t('operations.unknown')

onMounted(() => void loadAll())
</script>

<style scoped>
.operations-page { scroll-behavior: smooth; }
.operations-header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 1rem; }
.section-nav { display: flex; flex-wrap: wrap; gap: .65rem; }
.section-nav a { color: rgb(var(--v-theme-primary)); padding: .4rem .65rem; border-radius: .4rem; }
.section-nav a:focus-visible, .file-input:focus-visible { outline: 3px solid rgb(var(--v-theme-primary)); outline-offset: 3px; }
.section-block { margin-top: 2rem; scroll-margin-top: 1rem; }
.facts-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1rem; }
.facts-grid.one-column { grid-template-columns: 1fr; }
.facts-grid .wide { grid-column: 1 / -1; }
.facts-grid dt { font-size: .75rem; opacity: .75; }
.facts-grid dd { margin: 0; overflow-wrap: anywhere; }
.digest { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: .78rem; }
.channel-select { max-width: 12rem; min-width: 9rem; }
.table-scroll { overflow-x: auto; }
.status-table { width: 100%; border-collapse: collapse; }
.status-table th, .status-table td { padding: .65rem; text-align: left; vertical-align: top; border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); overflow-wrap: anywhere; }
.file-label { display: block; font-weight: 600; margin-bottom: .45rem; }
.file-input { display: block; width: 100%; max-width: 32rem; padding: .5rem; border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); border-radius: .35rem; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
@media (max-width: 600px) {
  .facts-grid { grid-template-columns: 1fr; }
  .facts-grid .wide { grid-column: auto; }
  .operations-header .v-btn { width: 100%; }
  .section-nav a { flex: 1 1 45%; text-align: center; }
}
@media (prefers-reduced-motion: reduce) {
  .operations-page { scroll-behavior: auto; }
  *, *::before, *::after { transition-duration: .01ms !important; animation-duration: .01ms !important; }
}
</style>
