<template>
  <AdminModal 
    v-model="editModal.visible"
    :visible="editModal.visible"
    :user="editModal.user"
    @close="closeEditModal"
    @save="saveEditModal"
  />
  <AdminAddModal
    v-model="addModal.visible"
    :visible="addModal.visible"
    @close="closeAddModal"
    @save="saveAddModal"
  />
  <AdminDeleteModal
    v-model="deleteModal.visible"
    :visible="deleteModal.visible"
    :user="deleteModal.user"
    @close="closeDeleteModal"
    @delete="deleteAdmin"
  />
  <ChangeModal 
    v-model="changesModal.visible"
    :visible="changesModal.visible"
    :admins="users.map((u:any) => u.username)"
    :actor="changesModal.actor"
    @close="closeChangesModal"
  />
  <TokenModal
    v-model="tokenModal.visible"
    :visible="tokenModal.visible"
    @close="closeTokenModal"
  />

  <AdminsNexusList
    v-if="nexus"
    :users="users"
    @add="showAddModal"
    @changes="showChangesModal"
    @del="showDeleteModal"
    @edit="showEditModal"
    @move="moveAdmin"
    @move-to="dragAdmin"
    @sort-by-name="sortAdminsByName"
    @logout-all="logoutAllAdmins"
    @token="showTokenModal"
  />

  <template v-else>
  <v-row>
    <v-col cols="12" justify="center" align="center">
      <v-btn color="primary" prepend-icon="mdi-account-plus" @click="showAddModal()" style="margin: 0 5px;">{{ $t('admin.addAdmin') }}</v-btn>
      <ManualSortButton
        :disabled="users.length < 2"
        style="margin: 0 5px;"
        @sort="sortAdminsByName"
      />
      <v-btn color="primary" @click="showChangesModal('')" style="margin: 0 5px;">{{ $t('admin.changes') }}</v-btn>
      <v-btn color="primary" @click="showTokenModal()" style="margin: 0 5px;">{{ $t('admin.api.token') }}</v-btn>
      <v-menu v-model="logoutAllMenu" :close-on-content-click="false" location="bottom center">
        <template v-slot:activator="{ props }">
          <v-btn color="error" variant="outlined" prepend-icon="mdi-logout-variant" v-bind="props" style="margin: 0 5px;">
            {{ $t('admin.logoutAll') }}
          </v-btn>
        </template>
        <v-card rounded="lg" max-width="420">
          <v-card-title>{{ $t('admin.logoutAll') }}</v-card-title>
          <v-divider></v-divider>
          <v-card-text>{{ $t('admin.logoutAllConfirm') }}</v-card-text>
          <v-card-actions>
            <v-spacer></v-spacer>
            <v-btn color="success" variant="outlined" @click="logoutAllMenu = false">{{ $t('no') }}</v-btn>
            <v-btn color="error" variant="tonal" @click="logoutAllAdmins">{{ $t('yes') }}</v-btn>
          </v-card-actions>
        </v-card>
      </v-menu>
    </v-col>
  </v-row>
  <v-row>
    <v-col
      cols="12"
      sm="4"
      md="3"
      lg="2"
      v-for="item in users"
      :key="item.id"
      :draggable="false"
      @pointerdown="adminDrag.prepare($event)"
      @dragstart="adminDrag.start($event, item.id)"
      @dragover="adminDrag.over($event)"
      @drop="onAdminDrop($event, item.id)"
      @dragend="adminDrag.clear($event)"
    >
      <v-card rounded="xl" elevation="5" min-width="200" :title="item.username">
        <v-card-subtitle style="margin-top: -15px;">
          {{ $t('admin.lastLogin') }}
        </v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col>{{ $t('admin.date') }}</v-col>
            <v-col>
              {{ item.loginDate }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>{{ $t('admin.time') }}</v-col>
            <v-col>
              {{ item.loginTime }}
            </v-col>
          </v-row>
          <v-row>
            <v-col>IP</v-col>
            <v-col>
              {{ item.ip }}
            </v-col>
          </v-row>
        </v-card-text>
        <v-divider></v-divider>
        <v-card-actions style="padding: 0;">
          <v-btn icon="mdi-account-edit" :aria-label="$t('actions.edit')" @click="showEditModal(item)">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('actions.edit')"></v-tooltip>
          </v-btn>
          <v-btn icon="mdi-list-box-outline" :aria-label="$t('admin.changes')" @click="showChangesModal(item.username)">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('admin.changes')"></v-tooltip>
          </v-btn>
          <v-btn v-if="!item.isCurrent" icon="mdi-delete" color="error" :aria-label="$t('admin.deleteAdmin')" @click="showDeleteModal(item)">
            <v-icon />
            <v-tooltip activator="parent" location="top" :text="$t('admin.deleteAdmin')"></v-tooltip>
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-col>
  </v-row>
  </template>
</template>

<script lang="ts" setup>
import AdminsNexusList from '@/views/admins/AdminsNexusList.vue'
import ManualSortButton from '@/components/ManualSortButton.vue'
import AdminModal from '@/layouts/modals/Admin.vue'
import AdminAddModal from '@/layouts/modals/AdminAdd.vue'
import AdminDeleteModal from '@/layouts/modals/AdminDelete.vue'
import ChangeModal  from '@/layouts/modals/Changes.vue'
import TokenModal from '@/layouts/modals/Token.vue'
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { clearCSRFToken } from '@/store/csrf'
import { Ref, computed, ref, inject, onMounted } from 'vue'
import router from '@/router'
import { useUiMode } from '@/uiMode/useUiMode'
import { useManualDrag } from '@/composables/useManualDrag'
import {
  type ManualSortDirection,
  sortRowsByText,
} from '@/composables/useManualReorder'

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
  const msg = await HttpUtils.get('api/users')
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
  const response = await HttpUtils.post('api/changePass',data)
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
  const response = await HttpUtils.post('api/addAdmin', data)
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
  const response = await HttpUtils.post('api/deleteAdmin', data)
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
  const response = await HttpUtils.post('api/logoutAllAdmins', {})
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

const dragAdmin = async (draggedId: number, targetId: number) => {
  if (draggedId === targetId) return
  const rows = [...users.value]
  const from = rows.findIndex(user => user.id === draggedId)
  const to = rows.findIndex(user => user.id === targetId)
  if (from < 0 || to < 0) return

  const [item] = rows.splice(from, 1)
  rows.splice(to, 0, item)
  await saveAdminOrder(rows)
}

const saveAdminOrder = async (rows: AdminListItem[]): Promise<boolean> => {
  const response = await HttpUtils.post('api/reorder', {
    object: 'admins',
    data: JSON.stringify(rows.map(user => user.id)),
  })
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
</script>
