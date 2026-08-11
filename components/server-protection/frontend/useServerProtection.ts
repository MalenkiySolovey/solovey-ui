import { computed, onMounted, ref } from 'vue'
import { protectionAPI } from './api'
import type {
  Diagnostics,
  FirewallPreview,
	FirewallWorkflowResult,
  GraylistEntry,
  IPAllowlistEntry,
  Inventory,
	OperationsState,
  ProbeEvent,
  Profile,
  PortAllowlistEntry,
  ProtectionSettings,
  ProtectionStatus,
  ProtectableResource,
} from './types'

interface Page<T> { items: T[]; page: number; limit: number; total: number }
interface SettingsResponse { settings: ProtectionSettings; defaults: ProtectionSettings; revision: number; degraded: boolean }

export function useServerProtection() {
  const tab = ref('overview')
  const loading = ref(false)
  const error = ref('')
  const status = ref<ProtectionStatus>()
  const inventory = ref<Inventory>({ generatedAt: 0, resources: [] })
  const profiles = ref<Profile[]>([])
  const events = ref<ProbeEvent[]>([])
  const graylist = ref<GraylistEntry[]>([])
  const diagnostics = ref<Diagnostics>()
  const settings = ref<ProtectionSettings>()
  const settingsRevision = ref(0)
  const firewallMessage = ref('')
  const firewallPreview = ref<FirewallPreview>()
  const portAllowlist = ref<PortAllowlistEntry[]>([])
  const ipAllowlist = ref<IPAllowlistEntry[]>([])
	const operations = ref<OperationsState>({ items: [], recoveryRequired: 0, confirmationTemplates: {} })
  const newPort = ref({ protocol: 'tcp' as 'tcp' | 'udp', listen: '*', portStart: 22, portEnd: 22, reason: 'SSH administration' })
  const newIP = ref({ ipCidr: '', reason: 'Administrator address' })
  const loadedTabs = new Set<string>()

  const profileByResource = computed(() => new Map(profiles.value.map(profile => [profile.resourceId, profile])))

  const run = async (action: () => Promise<void>) => {
    loading.value = true
    error.value = ''
    try {
      await action()
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      loading.value = false
    }
  }

  const loadStatus = async () => { status.value = await protectionAPI.get<ProtectionStatus>('/status') }
  const loadResources = async (refresh = false) => { inventory.value = await protectionAPI.get<Inventory>('/resources', { refresh }) }
  const loadProfiles = async () => { profiles.value = (await protectionAPI.get<Page<Profile>>('/profiles', { limit: 200 })).items }
  const loadEvents = async () => { events.value = (await protectionAPI.get<Page<ProbeEvent> & { droppedCount: number }>('/events', { limit: 100 })).items }
  const loadGraylist = async () => { graylist.value = (await protectionAPI.get<Page<GraylistEntry>>('/graylist', { limit: 100 })).items }
  const loadDiagnostics = async (refresh = false) => { diagnostics.value = await protectionAPI.get<Diagnostics>('/diagnostics', { refresh }) }
  const loadSettings = async () => {
    const response = await protectionAPI.get<SettingsResponse>('/settings')
    settings.value = response.settings
    settingsRevision.value = response.revision
  }
  const loadAllowlists = async () => {
    const [ports, ips] = await Promise.all([
      protectionAPI.get<Page<PortAllowlistEntry>>('/allowlist/ports', { limit: 200 }),
      protectionAPI.get<Page<IPAllowlistEntry>>('/allowlist/ips', { limit: 200 }),
    ])
    portAllowlist.value = ports.items
    ipAllowlist.value = ips.items
  }
	const loadOperations = async () => { operations.value = await protectionAPI.get<OperationsState>('/operations') }
  const loadInitial = () => run(async () => {
		await Promise.all([loadStatus(), loadResources(), loadProfiles(), loadOperations()])
    loadedTabs.add('overview')
    loadedTabs.add('resources')
    loadedTabs.add('profiles')
  })

  const activateTab = (value: string) => run(async () => {
    tab.value = value
    if (loadedTabs.has(value)) return
    if (value === 'observations') await Promise.all([loadEvents(), loadGraylist()])
    if (value === 'diagnostics') await loadDiagnostics()
    if (value === 'settings') await loadSettings()
    if (value === 'firewall') await loadAllowlists()
		if (value === 'recovery') await loadOperations()
    loadedTabs.add(value)
  })

  const refreshResources = () => run(async () => {
    await Promise.all([loadStatus(), loadResources(true), loadProfiles()])
  })

  const createProfile = (resource: ProtectableResource, mode: 'record_only' | 'metadata_only') => run(async () => {
    await protectionAPI.post<Profile>('/profiles', {
      resourceId: resource.id,
      resourceRevision: resource.fingerprint,
      mode,
      enabled: true,
      defaultAction: 'record_only',
    })
    await Promise.all([loadProfiles(), loadStatus()])
  })

  const setProfileEnabled = (profile: Profile, enabled: boolean) => run(async () => {
    await protectionAPI.put<Profile>(`/profiles/${profile.id}`, {
      mode: profile.mode,
      enabled,
      scoreThreshold: profile.scoreThreshold,
      graylistTtlSeconds: profile.graylistTtlSeconds,
      defaultAction: profile.defaultAction,
      revision: profile.revision,
    })
    await loadProfiles()
  })

  const removeProfile = (profile: Profile) => run(async () => {
    await protectionAPI.delete(`/profiles/${profile.id}`)
    await Promise.all([loadProfiles(), loadStatus()])
  })

  const clearEvents = () => run(async () => {
    await protectionAPI.delete('/events')
    await Promise.all([loadEvents(), loadStatus()])
  })

  const clearGraylist = () => run(async () => {
    await protectionAPI.delete('/graylist')
    await loadGraylist()
  })

  const requestFirewallPreview = () => run(async () => {
    firewallMessage.value = ''
    firewallPreview.value = undefined
    try {
      firewallPreview.value = await protectionAPI.post<FirewallPreview>('/firewall/preview', { includeGeneratedNft: true })
    } catch (reason) {
      firewallMessage.value = reason instanceof Error ? reason.message : String(reason)
    }
  })

  const addPortAllowlist = () => run(async () => {
    await protectionAPI.post('/allowlist/ports', newPort.value)
    await loadAllowlists()
    firewallPreview.value = undefined
  })

  const removePortAllowlist = (id: number) => run(async () => {
    await protectionAPI.delete(`/allowlist/ports/${id}`)
    await loadAllowlists()
    firewallPreview.value = undefined
  })

  const addIPAllowlist = () => run(async () => {
    await protectionAPI.post('/allowlist/ips', newIP.value)
    newIP.value.ipCidr = ''
    await loadAllowlists()
  })

  const removeIPAllowlist = (id: number) => run(async () => {
    await protectionAPI.delete(`/allowlist/ips/${id}`)
    await loadAllowlists()
  })

  const saveSettings = () => run(async () => {
    if (!settings.value) return
    const response = await protectionAPI.put<{ settings: ProtectionSettings; revision: number }>('/settings', {
      settings: settings.value,
      revision: settingsRevision.value,
    })
    settings.value = response.settings
    settingsRevision.value = response.revision
    await loadStatus()
  })

  const refreshDiagnostics = () => run(async () => { await loadDiagnostics(true) })

  const runMockFirewallWorkflow = () => run(async () => {
    if (!firewallPreview.value) return
    const revision = firewallPreview.value.revision
    const idempotencyKey = globalThis.crypto?.randomUUID?.() ?? `ui-${Date.now()}-${Math.random().toString(16).slice(2)}`
    const prepared = await protectionAPI.post<{ operation: { operationId: string } }>('/firewall/prepare', {
      planRevision: revision, idempotencyKey, confirmation: `PREPARE SERVER PROTECTION ${revision}`,
    })
    const result = await protectionAPI.post<FirewallWorkflowResult>('/firewall/apply', {
      operationId: prepared.operation.operationId,
      confirmation: `APPLY SERVER PROTECTION ${prepared.operation.operationId}`,
    })
    firewallMessage.value = `Managed firewall apply finished: ${result.state}`
    await Promise.all([loadOperations(), loadStatus(), loadDiagnostics(true)])
  })

  const rollbackMockOperation = (operationId: string) => run(async () => {
    const result = await protectionAPI.post<FirewallWorkflowResult>('/firewall/rollback', {
      operationId, confirmation: `ROLLBACK SERVER PROTECTION ${operationId}`,
    })
    firewallMessage.value = `Managed firewall rollback finished: ${result.state}`
    await Promise.all([loadOperations(), loadStatus()])
  })

  onMounted(loadInitial)

  return {
			 tab, loading, error, status, inventory, profiles, events, graylist, diagnostics, settings, operations,
    firewallMessage, firewallPreview, portAllowlist, ipAllowlist, newPort, newIP, profileByResource, activateTab, refreshResources, createProfile, setProfileEnabled,
    removeProfile, clearEvents, clearGraylist, requestFirewallPreview, saveSettings, refreshDiagnostics,
			 addPortAllowlist, removePortAllowlist, addIPAllowlist, removeIPAllowlist, runMockFirewallWorkflow, rollbackMockOperation,
  }
}

export function stateColor(value?: string): string {
  switch (value) {
		case 'supported': case 'active': case 'ok': case 'APPLIED': case 'ROLLED_BACK': case 'MANAGED_ENGINE_READY': return 'success'
		case 'degraded': case 'stale': case 'warning': case 'prepared': case 'abandoned': case 'PREPARED': case 'APPLYING': case 'HEALTH': case 'ROLLING_BACK': case 'DEGRADED': return 'warning'
		case 'unsupported': case 'error': case 'rollback_failed': case 'health_failed': case 'ROLLBACK_FAILED': case 'RECONCILE_REQUIRED': return 'error'
		case 'applying': case 'rolling_back': case 'lock_suspect': return 'warning'
    case 'missing_capability': case 'preview_only': return 'info'
    default: return 'grey'
  }
}
