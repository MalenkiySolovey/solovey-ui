<template>
  <div class="remote-outbounds" :class="{ 'remote-outbounds--nexus': mode === 'nexus' }">
    <page-header
      v-if="mode === 'nexus'"
      v-model:search="search"
      searchable
      :subtitle="subtitle"
      :title="$t('pages.remoteOutboundSubscriptions')"
    />

    <page-toolbar v-if="mode === 'nexus'">
      <template #actions>
        <v-btn
          append-icon="lucide:gauge"
          :disabled="testingAll || totalTestableConnections === 0"
          :loading="testingAll"
          variant="outlined"
          @click="testAll"
        >
          {{ $t('actions.testAll') }}
        </v-btn>
        <v-btn prepend-icon="lucide:refresh-cw" :loading="loading" variant="tonal" @click="load">
          {{ $t('actions.refresh') }}
        </v-btn>
      </template>
    </page-toolbar>

    <v-row v-else justify="center" align="center">
      <v-col cols="12" sm="6" md="4">
        <v-text-field
          v-model="search"
          density="compact"
          hide-details
          prepend-inner-icon="mdi-magnify"
          :label="$t('table.search')"
          variant="outlined"
        />
      </v-col>
      <v-col cols="auto">
        <v-btn color="secondary" variant="outlined" :loading="loading" @click="load">
          {{ $t('actions.refresh') }}
        </v-btn>
      </v-col>
      <v-col cols="auto">
        <v-btn
          append-icon="mdi-speedometer"
          color="secondary"
          variant="outlined"
          :disabled="testingAll || totalTestableConnections === 0"
          :loading="testingAll"
          @click="testAll"
        >
          {{ $t('actions.testAll') }}
        </v-btn>
      </v-col>
    </v-row>

    <v-form ref="subscriptionForm" class="remote-outbounds__form" @submit.prevent="saveSubscription">
      <v-row align="center">
        <v-col cols="12" md="3">
          <v-text-field
            v-model="form.name"
            density="compact"
            :label="$t('remoteOutbound.name')"
            :rules="requiredRules"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" md="5">
          <v-text-field
            v-model="form.url"
            density="compact"
            :label="$t('remoteOutbound.url')"
            :rules="requiredRules"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" md="2">
          <v-text-field v-model="form.tagPrefix" density="compact" :label="$t('remoteOutbound.tagPrefix')" variant="outlined" />
        </v-col>
        <v-col cols="12" sm="6" md="1">
          <v-switch v-model="form.enabled" color="primary" density="compact" hide-details :label="$t('enable')" />
        </v-col>
        <v-col cols="12" sm="6" md="1">
          <v-switch v-model="form.autoUpdate" color="primary" density="compact" hide-details :label="$t('remoteOutbound.autoUpdate')" />
        </v-col>
        <v-col cols="12" sm="6" md="1">
          <v-text-field
            v-model.number="updateIntervalMinutes"
            density="compact"
            hide-details
            min="5"
            :disabled="!form.autoUpdate"
            :label="$t('remoteOutbound.updateInterval')"
            suffix="min"
            type="number"
            variant="outlined"
          />
        </v-col>
        <v-col cols="12" sm="6" md="12" class="remote-outbounds__form-actions">
          <v-btn color="primary" :loading="saving" type="submit">
            {{ form.id ? $t('actions.update') : $t('actions.add') }}
          </v-btn>
          <v-btn v-if="form.id" variant="text" @click="resetForm">
            {{ $t('actions.cancel') }}
          </v-btn>
        </v-col>
      </v-row>
    </v-form>

    <v-progress-linear v-if="loading && subscriptions.length === 0" indeterminate />

    <v-expansion-panels v-if="filteredSubscriptions.length > 0" multiple variant="accordion">
      <v-expansion-panel
        v-for="subscription in filteredSubscriptions"
        :key="subscription.id"
        class="remote-outbounds__subscription"
      >
        <v-expansion-panel-title>
          <div class="remote-outbounds__title">
            <div>
              <strong>{{ subscription.name }}</strong>
              <span class="remote-outbounds__url">{{ subscription.url }}</span>
            </div>
            <div class="remote-outbounds__chips">
              <v-chip density="compact" size="small" :color="subscription.enabled ? 'success' : undefined" variant="flat">
                {{ subscription.enabled ? $t('enable') : $t('disable') }}
              </v-chip>
              <v-chip v-if="subscription.autoUpdate" density="compact" size="small" color="info" variant="tonal">
                {{ $t('remoteOutbound.autoUpdate') }}
              </v-chip>
              <v-chip density="compact" size="small" variant="tonal">
                {{ subscription.groups?.length ?? 0 }} {{ $t('remoteOutbound.groups') }}
              </v-chip>
              <v-chip density="compact" size="small" variant="tonal">
                {{ subscription.connections?.length ?? 0 }} {{ $t('remoteOutbound.connections') }}
              </v-chip>
              <v-chip v-if="subscription.lastError" density="compact" size="small" color="error" variant="flat">
                {{ $t('failed') }}
              </v-chip>
            </div>
          </div>
        </v-expansion-panel-title>

        <v-expansion-panel-text>
          <div class="remote-outbounds__meta">
            <span>{{ $t('remoteOutbound.tagPrefix') }}: <code>{{ subscription.tagPrefix || '-' }}</code></span>
            <span>{{ $t('remoteOutbound.updateInterval') }}: {{ formatInterval(subscription.updateInterval) }}</span>
            <span>{{ $t('remoteOutbound.lastUpdated') }}: {{ formatTime(subscription.lastUpdated) }}</span>
            <span v-if="subscription.lastError" class="remote-outbounds__error">{{ subscription.lastError }}</span>
          </div>

          <div class="remote-outbounds__actions">
            <v-btn size="small" prepend-icon="mdi-download" :loading="refreshing[subscription.id]" @click="refreshSubscription(subscription.id)">
              {{ $t('remoteOutbound.refreshSubscription') }}
            </v-btn>
            <v-btn
              size="small"
              prepend-icon="mdi-speedometer"
              :disabled="testableConnections(subscription).length === 0"
              :loading="testingSubscriptions[subscription.id]"
              @click="testSubscription(subscription.id)"
            >
              {{ $t('out.delay') }}
            </v-btn>
            <v-btn size="small" prepend-icon="mdi-pencil" variant="tonal" @click="editSubscription(subscription)">
              {{ $t('actions.edit') }}
            </v-btn>
            <v-btn size="small" color="error" prepend-icon="mdi-delete" variant="tonal" @click="deleteSubscription(subscription)">
              {{ $t('actions.del') }}
            </v-btn>
          </div>

          <div class="remote-outbounds__group-form">
            <v-text-field
              v-model="groupNames[subscription.id]"
              density="compact"
              hide-details
              :label="$t('remoteOutbound.newGroup')"
              variant="outlined"
              @keyup.enter="saveGroup(subscription.id)"
            />
            <v-btn size="small" variant="tonal" @click="saveGroup(subscription.id)">
              {{ $t('actions.add') }}
            </v-btn>
          </div>

          <div class="remote-outbounds__groups">
            <div
              v-for="group in subscription.groups ?? []"
              :key="group.id"
              class="remote-outbounds__group"
            >
              <div class="remote-outbounds__group-head">
                <div>
                  <strong>{{ group.name }}</strong>
                  <span>{{ groupConnectionCount(subscription, group) }} / {{ subscription.connections?.length ?? 0 }}</span>
                </div>
                <div class="remote-outbounds__group-actions">
                  <v-btn
                    size="small"
                    prepend-icon="mdi-speedometer"
                    :disabled="usableGroupCount(subscription, group) === 0"
                    :loading="testingGroups[group.id]"
                    variant="tonal"
                    @click="testGroup(subscription, group)"
                  >
                    {{ $t('out.delay') }}
                  </v-btn>
                  <v-btn
                    size="small"
                    :color="groupOutboundOn(group) ? 'success' : undefined"
                    :disabled="usableGroupCount(subscription, group) === 0"
                    :loading="togglingGroups[group.id]"
                    variant="tonal"
                    @click="toggleGroupOutbounds(group.id)"
                  >
                    {{ $t('remoteOutbound.outbound') }}
                  </v-btn>
                  <v-btn
                    v-if="!isDefaultGroup(group)"
                    :aria-label="$t('actions.del')"
                    color="error"
                    icon
                    size="x-small"
                    variant="tonal"
                    @click="deleteGroup(group)"
                  >
                    <v-icon icon="mdi-delete" size="18" />
                  </v-btn>
                </div>
              </div>
              <RemoteGroupConnectionList
                v-model:search="groupConnectionSearch[group.id]"
                :connections="subscription.connections ?? []"
                :empty-text="$t('table.noData')"
                :loading="savingGroups[group.id]"
                :search-label="$t('remoteOutbound.selectConnections')"
                :selected-ids="groupConnectionIds(subscription, group)"
                @toggle="(connectionId, checked) => toggleGroupConnection(subscription, group, connectionId, checked)"
              />
            </div>
          </div>

          <v-table density="compact" class="remote-outbounds__table">
            <thead>
              <tr>
                <th>{{ $t('remoteOutbound.connection') }}</th>
                <th>{{ $t('type') }}</th>
                <th>{{ $t('objects.tag') }}</th>
                <th>{{ $t('remoteOutbound.group') }}</th>
                <th>{{ $t('status') }}</th>
                <th>{{ $t('out.delay') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="connection in subscription.connections ?? []" :key="connection.id">
                <td>
                  <span class="remote-outbounds__connection-name">{{ connection.name }}</span>
                </td>
                <td>{{ connection.type }}</td>
                <td><code>{{ connection.outboundTag }}</code></td>
                <td>{{ connectionGroupNames(subscription, connection) }}</td>
                <td>
                  <div class="remote-outbounds__status">
                    <v-chip v-if="connection.synced" density="compact" size="small" color="success" variant="flat">
                      {{ $t('remoteOutbound.synced') }}
                    </v-chip>
                    <v-chip v-else density="compact" size="small" variant="tonal">
                      {{ $t('remoteOutbound.notSynced') }}
                    </v-chip>
                    <v-chip v-if="connection.missing" density="compact" size="small" color="warning" variant="flat">
                      {{ $t('remoteOutbound.missing') }}
                    </v-chip>
                  </div>
                </td>
                <td>
                  <div class="remote-outbounds__delay">
                    <v-progress-circular v-if="testingConnections[connection.id]" indeterminate size="18" />
                    <v-icon
                      v-else
                      class="remote-outbounds__delay-icon"
                      icon="mdi-speedometer"
                      :color="connection.enabled && !connection.missing ? undefined : 'disabled'"
                      @click="connection.enabled && !connection.missing ? testConnection(connection.id) : undefined"
                    >
                      <v-tooltip activator="parent" location="top" :text="$t('out.delay')" />
                    </v-icon>
                    <template v-if="testResults[connection.id]">
                      <v-chip
                        v-if="testResults[connection.id].ok"
                        color="success"
                        density="compact"
                        size="small"
                        variant="flat"
                      >
                        {{ testResults[connection.id].delay }}{{ $t('date.ms') }}
                      </v-chip>
                      <v-tooltip v-else location="top" :text="testResults[connection.id].error || $t('failed')">
                        <template #activator="{ props }">
                          <v-icon v-bind="props" color="error" icon="mdi-close-circle" size="small" />
                        </template>
                      </v-tooltip>
                    </template>
                  </div>
                </td>
              </tr>
              <tr v-if="!subscription.connections || subscription.connections.length === 0">
                <td colspan="6" class="remote-outbounds__empty">{{ $t('remoteOutbound.noConnections') }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>

    <div v-else-if="!loading" class="remote-outbounds__empty-state">
      <v-icon icon="mdi-cloud-download" size="32" />
      <strong>{{ $t('pages.remoteOutboundSubscriptions') }}</strong>
      <span>{{ $t('remoteOutbound.empty') }}</span>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'

import PageHeader from '@/components/nexus/primitives/PageHeader.vue'
import PageToolbar from '@/components/nexus/primitives/PageToolbar.vue'
import RemoteGroupConnectionList from '@/components/remote/RemoteGroupConnectionList.vue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import { useUiMode } from '@/uiMode/useUiMode'
import { runWithConcurrency, useAsyncTaskQueue } from '@/composables/useAsyncTaskQueue'

interface RemoteOutboundGroup {
  id: number
  subscriptionId: number
  name: string
  enabled: boolean
  outboundEnabled: boolean
  sortOrder: number
}

interface RemoteOutboundConnection {
  id: number
  subscriptionId: number
  groupId: number
  groupIds?: number[]
  name: string
  type: string
  outboundTag: string
  enabled: boolean
  missing: boolean
  synced: boolean
  sortOrder: number
}

interface RemoteOutboundSubscription {
  id: number
  sortOrder: number
  name: string
  url: string
  enabled: boolean
  tagPrefix: string
  autoUpdate: boolean
  updateInterval: number
  lastUpdated: number
  lastError: string
  groups?: RemoteOutboundGroup[]
  connections?: RemoteOutboundConnection[]
}

interface TestState {
  ok: boolean
  delay: number
  error: string
}

interface SubscriptionFormRef {
  validate: () => Promise<{ valid: boolean }>
  resetValidation: () => void
}

const { mode } = useUiMode()
const { t } = useI18n()

const subscriptions = ref<RemoteOutboundSubscription[]>([])
const subscriptionForm = ref<SubscriptionFormRef | null>(null)
const search = ref('')
const loading = ref(false)
const saving = ref(false)
const testingAll = ref(false)
const refreshing = reactive<Record<number, boolean>>({})
const testingSubscriptions = reactive<Record<number, boolean>>({})
const testingGroups = reactive<Record<number, boolean>>({})
const connectionCheckQueue = useAsyncTaskQueue(8)
const testingConnections = connectionCheckQueue.active
const savingGroups = reactive<Record<number, boolean>>({})
const togglingGroups = reactive<Record<number, boolean>>({})
const groupNames = reactive<Record<number, string>>({})
const groupConnectionSearch = reactive<Record<number, string>>({})
const testResults = reactive<Record<number, TestState>>({})
const requiredRules = computed(() => [
  (value: unknown) => Boolean(String(value ?? '').trim()) || t('remoteOutbound.requiredField'),
])

const form = reactive({
  id: 0,
  name: '',
  url: '',
  tagPrefix: '',
  enabled: true,
  autoUpdate: false,
  updateInterval: 86400,
})

const testableConnections = (subscription: RemoteOutboundSubscription): RemoteOutboundConnection[] => {
  return (subscription.connections ?? []).filter(connection => connection.enabled && !connection.missing)
}

const updateIntervalMinutes = computed({
  get: () => Math.max(5, Math.round((Number(form.updateInterval) || 86400) / 60)),
  set: (value: number) => {
    const minutes = Math.max(5, Number(value) || 1440)
    form.updateInterval = minutes * 60
  },
})

const totalConnections = computed(() => subscriptions.value.reduce((sum, subscription) => {
  return sum + (subscription.connections?.length ?? 0)
}, 0))

const totalTestableConnections = computed(() => subscriptions.value.reduce((sum, subscription) => {
  return sum + testableConnections(subscription).length
}, 0))

const totalSynced = computed(() => subscriptions.value.reduce((sum, subscription) => {
  return sum + (subscription.connections ?? []).filter(connection => connection.synced).length
}, 0))

const subtitle = computed(() => {
  const total = subscriptions.value.length
  return t('remoteOutbound.summary', { total, connections: totalConnections.value, synced: totalSynced.value })
})

const filteredSubscriptions = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return subscriptions.value
  return subscriptions.value.filter((subscription) => {
    if (subscription.name.toLowerCase().includes(query) || subscription.url.toLowerCase().includes(query)) return true
    if ((subscription.tagPrefix ?? '').toLowerCase().includes(query)) return true
    if ((subscription.groups ?? []).some(group => group.name.toLowerCase().includes(query))) return true
    return (subscription.connections ?? []).some(connection =>
      connection.name.toLowerCase().includes(query) ||
      connection.outboundTag.toLowerCase().includes(query) ||
      connection.type.toLowerCase().includes(query),
    )
  })
})

const load = async () => {
  loading.value = true
  try {
    const msg = await HttpUtils.get('api/remote-outbound-subscriptions')
    if (msg.success) {
      subscriptions.value = msg.obj ?? []
    }
  } finally {
    loading.value = false
  }
}

const saveSubscription = async () => {
  const validation = await subscriptionForm.value?.validate()
  if (validation && !validation.valid) return
  saving.value = true
  try {
    const payload = {
      id: form.id,
      name: form.name,
      url: form.url,
      tagPrefix: form.tagPrefix,
      enabled: form.enabled,
      autoUpdate: form.autoUpdate,
      updateInterval: form.updateInterval,
    }
    const msg = await HttpUtils.post('api/remote-outbound-subscriptions/save', {
      data: JSON.stringify(payload),
    })
    if (msg.success) {
      resetForm()
      await load()
      await Data().loadData()
    }
  } finally {
    saving.value = false
  }
}

const resetForm = () => {
  form.id = 0
  form.name = ''
  form.url = ''
  form.tagPrefix = ''
  form.enabled = true
  form.autoUpdate = false
  form.updateInterval = 86400
  nextTick(() => subscriptionForm.value?.resetValidation())
}

const editSubscription = (subscription: RemoteOutboundSubscription) => {
  form.id = subscription.id
  form.name = subscription.name
  form.url = subscription.url
  form.tagPrefix = subscription.tagPrefix
  form.enabled = subscription.enabled
  form.autoUpdate = Boolean(subscription.autoUpdate)
  form.updateInterval = subscription.updateInterval || 86400
}

const deleteSubscription = async (subscription: RemoteOutboundSubscription) => {
  if (!window.confirm(`${t('actions.del')} ${subscription.name}?`)) return
  const msg = await HttpUtils.post('api/remote-outbound-subscriptions/delete', { id: subscription.id })
  if (msg.success) {
    await load()
    await Data().loadData()
  }
}

const refreshSubscription = async (id: number) => {
  refreshing[id] = true
  try {
    const msg = await HttpUtils.post('api/remote-outbound-subscriptions/refresh', { id })
    if (msg.success) {
      await load()
      await Data().loadData()
    }
  } finally {
    refreshing[id] = false
  }
}

const saveGroup = async (subscriptionId: number) => {
  const name = (groupNames[subscriptionId] ?? '').trim()
  if (!name) return
  const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/save', {
    data: JSON.stringify({ subscriptionId, name, enabled: true }),
  })
  if (msg.success) {
    groupNames[subscriptionId] = ''
    await load()
  }
}

const deleteGroup = async (group: RemoteOutboundGroup) => {
  if (isDefaultGroup(group)) return
  if (!window.confirm(`${t('actions.del')} ${group.name}?`)) return
  const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/delete', { id: group.id })
  if (msg.success) {
    await load()
  }
}

const saveGroupConnections = async (groupId: number, ids: unknown) => {
  const connectionIds = Array.isArray(ids) ? ids.map(Number).filter(Boolean) : []
  savingGroups[groupId] = true
  try {
    const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/connections', {
      data: JSON.stringify({ groupId, connectionIds }),
    })
    if (msg.success) {
      await load()
    }
  } finally {
    savingGroups[groupId] = false
  }
}

const toggleGroupConnection = async (
  subscription: RemoteOutboundSubscription,
  group: RemoteOutboundGroup,
  connectionId: number,
  checked: boolean,
) => {
  const ids = new Set(groupConnectionIds(subscription, group))
  if (checked) ids.add(connectionId)
  else ids.delete(connectionId)
  await saveGroupConnections(group.id, [...ids])
}

const toggleGroupOutbounds = async (groupId: number) => {
  togglingGroups[groupId] = true
  try {
    const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/outbounds', { groupId })
    if (msg.success) {
      await load()
      await Data().loadData()
    }
  } finally {
    togglingGroups[groupId] = false
  }
}

const recordTestFailure = (connectionId: number, error: string) => {
  if (!connectionId) return
  testResults[connectionId] = {
    ok: false,
    delay: 0,
    error,
  }
}

const performConnectionTest = async (id: number): Promise<boolean> => {
  await nextTick()
  const msg = await HttpUtils.get('api/remote-outbound-subscriptions/connections/test', { id })
  if (msg.success) {
    recordTestResult(id, msg.obj)
    return Boolean(testResults[id]?.ok)
  }

  const error = msg.msg || t('failed')
  recordTestFailure(id, error)
  push.error({ message: error, duration: 5000 })
  return false
}

const testConnection = async (id: number): Promise<boolean> => {
  try {
    const result = await connectionCheckQueue.runOne(id, () => performConnectionTest(id))
    return Boolean(result)
  } catch (error: any) {
    const message = String(error?.message ?? error ?? t('failed'))
    recordTestFailure(id, message)
    push.error({ message, duration: 5000 })
    return false
  }
}

const testConnections = async (connections: RemoteOutboundConnection[]) => {
  const unique = new Map<number, RemoteOutboundConnection>()
  for (const connection of connections) unique.set(connection.id, connection)
  await runWithConcurrency([...unique.values()], async (connection) => {
    await testConnection(connection.id)
  }, 8)
}

const testSubscription = async (id: number) => {
  testingSubscriptions[id] = true
  try {
    const subscription = subscriptions.value.find(item => item.id === id)
    if (!subscription) return
    await testConnections(testableConnections(subscription))
  } finally {
    testingSubscriptions[id] = false
  }
}

const testGroup = async (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup) => {
  testingGroups[group.id] = true
  try {
    await testConnections(usableGroupConnections(subscription, group))
  } finally {
    testingGroups[group.id] = false
  }
}

const testAll = async () => {
  testingAll.value = true
  try {
    const unique = new Map<number, RemoteOutboundConnection>()
    for (const subscription of subscriptions.value) {
      for (const connection of testableConnections(subscription)) {
        unique.set(connection.id, connection)
      }
    }
    await testConnections([...unique.values()])
  } finally {
    testingAll.value = false
  }
}

const recordTestResult = (connectionId: number, payload: any) => {
  if (!connectionId || !payload) return
  const result = payload.result ?? payload.Result ?? payload
  const skippedError = payload.error ?? payload.Error ?? ''
  testResults[connectionId] = {
    ok: Boolean(result?.OK),
    delay: Number(result?.Delay ?? 0),
    error: String(result?.Error ?? skippedError ?? ''),
  }
}

const groupConnectionIds = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): number[] => {
  return (subscription.connections ?? [])
    .filter(connection => connectionGroupIds(connection).includes(group.id))
    .map(connection => connection.id)
}

const groupConnectionCount = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): number => {
  return groupConnectionIds(subscription, group).length
}

const usableGroupConnections = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): RemoteOutboundConnection[] => {
  return (subscription.connections ?? [])
    .filter(connection => connectionGroupIds(connection).includes(group.id) && connection.enabled && !connection.missing)
}

const usableGroupCount = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): number => {
  return usableGroupConnections(subscription, group).length
}

const groupOutboundOn = (group: RemoteOutboundGroup): boolean => {
  return Boolean(group.outboundEnabled)
}

const connectionGroupNames = (subscription: RemoteOutboundSubscription, connection: RemoteOutboundConnection): string => {
  const ids = connectionGroupIds(connection)
  const names = (subscription.groups ?? [])
    .filter(group => ids.includes(group.id))
    .map(group => group.name)
  return names.length > 0 ? names.join(', ') : '-'
}

const connectionGroupIds = (connection: RemoteOutboundConnection): number[] => {
  if (connection.groupIds && connection.groupIds.length > 0) return connection.groupIds
  return connection.groupId ? [connection.groupId] : []
}

const isDefaultGroup = (group: RemoteOutboundGroup): boolean => {
  return group.name === 'Default'
}

const formatTime = (value: number) => {
  if (!value) return '-'
  return new Date(value * 1000).toLocaleString()
}

const formatInterval = (value: number) => {
  const seconds = Number(value) || 0
  if (seconds <= 0) return '-'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} h`
  return `${Math.round(hours / 24)} d`
}

onMounted(load)
</script>

<style scoped>
.remote-outbounds {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.remote-outbounds__form {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
  padding: 16px;
}

.remote-outbounds__form-actions,
.remote-outbounds__actions,
.remote-outbounds__group-form,
.remote-outbounds__group-actions,
.remote-outbounds__status,
.remote-outbounds__delay,
.remote-outbounds__chips {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.remote-outbounds__title {
  align-items: center;
  display: flex;
  gap: 12px;
  justify-content: space-between;
  width: 100%;
}

.remote-outbounds__url {
  color: rgba(var(--v-theme-on-surface), .64);
  display: block;
  font-size: .78rem;
  margin-top: 2px;
  max-width: min(72vw, 760px);
  overflow-wrap: anywhere;
}

.remote-outbounds code {
  font-family: "Segoe UI", "Segoe UI Emoji", "Apple Color Emoji", "Noto Color Emoji", sans-serif;
  user-select: text;
}

.remote-outbounds__meta {
  color: rgba(var(--v-theme-on-surface), .72);
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
  margin-bottom: 12px;
}

.remote-outbounds__error {
  color: rgb(var(--v-theme-error));
  overflow-wrap: anywhere;
}

.remote-outbounds__group-form {
  margin: 12px 0;
  max-width: 420px;
}

.remote-outbounds__groups {
  display: grid;
  gap: 10px;
  margin: 12px 0 16px;
}

.remote-outbounds__group {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
  padding: 8px;
}

.remote-outbounds__group-head {
  align-items: center;
  display: flex;
  gap: 12px;
  justify-content: space-between;
  margin-bottom: 6px;
}

.remote-outbounds__group-head span {
  color: rgba(var(--v-theme-on-surface), .62);
  display: block;
  font-size: .78rem;
  margin-top: 2px;
}

.remote-outbounds__table {
  margin-top: 8px;
}

.remote-outbounds__connection-name {
  font-weight: 600;
}

.remote-outbounds__delay-icon {
  cursor: pointer;
}

.remote-outbounds__empty {
  color: rgba(var(--v-theme-on-surface), .64);
  text-align: center;
}

.remote-outbounds__empty-state {
  align-items: center;
  border: 1px dashed rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 8px;
  color: rgba(var(--v-theme-on-surface), .68);
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 32px;
  text-align: center;
}

.remote-outbounds--nexus .remote-outbounds__form,
.remote-outbounds--nexus .remote-outbounds__subscription,
.remote-outbounds--nexus .remote-outbounds__group {
  border-color: var(--nexus-border-subtle);
}

@media (max-width: 720px) {
  .remote-outbounds__title,
  .remote-outbounds__group-head {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
