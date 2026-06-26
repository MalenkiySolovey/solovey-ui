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
      class="manual-drop-grid-cell"
      :class="adminDrag.indicatorClasses(item.id)"
      :style="adminDrag.indicatorStyles(item.id)"
      :draggable="false"
      @pointerdown="adminDrag.prepare($event)"
      @dragstart="adminDrag.start($event, item.id)"
      @dragover="adminDrag.overTarget($event, item.id, users.map(row => row.id), [], false, 'grid')"
      @dragleave="adminDrag.leaveTarget($event, item.id)"
      @drop="onAdminDrop($event, item.id)"
      @dragend="adminDrag.clear($event)"
    >
      <v-card
        rounded="xl"
        elevation="5"
        min-width="200"
        :title="item.username"
        class="admins__card"
      >
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
import { useAdminsPage } from '@/shared/composables/pages/useAdminsPage'

const { mode, nexus, loading, users, editModal, showEditModal, closeEditModal, saveEditModal, addModal, showAddModal, closeAddModal, saveAddModal, deleteModal, showDeleteModal, closeDeleteModal, deleteAdmin, changesModal, showChangesModal, closeChangesModal, tokenModal, showTokenModal, closeTokenModal, logoutAllMenu, logoutAllAdmins, moveAdmin, dragAdmin, sortAdminsByName, adminDrag, onAdminDrop } = useAdminsPage()
</script>

<style scoped>
.admins__card {
  position: relative;
}
</style>
