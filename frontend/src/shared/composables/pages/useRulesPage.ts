import { computed, ref } from 'vue'
import { actionKeys, ruleset } from '@/types/rules'
import { i18n } from '@/locales'
import { useManualDrag, type ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import {
  moveArrayItemKeepingSelection,
  moveManyArrayItemsKeepingSelection,
  removeArrayItemKeepingSelection,
  type ManualSortDirection,
  sortArrayByTextKeepingSelection,
} from '@/shared/composables/dragSelection/manualReorder'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { useUiMode } from '@/uiMode/useUiMode'
import { isPresetManagedItem } from '@/components/presets/routingDnsPresets'
import { useRulesConfig } from '@/shared/composables/rules/useRulesConfig'
import { ruleActionsFor, ruleColumns, rulesetActionsFor, rulesetColumns } from '@/shared/composables/rules/tableConfig'

export const useRulesPage = () => {
  const { confirm } = useConfirm()

  const { mode } = useUiMode()

  const nexus = computed(() => mode.value === 'nexus')

  const tt = (key: string) => i18n.global.t(key)

  const {
    appConfig,
    applyPresetConfig,
    clients,
    defaultFallbackDelayMs,
    findProcess,
    inboundTags,
    loading,
    networkTypes,
    outboundTags,
    overrideAndroidVpn,
    route,
    routeDefaultNetworkStrategy,
    routeMark,
    routePreset,
    routePresets,
    rules,
    rulesetTags,
    rulesets,
    saveConfig,
    stateChange,
  } = useRulesConfig()

  const actionMenu = ref(false)

  const search = ref('')

  const regionalPresetDrawer = ref(false)

  const presetSourceLabel = (item: any) => isPresetManagedItem(item) ? tt('presets.presetManaged') : tt('presets.custom')

  const rulesetsExpanded = ref(true)
  const rulesetSelectMode = ref(false)
  const selectedRulesetIndexes = ref<Array<number | string>>([])
  const ruleSelectMode = ref(false)
  const selectedRuleIndexes = ref<Array<number | string>>([])

  // ---- Nexus table projections (read-only; actions carry the array index) ----
  // _index keeps the ORIGINAL array index (move/edit/delete operate by index), so
  // filter AFTER mapping. Search matches tag/type/format (rulesets) and
  // action/outbound (rules).
  const matchesSearch = (text: string): boolean => {
    const q = search.value.trim().toLowerCase()
    return !q || text.toLowerCase().includes(q)
  }

  const rulesetRows = computed(() =>
    rulesets.value
      .map((rs: any, i: number) => ({ ...rs, _index: i }))
      .filter((rs: any) => matchesSearch(`${rs.tag ?? ''} ${rs.type ?? ''} ${rs.format ?? ''}`)))

  const ruleRows = computed(() =>
    rules.value
      .map((r: any, i: number) => ({
        ...r,
        _index: i,
        _rulesCount: r.rules ? r.rules.length : Object.keys(r).filter((k: string) => !actionKeys.includes(k)).length,
      }))
      .filter((r: any) => matchesSearch(`${r.action ?? ''} ${r.outbound ?? ''}`)))

  const subtitle = computed(() =>
    i18n.global.t('nexus.summary.rules', { rulesets: rulesets.value.length, rules: rules.value.length }))

  const rulesetActions = (item: any) => rulesetActionsFor(item, search.value, rulesets.value.length)

  const ruleActions = (item: any) => ruleActionsFor(item, search.value, rules.value.length)

  const handleRulesetAction = async (key: string, item: any) => {
    if (key === 'up') { moveRuleset(item._index, -1); return }
    if (key === 'down') { moveRuleset(item._index, 1); return }
    if (key === 'edit') { showRulesetModal(item._index); return }
    if (key === 'del') {
      const ok = await confirm({ title: `${tt('actions.del')} ${tt('objects.ruleset')}`, message: item.tag, confirmLabel: tt('actions.del'), tone: 'error' })
      if (ok) delRuleset(item._index)
    }
  }

  const moveRuleset = (index: number, dir: number) => {
    moveRulesetTo(index, index + dir)
  }

  const moveRulesetTo = (index: number, target: number, position: ManualDropPosition | null = null) => {
    const result = moveArrayItemKeepingSelection(rulesets.value, index, target, selectedRulesetIndexes.value, position)
    if (result.moved) selectedRulesetIndexes.value = result.selectedIndexes
  }

  const moveRulesetsTo = (indexes: Array<number | string>, target: number, position: ManualDropPosition | null = null) => {
    const result = moveManyArrayItemsKeepingSelection(rulesets.value, indexes, target, selectedRulesetIndexes.value, position)
    if (result.moved) selectedRulesetIndexes.value = result.selectedIndexes
  }

  const sortRulesetsByName = (direction: ManualSortDirection) => {
    const result = sortArrayByTextKeepingSelection(rulesets.value, direction, selectedRulesetIndexes.value, "tag")
    if (result.moved) selectedRulesetIndexes.value = result.selectedIndexes
  }

  const moveRule = (index: number, dir: number) => {
    moveRuleTo(index, index + dir)
  }

  const moveRuleTo = (index: number, target: number, position: ManualDropPosition | null = null) => {
    const result = moveArrayItemKeepingSelection(rules.value, index, target, selectedRuleIndexes.value, position)
    if (result.moved) selectedRuleIndexes.value = result.selectedIndexes
  }

  const moveRulesTo = (indexes: Array<number | string>, target: number, position: ManualDropPosition | null = null) => {
    const result = moveManyArrayItemsKeepingSelection(rules.value, indexes, target, selectedRuleIndexes.value, position)
    if (result.moved) selectedRuleIndexes.value = result.selectedIndexes
  }

  const handleRuleAction = async (key: string, item: any) => {
    if (key === 'edit') { showRuleModal(item._index); return }
    if (key === 'up') { moveRule(item._index, -1); return }
    if (key === 'down') { moveRule(item._index, 1); return }
    if (key === 'del') {
      const ok = await confirm({ title: `${tt('actions.del')} ${tt('pages.rules')}`, message: String(item._index + 1), confirmLabel: tt('actions.del'), tone: 'error' })
      if (ok) delRule(item._index)
    }
  }

  let delRuleOverlay = ref(new Array<boolean>)

  let delRulesetOverlay = ref(new Array<boolean>)

  const ruleModal = ref({ visible: false, index: -1, data: "" })

  const showRuleModal = (index: number) => {
    ruleModal.value.index = index
    ruleModal.value.data = index == -1 ? '' : JSON.stringify(rules.value[index])
    ruleModal.value.visible = true
  }

  const closeRuleModal = () => { ruleModal.value.visible = false }

  const saveRuleModal = (data:any) => {
    if (ruleModal.value.index == -1) rules.value.push(data)
    else rules.value[ruleModal.value.index] = data
    ruleModal.value.visible = false
  }

  const delRule = (index: number) => {
    const result = removeArrayItemKeepingSelection(rules.value, index, selectedRuleIndexes.value)
    delRuleOverlay.value[index] = false
    selectedRuleIndexes.value = result.selectedIndexes
  }

  const rulesetModal = ref({ visible: false, index: -1, data: "" })

  const showRulesetModal = (index: number) => {
    rulesetModal.value.index = index
    rulesetModal.value.data = index == -1 ? '' : JSON.stringify(rulesets.value[index])
    rulesetModal.value.visible = true
  }

  const closeRulesetModal = () => { rulesetModal.value.visible = false }

  const saveRulesetModal = (data:ruleset) => {
    if (rulesetModal.value.index == -1) rulesets.value.push(data)
    else rulesets.value[rulesetModal.value.index] = data
    rulesetModal.value.visible = false
  }

  const delRuleset = (index: number) => {
    const result = removeArrayItemKeepingSelection(rulesets.value, index, selectedRulesetIndexes.value)
    delRulesetOverlay.value[index] = false
    selectedRulesetIndexes.value = result.selectedIndexes
  }

  const rulesetDrag = useManualDrag<number>()

  const ruleDrag = useManualDrag<number>()

  const onRulesetDrop = (event: DragEvent, target: number) => {
    rulesetDrag.drop(event, target, (dragged, dropTarget, position) => {
      if (rulesetSelectMode.value && selectedRulesetIndexes.value.map(String).includes(String(dragged))) {
        moveRulesetsTo(selectedRulesetIndexes.value, dropTarget, position)
        return
      }
      moveRulesetTo(dragged, dropTarget, position)
    })
  }

  const onRuleDrop = (event: DragEvent, target: number) => {
    ruleDrag.drop(event, target, (dragged, dropTarget, position) => {
      if (ruleSelectMode.value && selectedRuleIndexes.value.map(String).includes(String(dragged))) {
        moveRulesTo(selectedRuleIndexes.value, dropTarget, position)
        return
      }
      moveRuleTo(dragged, dropTarget, position)
    })
  }

  const toggleRulesetSelectMode = () => {
    rulesetSelectMode.value = !rulesetSelectMode.value
    if (!rulesetSelectMode.value) selectedRulesetIndexes.value = []
  }

  const toggleRuleSelectMode = () => {
    ruleSelectMode.value = !ruleSelectMode.value
    if (!ruleSelectMode.value) selectedRuleIndexes.value = []
  }

  const isRulesetSelected = (index: number) => selectedRulesetIndexes.value.map(String).includes(String(index))

  const isRuleSelected = (index: number) => selectedRuleIndexes.value.map(String).includes(String(index))

  const toggleRulesetSelection = (index: number, selected?: boolean) => {
    const next = new Set(selectedRulesetIndexes.value.map(String))
    const key = String(index)
    const checked = selected ?? !next.has(key)
    if (checked) next.add(key)
    else next.delete(key)
    selectedRulesetIndexes.value = [...next]
  }

  const toggleRuleSelection = (index: number, selected?: boolean) => {
    const next = new Set(selectedRuleIndexes.value.map(String))
    const key = String(index)
    const checked = selected ?? !next.has(key)
    if (checked) next.add(key)
    else next.delete(key)
    selectedRuleIndexes.value = [...next]
  }

  const deleteSelectedRulesets = async () => {
    const indexes = [...new Set(selectedRulesetIndexes.value.map(Number))]
      .filter(index => index >= 0 && index < rulesets.value.length)
      .sort((a, b) => b - a)
    if (indexes.length === 0) return
    const names = indexes.map(index => rulesets.value[index]?.tag ?? String(index + 1)).reverse()
    const ok = await confirm({ title: `${tt('actions.delbulk')} ${tt('objects.ruleset')}`, message: names.join('\n'), confirmLabel: tt('actions.del'), tone: 'error' })
    if (!ok) return
    for (const index of indexes) rulesets.value.splice(index, 1)
    selectedRulesetIndexes.value = []
    delRulesetOverlay.value = []
  }

  const deleteSelectedRules = async () => {
    const indexes = [...new Set(selectedRuleIndexes.value.map(Number))]
      .filter(index => index >= 0 && index < rules.value.length)
      .sort((a, b) => b - a)
    if (indexes.length === 0) return
    const names = indexes.map(index => String(index + 1)).reverse()
    const ok = await confirm({ title: `${tt('actions.delbulk')} ${tt('pages.rules')}`, message: names.join('\n'), confirmLabel: tt('actions.del'), tone: 'error' })
    if (!ok) return
    for (const index of indexes) rules.value.splice(index, 1)
    selectedRuleIndexes.value = []
    delRuleOverlay.value = []
  }

  const importRulesModal = ref({ visible: false })

  function showImportRule() {
    importRulesModal.value.visible = true
  }

  function closeImportRule() {
    importRulesModal.value.visible = false
  }

  function saveImportRule(block: any, mode: 'merge' | 'replace', applyFinal: boolean) {
    if (mode === 'replace') {
      route.value.rules = block.rules ?? []
      route.value.rule_set = block.rule_set ?? []
    } else {
      const existingTags = new Set(rulesetTags.value)
      if (block.rules) rules.value.push(...block.rules)
      if (block.rule_set) {
        for (const rs of block.rule_set) {
          if (!existingTags.has(rs.tag)) rulesets.value.push(rs)
        }
      }
    }
    if (applyFinal && block.final) route.value.final = block.final
    importRulesModal.value.visible = false
  }

  const importRulesetsModal = ref({ visible: false })

  function showImportRulesets() {
    importRulesetsModal.value.visible = true
  }

  function closeImportRulesets() {
    importRulesetsModal.value.visible = false
  }

  function saveImportRulesets(items: any[]) {
    rulesets.value.push(...items)
    importRulesetsModal.value.visible = false
  }

  return {
    actionKeys,
    actionMenu,
    appConfig,
    applyPresetConfig,
    clients,
    closeImportRule,
    closeImportRulesets,
    closeRuleModal,
    closeRulesetModal,
    confirm,
    deleteSelectedRules,
    deleteSelectedRulesets,
    defaultFallbackDelayMs,
    delRule,
    delRuleOverlay,
    delRuleset,
    delRulesetOverlay,
    findProcess,
    handleRuleAction,
    handleRulesetAction,
    importRulesModal,
    importRulesetsModal,
    inboundTags,
    isRuleSelected,
    isRulesetSelected,
    loading,
    mode,
    moveRuleTo,
    moveRulesTo,
    moveRulesetsTo,
    moveRulesetTo,
    networkTypes,
    nexus,
    onRuleDrop,
    onRulesetDrop,
    outboundTags,
    overrideAndroidVpn,
    presetSourceLabel,
    regionalPresetDrawer,
    route,
    routeDefaultNetworkStrategy,
    routeMark,
    routePreset,
    routePresets,
    ruleActions,
    ruleColumns,
    ruleDrag,
    ruleModal,
    ruleRows,
    ruleSelectMode,
    rules,
    rulesetActions,
    rulesetColumns,
    rulesetDrag,
    rulesetModal,
    rulesetRows,
    rulesetSelectMode,
    rulesetTags,
    rulesets,
    rulesetsExpanded,
    saveConfig,
    saveImportRule,
    saveImportRulesets,
    saveRuleModal,
    saveRulesetModal,
    search,
    selectedRuleIndexes,
    selectedRulesetIndexes,
    showImportRule,
    showImportRulesets,
    showRuleModal,
    showRulesetModal,
    sortRulesetsByName,
    stateChange,
    subtitle,
    toggleRuleSelectMode,
    toggleRuleSelection,
    toggleRulesetSelectMode,
    toggleRulesetSelection,
  }
}

export type RulesPage = ReturnType<typeof useRulesPage>
