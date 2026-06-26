import { computed, ref } from 'vue'
import { actionDnsRuleKeys, dnsRule } from '@/types/dns'
import { useManualDrag, type ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import {
  moveArrayItemKeepingSelection,
  moveManyArrayItemsKeepingSelection,
  removeArrayItemKeepingSelection,
  type ManualSortDirection,
  sortArrayByTextKeepingSelection,
} from '@/shared/composables/dragSelection/manualReorder'
import type { Column } from '@/components/nexus/data/dataTableColumns'
import type { RowAction } from '@/components/nexus/data/rowActions'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { useUiMode } from '@/uiMode/useUiMode'
import { isPresetManagedItem } from '@/components/presets/routingDnsPresets'
import { useDnsConfig } from '@/shared/composables/dns/useDnsConfig'

export const useDnsPage = () => {
  const { confirm } = useConfirm()

  const { mode } = useUiMode()

  const nexus = computed(() => mode.value === 'nexus')

  const {
    appConfig,
    applyPresetConfig,
    clients,
    dns,
    dnsRules,
    dnsServerTags,
    finalDns,
    inboundTags,
    loading,
    outboundTags,
    rslvdTags,
    ruleSets,
    saveConfig,
    stateChange,
    subtitle,
    t,
    tsTags,
  } = useDnsConfig()

  const search = ref('')

  const regionalPresetDrawer = ref(false)

  const presetSourceLabel = (item: any) => isPresetManagedItem(item) ? t('presets.presetManaged') : t('presets.custom')

  const dnsServersExpanded = ref(true)
  const dnsServerSelectMode = ref(false)
  const selectedDnsServerIndexes = ref<Array<number | string>>([])
  const dnsRuleSelectMode = ref(false)
  const selectedDnsRuleIndexes = ref<Array<number | string>>([])

  let delDnsOverlay = ref(new Array<boolean>)

  let delDnsRuleOverlay = ref(new Array<boolean>)

  const dnsModal = ref({
    visible: false,
    index: -1,
    data: "",
  })

  const showDnsModal = (index: number) => {
    dnsModal.value.index = index
    dnsModal.value.data = index == -1 ? '' : JSON.stringify(dns.value.servers[index])
    dnsModal.value.visible = true
  }

  const closeDnsModal = () => {
    dnsModal.value.visible = false
  }

  const saveDnsModal = (data:any) => {
    // New or Edit
    if (dnsModal.value.index == -1) {
      dns.value.servers.push(data)
    } else {
      dns.value.servers[dnsModal.value.index] = data
    }
    dnsModal.value.visible = false
  }

  const delDns = (index: number) => {
    const result = removeArrayItemKeepingSelection(dns.value.servers, index, selectedDnsServerIndexes.value)
    delDnsOverlay.value[index] = false
    selectedDnsServerIndexes.value = result.selectedIndexes
  }

  const dnsRuleModal = ref({
    visible: false,
    index: -1,
    data: "",
  })

  const showDnsRuleModal = (index: number) => {
    dnsRuleModal.value.index = index
    dnsRuleModal.value.data = index == -1 ? '' : JSON.stringify(dnsRules.value[index])
    dnsRuleModal.value.visible = true
  }

  const closeDnsRuleModal = () => {
    dnsRuleModal.value.visible = false
  }

  const saveDnsRuleModal = (data:dnsRule) => {
    // New or Edit
    if (dnsRuleModal.value.index == -1) {
      dnsRules.value.push(data)
    } else {
      dnsRules.value[dnsRuleModal.value.index] = data
    }
    dnsRuleModal.value.visible = false
  }

  const delDnsRule = (index: number) => {
    const result = removeArrayItemKeepingSelection(dnsRules.value, index, selectedDnsRuleIndexes.value)
    delDnsRuleOverlay.value[index] = false
    selectedDnsRuleIndexes.value = result.selectedIndexes
  }

  // ---- Nexus table projections (read-only; actions carry the array index) ----
  // _index keeps the ORIGINAL array index (edit/delete operate by index), so filter
  // AFTER mapping. Search matches tag/type/server (servers) and action/server (rules).
  const matchesSearch = (text: string): boolean => {
    const q = search.value.trim().toLowerCase()
    return !q || text.toLowerCase().includes(q)
  }

  const dnsServerRows = computed(() =>
    (dns.value?.servers ?? [])
      .map((s: any, i: number) => ({ ...s, _index: i }))
      .filter((s: any) => matchesSearch(`${s.tag ?? ''} ${s.type ?? ''} ${s.server ?? ''}`)))

  const dnsRuleRows = computed(() =>
    dnsRules.value
      .map((r: any, i: number) => ({
        ...r,
        _index: i,
        _rulesCount: r.rules ? r.rules.length : Object.keys(r).filter((k: string) => !actionDnsRuleKeys.includes(k)).length,
      }))
      .filter((r: any) => matchesSearch(`${r.action ?? ''} ${r.server ?? ''}`)))

  const serverColumns: Column<any>[] = [
    { key: 'tag', labelKey: 'objects.tag' },
    { key: 'type', labelKey: 'type' },
    { key: 'server', labelKey: 'dns.server' },
    { key: 'server_port', labelKey: 'in.port' },
    { key: 'tls', labelKey: 'objects.tls' },
    { key: 'source', labelKey: 'presets.source' },
  ]

  const ruleColumns: Column<any>[] = [
    { key: '_index', labelKey: 'table.rowNumber' },
    { key: 'type', labelKey: 'type' },
    { key: 'action', labelKey: 'admin.action' },
    { key: 'server', labelKey: 'dns.server' },
    { key: '_rulesCount', labelKey: 'pages.rules' },
    { key: 'invert', labelKey: 'rule.invert' },
    { key: 'source', labelKey: 'presets.source' },
  ]

  const serverActions = (item: any): RowAction[] => [
    { key: 'up', labelKey: 'table.moveUp', icon: 'lucide:arrow-up', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || item._index === 0 },
    { key: 'down', labelKey: 'table.moveDown', icon: 'lucide:arrow-down', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || item._index === (dns.value?.servers?.length ?? 0) - 1 },
    { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
    { key: 'del', labelKey: 'actions.del', icon: 'lucide:trash-2', tone: 'error', inline: true },
  ]

  const ruleActions = (item: any): RowAction[] => [
    { key: 'up', labelKey: 'table.moveUp', icon: 'lucide:arrow-up', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || item._index === 0 },
    { key: 'down', labelKey: 'table.moveDown', icon: 'lucide:arrow-down', inline: true, reserveSpace: true, hidden: search.value.trim().length > 0 || item._index === dnsRules.value.length - 1 },
    { key: 'edit', labelKey: 'actions.edit', icon: 'lucide:pencil', inline: true },
    { key: 'del', labelKey: 'actions.del', icon: 'lucide:trash-2', tone: 'error', inline: true },
  ]

  const handleServerAction = async (key: string, item: any) => {
    if (key === 'up') { moveDnsServer(item._index, -1); return }
    if (key === 'down') { moveDnsServer(item._index, 1); return }
    if (key === 'edit') { showDnsModal(item._index); return }
    if (key === 'del') {
      const ok = await confirm({ title: `${t('actions.del')} ${t('objects.dnsserver')}`, message: item.tag, confirmLabel: t('actions.del'), tone: 'error' })
      if (ok) delDns(item._index)
    }
  }

  const moveDnsServer = (index: number, dir: number) => {
    moveDnsServerTo(index, index + dir)
  }

  const preserveImplicitDnsFinal = () => {
    const servers = dns.value?.servers ?? []
    if (!dns.value?.final && servers[0]?.tag) dns.value.final = servers[0].tag
  }

  const moveDnsServerTo = (index: number, target: number, position: ManualDropPosition | null = null) => {
    const servers = dns.value?.servers ?? []
    if (target < 0 || target >= servers.length) return
    preserveImplicitDnsFinal()
    const result = moveArrayItemKeepingSelection(servers, index, target, selectedDnsServerIndexes.value, position)
    if (result.moved) selectedDnsServerIndexes.value = result.selectedIndexes
  }

  const moveDnsServersTo = (indexes: Array<number | string>, target: number, position: ManualDropPosition | null = null) => {
    const servers = dns.value?.servers ?? []
    if (target < 0 || target >= servers.length) return
    preserveImplicitDnsFinal()
    const result = moveManyArrayItemsKeepingSelection(servers, indexes, target, selectedDnsServerIndexes.value, position)
    if (result.moved) selectedDnsServerIndexes.value = result.selectedIndexes
  }

  const sortDnsServersByName = (direction: ManualSortDirection) => {
    const servers = dns.value?.servers ?? []
    preserveImplicitDnsFinal()
    const result = sortArrayByTextKeepingSelection(servers, direction, selectedDnsServerIndexes.value, "tag")
    if (result.moved) selectedDnsServerIndexes.value = result.selectedIndexes
  }

  const moveDnsRule = (index: number, dir: number) => {
    moveDnsRuleTo(index, index + dir)
  }

  const moveDnsRuleTo = (index: number, target: number, position: ManualDropPosition | null = null) => {
    const result = moveArrayItemKeepingSelection(dnsRules.value, index, target, selectedDnsRuleIndexes.value, position)
    if (result.moved) selectedDnsRuleIndexes.value = result.selectedIndexes
  }

  const moveDnsRulesTo = (indexes: Array<number | string>, target: number, position: ManualDropPosition | null = null) => {
    const result = moveManyArrayItemsKeepingSelection(dnsRules.value, indexes, target, selectedDnsRuleIndexes.value, position)
    if (result.moved) selectedDnsRuleIndexes.value = result.selectedIndexes
  }

  const handleRuleAction = async (key: string, item: any) => {
    if (key === 'edit') { showDnsRuleModal(item._index); return }
    if (key === 'up') { moveDnsRule(item._index, -1); return }
    if (key === 'down') { moveDnsRule(item._index, 1); return }
    if (key === 'del') {
      const ok = await confirm({ title: `${t('actions.del')} ${t('dns.rule.title')}`, message: String(item._index + 1), confirmLabel: t('actions.del'), tone: 'error' })
      if (ok) delDnsRule(item._index)
    }
  }

  const dnsServerDrag = useManualDrag<number>()

  const dnsRuleDrag = useManualDrag<number>()

  const onDnsServerDrop = (event: DragEvent, target: number) => {
    dnsServerDrag.drop(event, target, (dragged, dropTarget, position) => {
      if (dnsServerSelectMode.value && selectedDnsServerIndexes.value.map(String).includes(String(dragged))) {
        moveDnsServersTo(selectedDnsServerIndexes.value, dropTarget, position)
        return
      }
      moveDnsServerTo(dragged, dropTarget, position)
    })
  }

  const onDnsRuleDrop = (event: DragEvent, target: number) => {
    dnsRuleDrag.drop(event, target, (dragged, dropTarget, position) => {
      if (dnsRuleSelectMode.value && selectedDnsRuleIndexes.value.map(String).includes(String(dragged))) {
        moveDnsRulesTo(selectedDnsRuleIndexes.value, dropTarget, position)
        return
      }
      moveDnsRuleTo(dragged, dropTarget, position)
    })
  }

  const toggleDnsServerSelectMode = () => {
    dnsServerSelectMode.value = !dnsServerSelectMode.value
    if (!dnsServerSelectMode.value) selectedDnsServerIndexes.value = []
  }

  const toggleDnsRuleSelectMode = () => {
    dnsRuleSelectMode.value = !dnsRuleSelectMode.value
    if (!dnsRuleSelectMode.value) selectedDnsRuleIndexes.value = []
  }

  const isDnsServerSelected = (index: number) => selectedDnsServerIndexes.value.map(String).includes(String(index))

  const isDnsRuleSelected = (index: number) => selectedDnsRuleIndexes.value.map(String).includes(String(index))

  const toggleDnsServerSelection = (index: number, selected?: boolean) => {
    const next = new Set(selectedDnsServerIndexes.value.map(String))
    const key = String(index)
    const checked = selected ?? !next.has(key)
    if (checked) next.add(key)
    else next.delete(key)
    selectedDnsServerIndexes.value = [...next]
  }

  const toggleDnsRuleSelection = (index: number, selected?: boolean) => {
    const next = new Set(selectedDnsRuleIndexes.value.map(String))
    const key = String(index)
    const checked = selected ?? !next.has(key)
    if (checked) next.add(key)
    else next.delete(key)
    selectedDnsRuleIndexes.value = [...next]
  }

  const deleteSelectedDnsServers = async () => {
    const indexes = [...new Set(selectedDnsServerIndexes.value.map(Number))]
      .filter(index => index >= 0 && index < (dns.value?.servers?.length ?? 0))
      .sort((a, b) => b - a)
    if (indexes.length === 0) return
    const names = indexes.map(index => dns.value.servers[index]?.tag ?? String(index + 1)).reverse()
    const ok = await confirm({ title: `${t('actions.delbulk')} ${t('objects.dnsserver')}`, message: names.join('\n'), confirmLabel: t('actions.del'), tone: 'error' })
    if (!ok) return
    for (const index of indexes) dns.value.servers.splice(index, 1)
    selectedDnsServerIndexes.value = []
    delDnsOverlay.value = []
  }

  const deleteSelectedDnsRules = async () => {
    const indexes = [...new Set(selectedDnsRuleIndexes.value.map(Number))]
      .filter(index => index >= 0 && index < dnsRules.value.length)
      .sort((a, b) => b - a)
    if (indexes.length === 0) return
    const names = indexes.map(index => String(index + 1)).reverse()
    const ok = await confirm({ title: `${t('actions.delbulk')} ${t('dns.rule.title')}`, message: names.join('\n'), confirmLabel: t('actions.del'), tone: 'error' })
    if (!ok) return
    for (const index of indexes) dnsRules.value.splice(index, 1)
    selectedDnsRuleIndexes.value = []
    delDnsRuleOverlay.value = []
  }

  return {
    actionDnsRuleKeys,
    appConfig,
    applyPresetConfig,
    clients,
    closeDnsModal,
    closeDnsRuleModal,
    confirm,
    delDns,
    delDnsOverlay,
    delDnsRule,
    delDnsRuleOverlay,
    deleteSelectedDnsRules,
    deleteSelectedDnsServers,
    dns,
    dnsModal,
    dnsRuleDrag,
    dnsRuleModal,
    dnsRuleRows,
    dnsRuleSelectMode,
    dnsRules,
    dnsServerDrag,
    dnsServerRows,
    dnsServerSelectMode,
    dnsServerTags,
    dnsServersExpanded,
    finalDns,
    handleRuleAction,
    handleServerAction,
    inboundTags,
    isDnsRuleSelected,
    isDnsServerSelected,
    loading,
    mode,
    moveDnsRulesTo,
    moveDnsServersTo,
    moveDnsRuleTo,
    moveDnsServerTo,
    nexus,
    onDnsRuleDrop,
    onDnsServerDrop,
    outboundTags,
    presetSourceLabel,
    regionalPresetDrawer,
    rslvdTags,
    ruleActions,
    ruleColumns,
    ruleSets,
    saveConfig,
    saveDnsModal,
    saveDnsRuleModal,
    search,
    selectedDnsRuleIndexes,
    selectedDnsServerIndexes,
    serverActions,
    serverColumns,
    showDnsModal,
    showDnsRuleModal,
    sortDnsServersByName,
    stateChange,
    subtitle,
    t,
    toggleDnsRuleSelectMode,
    toggleDnsRuleSelection,
    toggleDnsServerSelectMode,
    toggleDnsServerSelection,
    tsTags,
  }
}
