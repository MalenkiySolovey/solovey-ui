import Data from '@/store/modules/data'
import { checkOutboundConnection } from '@/shared/composables/useOutboundChecks'
import { Outbound } from '@/types/outbounds'
import { computed, defineAsyncComponent, ref } from 'vue'
import { useUiMode } from '@/uiMode/useUiMode'
import { useI18n } from 'vue-i18n'
import { useManualDrag, type ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import type { ManualSortDirection } from '@/shared/composables/dragSelection/manualReorder'
import { usePendingManualOrder } from '@/shared/composables/usePendingManualOrder'
import { useAsyncTaskQueue } from '@/shared/composables/useAsyncTaskQueue'
import { useFailoverStatus } from '@/shared/composables/useFailoverStatus'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'

export const useOutboundsPage = (classicForm: any) => {
const { mode } = useUiMode()
const { t } = useI18n()
const { confirm } = useConfirm()

const OutboundsNexusList = defineAsyncComponent(
  () => import('@/views/outbounds/OutboundsNexusList.vue'),
)
const OutboundDrawer = defineAsyncComponent(
  () => import('@/components/nexus/drawers/OutboundDrawer.vue'),
)

const EntityForm = computed(() => (mode.value === 'nexus' ? OutboundDrawer : classicForm))

interface CheckResult {
  loading?: boolean
  success: boolean
  data?: { OK?: boolean; Delay?: number; Error?: string } | null
  errorMessage?: string
}

const checkResults = ref<Record<string, CheckResult>>({})
const outboundCheckQueue = useAsyncTaskQueue(8)

const performOutboundCheck = async (tag: string) => {
  checkResults.value = { ...checkResults.value, [tag]: { loading: true, success: false } }
  const msg = await checkOutboundConnection(tag)
  const success = msg.success && msg.obj?.OK
  const errorMessage = success ? undefined : (msg.obj?.Error ?? msg.msg ?? '')
  checkResults.value = {
    ...checkResults.value,
    [tag]: { loading: false, success, data: msg.obj ?? null, errorMessage }
  }
}

const checkOutbound = async (tag: string) => {
  await outboundCheckQueue.runOne(tag, () => performOutboundCheck(tag))
}

const testingAll = outboundCheckQueue.runningAll

const checkAllOutbounds = async () => {
  const list = outbounds.value
  if (list.length === 0) return
  await outboundCheckQueue.runMany(list, item => item.tag, item => performOutboundCheck(item.tag))
}

const outbounds = computed((): Outbound[] => {
  return <Outbound[]> Data().outbounds
})
const { statusByTag: failoverStatus } = useFailoverStatus(outbounds)
const outboundsOrder = usePendingManualOrder<Outbound>('outbounds', outbounds)
const orderedOutbounds = outboundsOrder.displayItems
const outboundOrderDirty = outboundsOrder.dirty
const outboundOrderSaving = outboundsOrder.saving
const outboundSelectMode = ref(false)
const selectedOutboundIds = ref<number[]>([])
const selectedOutboundSet = computed(() => new Set(selectedOutboundIds.value))
const selectedOutbounds = computed(() => orderedOutbounds.value.filter(item => selectedOutboundSet.value.has(item.id)))
const selectedOutboundCount = computed(() => selectedOutbounds.value.length)

const outboundTags = computed((): string[] => {
  return [...Data().outbounds?.map((o:Outbound) => o.tag), ...Data().endpoints?.map((e:any) => e.tag)]
})

const onlines = computed(() => {
  return Data().onlines.outbound?? []
})

const enableTraffic = computed((): boolean => {
  return Data().enableTraffic
})

const modal = ref({
  visible: false,
  id: 0,
  data: "",
})

let delOverlay = ref(new Array<boolean>)

const showModal = (id: number) => {
  modal.value.id = id
  modal.value.data = id == 0 ? '' : JSON.stringify(outbounds.value.findLast(o => o.id == id))
  modal.value.visible = true
}

const closeModal = () => {
  modal.value.visible = false
}

const bulkModal = ref({ visible: false })

const showBulkModal = () => {
  bulkModal.value.visible = true
}

const closeBulkModal = () => {
  bulkModal.value.visible = false
}

const stats = ref({
  visible: false,
  resource: "outbound",
  tag: "",
})

const delOutbound = async (tag: string) => {
  const success = await Data().save("outbounds", "del", tag)
  if (success) delOverlay.value = []
}

const delOutboundsBulk = async (tags: string[]) => {
  const uniqueTags = [...new Set(tags.map(tag => String(tag)).filter(Boolean))]
  let success = true
  for (const tag of uniqueTags) {
    success = await Data().save("outbounds", "del", tag)
    if (!success) break
  }
  if (success) {
    delOverlay.value = []
    selectedOutboundIds.value = []
  }
  return success
}

const deleteSelectedOutbounds = async () => {
  const tags = selectedOutbounds.value.map(item => item.tag)
  if (tags.length === 0) return
  const accepted = await confirm({
    title: `${t('actions.delbulk')} ${t('objects.outbound')}`,
    message: tags.join('\n'),
    confirmLabel: t('actions.del'),
    tone: 'error',
  })
  if (!accepted) return
  await delOutboundsBulk(tags)
}

const toggleOutboundSelectMode = () => {
  outboundSelectMode.value = !outboundSelectMode.value
  if (!outboundSelectMode.value) selectedOutboundIds.value = []
}

const isOutboundSelected = (id: number) => selectedOutboundSet.value.has(id)

const toggleOutboundSelection = (id: number, selected?: boolean) => {
  const next = new Set(selectedOutboundIds.value)
  const checked = selected ?? !next.has(id)
  if (checked) next.add(id)
  else next.delete(id)
  selectedOutboundIds.value = [...next]
}

const moveOutbound = (id: number, dir: number) => {
  outboundsOrder.move(id, dir)
}

const dragOutbound = (draggedId: number, targetId: number, position: ManualDropPosition | null = null) => {
  outboundsOrder.moveTo(draggedId, targetId, position)
}

const dragSelectedOutbounds = (draggedIds: number[], targetId: number, position: ManualDropPosition | null = null) => {
  outboundsOrder.moveManyTo(draggedIds, targetId, position)
}

const sortOutboundsByName = (direction: ManualSortDirection) => {
  outboundsOrder.sortByText(direction, "tag")
}

const saveOutboundOrder = () => outboundsOrder.save()
const cancelOutboundOrder = () => outboundsOrder.reset()

const outboundDrag = useManualDrag<number>()
const onOutboundDrop = (event: DragEvent, targetId: number) => {
  outboundDrag.drop(event, targetId, (draggedId, dropTargetId, position) => {
    if (outboundSelectMode.value && selectedOutboundSet.value.has(draggedId)) {
      dragSelectedOutbounds(selectedOutboundIds.value, dropTargetId, position)
      return
    }
    dragOutbound(draggedId, dropTargetId, position)
  })
}

const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
const closeStats = () => {
  stats.value.visible = false
}
  return {
    mode, EntityForm, OutboundsNexusList,
    checkResults, testingAll, checkOutbound, checkAllOutbounds, failoverStatus,
    outbounds, orderedOutbounds, outboundOrderDirty, outboundOrderSaving,
    outboundTags, onlines, enableTraffic,
    modal, showModal, closeModal, bulkModal, showBulkModal, closeBulkModal,
    stats, showStats, closeStats, delOverlay, delOutbound,
    delOutboundsBulk, deleteSelectedOutbounds,
    outboundSelectMode, selectedOutboundCount, selectedOutboundIds, selectedOutbounds,
    isOutboundSelected, toggleOutboundSelectMode, toggleOutboundSelection,
    moveOutbound, dragOutbound, dragSelectedOutbounds, sortOutboundsByName, saveOutboundOrder, cancelOutboundOrder,
    outboundDrag, onOutboundDrop,
  }
}
