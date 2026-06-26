import Data from '@/store/modules/data'
import { Client } from '@/types/clients'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { HumanReadable } from '@/plugins/utils'
import { i18n, locale } from '@/locales'
import { useDisplay } from 'vuetify'
import { useUiMode } from '@/uiMode/useUiMode'
import { useManualDrag, type ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import {
  dragManualOrder,
  type ManualSortDirection,
  moveManyManualOrder,
  moveManualOrder,
  sortManualOrderByText,
} from '@/shared/composables/dragSelection/manualReorder'
import { useBulkSelection } from '@/shared/composables/dragSelection/bulkSelection'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'

export const useClientsPage = () => {
  const { smAndDown } = useDisplay()
  const { t } = useI18n()
  const { confirm } = useConfirm()

  const { mode } = useUiMode()

  const enableTraffic = computed((): boolean => Data().enableTraffic)

  const onlineUsers = computed((): string[] => Data().onlines.user ?? [])

  const formatSize = (value: number) => HumanReadable.sizeFormat(value)

  const remainedDays = (value: number) => HumanReadable.remainedDays(value)

  const clients = computed((): any[] => {
    return Data().clients
  })
  const clientSelection = useBulkSelection(clients, item => item.id)
  const clientSelectMode = clientSelection.active
  const selectedClientIds = clientSelection.selectedIds
  const selectedClientCount = clientSelection.selectedCount
  const toggleClientSelectMode = clientSelection.toggleActive

  const isOnline = (cname: string) => computed(() => {
    return Data().onlines?.user ? Data().onlines.user.includes(cname) : false
  })

  const inbounds = computed((): any[] => {
    return Data().inbounds?? []
  })

  const inboundTags = computed((): any[] => {
    if (!inbounds.value) return []
    return inbounds.value?.filter(i => i.tag != "" && i.users).map(i => { return { title: i.tag, value: i.id } })
  })

  const groups = computed((): string[] => {
    if (!clients.value) return []
    if (filterSettings?.value.enabled) return Array.from(new Set(filterSettings.value.filteredClients?.map(c => c.group)))
    return Array.from(new Set(clients.value?.map(c => c.group)))
  })

  const actionMenu = ref(false)

  const filterMenu = ref(false)

  const filterSettings = ref({
    enabled: false,
    state: '',
    group: '-',
    text: '',
    filteredClients: <any[]>[]
  })

  const filterItems = [
    { title: i18n.global.t('none'), value: '' },
    { title: i18n.global.t('disable'), value: 'disable' },
    { title: i18n.global.t('date.expired'), value: 'expired' },
    { title: i18n.global.t('online'), value: 'online' },
  ]

  const headers = [
    { title: i18n.global.t('client.name'), key: 'name' },
    { title: i18n.global.t('client.desc'), key: 'desc' },
    { title: i18n.global.t('client.group'), key: 'group' },
    { title: i18n.global.t('pages.inbounds'), key: 'inbounds', width: 10 },
    { title: i18n.global.t('actions.action'), key: 'actions', sortable: false },
    { title: i18n.global.t('stats.volume'), key: 'volume' },
    { title: i18n.global.t('date.expiry'), key: 'expiry' },
    { title: i18n.global.t('online'), key: 'online' },
    { title: i18n.global.t('client.lastIpCount'), key: 'lastIpCount' },
    { key: 'data-table-group', width: 0 },
  ]

  const itemPerPage = ref(localStorage.getItem('items-per-page') || '10')

  const setItemPerPage = (items: number) => {
    itemPerPage.value = items.toString()
    localStorage.setItem('items-per-page', items.toString())
  }

  const modal = ref({
    visible: false,
    id: 0,
  })

  const delOverlay = ref(new Array<boolean>(clients.value.length).fill(false))

  const showModal = async (id: number) => {
    modal.value.id = id
    modal.value.visible = true
  }

  const closeModal = () => {
    modal.value.visible = false
  }

  const delClient = async (id: number) => {
    const index = clients.value.findIndex(c => c.id === id)
    const success = await Data().save("clients", "del", id)
    if (success) delOverlay.value[index] = false
  }

  const delClientsBulk = async (ids: number[]) => {
    const uniqueIds = [...new Set(ids.map(Number).filter(Boolean))]
    let success = true
    for (const id of uniqueIds) {
      success = await Data().save("clients", "del", id)
      if (!success) break
    }
    if (success) {
      delOverlay.value = []
      clientSelection.clear()
    }
    return success
  }

  const deleteSelectedClients = async () => {
    const rows = clientSelection.selectedItems.value
    if (rows.length === 0) return
    const accepted = await confirm({
      title: `${t('actions.delbulk')} ${t('objects.client')}`,
      message: rows.map(item => item.name).join('\n'),
      confirmLabel: t('actions.del'),
      tone: 'error',
    })
    if (!accepted) return
    await delClientsBulk(rows.map(item => item.id))
  }

  const moveClient = async (id: number, dir: number) => {
    await moveManualOrder("clients", clients.value as any[], id, dir)
  }

  const dragClient = async (draggedId: number, targetId: number, position: ManualDropPosition | null = null) => {
    await dragManualOrder("clients", clients.value as any[], draggedId, targetId, "id", position)
  }

  const dragSelectedClients = async (draggedIds: number[], targetId: number, position: ManualDropPosition | null = null) => {
    await moveManyManualOrder("clients", clients.value as any[], draggedIds, targetId, "id", position)
  }

  const sortClientsByName = async (direction: ManualSortDirection) => {
    await sortManualOrderByText("clients", clients.value as any[], direction, "name")
  }

  const clientDrag = useManualDrag<number>()

  const onClientDrop = (event: DragEvent, targetId: number) => {
    clientDrag.drop(event, targetId, (draggedId, dropTargetId, position) => {
      if (clientSelectMode.value && clientSelection.isSelected(draggedId)) {
        void dragSelectedClients(selectedClientIds.value.map(Number), dropTargetId, position)
        return
      }
      void dragClient(draggedId, dropTargetId, position)
    }, filterSettings.value.enabled)
  }

  const clientRowProps = ({ item }: { item: any }) => ({
    class: clientDrag.indicatorClasses(item.id),
    draggable: false,
    onPointerdown: (event: PointerEvent) => clientDrag.prepare(event, filterSettings.value.enabled),
    onDragstart: (event: DragEvent) => clientDrag.start(event, item.id, filterSettings.value.enabled),
    onDragover: (event: DragEvent) => clientDrag.overTarget(
      event,
      item.id,
      clients.value.map(row => row.id),
      clientSelectMode.value ? selectedClientIds.value.map(Number) : [],
      filterSettings.value.enabled,
      'vertical',
    ),
    onDragleave: (event: DragEvent) => clientDrag.leaveTarget(event, item.id),
    onDrop: (event: DragEvent) => onClientDrop(event, item.id),
    onDragend: (event: DragEvent) => clientDrag.clear(event),
  })

  const qrcode = ref({
    visible: false,
    id: 0,
  })

  const showQrCode = (id: number) => {
    qrcode.value.id = id
    qrcode.value.visible = true
  }

  const closeQrCode = () => {
    qrcode.value.visible = false
  }

  const doctor = ref({
    visible: false,
    id: 0,
  })

  const showDoctor = (id: number) => {
    doctor.value.id = id
    doctor.value.visible = true
  }

  const closeDoctor = () => {
    doctor.value.visible = false
  }

  const stats = ref({
    visible: false,
    resource: "user",
    tag: "",
  })

  const ipModal = ref({
    visible: false,
    client: '',
  })

  const showClientIps = (clientName: string) => {
    ipModal.value.visible = true
    ipModal.value.client = clientName
  }

  const onClientIpsCleared = () => {
    Data().loadData()
  }

  const showStats = (tag: string) => {
    stats.value.tag = tag
    stats.value.visible = true
  }

  const closeStats = () => {
    stats.value.visible = false
  }

  const doFilter = () => {
    let filteredClients = clients.value.slice()
    if (filterSettings.value.group != '-') {
      filteredClients = filteredClients.filter(c => c.group == filterSettings.value.group)
    }
    if (filterSettings.value.text.length>0) {
      const txt = filterSettings.value.text
      filteredClients = filteredClients.filter(c => c.name.search(txt) != -1 || c.desc.search(txt) != -1)
    }
    switch (filterSettings.value.state) {
      case "disable":
        filteredClients = filteredClients.filter(c => c.enable == false)
        break
      case "expired":
        filteredClients = filteredClients.filter(c => c.expiry > 0 && c.expiry < (Date.now()/1000) )
        break
      case "online":
        filteredClients = filteredClients.filter(c => Data().onlines?.user?.includes(c.name))
        break
    }
    filterSettings.value.filteredClients = filteredClients
    filterSettings.value.enabled = true
    filterMenu.value = false
  }

  const clearFilter = () => {
    filterSettings.value = {
      enabled: false,
      state: '',
      group: '-',
      text: '',
      filteredClients: <any[]>[]
    }
    filterMenu.value = false
  }

  const addBulkModal = ref(false)

  const addBulk = () => {
    addBulkModal.value = true
    actionMenu.value = false
  }

  const closeAddBulk = () => {
    addBulkModal.value = false
  }

  const editBulkModal = ref(false)

  const editBulk = () => {
    editBulkModal.value = true
    actionMenu.value = false
  }

  const closeEditBulk = () => {
    editBulkModal.value = false
  }

  const percent = (c: Client) => { return c.volume>0 ? Math.round((c.up+c.down) *100 / c.volume) : 0 }

  const percentColor = (c: Client) => { return (c.up+c.down) >= c.volume ? 'error' : percent(c)>90 ? 'warning' : 'success' }

  return {
    actionMenu,
    addBulk,
    addBulkModal,
    clearFilter,
    clientRowProps,
    clientSelectMode,
    clients,
    closeAddBulk,
    closeDoctor,
    closeEditBulk,
    closeModal,
    closeQrCode,
    closeStats,
    delClient,
    delClientsBulk,
    deleteSelectedClients,
    delOverlay,
    doFilter,
    doctor,
    dragClient,
    dragSelectedClients,
    editBulk,
    editBulkModal,
    enableTraffic,
    formatSize,
    filterItems,
    filterMenu,
    filterSettings,
    groups,
    headers,
    inboundTags,
    inbounds,
    ipModal,
    isOnline,
    itemPerPage,
    locale,
    modal,
    mode,
    moveClient,
    onClientIpsCleared,
    onlineUsers,
    percent,
    percentColor,
    qrcode,
    remainedDays,
    selectedClientCount,
    selectedClientIds,
    setItemPerPage,
    showClientIps,
    showDoctor,
    showModal,
    showQrCode,
    showStats,
    smAndDown,
    sortClientsByName,
    stats,
    toggleClientSelectMode,
  }
}
