import { i18n } from '@/locales'
import { changeAdminPassword, createAdmin, loadAdmins, logoutAllAdmins as logoutEveryAdmin, removeAdmin, reorderAdmins } from '@/shared/composables/useAdminOperations'
import { clearCSRFToken } from '@/store/csrf'
import { Ref, computed, ref, inject, onMounted } from 'vue'
import router from '@/router'
import { useUiMode } from '@/uiMode/useUiMode'
import { useManualDrag, type ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import {
  type ManualSortDirection,
  moveRowsToPosition,
  sortRowsByText,
} from '@/shared/composables/dragSelection/manualReorder'

export const useAdminsPage = () => {
const { mode } = useUiMode()
const nexus = computed(() => mode.value === 'nexus')

const loading:Ref = inject('loading')?? ref(false)

interface AdminListItem {
  id: number
  username: string
  loginDate: string
  loginTime: string
  ip: string
  isCurrent: boolean
}

const emptyAdmin: AdminListItem = {
  id: 0,
  username: '',
  loginDate: '-',
  loginTime: '-',
  ip: '-',
  isCurrent: false,
}

const users = ref<AdminListItem[]>([])

onMounted(async () => {
  loading.value = true
  await loadData()
  loading.value = false
})

const loadData = async () => {
  loading.value = true
  const msg = await loadAdmins()
  loading.value = false
  users.value = []
  if (msg.success) {
    const payload = Array.isArray(msg.obj) ? msg.obj : []
    users.value = payload.map(adminListItemFromPayload)
  }
}

const adminListItemFromPayload = (u: any): AdminListItem => {
  const lastLogin = String(u.lastLogin ?? '').split(" ")
  const localLastLogin = lastLogin.length > 2 ? dateFormatted(Date.parse(lastLogin[0] + " " + lastLogin[1])) : "- -"
  const loginDateTime = localLastLogin.split(" ")
  return {
    id: Number(u.id),
    username: String(u.username ?? ''),
    loginDate: loginDateTime[0],
    loginTime: loginDateTime[1],
    ip: lastLogin[2]?? "-",
    isCurrent: Boolean(u.isCurrent),
  }
}

const dateFormatted = (dt: number): string => {
  const locale = i18n.global.locale.value.replace('zh', 'zh-')
  const date = new Date(dt)
  return date.toLocaleString(locale)
}

const editModal = ref({
  visible: false,
  user: {},
})

const showEditModal = (user: any) => {
  editModal.value.user = user
  editModal.value.visible = true
}
const closeEditModal = () => {
  editModal.value.visible = false
  editModal.value.user = {}
}
const saveEditModal = async (data:any) => {
  loading.value=true
  const response = await changeAdminPassword(data)
  if(response.success){
    setTimeout(() => {
      loading.value=false
      editModal.value.visible = false
    }, 500)
  } else {
    loading.value=false
  }
}

const addModal = ref({
  visible: false,
})
const showAddModal = () => {
  addModal.value.visible = true
}
const closeAddModal = () => {
  addModal.value.visible = false
}
const saveAddModal = async (data:any) => {
  loading.value = true
  const response = await createAdmin(data)
  if (response.success) {
    addModal.value.visible = false
    await loadData()
  }
  loading.value = false
}

const deleteModal = ref({
  visible: false,
  user: { ...emptyAdmin },
})
const showDeleteModal = (user: AdminListItem) => {
  if (user.isCurrent) return
  deleteModal.value.user = user
  deleteModal.value.visible = true
}
const closeDeleteModal = () => {
  deleteModal.value.visible = false
  deleteModal.value.user = { ...emptyAdmin }
}
const deleteAdmin = async (data:any) => {
  loading.value = true
  const response = await removeAdmin(data)
  if (response.success) {
    closeDeleteModal()
    await loadData()
  }
  loading.value = false
}

const changesModal = ref({
  visible: false,
  actor: '',
})
const showChangesModal = (actor: string) => {
  changesModal.value.actor = actor
  changesModal.value.visible = true
}
const closeChangesModal = () => {
  changesModal.value.visible = false
  changesModal.value.actor = ''
}

const tokenModal = ref({
  visible: false,
})
const showTokenModal = () => {
  tokenModal.value.visible = true
}
const closeTokenModal = () => {
  tokenModal.value.visible = false
}

const logoutAllMenu = ref(false)
const logoutAllAdmins = async () => {
  loading.value = true
  const response = await logoutEveryAdmin()
  loading.value = false
  logoutAllMenu.value = false
  if (response.success) {
    clearCSRFToken()
    router.push('/login')
  }
}

const moveAdmin = async (id: number, dir: number) => {
  const rows = [...users.value]
  const index = rows.findIndex(user => user.id === id)
  const target = index + dir
  if (index < 0 || target < 0 || target >= rows.length) return

  const [item] = rows.splice(index, 1)
  rows.splice(target, 0, item)
  await saveAdminOrder(rows)
}

const dragAdmin = async (draggedId: number, targetId: number, position: ManualDropPosition | null = null) => {
  const rows = moveRowsToPosition(users.value, [draggedId], targetId, position)
  if (!rows) return
  await saveAdminOrder(rows)
}

const saveAdminOrder = async (rows: AdminListItem[]): Promise<boolean> => {
  const response = await reorderAdmins(rows.map(user => user.id))
  if (response.success) {
    const payload = response.obj?.users
    users.value = Array.isArray(payload) ? payload.map(adminListItemFromPayload) : rows
  }
  return response.success
}

const sortAdminsByName = async (direction: ManualSortDirection) => {
  if (users.value.length < 2) return
  await saveAdminOrder(sortRowsByText(users.value, direction, "username"))
}

const adminDrag = useManualDrag<number>()
const onAdminDrop = (event: DragEvent, targetId: number) => {
  adminDrag.drop(event, targetId, dragAdmin)
}
  return {
    mode, nexus, loading, users,
    editModal, showEditModal, closeEditModal, saveEditModal,
    addModal, showAddModal, closeAddModal, saveAddModal,
    deleteModal, showDeleteModal, closeDeleteModal, deleteAdmin,
    changesModal, showChangesModal, closeChangesModal,
    tokenModal, showTokenModal, closeTokenModal,
    logoutAllMenu, logoutAllAdmins,
    moveAdmin, dragAdmin, sortAdminsByName, adminDrag, onAdminDrop,
  }
}
