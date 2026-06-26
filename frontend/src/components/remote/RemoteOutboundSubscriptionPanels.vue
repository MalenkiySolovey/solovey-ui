<template>
  <v-expansion-panels v-if="page.filteredSubscriptions.length > 0" multiple variant="accordion">
    <v-expansion-panel
      v-for="subscription in page.filteredSubscriptions"
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
          <span>{{ $t('remoteOutbound.updateInterval') }}: {{ page.formatInterval(subscription.updateInterval) }}</span>
          <span>{{ $t('remoteOutbound.lastUpdated') }}: {{ page.formatTime(subscription.lastUpdated) }}</span>
          <span v-if="subscription.lastError" class="remote-outbounds__error">{{ subscription.lastError }}</span>
        </div>

        <div class="remote-outbounds__actions">
          <v-btn size="small" prepend-icon="mdi-download" :loading="page.refreshing[subscription.id]" variant="tonal" @click="page.refreshSubscription(subscription.id)">
            {{ $t('actions.refresh') }}
          </v-btn>
          <v-btn size="small" prepend-icon="mdi-file-document" variant="tonal" @click="page.openCollectedData(subscription)">
            {{ $t('remoteOutbound.collectedData') }}
          </v-btn>
          <v-btn
            size="small"
            prepend-icon="mdi-speedometer"
            :disabled="page.testableConnections(subscription).length === 0"
            :loading="page.testingSubscriptions[subscription.id]"
            variant="tonal"
            @click="page.testSubscription(subscription.id)"
          >
            {{ $t('out.delay') }}
          </v-btn>
          <v-btn size="small" prepend-icon="mdi-pencil" variant="tonal" @click="page.editSubscription(subscription)">
            {{ $t('actions.edit') }}
          </v-btn>
          <v-btn size="small" color="error" prepend-icon="mdi-delete" variant="tonal" @click="page.deleteSubscription(subscription)">
            {{ $t('actions.del') }}
          </v-btn>
        </div>

        <div class="remote-outbounds__group-form">
          <v-text-field
            v-model="page.groupNames[subscription.id]"
            density="compact"
            hide-details
            :label="$t('remoteOutbound.newGroup')"
            variant="outlined"
            @keyup.enter="page.saveGroup(subscription.id)"
          />
          <v-btn size="small" variant="tonal" @click="page.saveGroup(subscription.id)">
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
                <span>{{ page.groupConnectionCount(subscription, group) }} / {{ subscription.connections?.length ?? 0 }}</span>
              </div>
              <div class="remote-outbounds__group-actions">
                <v-btn
                  size="small"
                  prepend-icon="lucide:list-plus"
                  :disabled="page.savingGroups[group.id] || (subscription.connections?.length ?? 0) === 0"
                  variant="tonal"
                  @click="page.setGroupConnectionsBulk(subscription, group, 'all')"
                >
                  Add all
                </v-btn>
                <v-btn
                  size="small"
                  prepend-icon="lucide:list-x"
                  :disabled="page.savingGroups[group.id] || page.groupConnectionCount(subscription, group) === 0"
                  variant="tonal"
                  @click="page.setGroupConnectionsBulk(subscription, group, 'none')"
                >
                  Remove all
                </v-btn>
                <v-btn
                  size="small"
                  prepend-icon="lucide:shuffle"
                  :disabled="page.savingGroups[group.id] || (subscription.connections?.length ?? 0) === 0"
                  variant="tonal"
                  @click="page.setGroupConnectionsBulk(subscription, group, 'invert')"
                >
                  Invert
                </v-btn>
                <v-btn
                  size="small"
                  prepend-icon="mdi-speedometer"
                  :disabled="page.usableGroupCount(subscription, group) === 0"
                  :loading="page.testingGroups[group.id]"
                  variant="tonal"
                  @click="page.testGroup(subscription, group)"
                >
                  {{ $t('out.delay') }}
                </v-btn>
                <v-btn
                  size="small"
                  :color="page.groupOutboundOn(group) ? 'success' : undefined"
                  :disabled="page.usableGroupCount(subscription, group) === 0"
                  :loading="page.togglingGroups[group.id]"
                  variant="tonal"
                  @click="page.toggleGroupOutbounds(group.id)"
                >
                  {{ $t('remoteOutbound.outbound') }}
                </v-btn>
                <v-btn
                  v-if="!page.isDefaultGroup(group)"
                  :aria-label="$t('actions.del')"
                  color="error"
                  icon
                  size="x-small"
                  variant="tonal"
                  @click="page.deleteGroup(group)"
                >
                  <v-icon icon="mdi-delete" size="18" />
                </v-btn>
              </div>
            </div>
            <RemoteGroupConnectionList
              v-model:search="page.groupConnectionSearch[group.id]"
              :connections="subscription.connections ?? []"
              :empty-text="$t('table.noData')"
              :loading="page.savingGroups[group.id]"
              :search-label="$t('remoteOutbound.selectConnections')"
              :selected-ids="page.groupConnectionIds(subscription, group)"
              @toggle="(connectionId, checked) => page.toggleGroupConnection(subscription, group, connectionId, checked)"
            />
          </div>
        </div>

        <v-table density="compact" class="remote-outbounds__table">
          <thead>
            <tr>
              <th>{{ $t('remoteOutbound.connection') }}</th>
              <th>{{ $t('type') }}</th>
              <th>{{ $t('remoteOutbound.convertedType') }}</th>
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
              <td>{{ page.connectionSourceType(connection) }}</td>
              <td>{{ page.connectionConvertedType(connection) }}</td>
              <td><code>{{ connection.outboundTag }}</code></td>
              <td>{{ page.connectionGroupNames(subscription, connection) }}</td>
              <td>
                <div class="remote-outbounds__status">
                  <v-chip v-if="connection.synced" density="compact" size="small" color="success" variant="flat">
                    {{ $t('remoteOutbound.synced') }}
                  </v-chip>
                  <v-chip v-else density="compact" size="small" variant="tonal">
                    {{ $t('remoteOutbound.notSynced') }}
                  </v-chip>
                </div>
              </td>
              <td>
                <div class="remote-outbounds__delay">
                  <v-progress-circular v-if="page.testingConnections[connection.id]" indeterminate size="18" />
                  <v-icon
                    v-else
                    class="remote-outbounds__delay-icon"
                    icon="mdi-speedometer"
                    :color="connection.enabled ? undefined : 'disabled'"
                    @click="connection.enabled ? page.testConnection(connection.id) : undefined"
                  >
                    <v-tooltip activator="parent" location="top" :text="$t('out.delay')" />
                  </v-icon>
                  <template v-if="page.testResults[connection.id]">
                    <v-chip
                      v-if="page.testResults[connection.id].ok"
                      color="success"
                      density="compact"
                      size="small"
                      variant="flat"
                    >
                      {{ page.testResults[connection.id].delay }}{{ $t('date.ms') }}
                    </v-chip>
                    <v-tooltip v-else location="top" :text="page.testResults[connection.id].error || $t('failed')">
                      <template #activator="{ props }">
                        <v-icon v-bind="props" color="error" icon="mdi-close-circle" size="small" />
                      </template>
                    </v-tooltip>
                  </template>
                </div>
              </td>
            </tr>
            <tr v-if="!subscription.connections || subscription.connections.length === 0">
              <td colspan="7" class="remote-outbounds__empty">{{ $t('remoteOutbound.noConnections') }}</td>
            </tr>
          </tbody>
        </v-table>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>

  <div v-else-if="!page.loading" class="remote-outbounds__empty-state">
    <v-icon icon="mdi-cloud-download" size="32" />
    <strong>{{ $t('pages.remoteOutboundSubscriptions') }}</strong>
    <span>{{ $t('remoteOutbound.empty') }}</span>
  </div>
</template>

<script lang="ts" setup>
import RemoteGroupConnectionList from '@/components/remote/RemoteGroupConnectionList.vue'

defineProps<{
  page: Record<string, any>
}>()
</script>

<style scoped lang="scss" src="../../views/RemoteOutboundSubscriptions.scss"></style>
