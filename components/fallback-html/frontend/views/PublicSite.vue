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
            <v-card class="mb-4 fallback-section" variant="outlined" role="status" aria-live="polite">
              <v-card-title class="text-subtitle-2">{{ t('fallbackHtml.providerAuthority') }}</v-card-title>
              <v-card-text>
                <v-alert type="info" variant="tonal" density="compact" class="mb-3">
                  {{ t('fallbackHtml.nativeFallbackManagedElsewhere') }}
                </v-alert>
                <div class="d-flex ga-2 flex-wrap mb-3">
                  <v-chip size="small">{{ providerStatus(site).targetId }}</v-chip>
                  <v-chip size="small">{{ t('fallbackHtml.endpointMode') }}: {{ providerStatus(site).endpointMode }}</v-chip>
                  <v-chip size="small">{{ t('fallbackHtml.readiness') }}: {{ providerStatus(site).readiness }}</v-chip>
                  <v-chip size="small">{{ t('fallbackHtml.healthFreshness') }}: {{ providerStatus(site).healthFreshness }}</v-chip>
                  <v-chip size="small">
                    {{ t('fallbackHtml.capacity') }}:
                    {{ providerStatus(site).capacitySlotsUsed }}/{{ providerStatus(site).capacitySlotsTotal }}
                    ({{ providerStatus(site).capacityState }})
                  </v-chip>
                  <v-chip size="small" :color="providerStatus(site).inUse ? 'warning' : 'success'">
                    {{ providerStatus(site).inUse ? t('fallbackHtml.reservedInUse') : t('fallbackHtml.notReserved') }}
                  </v-chip>
                  <v-chip v-if="providerStatus(site).reconcileRequired" size="small" color="error">
                    {{ t('fallbackHtml.reconcileRequired') }}
                  </v-chip>
                </div>
                <v-alert v-if="providerStatus(site).reasonCodes.length" type="warning" variant="tonal" density="compact" class="mb-3">
                  <div v-for="reason in providerStatus(site).reasonCodes" :key="reason">{{ reason }}</div>
                </v-alert>
              </v-card-text>
            </v-card>

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
import { usePublicSite } from '../usePublicSite'

const {
  t,
  sites,
  templates,
  remoteTemplates,
  portCandidates,
  loading,
  catalogLoading,
  installingTemplate,
  error,
  catalogError,
  previewOpen,
  previewLoading,
  previewHtml,
  previewPath,
  previewWarnings,
  previewError,
  importOpen,
  importSiteRef,
  importText,
  targetsBySite,
  targetDraftsBySite,
  siteMessages,
  collapsedSites,
  assetsBySite,
  externalResourcesBySite,
  publishesBySite,
  pruneKeepBySite,
  rollbackSelectionBySite,
  safetyBySite,
  providerStatusBySite,
  bodyEditorOpen,
  bodyEditorPageRef,
  bodyEditorText,
  redirectStatuses,
  externalResourceKinds,
  contentModes,
  endpointChipColor,
  endpointChipTitle,
  endpointLabel,
  endpointStatusText,
  siteCollapsed,
  toggleSiteCollapse,
  siteStatusColor,
  siteStatusText,
  publicTargetSummary,
  templateAffectsSite,
  templateSelectItems,
  templateHelp,
  loadSites,
  loadTemplateCatalog,
  installRemoteTemplate,
  deleteRemoteTemplate,
  createSite,
  createSiteFromTemplate,
  saveSite,
  saveSiteMetadata,
  deleteSite,
  addPage,
  addRedirect,
  openImport,
  openBodyEditor,
  applyBodyEditor,
  applyImport,
  savePage,
  saveRedirect,
  saveTarget,
  useCurrentPanelTarget,
  deletePage,
  deleteRedirect,
  uploadAsset,
  deleteAsset,
  addExternalResource,
  saveExternalResource,
  deleteExternalResource,
  checkSafety,
  previewSite,
  openPublishedSite,
  publicSiteURL,
  publishSite,
  rollbackSite,
  unpublishSite,
  prunePublishes,
  rollbackVersions,
  activePublish,
  formatUnix,
  pathPreview,
  pathIsReserved,
  isExternalTarget,
  targetDraft,
  providerStatus,
} = usePublicSite()
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
