<template>
  <v-container fluid>
    <v-row class="mb-3" align="center">
      <v-col>
        <h2>{{ t('fallbackHtml.title') }}</h2>
        <p class="text-medium-emphasis mb-0">{{ t('fallbackHtml.subtitle') }}</p>
      </v-col>
      <v-col cols="auto" class="d-flex ga-2">
        <v-btn color="secondary" prepend-icon="mdi-refresh" :loading="loading" @click="loadSites">
          {{ t('fallbackHtml.refresh') }}
        </v-btn>
        <v-btn color="primary" prepend-icon="mdi-plus" @click="createSite">
          {{ t('fallbackHtml.create') }}
        </v-btn>
      </v-col>
    </v-row>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-alert v-if="!sites.length && !loading" type="info" variant="tonal">
      {{ t('fallbackHtml.noSites') }}
    </v-alert>

    <v-dialog v-model="previewOpen" max-width="1180">
      <v-card>
        <v-card-title class="d-flex align-center justify-space-between">
          <span>{{ t('fallbackHtml.preview') }} {{ previewPath }}</span>
          <v-btn icon="mdi-close" variant="text" @click="previewOpen = false" />
        </v-card-title>
        <v-card-text>
          <v-alert v-if="previewLoading" type="info" variant="tonal" class="mb-3">
            {{ t('fallbackHtml.previewLoading') }}
          </v-alert>
          <v-alert v-if="previewError" type="error" variant="tonal" class="mb-3">
            {{ previewError }}
          </v-alert>
          <v-alert v-if="previewWarnings.length" type="warning" variant="tonal" class="mb-3">
            <div v-for="warning in previewWarnings" :key="warning">{{ warning }}</div>
          </v-alert>
          <v-alert v-if="!previewLoading && !previewError && !previewHtml" type="warning" variant="tonal">
            {{ t('fallbackHtml.previewEmpty') }}
          </v-alert>
          <iframe
            v-else-if="previewHtml"
            :key="`${previewPath}-${previewHtml.length}`"
            class="fallback-preview-frame"
            :srcdoc="previewHtml"
            :title="t('fallbackHtml.preview')"
          />
        </v-card-text>
      </v-card>
    </v-dialog>

    <v-dialog v-model="importOpen" max-width="820">
      <v-card>
        <v-card-title class="d-flex align-center justify-space-between">
          <span>{{ t('fallbackHtml.importJson') }} {{ importSiteRef?.name }}</span>
          <v-btn icon="mdi-close" variant="text" @click="importOpen = false" />
        </v-card-title>
        <v-card-text>
          <v-alert type="info" variant="tonal" class="mb-3">
            {{ t('fallbackHtml.importHelp') }}
          </v-alert>
          <v-textarea v-model="importText" rows="14" auto-grow :label="t('fallbackHtml.importJson')" />
        </v-card-text>
        <v-card-actions class="justify-end">
          <v-btn variant="tonal" @click="importOpen = false">{{ t('fallbackHtml.cancel') }}</v-btn>
          <v-btn color="primary" prepend-icon="mdi-import" @click="applyImport">{{ t('fallbackHtml.applyImport') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="bodyEditorOpen" max-width="1100" scrollable>
      <v-card>
        <v-card-title class="d-flex align-center justify-space-between">
          <span>{{ t('fallbackHtml.bodyEditor') }} {{ bodyEditorPageRef?.title }}</span>
          <v-btn icon="mdi-close" variant="text" @click="bodyEditorOpen = false" />
        </v-card-title>
        <v-card-text>
          <v-alert type="info" variant="tonal" class="mb-3">
            {{ t('fallbackHtml.bodyEditorHelp') }}
          </v-alert>
          <v-textarea
            v-model="bodyEditorText"
            class="fallback-body-editor"
            rows="24"
            auto-grow
            :label="t('fallbackHtml.body')"
          />
        </v-card-text>
        <v-card-actions class="justify-end">
          <v-btn variant="tonal" @click="bodyEditorOpen = false">{{ t('fallbackHtml.cancel') }}</v-btn>
          <v-btn color="primary" prepend-icon="mdi-content-save" @click="applyBodyEditor">{{ t('fallbackHtml.applyBody') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-expansion-panels class="mb-4">
      <v-expansion-panel>
        <v-expansion-panel-title>
          <div class="d-flex align-center justify-space-between w-100 pe-3">
            <div>
              <div class="text-subtitle-1">{{ t('fallbackHtml.templateCatalog') }}</div>
              <div class="text-caption text-medium-emphasis">{{ t('fallbackHtml.templateCatalogShortHelp') }}</div>
            </div>
            <v-chip size="small" color="info">{{ remoteTemplates.length }}</v-chip>
          </div>
        </v-expansion-panel-title>
        <v-expansion-panel-text>
          <div class="d-flex justify-space-between align-center mb-3">
            <v-alert type="info" variant="tonal" density="compact" class="flex-grow-1 me-3">
              {{ t('fallbackHtml.templateCatalogHelp') }}
            </v-alert>
            <v-btn size="small" variant="tonal" prepend-icon="mdi-cloud-sync" :loading="catalogLoading" @click="loadTemplateCatalog">
              {{ t('fallbackHtml.refreshCatalog') }}
            </v-btn>
          </div>
          <v-alert v-if="catalogError" type="error" variant="tonal" class="mb-3">{{ catalogError }}</v-alert>
          <div class="fallback-template-grid">
            <v-card v-for="template in remoteTemplates" :key="template.id" class="fallback-template-card" variant="outlined">
              <v-card-title class="fallback-template-title">
                <span>{{ template.name }}</span>
                <v-chip size="x-small" :color="template.installed ? 'success' : 'grey'">
                  {{ template.installed ? t('fallbackHtml.installed') : t('fallbackHtml.available') }}
                </v-chip>
              </v-card-title>
              <v-card-text class="py-2">
                <div class="text-caption text-medium-emphasis">{{ template.contentTypeProfile }}</div>
                <div class="text-caption">{{ template.license }}</div>
              </v-card-text>
              <v-card-actions class="fallback-template-actions">
                <v-btn
                  size="small"
                  color="primary"
                  variant="tonal"
                  prepend-icon="mdi-download"
                  :loading="installingTemplate === template.id"
                  @click="installRemoteTemplate(template)"
                >
                  {{ template.installed ? t('fallbackHtml.updateTemplate') : t('fallbackHtml.installTemplate') }}
                </v-btn>
                <v-btn size="small" variant="tonal" prepend-icon="mdi-plus" :disabled="!template.installed" @click="createSiteFromTemplate(template.id)">
                  {{ t('fallbackHtml.createSite') }}
                </v-btn>
                <v-btn size="small" color="error" variant="text" icon="mdi-delete" :disabled="!template.installed" @click="deleteRemoteTemplate(template)" />
              </v-card-actions>
            </v-card>
          </div>
          <div v-if="!remoteTemplates.length && !catalogLoading" class="text-medium-emphasis">
            {{ t('fallbackHtml.noRemoteTemplates') }}
          </div>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>

    <div class="fallback-site-list">
        <v-card v-for="site in sites" :key="site.id" class="fallback-site-card">
          <v-card-title class="fallback-site-title">
            <div class="fallback-site-heading">
              <span>{{ site.name }}</span>
              <v-chip size="small" :color="siteStatusColor(site)">
                {{ siteStatusText(site) }}
              </v-chip>
              <v-chip v-if="targetsBySite[site.id]?.[0]" size="small" variant="tonal">
                {{ publicTargetSummary(site) }}
              </v-chip>
            </div>
            <v-btn
              size="small"
              variant="text"
              :prepend-icon="siteCollapsed(site) ? 'mdi-chevron-down' : 'mdi-arrow-up'"
              @click="toggleSiteCollapse(site)"
            >
              {{ siteCollapsed(site) ? t('fallbackHtml.expandSite') : t('fallbackHtml.collapseSite') }}
            </v-btn>
          </v-card-title>
          <v-card-text v-if="!siteCollapsed(site)">
            <v-text-field v-model="site.name" :label="t('fallbackHtml.siteName')" density="compact" />
            <v-select
              v-model="site.templateId"
              :items="templateSelectItems(site)"
              item-title="name"
              item-value="id"
              :label="t('fallbackHtml.template')"
              density="compact"
              :disabled="!templateAffectsSite(site)"
            />
            <div class="text-caption text-medium-emphasis mb-2">
              {{ templateHelp(site) }}
            </div>
            <v-switch v-model="site.enabled" :label="t('fallbackHtml.enabled')" color="primary" hide-details />
            <v-alert v-if="siteMessages[site.id]" class="mt-3" :type="siteMessages[site.id].type" variant="tonal" density="compact">
              {{ siteMessages[site.id].text }}
            </v-alert>

            <v-divider class="my-4" />
            <div class="mb-4 fallback-section">
              <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.publishTarget') }}</div>
              <v-alert type="info" variant="tonal" density="compact" class="mb-3">
                {{ t('fallbackHtml.publishTargetHelp') }}
              </v-alert>
              <v-row density="compact" align="center">
                <v-col cols="12" md="3">
                  <v-text-field v-model="targetDraft(site).listen" :label="t('fallbackHtml.listen')" density="compact" />
                </v-col>
                <v-col cols="12" md="2">
                  <v-text-field v-model.number="targetDraft(site).port" :label="t('fallbackHtml.port')" density="compact" type="number" />
                </v-col>
                <v-col cols="12" md="2">
                  <v-switch v-model="targetDraft(site).tls" :label="t('fallbackHtml.tls')" color="primary" density="compact" hide-details />
                </v-col>
                <v-col cols="12" md="5" class="d-flex ga-2 align-center flex-wrap">
                  <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-content-save" @click="saveTarget(site)">
                    {{ t('fallbackHtml.saveTarget') }}
                  </v-btn>
                  <v-btn size="small" variant="tonal" prepend-icon="mdi-server-network" @click="useCurrentPanelTarget(site)">
                    {{ t('fallbackHtml.usePanelPort') }}
                  </v-btn>
                  <v-chip
                    v-for="target in targetsBySite[site.id] || []"
                    :key="target.id || `${target.kind}-${target.port}`"
                    size="small"
                    :color="endpointChipColor(target.status)"
                    :title="endpointChipTitle(target)"
                  >
                    {{ endpointLabel(target) }}
                    <span class="fallback-target-reason">{{ endpointStatusText(target) }}</span>
                  </v-chip>
                </v-col>
              </v-row>
            </div>

            <v-divider class="my-4" />
            <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.pages') }}</div>
            <v-card v-for="page in site.pages" :key="page.id || page.path" class="mb-3 fallback-page-card" variant="outlined">
              <v-card-text>
              <v-row density="compact">
                <v-col cols="12" md="4">
                  <v-text-field v-model="page.path" :label="t('fallbackHtml.path')" density="compact" />
                  <div class="fallback-path-hint" :class="{ 'text-error': pathIsReserved(page.path) }">
                    {{ t('fallbackHtml.pathPreview') }} {{ pathPreview(page.path) }}
                    <span v-if="pathIsReserved(page.path)"> - {{ t('fallbackHtml.reservedPathWarning') }}</span>
                  </div>
                </v-col>
                <v-col cols="12" md="8">
                  <v-text-field v-model="page.title" :label="t('fallbackHtml.pageTitle')" density="compact" />
                </v-col>
                <v-col cols="12" md="4">
                  <v-select
                    v-model="page.contentMode"
                    :items="contentModes"
                    item-title="title"
                    item-value="value"
                    :label="t('fallbackHtml.contentMode')"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12">
                  <div class="fallback-body-summary">
                    <span>{{ page.body || t('fallbackHtml.emptyBody') }}</span>
                    <v-btn size="small" variant="tonal" prepend-icon="mdi-arrow-expand" @click="openBodyEditor(page)">
                      {{ t('fallbackHtml.openBodyEditor') }}
                    </v-btn>
                  </div>
                </v-col>
                <v-col cols="12" class="d-flex ga-2">
                  <v-btn size="small" color="primary" prepend-icon="mdi-content-save" @click="savePage(site, page)">
                    {{ t('fallbackHtml.savePage') }}
                  </v-btn>
                  <v-btn size="small" color="error" variant="tonal" prepend-icon="mdi-delete" @click="deletePage(site, page)">
                    {{ t('fallbackHtml.deletePage') }}
                  </v-btn>
                </v-col>
              </v-row>
              </v-card-text>
            </v-card>
            <v-btn size="small" variant="tonal" prepend-icon="mdi-plus" @click="addPage(site)">
              {{ t('fallbackHtml.addPage') }}
            </v-btn>

            <v-divider class="my-4" />
            <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.redirects') }}</div>
            <div v-for="redirect in site.redirects" :key="redirect.id || redirect.fromPath" class="mb-3">
              <v-row density="compact">
                <v-col cols="12" md="3">
                  <v-text-field v-model="redirect.fromPath" :label="t('fallbackHtml.fromPath')" density="compact" />
                  <div class="fallback-path-hint" :class="{ 'text-error': pathIsReserved(redirect.fromPath) }">
                    {{ t('fallbackHtml.pathPreview') }} {{ pathPreview(redirect.fromPath) }}
                    <span v-if="pathIsReserved(redirect.fromPath)"> - {{ t('fallbackHtml.reservedPathWarning') }}</span>
                  </div>
                </v-col>
                <v-col cols="12" md="5">
                  <v-text-field v-model="redirect.toPath" :label="t('fallbackHtml.toPath')" density="compact" />
                  <div
                    v-if="!isExternalTarget(redirect.toPath)"
                    class="fallback-path-hint"
                    :class="{ 'text-error': pathIsReserved(redirect.toPath) }"
                  >
                    {{ t('fallbackHtml.pathPreview') }} {{ pathPreview(redirect.toPath) }}
                    <span v-if="pathIsReserved(redirect.toPath)"> - {{ t('fallbackHtml.reservedPathWarning') }}</span>
                  </div>
                </v-col>
                <v-col cols="12" md="2">
                  <v-select
                    v-model="redirect.statusCode"
                    :items="redirectStatuses"
                    :label="t('fallbackHtml.statusCode')"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" md="2" class="d-flex ga-2 align-center">
                  <v-btn size="small" color="primary" prepend-icon="mdi-content-save" @click="saveRedirect(site, redirect)">
                    {{ t('fallbackHtml.saveRedirect') }}
                  </v-btn>
                  <v-btn size="small" color="error" variant="tonal" prepend-icon="mdi-delete" @click="deleteRedirect(site, redirect)">
                    {{ t('fallbackHtml.deleteRedirect') }}
                  </v-btn>
                </v-col>
              </v-row>
            </div>
            <v-btn size="small" variant="tonal" prepend-icon="mdi-plus" @click="addRedirect(site)">
              {{ t('fallbackHtml.addRedirect') }}
            </v-btn>

            <v-divider class="my-4" />
            <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.assets') }}</div>
            <div class="d-flex ga-2 align-center flex-wrap mb-3">
              <input
                class="fallback-asset-input"
                type="file"
                accept=".css,.ico,.jpg,.jpeg,.png,.txt,.webp,.woff,.woff2"
                @change="uploadAsset(site, $event)"
              >
              <span class="text-caption text-medium-emphasis">{{ t('fallbackHtml.assetHelp') }}</span>
            </div>
            <v-table v-if="assetsBySite[site.id]?.length" density="compact">
              <tbody>
                <tr v-for="asset in assetsBySite[site.id]" :key="asset.id">
                  <td>{{ asset.logicalPath }}</td>
                  <td>{{ asset.mimeType }}</td>
                  <td>{{ asset.sizeBytes }} B</td>
                  <td class="text-right">
                    <v-btn icon="mdi-delete" size="small" color="error" variant="text" @click="deleteAsset(site, asset)" />
                  </td>
                </tr>
              </tbody>
            </v-table>

            <v-divider class="my-4" />
            <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.externalResources') }}</div>
            <div v-for="resource in externalResourcesBySite[site.id] || []" :key="resource.id || resource.url" class="mb-3">
              <v-row density="compact">
                <v-col cols="12" md="3">
                  <v-select v-model="resource.kind" :items="externalResourceKinds" :label="t('fallbackHtml.kind')" density="compact" />
                </v-col>
                <v-col cols="12" md="5">
                  <v-text-field v-model="resource.url" :label="t('fallbackHtml.url')" density="compact" />
                </v-col>
                <v-col cols="12" md="2">
                  <v-switch v-model="resource.allowed" :label="t('fallbackHtml.allowed')" color="primary" hide-details />
                </v-col>
                <v-col cols="12" md="2" class="d-flex ga-2 align-center">
                  <v-btn size="small" color="primary" prepend-icon="mdi-content-save" @click="saveExternalResource(site, resource)">
                    {{ t('fallbackHtml.save') }}
                  </v-btn>
                  <v-btn size="small" color="error" variant="tonal" prepend-icon="mdi-delete" @click="deleteExternalResource(site, resource)" />
                </v-col>
              </v-row>
            </div>
            <v-btn size="small" variant="tonal" prepend-icon="mdi-plus" @click="addExternalResource(site)">
              {{ t('fallbackHtml.addExternalResource') }}
            </v-btn>

            <v-alert v-if="safetyBySite[site.id]?.warnings.length" class="mt-4" type="warning" variant="tonal">
              <div class="font-weight-bold">{{ t('fallbackHtml.warnings') }}</div>
              <div v-for="warning in safetyBySite[site.id].warnings" :key="warning">{{ warning }}</div>
            </v-alert>

            <v-divider class="my-4" />
            <div class="mb-4 fallback-section">
              <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.selfStealDraft') }}</div>
              <v-alert type="info" variant="tonal" density="compact" class="mb-3">
                {{ t('fallbackHtml.selfStealHelp') }}
              </v-alert>
              <v-row density="compact" align="center">
                <v-col cols="12" md="3">
                  <v-select
                    v-model="selfStealOptions(site).profile"
                    :items="selfStealProfiles"
                    item-title="title"
                    item-value="value"
                    :label="t('fallbackHtml.selfStealProfile')"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" md="2">
                  <v-select
                    v-model="selfStealOptions(site).transport"
                    :items="selfStealTransports"
                    item-title="title"
                    item-value="value"
                    :label="t('fallbackHtml.selfStealTransport')"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" md="3">
                  <v-text-field
                    v-model="selfStealOptions(site).publicListen"
                    :label="t('fallbackHtml.selfStealPublicListen')"
                    density="compact"
                  />
                </v-col>
                <v-col cols="12" md="2">
                  <v-switch
                    v-model="selfStealOptions(site).prepareTransfer"
                    :label="t('fallbackHtml.preparePortTransfer')"
                    color="primary"
                    density="compact"
                    hide-details
                  />
                </v-col>
                <v-col cols="12" md="2" class="d-flex align-center">
                  <v-btn
                    variant="tonal"
                    prepend-icon="mdi-shield-outline"
                    :disabled="!canCreateSelfStealDraft(site)"
                    @click="createSelfStealDraft(site)"
                  >
                    {{ t('fallbackHtml.selfStealDraft') }}
                  </v-btn>
                </v-col>
              </v-row>
            </div>

            <v-alert v-if="selfStealDraftBySite[site.id]" class="mt-4" type="info" variant="tonal">
              <div class="font-weight-bold">
                {{ t('fallbackHtml.selfStealDraft') }}: {{ selfStealDraftBySite[site.id].status }}
              </div>
              <div>
                {{ selfStealDraftBySite[site.id].payload.profile }} /
                {{ selfStealDraftBySite[site.id].payload.transport }} /
                {{ selfStealDraftBySite[site.id].payload.publicListen }}:{{ selfStealDraftBySite[site.id].payload.publicPort }}
              </div>
              <div v-if="selfStealDraftBySite[site.id].payload.portTransfer">
                {{ t('fallbackHtml.portTransfer') }}:
                {{ selfStealDraftBySite[site.id].payload.portTransfer?.prepared ? t('fallbackHtml.prepared') : t('fallbackHtml.required') }}
              </div>
              <div v-if="selfStealDraftBySite[site.id].coreDraftId">
                {{ t('fallbackHtml.coreDraft') }}: #{{ selfStealDraftBySite[site.id].coreDraftId }}
              </div>
              <div v-for="block in selfStealDraftBySite[site.id].payload.blocks" :key="block">
                {{ block }}
              </div>
              <div v-if="selfStealDraftBySite[site.id].payload.nextSteps.length" class="mt-2">
                {{ t('fallbackHtml.nextSteps') }}:
                {{ selfStealDraftBySite[site.id].payload.nextSteps.join('; ') }}
              </div>
              <v-btn
                v-if="canReviewSelfStealDraft(selfStealDraftBySite[site.id])"
                class="mt-3"
                size="small"
                variant="tonal"
                prepend-icon="mdi-file-eye-outline"
                @click="reviewSelfStealDraft(selfStealDraftBySite[site.id])"
              >
                {{ t('fallbackHtml.reviewInInbounds') }}
              </v-btn>
            </v-alert>

            <v-divider class="my-4" />
            <div class="d-flex align-center justify-space-between flex-wrap ga-2 mb-2">
              <div class="text-subtitle-2">{{ t('fallbackHtml.publishes') }}</div>
              <div class="d-flex align-center ga-2 flex-wrap">
                <v-text-field
                  v-model.number="pruneKeepBySite[site.id]"
                  class="fallback-prune-keep"
                  density="compact"
                  type="number"
                  min="0"
                  max="50"
                  hide-details
                  :label="t('fallbackHtml.keepRollbackVersions')"
                />
                <v-btn size="small" variant="tonal" prepend-icon="mdi-delete-outline" @click="prunePublishes(site)">
                  {{ t('fallbackHtml.pruneOldVersions') }}
                </v-btn>
              </div>
            </div>
            <div class="d-flex ga-2 align-center flex-wrap">
              <v-chip
                v-for="publish in publishesBySite[site.id] || []"
                :key="publish.id"
                size="small"
                :color="publish.active ? 'success' : 'grey'"
              >
                {{ publish.version }} - {{ publish.files }} {{ t('fallbackHtml.files') }}
              </v-chip>
            </div>
            <v-row v-if="rollbackVersions(site).length" class="mt-2" density="compact">
              <v-col cols="12" md="8">
                <v-select
                  v-model="rollbackSelectionBySite[site.id]"
                  :items="rollbackVersions(site)"
                  item-title="version"
                  item-value="version"
                  :label="t('fallbackHtml.rollbackTo')"
                  density="compact"
                />
              </v-col>
              <v-col cols="12" md="4" class="d-flex align-center">
                <v-btn variant="tonal" color="warning" prepend-icon="mdi-restore" @click="rollbackSite(site)">
                  {{ t('fallbackHtml.rollback') }}
                </v-btn>
              </v-col>
            </v-row>
          </v-card-text>
          <v-card-actions v-if="!siteCollapsed(site)" class="justify-end flex-wrap fallback-site-actions">
            <v-btn variant="tonal" prepend-icon="mdi-database-import" @click="openImport(site)">
              {{ t('fallbackHtml.importJson') }}
            </v-btn>
            <v-btn
              variant="tonal"
              prepend-icon="mdi-shield-search"
              @click="checkSafety(site)"
            >
              {{ t('fallbackHtml.safety') }}
            </v-btn>
            <v-btn variant="tonal" prepend-icon="mdi-eye" @click="previewSite(site)">
              {{ t('fallbackHtml.preview') }}
            </v-btn>
            <v-btn variant="tonal" prepend-icon="mdi-open-in-new" @click="openPublishedSite(site)">
              {{ t('fallbackHtml.openPublishedPage') }}
            </v-btn>
            <v-btn color="secondary" prepend-icon="mdi-content-save" @click="saveSite(site)">
              {{ t('fallbackHtml.save') }}
            </v-btn>
            <v-btn color="primary" prepend-icon="mdi-upload" @click="publishSite(site)">
              {{ t('fallbackHtml.publish') }}
            </v-btn>
            <v-btn color="warning" variant="tonal" prepend-icon="mdi-server-off" @click="unpublishSite(site)">
              {{ t('fallbackHtml.unpublish') }}
            </v-btn>
            <v-btn color="error" variant="tonal" prepend-icon="mdi-delete" @click="deleteSite(site)">
              {{ t('fallbackHtml.deleteSite') }}
            </v-btn>
          </v-card-actions>
        </v-card>
    </div>
  </v-container>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import api from '@/plugins/api'
import Data from '@/store/modules/data'
import {
  endpointChipColor,
  endpointChipTitle,
  endpointLabel,
  endpointStatusText,
  isExternalURL,
  isReservedPublicPath,
  normalizePublicPathPreview,
} from '../publicSiteLogic'

interface Page {
  id: number
  path: string
  title: string
  body: string
  contentMode: string
  isHome: boolean
}

interface Redirect {
  id: number
  fromPath: string
  toPath: string
  statusCode: number
  external: boolean
}

interface AssetView {
  id: number
  logicalPath: string
  mimeType: string
  sha256: string
  sizeBytes: number
  provenance: string
  createdAt: number
}

interface ExternalResourceView {
  id: number
  kind: string
  url: string
  allowed: boolean
  createdAt: number
}

interface Site {
  id: number
  name: string
  enabled: boolean
  status: string
  templateId: string
  hostname?: string
  pages: Page[]
  redirects: Redirect[]
}

interface TargetView {
  id: number
  kind: string
  host: string
  listen: string
  port: number
  rootPath: string
  runtime: string
  tls: boolean
  status: string
  reason: string
  current: boolean
}

interface PortCandidate {
  kind: string
  listen: string
  port: number
  runtime: string
  tls: boolean
  status: string
  reason: string
}

interface SafetyReport {
  ok: boolean
  warnings: string[]
}

interface PreviewResult {
  path: string
  html: string
  warnings: string[]
}

interface TemplateDefinition {
  id: string
  name: string
  source: string
  license: string
  contentTypeProfile: string
  renderable: boolean
}

interface RemoteTemplateView {
  id: string
  name: string
  source: string
  license: string
  contentTypeProfile: string
  manifestUrl: string
  installed: boolean
  installedAt: number
  notes: string[]
}

interface PublishView {
  id: number
  version: string
  active: boolean
  files: number
  createdAt: number
}

interface PrunePublishesResult {
  removed: number
  kept: number
}

interface SelfStealDraftView {
  id: number
  siteId: number
  coreDraftId: number
  status: string
  payload: {
    noApply: boolean
    requiresCapability: string
    coreDraftId?: number
    activePublish: string
    profile?: string
    transport?: string
    publicListen?: string
    publicPort?: number
    portTransfer?: {
      required: boolean
      prepared: boolean
      reason: string
    }
    inboundType?: string
    inboundTag?: string
    inboundCandidate?: unknown
    warnings: string[]
    blocks: string[]
    nextSteps: string[]
  }
  createdAt: number
}

interface SelfStealOptions {
  profile: string
  transport: string
  publicListen: string
  prepareTransfer: boolean
}

const { t } = useI18n()
const router = useRouter()
const sites = ref<Site[]>([])
const templates = ref<TemplateDefinition[]>([])
const remoteTemplates = ref<RemoteTemplateView[]>([])
const portCandidates = ref<PortCandidate[]>([])
const loading = ref(false)
const catalogLoading = ref(false)
const installingTemplate = ref('')
const error = ref('')
const catalogError = ref('')
const previewOpen = ref(false)
const previewLoading = ref(false)
const previewHtml = ref('')
const previewPath = ref('')
const previewWarnings = ref<string[]>([])
const previewError = ref('')
const importOpen = ref(false)
const importSiteRef = ref<Site | null>(null)
const importText = ref('')
const targetsBySite = reactive<Record<number, TargetView[]>>({})
const targetDraftsBySite = reactive<Record<number, TargetView>>({})
const siteMessages = reactive<Record<number, { type: 'success' | 'info' | 'warning' | 'error', text: string }>>({})
const collapsedSites = reactive<Record<number, boolean>>({})
const assetsBySite = reactive<Record<number, AssetView[]>>({})
const externalResourcesBySite = reactive<Record<number, ExternalResourceView[]>>({})
const publishesBySite = reactive<Record<number, PublishView[]>>({})
const pruneKeepBySite = reactive<Record<number, number>>({})
const rollbackSelectionBySite = reactive<Record<number, string>>({})
const safetyBySite = reactive<Record<number, SafetyReport>>({})
const selfStealDraftBySite = reactive<Record<number, SelfStealDraftView>>({})
const selfStealOptionsBySite = reactive<Record<number, SelfStealOptions>>({})
const bodyEditorOpen = ref(false)
const bodyEditorPageRef = ref<Page | null>(null)
const bodyEditorText = ref('')
const jsonPost = { headers: { 'Content-Type': 'application/json' } }
const redirectStatuses = [301, 302, 307, 308]
const externalResourceKinds = ['link', 'image', 'font']
const contentModes = [
  { title: 'Text', value: 'text' },
  { title: 'Safe HTML', value: 'html' },
  { title: 'Downloaded static HTML', value: 'static-html' },
]
const selfStealProfiles = [
  { title: 'VLESS + REALITY', value: 'vless-reality' },
  { title: 'Trojan + TLS fallback', value: 'trojan-tls-fallback' },
]
const selfStealTransports = [
  { title: 'TCP', value: 'tcp' },
  { title: 'WebSocket', value: 'ws' },
  { title: 'HTTP', value: 'http' },
  { title: 'gRPC', value: 'grpc' },
  { title: 'HTTP Upgrade', value: 'httpupgrade' },
]

const apiObj = <T>(response: { data: { success?: boolean, msg?: string, obj?: T } | T }): T => {
  const data = response.data as { success?: boolean, msg?: string, obj?: T } | T
  if (data && typeof data === 'object' && 'success' in data && 'obj' in data) {
    if (data.success === false) {
      throw new Error(data.msg || 'Request failed')
    }
    return data.obj as T
  }
  return data as T
}

function normalizeSites(items: Site[]): Site[] {
  return items.map(site => ({
    ...site,
    templateId: site.templateId || 'generated-portal',
    pages: (site.pages ?? []).map(page => ({ ...page, contentMode: page.contentMode || 'text' })),
    redirects: site.redirects ?? [],
  }))
}

function siteCollapsed(site: Site): boolean {
  return collapsedSites[site.id] === true
}

function toggleSiteCollapse(site: Site) {
  collapsedSites[site.id] = !siteCollapsed(site)
}

function siteStatusColor(site: Site): string {
  if (!site.enabled) return 'warning'
  if (site.status === 'published') return 'success'
  if (site.status === 'error') return 'error'
  return 'grey'
}

function siteStatusText(site: Site): string {
  if (!site.enabled) return t('fallbackHtml.statusDisabled')
  if (site.status === 'published') return t('fallbackHtml.statusPublished')
  if (site.status === 'error') return t('fallbackHtml.statusError')
  return t('fallbackHtml.statusDraft')
}

function publicTargetSummary(site: Site): string {
  const target = targetsBySite[site.id]?.[0]
  if (!target) return t('fallbackHtml.noPublishTarget')
  return endpointLabel(target)
}

function templateAffectsSite(site: Site): boolean {
  return site.pages.some(page => (page.contentMode || 'text') !== 'static-html')
}

function templateSelectItems(site: Site): TemplateDefinition[] {
  if (!templateAffectsSite(site)) {
    return templates.value
  }
  return templates.value.filter(template => template.renderable)
}

function templateHelp(site: Site): string {
  if (!templateAffectsSite(site)) {
    return t('fallbackHtml.templateStaticHelp')
  }
  return t('fallbackHtml.templateGeneratedHelp')
}

async function loadSites() {
  loading.value = true
  error.value = ''
  try {
    sites.value = normalizeSites(apiObj(await api.get('api/components/fallback-html/sites')) ?? [])
    templates.value = apiObj(await api.get('api/components/fallback-html/templates')) ?? []
    portCandidates.value = apiObj(await api.get('api/components/fallback-html/ports')) ?? []
    await loadTemplateCatalog()
    await Promise.all(sites.value.flatMap(site => [loadTargets(site), loadAssets(site), loadExternalResources(site), loadPublishes(site)]))
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function loadTargets(site: Site) {
  const targets = apiObj<TargetView[]>(await api.get(`api/components/fallback-html/sites/${site.id}/targets`)) ?? []
  targetsBySite[site.id] = targets
  targetDraftsBySite[site.id] = cloneTarget(targets[0] ?? defaultTargetDraft())
}

async function loadAssets(site: Site) {
  assetsBySite[site.id] = apiObj(await api.get(`api/components/fallback-html/sites/${site.id}/assets`)) ?? []
}

async function loadExternalResources(site: Site) {
  externalResourcesBySite[site.id] = apiObj(await api.get(`api/components/fallback-html/sites/${site.id}/external-resources`)) ?? []
}

async function loadPublishes(site: Site) {
  const publishes = apiObj<PublishView[]>(await api.get(`api/components/fallback-html/sites/${site.id}/publishes`)) ?? []
  publishesBySite[site.id] = publishes
  if (pruneKeepBySite[site.id] === undefined) {
    pruneKeepBySite[site.id] = 2
  }
  const selected = rollbackSelectionBySite[site.id]
  const versions = publishes.filter(publish => !publish.active)
  if (!selected || !versions.some(publish => publish.version === selected)) {
    rollbackSelectionBySite[site.id] = versions[0]?.version ?? ''
  }
}

async function loadTemplateCatalog() {
  catalogLoading.value = true
  catalogError.value = ''
  try {
    const catalog = apiObj<{ templates: RemoteTemplateView[] }>(await api.get('api/components/fallback-html/template-catalog'))
    remoteTemplates.value = catalog?.templates ?? []
  } catch (err) {
    catalogError.value = err instanceof Error ? err.message : String(err)
  } finally {
    catalogLoading.value = false
  }
}

async function installRemoteTemplate(template: RemoteTemplateView) {
  installingTemplate.value = template.id
  try {
    await api.post(`api/components/fallback-html/template-catalog/${encodeURIComponent(template.id)}/install`)
    templates.value = apiObj(await api.get('api/components/fallback-html/templates')) ?? []
    await loadTemplateCatalog()
  } finally {
    installingTemplate.value = ''
  }
}

async function deleteRemoteTemplate(template: RemoteTemplateView) {
  await api.delete(`api/components/fallback-html/template-catalog/${encodeURIComponent(template.id)}`)
  templates.value = apiObj(await api.get('api/components/fallback-html/templates')) ?? []
  await loadTemplateCatalog()
}

async function createSite() {
  const created = apiObj<Site>(await api.post('api/components/fallback-html/sites', { name: 'Public Site', enabled: true }, jsonPost))
  await api.post(`api/components/fallback-html/sites/${created.id}/targets`, {
    kind: 'standalone',
    listen: defaultExactListen(),
    port: 443,
    rootPath: '/',
    runtime: 'gin',
    tls: false,
  }, jsonPost)
  await loadSites()
}

async function createSiteFromTemplate(templateId: string) {
  const created = apiObj<Site>(await api.post(`api/components/fallback-html/templates/${encodeURIComponent(templateId)}/create-site`))
  await api.post(`api/components/fallback-html/sites/${created.id}/targets`, defaultTargetPayload(), jsonPost)
  await loadSites()
}

async function saveSite(site: Site) {
  const saved = apiObj<Site>(await api.post('api/components/fallback-html/sites', {
    id: site.id,
    name: site.name,
    enabled: site.enabled,
    templateId: site.templateId,
  }, jsonPost))
  Object.assign(site, normalizeSites([saved])[0])
}

async function saveSiteMetadata(site: Site) {
  await saveSite(site)
}

async function deleteSite(site: Site) {
  await api.delete(`api/components/fallback-html/sites/${site.id}`)
  await loadSites()
}

function addPage(site: Site) {
  site.pages.push({
    id: 0,
    path: `/page-${site.pages.length + 1}/`,
    title: 'New page',
    body: 'Write public content for this page.',
    contentMode: 'text',
    isHome: false,
  })
}

function addRedirect(site: Site) {
  site.redirects.push({
    id: 0,
    fromPath: `/old-${site.redirects.length + 1}/`,
    toPath: '/',
    statusCode: 302,
    external: false,
  })
}

function openImport(site: Site) {
  importSiteRef.value = site
  importText.value = JSON.stringify({
    schema: 'solovey-ui/fallback-html-site/v1',
    pages: site.pages.map(page => ({
      path: page.path,
      title: page.title,
      body: page.body,
      contentMode: page.contentMode,
      isHome: page.isHome,
    })),
    redirects: site.redirects.map(redirect => ({
      fromPath: redirect.fromPath,
      toPath: redirect.toPath,
      statusCode: redirect.statusCode,
    })),
  }, null, 2)
  importOpen.value = true
}

function openBodyEditor(page: Page) {
  bodyEditorPageRef.value = page
  bodyEditorText.value = page.body
  bodyEditorOpen.value = true
}

function applyBodyEditor() {
  if (bodyEditorPageRef.value) {
    bodyEditorPageRef.value.body = bodyEditorText.value
  }
  bodyEditorOpen.value = false
}

async function applyImport() {
  if (!importSiteRef.value) return
  const payload = JSON.parse(importText.value)
  await api.post(`api/components/fallback-html/sites/${importSiteRef.value.id}/import`, payload, jsonPost)
  importOpen.value = false
  await loadSites()
}

async function savePage(site: Site, page: Page) {
  const saved = apiObj<Page>(await api.post(`api/components/fallback-html/sites/${site.id}/pages`, page, jsonPost))
  Object.assign(page, saved)
  await loadSites()
}

async function saveRedirect(site: Site, redirect: Redirect) {
  const saved = apiObj<Redirect>(await api.post(`api/components/fallback-html/sites/${site.id}/redirects`, redirect, jsonPost))
  Object.assign(redirect, saved)
  await loadSites()
}

async function saveTarget(site: Site) {
  clearSiteMessage(site.id)
  const draft = targetDraft(site)
  try {
    const saved = apiObj<TargetView>(await api.post(`api/components/fallback-html/sites/${site.id}/targets`, targetPayloadFromDraft(draft), jsonPost))
    targetsBySite[site.id] = [saved]
    targetDraftsBySite[site.id] = cloneTarget(saved)
    portCandidates.value = apiObj(await api.get('api/components/fallback-html/ports')) ?? []
    setSiteMessage(site.id, 'success', t('fallbackHtml.targetSaved'))
  } catch (err) {
    setSiteMessage(site.id, 'error', formatSiteError(err, t('fallbackHtml.targetSaveFailed')))
  }
}

async function useCurrentPanelTarget(site: Site) {
  clearSiteMessage(site.id)
  try {
    portCandidates.value = apiObj(await api.get('api/components/fallback-html/ports')) ?? []
  } catch (err) {
    setSiteMessage(site.id, 'error', formatSiteError(err, t('fallbackHtml.targetSaveFailed')))
    return
  }
  const current = portCandidates.value.find(candidate => candidate.kind === 'web-current')
  if (!current) {
    setSiteMessage(site.id, 'warning', t('fallbackHtml.noTargets'))
    return
  }
  targetDraftsBySite[site.id] = cloneTarget({
    id: targetDraft(site).id,
    kind: 'web-current',
    host: '',
    listen: current.listen || defaultExactListen(),
    port: current.port || 0,
    rootPath: '/',
    runtime: current.runtime || 'gin',
    tls: current.tls || false,
    status: current.status || 'managed',
    reason: current.reason || '',
    current: true,
  })
}

async function deletePage(site: Site, page: Page) {
  if (!page.id) {
    site.pages = site.pages.filter(candidate => candidate !== page)
    return
  }
  await api.delete(`api/components/fallback-html/sites/${site.id}/pages/${page.id}`)
  await loadSites()
}

async function deleteRedirect(site: Site, redirect: Redirect) {
  if (!redirect.id) {
    site.redirects = site.redirects.filter(candidate => candidate !== redirect)
    return
  }
  await api.delete(`api/components/fallback-html/sites/${site.id}/redirects/${redirect.id}`)
  await loadSites()
}

async function uploadAsset(site: Site, event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const form = new FormData()
  form.append('file', file)
  await api.post(`api/components/fallback-html/sites/${site.id}/assets`, form)
  input.value = ''
  await loadAssets(site)
}

async function deleteAsset(site: Site, asset: AssetView) {
  await api.delete(`api/components/fallback-html/sites/${site.id}/assets/${asset.id}`)
  await loadAssets(site)
}

function addExternalResource(site: Site) {
  const list = externalResourcesBySite[site.id] ?? []
  externalResourcesBySite[site.id] = [
    ...list,
    { id: 0, kind: 'link', url: 'https://example.com/', allowed: true, createdAt: 0 },
  ]
}

async function saveExternalResource(site: Site, resource: ExternalResourceView) {
  const saved = apiObj<ExternalResourceView>(await api.post(
    `api/components/fallback-html/sites/${site.id}/external-resources`,
    resource,
    jsonPost,
  ))
  const list = externalResourcesBySite[site.id] ?? []
  const index = list.indexOf(resource)
  if (index >= 0) {
    list.splice(index, 1, saved)
  } else {
    externalResourcesBySite[site.id] = [...list, saved]
  }
}

async function deleteExternalResource(site: Site, resource: ExternalResourceView) {
  if (!resource.id) {
    externalResourcesBySite[site.id] = (externalResourcesBySite[site.id] ?? []).filter(candidate => candidate !== resource)
    return
  }
  await api.delete(`api/components/fallback-html/sites/${site.id}/external-resources/${resource.id}`)
  await loadExternalResources(site)
}

async function checkSafety(site: Site) {
  clearSiteMessage(site.id)
  try {
    safetyBySite[site.id] = apiObj(await api.post(`api/components/fallback-html/sites/${site.id}/safety`))
  } catch (err) {
    setSiteMessage(site.id, 'error', formatSiteError(err, t('fallbackHtml.safetyFailed')))
  }
}

async function previewSite(site: Site) {
  previewOpen.value = true
  previewLoading.value = true
  previewError.value = ''
  previewHtml.value = ''
  previewPath.value = ''
  previewWarnings.value = []
  try {
    await saveSiteMetadata(site)
    const result = apiObj<PreviewResult | null>(await api.post(`api/components/fallback-html/sites/${site.id}/preview`, {}, jsonPost))
    if (!result) {
      throw new Error(t('fallbackHtml.previewEmpty'))
    }
    previewHtml.value = result.html || ''
    previewPath.value = result.path || '/'
    previewWarnings.value = result.warnings ?? []
  } catch (err) {
    previewError.value = formatSiteError(err, t('fallbackHtml.previewFailed'))
    setSiteMessage(site.id, 'error', previewError.value)
  } finally {
    previewLoading.value = false
  }
}

function openPublishedSite(site: Site) {
  const url = publicSiteURL(site)
  if (!url) {
    setSiteMessage(site.id, 'warning', t('fallbackHtml.openPublishedPageUnavailable'))
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

function publicSiteURL(site: Site): string {
  const target = targetsBySite[site.id]?.[0] ?? targetDraft(site)
  const rootPath = target.rootPath || '/'
  if (target.kind === 'web-current') {
    return new URL(rootPath, window.location.origin).toString()
  }
  const protocol = target.tls ? 'https:' : 'http:'
  let host = target.host || target.listen || window.location.hostname || '127.0.0.1'
  if (host === '0.0.0.0' || host === '::' || host === '[::]') {
    host = window.location.hostname || '127.0.0.1'
  }
  if (host.includes(':') && !host.startsWith('[')) {
    host = `[${host}]`
  }
  const port = Number(target.port) || (target.tls ? 443 : 80)
  const defaultPort = (protocol === 'https:' && port === 443) || (protocol === 'http:' && port === 80)
  return `${protocol}//${host}${defaultPort ? '' : `:${port}`}${rootPath.startsWith('/') ? rootPath : `/${rootPath}`}`
}

async function createSelfStealDraft(site: Site) {
  const draft = apiObj<SelfStealDraftView>(await api.post(
    `api/components/fallback-html/sites/${site.id}/self-steal/draft`,
    selfStealOptions(site),
    jsonPost,
  ))
  selfStealDraftBySite[site.id] = draft
  if (canReviewSelfStealDraft(draft)) {
    await reviewSelfStealDraft(draft)
  }
}

function canReviewSelfStealDraft(draft?: SelfStealDraftView): boolean {
  return !!draft?.coreDraftId && draft.status === 'ready' && !!draft.payload?.inboundCandidate
}

function canCreateSelfStealDraft(site: Site): boolean {
  return !!activePublish(site) && safetyBySite[site.id]?.ok === true
}

function selfStealOptions(site: Site): SelfStealOptions {
  if (!selfStealOptionsBySite[site.id]) {
    selfStealOptionsBySite[site.id] = {
      profile: 'vless-reality',
      transport: 'tcp',
      publicListen: defaultSelfStealPublicListen(site),
      prepareTransfer: true,
    }
  }
  return selfStealOptionsBySite[site.id]
}

function defaultSelfStealPublicListen(site: Site): string {
  const target = targetsBySite[site.id]?.[0] ?? targetDraft(site)
  for (const candidate of [site.hostname, target.host, target.listen, window.location.hostname]) {
    const value = String(candidate || '').trim()
    if (value && value !== '0.0.0.0' && value !== '::' && value !== '[::]') {
      return value
    }
  }
  return '127.0.0.1'
}

async function reviewSelfStealDraft(draft: SelfStealDraftView) {
  await Data().loadInboundDrafts()
  await router.push({ path: '/inbounds', query: { draft: String(draft.coreDraftId) } })
}

async function publishSite(site: Site) {
  clearSiteMessage(site.id)
  try {
    await saveSiteMetadata(site)
    apiObj<unknown>(await api.post(`api/components/fallback-html/sites/${site.id}/publish`))
    await loadSites()
    setSiteMessage(site.id, 'success', t('fallbackHtml.publishSucceeded'))
  } catch (err) {
    setSiteMessage(site.id, 'error', formatSiteError(err, t('fallbackHtml.publishFailed')))
  }
}

async function rollbackSite(site: Site) {
  clearSiteMessage(site.id)
  try {
    apiObj<unknown>(await api.post(`api/components/fallback-html/sites/${site.id}/rollback`, {
      version: rollbackSelectionBySite[site.id] || '',
    }, jsonPost))
    await loadSites()
    setSiteMessage(site.id, 'success', t('fallbackHtml.rollbackSucceeded'))
  } catch (err) {
    setSiteMessage(site.id, 'error', formatSiteError(err, t('fallbackHtml.rollbackFailed')))
  }
}

async function unpublishSite(site: Site) {
  clearSiteMessage(site.id)
  try {
    apiObj<unknown>(await api.post(`api/components/fallback-html/sites/${site.id}/unpublish`))
    await loadSites()
    setSiteMessage(site.id, 'success', t('fallbackHtml.unpublishSucceeded'))
  } catch (err) {
    setSiteMessage(site.id, 'error', formatSiteError(err, t('fallbackHtml.unpublishFailed')))
  }
}

async function prunePublishes(site: Site) {
  clearSiteMessage(site.id)
  try {
    const keep = Number(pruneKeepBySite[site.id] ?? 2)
    const result = apiObj<PrunePublishesResult>(await api.post(`api/components/fallback-html/sites/${site.id}/publishes/prune`, { keep }, jsonPost))
    await loadPublishes(site)
    setSiteMessage(site.id, 'success', t('fallbackHtml.pruneSucceeded', { removed: result.removed, kept: result.kept }))
  } catch (err) {
    setSiteMessage(site.id, 'error', formatSiteError(err, t('fallbackHtml.pruneFailed')))
  }
}

function rollbackVersions(site: Site): PublishView[] {
  return (publishesBySite[site.id] ?? []).filter(publish => !publish.active)
}

function activePublish(site: Site): PublishView | undefined {
  return (publishesBySite[site.id] ?? []).find(publish => publish.active)
}

function formatUnix(value: number): string {
  if (!value) return '-'
  return new Date(value * 1000).toLocaleString()
}

function pathPreview(value: string): string {
  return normalizePublicPathPreview(value)
}

function pathIsReserved(value: string): boolean {
  return isReservedPublicPath(value)
}

function isExternalTarget(value: string): boolean {
  return isExternalURL(value)
}

function targetDraft(site: Site): TargetView {
  if (!targetDraftsBySite[site.id]) {
    targetDraftsBySite[site.id] = cloneTarget(targetsBySite[site.id]?.[0] ?? defaultTargetDraft())
  }
  return targetDraftsBySite[site.id]
}

function defaultTargetDraft(): TargetView {
  return {
    id: 0,
    kind: 'standalone',
    host: '',
    listen: defaultExactListen(),
    port: 443,
    rootPath: '/',
    runtime: 'gin',
    tls: false,
    status: '',
    reason: '',
    current: false,
  }
}

function cloneTarget(target: TargetView): TargetView {
  return { ...defaultTargetDraft(), ...target }
}

function defaultTargetPayload() {
  return {
    kind: 'standalone',
    listen: defaultExactListen(),
    port: 443,
    rootPath: '/',
    runtime: 'gin',
    tls: false,
  }
}

function targetPayloadFromDraft(draft: TargetView) {
  return {
    id: draft.id ?? 0,
    kind: draft.kind || 'standalone',
    host: draft.host || '',
    listen: draft.listen || defaultExactListen(),
    port: Number(draft.port) || 443,
    rootPath: draft.rootPath || '/',
    runtime: draft.runtime || 'gin',
    tls: !!draft.tls,
  }
}

function defaultExactListen(): string {
  const host = window.location.hostname || '127.0.0.1'
  return host === '0.0.0.0' || host === '::' || host === '[::]' ? '127.0.0.1' : host
}

function setSiteMessage(siteId: number, type: 'success' | 'info' | 'warning' | 'error', text: string) {
  siteMessages[siteId] = { type, text }
}

function clearSiteMessage(siteId: number) {
  delete siteMessages[siteId]
}

function formatSiteError(err: unknown, fallback: string): string {
  if (typeof err === 'object' && err && 'response' in err) {
    const response = (err as { response?: { data?: { msg?: string, error?: string } } }).response
    const message = response?.data?.msg || response?.data?.error
    if (message) return message
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}

onMounted(loadSites)
</script>

<style scoped>
.fallback-preview-frame {
  width: 100%;
  min-height: 640px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
  background: #fff;
}

.fallback-asset-input {
  color: rgb(var(--v-theme-on-surface));
}

.fallback-target-reason {
  margin-left: 0.4rem;
  opacity: 0.8;
}

.fallback-template-card {
  min-width: 240px;
}

.fallback-template-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
}

.fallback-template-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 0.95rem;
}

.fallback-template-actions {
  flex-wrap: wrap;
  gap: 6px;
}

.fallback-site-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.fallback-site-card {
  width: 100%;
}

.fallback-site-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.fallback-site-heading {
  display: flex;
  flex: 1 1 auto;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.fallback-site-heading > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fallback-site-actions {
  gap: 8px;
}

.fallback-site-actions .v-btn {
  min-width: max-content;
}

.fallback-prune-keep {
  max-width: 190px;
}

.fallback-section {
  padding: 12px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 10px;
}

.fallback-page-card {
  background: rgba(var(--v-theme-surface), 0.5);
}

.fallback-body-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 48px;
  padding: 10px 12px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
}

.fallback-body-summary span {
  overflow: hidden;
  color: rgba(var(--v-theme-on-surface), 0.76);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fallback-body-editor :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Consolas, Liberation Mono, Menlo, monospace;
  line-height: 1.45;
}

.fallback-path-hint {
  margin-top: -0.55rem;
  margin-bottom: 0.35rem;
  font-size: 0.75rem;
  opacity: 0.78;
}
</style>
