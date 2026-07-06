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

    <v-dialog v-model="previewOpen" max-width="960">
      <v-card>
        <v-card-title class="d-flex align-center justify-space-between">
          <span>{{ t('fallbackHtml.preview') }} {{ previewPath }}</span>
          <v-btn icon="mdi-close" variant="text" @click="previewOpen = false" />
        </v-card-title>
        <v-card-text>
          <v-alert v-if="previewWarnings.length" type="warning" variant="tonal" class="mb-3">
            <div v-for="warning in previewWarnings" :key="warning">{{ warning }}</div>
          </v-alert>
          <iframe class="fallback-preview-frame" :srcdoc="previewHtml" :title="t('fallbackHtml.preview')" />
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

    <v-card class="mb-4" variant="tonal">
      <v-card-title>{{ t('fallbackHtml.targets') }}</v-card-title>
      <v-card-text>
        <v-chip
          v-for="candidate in portCandidates"
          :key="`${candidate.kind}-${candidate.listen}-${candidate.port}`"
          class="me-2 mb-2"
          size="small"
          :color="endpointChipColor(candidate.status)"
          :title="endpointChipTitle(candidate)"
        >
          {{ endpointLabel(candidate) }}
          <span class="fallback-target-reason">{{ endpointStatusText(candidate) }}</span>
        </v-chip>
        <div v-if="!portCandidates.length" class="text-medium-emphasis">
          {{ t('fallbackHtml.noTargets') }}
        </div>
      </v-card-text>
    </v-card>

    <v-card class="mb-4" variant="tonal">
      <v-card-title>{{ t('fallbackHtml.runtimes') }}</v-card-title>
      <v-card-text>
        <v-chip
          v-for="option in runtimeOptions"
          :key="option.id"
          class="me-2 mb-2"
          size="small"
          :color="endpointChipColor(option.status)"
          :title="option.reason"
        >
          {{ option.label }}
          <span class="fallback-target-reason">
            {{ option.status }}<template v-if="option.nodeSide"> / {{ t('fallbackHtml.nodeSide') }}</template>
          </span>
        </v-chip>
        <div v-if="!runtimeOptions.length" class="text-medium-emphasis">
          {{ t('fallbackHtml.noRuntimes') }}
        </div>
      </v-card-text>
    </v-card>

    <v-card class="mb-4" variant="tonal">
      <v-card-title class="d-flex align-center justify-space-between">
        <span>{{ t('fallbackHtml.templateCatalog') }}</span>
        <v-btn size="small" variant="tonal" prepend-icon="mdi-cloud-sync" :loading="catalogLoading" @click="loadTemplateCatalog">
          {{ t('fallbackHtml.refreshCatalog') }}
        </v-btn>
      </v-card-title>
      <v-card-text>
        <v-alert type="info" variant="tonal" class="mb-3">
          {{ t('fallbackHtml.templateCatalogHelp') }}
        </v-alert>
        <v-alert v-if="catalogError" type="error" variant="tonal" class="mb-3">{{ catalogError }}</v-alert>
        <div class="d-flex ga-3 flex-wrap">
          <v-card v-for="template in remoteTemplates" :key="template.id" class="fallback-template-card" variant="outlined">
            <v-card-title class="text-subtitle-1">
              {{ template.name }}
              <v-chip size="x-small" class="ms-2" :color="template.installed ? 'success' : 'grey'">
                {{ template.installed ? t('fallbackHtml.installed') : t('fallbackHtml.available') }}
              </v-chip>
            </v-card-title>
            <v-card-text>
              <div class="text-caption text-medium-emphasis">{{ template.contentTypeProfile }}</div>
              <div class="text-caption">{{ template.license }}</div>
              <div class="text-caption text-medium-emphasis fallback-source">{{ template.source }}</div>
            </v-card-text>
            <v-card-actions>
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
              <v-btn size="small" color="error" variant="text" prepend-icon="mdi-delete" :disabled="!template.installed" @click="deleteRemoteTemplate(template)">
                {{ t('fallbackHtml.deleteTemplate') }}
              </v-btn>
            </v-card-actions>
          </v-card>
        </div>
        <div v-if="!remoteTemplates.length && !catalogLoading" class="text-medium-emphasis">
          {{ t('fallbackHtml.noRemoteTemplates') }}
        </div>
      </v-card-text>
    </v-card>

    <v-card class="mb-4" variant="tonal">
      <v-card-title>{{ t('fallbackHtml.nodeEndpoints') }}</v-card-title>
      <v-card-text>
        <v-alert type="info" variant="tonal" class="mb-3">
          {{ t('fallbackHtml.nodeEndpointHelp') }}
        </v-alert>
        <v-row density="compact">
          <v-col cols="12" md="3">
            <v-text-field v-model="nodeEndpointDraft.nodeId" :label="t('fallbackHtml.nodeId')" density="compact" />
          </v-col>
          <v-col cols="12" md="4">
            <v-text-field v-model="nodeEndpointDraft.baseUrl" :label="t('fallbackHtml.nodeBaseUrl')" density="compact" />
          </v-col>
          <v-col cols="12" md="2">
            <v-select v-model="nodeEndpointDraft.runtime" :items="nodeRuntimeItems" :label="t('fallbackHtml.runtime')" density="compact" />
          </v-col>
          <v-col cols="12" md="2">
            <v-text-field v-model="nodeEndpointDraft.sharedSecret" :label="t('fallbackHtml.sharedSecret')" density="compact" type="password" />
          </v-col>
          <v-col cols="12" md="1" class="d-flex align-center">
            <v-btn color="primary" variant="tonal" prepend-icon="mdi-server-plus" @click="saveNodeEndpoint">
              {{ t('fallbackHtml.registerNode') }}
            </v-btn>
          </v-col>
        </v-row>
        <div class="d-flex ga-2 flex-wrap">
          <v-chip
            v-for="node in nodeEndpoints"
            :key="node.nodeId"
            size="small"
            :color="node.enabled ? 'success' : 'grey'"
            closable
            @click:close="deleteNodeEndpoint(node)"
          >
            {{ node.nodeId }}
            <span class="fallback-target-reason">{{ node.runtime }} / {{ node.baseUrl }}</span>
            <span v-if="node.hasSharedSecret" class="fallback-target-reason"> / {{ t('fallbackHtml.signedOperation') }}</span>
          </v-chip>
        </div>
        <div v-if="!nodeEndpoints.length" class="text-caption text-medium-emphasis">
          {{ t('fallbackHtml.noNodeEndpoints') }}
        </div>
      </v-card-text>
    </v-card>

    <v-row>
      <v-col v-for="site in sites" :key="site.id" cols="12" lg="6">
        <v-card>
          <v-card-title class="d-flex align-center justify-space-between">
            <span>{{ site.name }}</span>
            <v-chip size="small" :color="site.status === 'published' ? 'success' : 'grey'">
              {{ site.status }}
            </v-chip>
          </v-card-title>
          <v-card-text>
            <v-text-field v-model="site.name" :label="t('fallbackHtml.siteName')" density="compact" />
            <v-select
              v-model="site.templateId"
              :items="templates"
              item-title="name"
              item-value="id"
              :label="t('fallbackHtml.template')"
              density="compact"
            />
            <v-switch v-model="site.enabled" :label="t('fallbackHtml.enabled')" color="primary" hide-details />

            <v-divider class="my-4" />
            <div class="mb-4">
              <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.publishTarget') }}</div>
              <v-chip
                v-for="target in targetsBySite[site.id] || []"
                :key="target.id || `${target.kind}-${target.port}`"
                class="me-2 mb-2"
                size="small"
                :color="endpointChipColor(target.status)"
                :title="endpointChipTitle(target)"
              >
                {{ endpointLabel(target) }}
                <span class="fallback-target-reason">{{ endpointStatusText(target) }}</span>
              </v-chip>
              <v-btn size="small" variant="tonal" prepend-icon="mdi-sync" @click="syncTarget(site)">
                {{ t('fallbackHtml.syncTarget') }}
              </v-btn>
            </div>

            <v-divider class="my-4" />
            <div v-for="page in site.pages" :key="page.id" class="mb-4">
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
                  <v-textarea v-model="page.body" :label="t('fallbackHtml.body')" rows="3" density="compact" />
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
            </div>
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

            <v-alert v-if="selfStealDraftBySite[site.id]" class="mt-4" type="info" variant="tonal">
              <div class="font-weight-bold">
                {{ t('fallbackHtml.selfStealDraft') }}: {{ selfStealDraftBySite[site.id].status }}
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
            <div class="text-subtitle-2 mb-2">{{ t('fallbackHtml.publishes') }}</div>
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
            <div class="mt-3">
              <div class="text-caption text-medium-emphasis mb-1">{{ t('fallbackHtml.nodePublications') }}</div>
              <v-chip
                v-for="publication in nodePublicationsBySite[site.id] || []"
                :key="publication.id || `${publication.nodeId}-${publication.publishVersion}`"
                class="me-2 mb-2"
                size="small"
                :color="endpointChipColor(publication.status)"
                :title="publication.lastError || publication.artifactSha256"
              >
                {{ publication.nodeId || t('fallbackHtml.nodeUnknown') }}
                <span class="fallback-target-reason">{{ publication.status }} / {{ publication.runtime }}</span>
              </v-chip>
              <div v-if="!(nodePublicationsBySite[site.id] || []).length" class="text-caption text-medium-emphasis">
                {{ t('fallbackHtml.noNodePublications') }}
              </div>
              <v-row class="mt-2" density="compact">
                <v-col cols="12" md="8">
                  <v-select
                    v-model="nodeSelectionBySite[site.id]"
                    :items="nodeEndpoints.filter(node => node.enabled)"
                    item-title="nodeId"
                    item-value="nodeId"
                    :label="t('fallbackHtml.selectNode')"
                    density="compact"
                    :disabled="!nodeEndpoints.length"
                  />
                </v-col>
                <v-col cols="12" md="4" class="d-flex align-center">
                  <v-btn
                    variant="tonal"
                    color="primary"
                    prepend-icon="mdi-cloud-upload-outline"
                    :loading="applyingNodeBySite[site.id]"
                    :disabled="!activePublish(site) || !nodeSelectionBySite[site.id]"
                    @click="applyPublishToNode(site)"
                  >
                    {{ t('fallbackHtml.applyToNode') }}
                  </v-btn>
                </v-col>
              </v-row>
              <v-alert v-if="nodePlanBySite[site.id]" class="mt-2" type="info" variant="tonal">
                <div class="font-weight-bold">{{ t('fallbackHtml.nodePlan') }}</div>
                <div>{{ nodePlanBySite[site.id].artifact.filename }}</div>
                <div>sha256: {{ nodePlanBySite[site.id].artifact.sha256 }}</div>
                <div>{{ t('fallbackHtml.signatureMode') }}: {{ nodePlanBySite[site.id].signature.mode }}</div>
              </v-alert>
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
          <v-card-actions class="justify-end">
            <v-btn variant="tonal" prepend-icon="mdi-database-import" @click="openImport(site)">
              {{ t('fallbackHtml.importJson') }}
            </v-btn>
            <v-btn variant="tonal" prepend-icon="mdi-server-network" :disabled="!activePublish(site)" @click="loadNodePlan(site)">
              {{ t('fallbackHtml.nodePlan') }}
            </v-btn>
            <v-btn
              variant="tonal"
              prepend-icon="mdi-shield-outline"
              :disabled="!canCreateSelfStealDraft(site)"
              @click="createSelfStealDraft(site)"
            >
              {{ t('fallbackHtml.selfStealDraft') }}
            </v-btn>
            <v-btn variant="tonal" prepend-icon="mdi-shield-search" @click="checkSafety(site)">
              {{ t('fallbackHtml.safety') }}
            </v-btn>
            <v-btn variant="tonal" prepend-icon="mdi-eye" @click="previewSite(site)">
              {{ t('fallbackHtml.preview') }}
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
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>
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

interface RuntimeOption {
  id: string
  label: string
  status: string
  reason: string
  nodeSide: boolean
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
    inboundType?: string
    inboundTag?: string
    inboundCandidate?: unknown
    warnings: string[]
    blocks: string[]
    nextSteps: string[]
  }
  createdAt: number
}

interface NodePublicationView {
  id: number
  siteId: number
  nodeId: string
  publishVersion: string
  runtime: string
  status: string
  artifactSha256?: string
  operationId?: string
  lastError?: string
  createdAt?: number
  updatedAt?: number
  appliedAt?: number
}

interface NodeEndpointView {
  id: number
  nodeId: string
  baseUrl: string
  runtime: string
  hasSharedSecret: boolean
  enabled: boolean
  createdAt: number
  updatedAt: number
}

interface NodePublishPlan {
  schema: string
  siteId: number
  nodeId?: string
  version: string
  artifact: {
    filename: string
    contentType: string
    sha256: string
    sizeBytes: number
  }
  signature: {
    mode: string
    required: boolean
  }
}

const { t } = useI18n()
const router = useRouter()
const sites = ref<Site[]>([])
const templates = ref<TemplateDefinition[]>([])
const remoteTemplates = ref<RemoteTemplateView[]>([])
const portCandidates = ref<PortCandidate[]>([])
const runtimeOptions = ref<RuntimeOption[]>([])
const nodeEndpoints = ref<NodeEndpointView[]>([])
const loading = ref(false)
const catalogLoading = ref(false)
const installingTemplate = ref('')
const error = ref('')
const catalogError = ref('')
const previewOpen = ref(false)
const previewHtml = ref('')
const previewPath = ref('')
const previewWarnings = ref<string[]>([])
const importOpen = ref(false)
const importSiteRef = ref<Site | null>(null)
const importText = ref('')
const targetsBySite = reactive<Record<number, TargetView[]>>({})
const assetsBySite = reactive<Record<number, AssetView[]>>({})
const externalResourcesBySite = reactive<Record<number, ExternalResourceView[]>>({})
const publishesBySite = reactive<Record<number, PublishView[]>>({})
const nodePublicationsBySite = reactive<Record<number, NodePublicationView[]>>({})
const nodePlanBySite = reactive<Record<number, NodePublishPlan>>({})
const nodeSelectionBySite = reactive<Record<number, string>>({})
const applyingNodeBySite = reactive<Record<number, boolean>>({})
const rollbackSelectionBySite = reactive<Record<number, string>>({})
const safetyBySite = reactive<Record<number, SafetyReport>>({})
const selfStealDraftBySite = reactive<Record<number, SelfStealDraftView>>({})
const jsonPost = { headers: { 'Content-Type': 'application/json' } }
const redirectStatuses = [301, 302, 307, 308]
const externalResourceKinds = ['link', 'image', 'font']
const nodeRuntimeItems = ['gin', 'nginx', 'caddy']
const nodeEndpointDraft = reactive({
  nodeId: '',
  baseUrl: '',
  runtime: 'gin',
  sharedSecret: '',
})
const contentModes = [
  { title: 'Text', value: 'text' },
  { title: 'Safe HTML', value: 'html' },
  { title: 'Downloaded static HTML', value: 'static-html' },
]

const apiObj = <T>(response: { data: { obj: T } }) => response.data.obj

function normalizeSites(items: Site[]): Site[] {
  return items.map(site => ({
    ...site,
    templateId: site.templateId || 'generated-portal',
    pages: (site.pages ?? []).map(page => ({ ...page, contentMode: page.contentMode || 'text' })),
    redirects: site.redirects ?? [],
  }))
}

async function loadSites() {
  loading.value = true
  error.value = ''
  try {
    sites.value = normalizeSites(apiObj(await api.get('api/components/fallback-html/sites')) ?? [])
    templates.value = apiObj(await api.get('api/components/fallback-html/templates')) ?? []
    portCandidates.value = apiObj(await api.get('api/components/fallback-html/ports')) ?? []
    runtimeOptions.value = apiObj(await api.get('api/components/fallback-html/runtimes')) ?? []
    await loadNodeEndpoints()
    await loadTemplateCatalog()
    await Promise.all(sites.value.flatMap(site => [loadTargets(site), loadAssets(site), loadExternalResources(site), loadPublishes(site), loadNodePublications(site)]))
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

async function loadTargets(site: Site) {
  targetsBySite[site.id] = apiObj(await api.get(`api/components/fallback-html/sites/${site.id}/targets`)) ?? []
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
  const selected = rollbackSelectionBySite[site.id]
  const versions = publishes.filter(publish => !publish.active)
  if (!selected || !versions.some(publish => publish.version === selected)) {
    rollbackSelectionBySite[site.id] = versions[0]?.version ?? ''
  }
}

async function loadNodePublications(site: Site) {
  nodePublicationsBySite[site.id] = apiObj(await api.get(`api/components/fallback-html/sites/${site.id}/node-publications`)) ?? []
}

async function loadNodeEndpoints() {
  nodeEndpoints.value = apiObj(await api.get('api/components/fallback-html/nodes')) ?? []
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

async function saveNodeEndpoint() {
  await api.post('api/components/fallback-html/nodes', {
    nodeId: nodeEndpointDraft.nodeId,
    baseUrl: nodeEndpointDraft.baseUrl,
    runtime: nodeEndpointDraft.runtime,
    sharedSecret: nodeEndpointDraft.sharedSecret,
    enabled: true,
  }, jsonPost)
  nodeEndpointDraft.nodeId = ''
  nodeEndpointDraft.baseUrl = ''
  nodeEndpointDraft.runtime = 'gin'
  nodeEndpointDraft.sharedSecret = ''
  await loadNodeEndpoints()
}

async function deleteNodeEndpoint(node: NodeEndpointView) {
  await api.delete(`api/components/fallback-html/nodes/${encodeURIComponent(node.nodeId)}`)
  for (const siteID of Object.keys(nodeSelectionBySite)) {
    if (nodeSelectionBySite[Number(siteID)] === node.nodeId) {
      nodeSelectionBySite[Number(siteID)] = ''
    }
  }
  await loadNodeEndpoints()
}

async function applyPublishToNode(site: Site) {
  const publish = activePublish(site)
  const nodeId = nodeSelectionBySite[site.id]
  if (!publish || !nodeId) return
  applyingNodeBySite[site.id] = true
  try {
    await api.post(`api/components/fallback-html/sites/${site.id}/node-publications/${publish.version}/apply`, { nodeId }, jsonPost)
    await Promise.all([loadNodePublications(site), loadNodePlan(site)])
  } finally {
    applyingNodeBySite[site.id] = false
  }
}

async function createSite() {
  await api.post('api/components/fallback-html/sites', { name: 'Public site', enabled: true }, jsonPost)
  await loadSites()
}

async function createSiteFromTemplate(templateId: string) {
  await api.post(`api/components/fallback-html/templates/${encodeURIComponent(templateId)}/create-site`)
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

async function syncTarget(site: Site) {
  const current = targetsBySite[site.id]?.[0]
  const saved = apiObj<TargetView>(await api.post(`api/components/fallback-html/sites/${site.id}/targets`, {
    id: current?.id ?? 0,
    kind: 'web-current',
  }, jsonPost))
  targetsBySite[site.id] = [saved]
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
  safetyBySite[site.id] = apiObj(await api.post(`api/components/fallback-html/sites/${site.id}/safety`))
}

async function previewSite(site: Site) {
  const result = apiObj<PreviewResult>(await api.post(`api/components/fallback-html/sites/${site.id}/preview`, {}, jsonPost))
  previewHtml.value = result.html
  previewPath.value = result.path
  previewWarnings.value = result.warnings ?? []
  previewOpen.value = true
}

async function createSelfStealDraft(site: Site) {
  const draft = apiObj<SelfStealDraftView>(await api.post(`api/components/fallback-html/sites/${site.id}/self-steal/draft`, {}, jsonPost))
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

async function reviewSelfStealDraft(draft: SelfStealDraftView) {
  await Data().loadInboundDrafts()
  await router.push({ path: '/inbounds', query: { draft: String(draft.coreDraftId) } })
}

async function loadNodePlan(site: Site) {
  const publish = activePublish(site)
  if (!publish) return
  nodePlanBySite[site.id] = apiObj(await api.get(`api/components/fallback-html/sites/${site.id}/node-plan/${publish.version}`))
}

async function publishSite(site: Site) {
  await api.post(`api/components/fallback-html/sites/${site.id}/publish`)
  await loadSites()
}

async function rollbackSite(site: Site) {
  await api.post(`api/components/fallback-html/sites/${site.id}/rollback`, {
    version: rollbackSelectionBySite[site.id] || '',
  }, jsonPost)
  await loadSites()
}

async function unpublishSite(site: Site) {
  await api.post(`api/components/fallback-html/sites/${site.id}/unpublish`)
  await loadSites()
}

function rollbackVersions(site: Site): PublishView[] {
  return (publishesBySite[site.id] ?? []).filter(publish => !publish.active)
}

function activePublish(site: Site): PublishView | undefined {
  return (publishesBySite[site.id] ?? []).find(publish => publish.active)
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

onMounted(loadSites)
</script>

<style scoped>
.fallback-preview-frame {
  width: 100%;
  min-height: 560px;
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
  width: min(100%, 360px);
}

.fallback-source {
  overflow-wrap: anywhere;
}

.fallback-path-hint {
  margin-top: -0.55rem;
  margin-bottom: 0.35rem;
  font-size: 0.75rem;
  opacity: 0.78;
}
</style>
