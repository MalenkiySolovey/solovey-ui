import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '@/plugins/api'
import {
  endpointChipColor,
  endpointChipTitle,
  endpointLabel,
  endpointStatusText,
  isExternalURL,
  isReservedPublicPath,
  normalizePublicPathPreview,
} from './publicSiteLogic'

import type {
  Page,
  Redirect,
  AssetView,
  ExternalResourceView,
  Site,
  TargetView,
  PortCandidate,
  SafetyReport,
  PreviewResult,
  TemplateDefinition,
  RemoteTemplateView,
  PublishView,
  PrunePublishesResult,
  ProviderStatusView,
} from './publicSiteTypes'

export function usePublicSite() {
  const { t } = useI18n()
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
  const providerStatusBySite = reactive<Record<number, ProviderStatusView>>({})
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
      await Promise.all(sites.value.flatMap(site => [loadTargets(site), loadProviderStatus(site), loadAssets(site), loadExternalResources(site), loadPublishes(site)]))
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

  async function loadProviderStatus(site: Site) {
    providerStatusBySite[site.id] = apiObj<ProviderStatusView>(
      await api.get(`api/components/fallback-html/sites/${site.id}/provider-status`),
    )
  }

  function providerStatus(site: Site): ProviderStatusView {
    return providerStatusBySite[site.id] ?? {
      targetId: `site:${site.id}`,
      endpointMode: 'UNKNOWN',
      readiness: 'UNKNOWN',
      healthFreshness: 'UNKNOWN',
      healthObservedAt: 0,
      healthExpiresAt: 0,
      capacityState: 'UNKNOWN',
      capacitySlotsUsed: 0,
      capacitySlotsTotal: 4,
      inUse: false,
      reconcileRequired: false,
      reservations: [],
      reasonCodes: ['provider_status_unavailable'],
    }
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

  return {
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
  }
}
