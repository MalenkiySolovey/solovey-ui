<template>
  <v-dialog v-model="open" max-width="1050" scrollable>
    <v-card :loading="busy">
      <v-card-title>{{ $t('clientDevices.title') }}</v-card-title>
      <v-card-text>
        <v-alert v-if="failed" type="warning" role="alert" class="mb-3">{{ $t('clientDevices.failed') }}</v-alert>
        <v-alert v-if="view && !view.available" type="warning" class="mb-3">{{ $t('clientDevices.preserved') }}</v-alert>
        <p class="mb-3">{{ $t('clientDevices.privateHint') }}</p>
        <template v-if="view?.available">
          <v-row v-if="creatableEndpoints.length">
            <v-col cols="12" sm="6"><v-select v-model="endpointId" :items="creatableEndpoints" item-title="tag" item-value="id" :label="$t('objects.endpoint')" /></v-col>
            <v-col cols="12" sm="3"><v-text-field v-model.number="deviceLimit" type="number" min="0" max="100" :label="$t('clientDevices.limit')" :hint="$t('clientDevices.defaultLimit')" persistent-hint /></v-col>
            <v-col cols="auto"><v-btn :disabled="busy || !endpointId" @click="mutate('grant')">{{ $t('clientDevices.grant') }}</v-btn></v-col>
          </v-row>
          <v-table v-if="view.grants.length" density="compact">
            <thead><tr><th>{{ $t('objects.endpoint') }}</th><th>{{ $t('clientDevices.limit') }}</th><th>{{ $t('actions.action') }}</th></tr></thead>
            <tbody><tr v-for="grant in view.grants" :key="grant.endpointId">
              <td>{{ endpointName(grant.endpointId) }}<span v-if="!endpointAvailable(grant.endpointId)"> — {{ $t('clientDevices.states.unavailable') }}</span></td>
              <td>{{ grant.deviceLimit || $t('clientDevices.defaultLimit') }}</td>
              <td><v-btn :disabled="busy" size="small" @click="mutate('remove-grant', undefined, grant.endpointId)">{{ $t('clientDevices.removeGrant') }}</v-btn></td>
            </tr></tbody>
          </v-table>
          <v-row v-if="view.canCreate && grantedEndpoints.length" class="mt-3">
            <v-col cols="12" sm="4"><v-select v-model="createEndpointId" :items="grantedEndpoints" item-title="tag" item-value="id" :label="$t('objects.endpoint')" /></v-col>
            <v-col cols="12" sm="4"><v-text-field v-model="name" :label="$t('clientDevices.name')" maxlength="64" /></v-col>
            <v-col cols="12" sm="4"><v-text-field v-model="expiry" type="datetime-local" :label="$t('date.expiry')" clearable /></v-col>
            <v-col cols="auto"><v-btn :disabled="busy || !createEndpointId || !name.trim()" @click="mutate('create', undefined, createEndpointId)">{{ $t('clientDevices.create') }}</v-btn></v-col>
          </v-row>
        </template>
        <v-table v-if="view?.devices.length" density="compact">
          <thead><tr><th>{{ $t('clientDevices.name') }}</th><th>{{ $t('objects.endpoint') }}</th><th>{{ $t('clientDevices.address') }}</th><th>{{ $t('clientDevices.state') }}</th><th>{{ $t('clientDevices.runtime') }}</th><th>{{ $t('clientDevices.traffic') }}</th><th>{{ $t('actions.action') }}</th></tr></thead>
          <tbody><tr v-for="device in view.devices" :key="device.id">
            <td>{{ device.name }}<br /><small>{{ device.id }}</small></td><td>{{ endpointName(device.endpointId) }}</td><td dir="ltr">{{ device.address }}</td>
            <td>{{ $t('clientDevices.states.' + device.status) }}<br /><small v-if="device.expiresAt">{{ new Date(device.expiresAt * 1000).toLocaleString() }}</small></td>
            <td>{{ $t('clientDevices.runtimeStates.' + device.runtimeState) }}<br /><small v-if="device.observation.lastHandshake">{{ $t('clientDevices.handshake') }}: {{ new Date(device.observation.lastHandshake * 1000).toLocaleString() }}</small></td>
            <td dir="ltr">↑ {{ utils.sizeFormat(device.up) }}<br />↓ {{ utils.sizeFormat(device.down) }}</td>
            <td>
              <v-btn v-if="device.canExport" :disabled="busy" size="small" @click="download(device)">{{ $t('clientDevices.download') }}</v-btn>
              <v-btn v-if="device.status === 'eligible'" :disabled="busy" size="small" @click="confirmMutation('rotate', device)">{{ $t('clientDevices.rotate') }}</v-btn>
              <v-btn v-if="!device.revokedAt && !device.deletedAt" :disabled="busy" size="small" color="warning" @click="confirmMutation('revoke', device)">{{ $t('clientDevices.revoke') }}</v-btn>
              <v-btn v-if="!device.deletedAt" :disabled="busy" size="small" color="error" @click="confirmMutation('delete', device)">{{ $t('actions.del') }}</v-btn>
            </td>
          </tr></tbody>
        </v-table>
      </v-card-text>
      <v-card-actions><v-btn :disabled="busy" @click="refresh">{{ $t('actions.refresh') }}</v-btn><v-spacer /><v-btn @click="open = false">{{ $t('actions.close') }}</v-btn></v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HttpUtils from '@/plugins/httputil'
import api from '@/plugins/api'
import Data from '@/store/modules/data'
import { HumanReadable as utils } from '@/plugins/utils'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'

interface Device { id: string; endpointId: number; name: string; address: string; publicKey: string; expiresAt: number; revokedAt: number; deletedAt: number; status: string; canExport: boolean; runtimeState: string; up: number; down: number; observation: { lastHandshake: number } }
interface Endpoint { id: number; tag: string; available: boolean }
interface View { available: boolean; canCreate: boolean; grants: { endpointId: number; deviceLimit: number }[]; devices: Device[]; endpoints: Endpoint[] }
const props = defineProps<{ clientId: number }>()
const emit = defineEmits<{ changed: [] }>()
const open = defineModel<boolean>({ default: false })
const { t } = useI18n()
const { confirm } = useConfirm()
const view = ref<View>()
const busy = ref(false), failed = ref(false)
const endpointId = ref<number>(), createEndpointId = ref<number>(), deviceLimit = ref(0), name = ref(''), expiry = ref('')
const creatableEndpoints = computed(() => view.value?.endpoints.filter(endpoint => endpoint.available) ?? [])
const grantedEndpoints = computed(() => creatableEndpoints.value.filter(endpoint => view.value?.grants.some(grant => grant.endpointId === endpoint.id)))
const endpointName = (id: number) => view.value?.endpoints.find(endpoint => endpoint.id === id)?.tag ?? `#${id}`
const endpointAvailable = (id: number) => view.value?.endpoints.some(endpoint => endpoint.id === id && endpoint.available) === true
let epoch = 0, controller: AbortController | undefined
const requestKeys = new Map<string, string>()

async function refresh() {
  if (!open.value || !props.clientId) return
  const current = ++epoch
  controller?.abort()
  controller = new AbortController()
  busy.value = true
  failed.value = false
  try {
    const response = await HttpUtils.getRaw<{ success: boolean; obj: View }>('api/client-devices', { clientId: props.clientId }, { signal: controller.signal, timeout: 10000 })
    if (current !== epoch) return
    if (!response.success || !response.obj) throw new Error('unavailable')
    view.value = response.obj
    if (!creatableEndpoints.value.some(endpoint => endpoint.id === endpointId.value)) endpointId.value = creatableEndpoints.value[0]?.id
    if (!grantedEndpoints.value.some(endpoint => endpoint.id === createEndpointId.value)) createEndpointId.value = grantedEndpoints.value[0]?.id
  } catch { if (current === epoch) failed.value = true }
  finally { if (current === epoch) busy.value = false }
}

async function mutate(operation: string, device?: Device, target = endpointId.value) {
  if (busy.value || !target || !open.value) return
  const current = epoch, clientId = props.clientId
  busy.value = true
  failed.value = false
  const expiresAt = expiry.value ? Math.floor(new Date(expiry.value).getTime() / 1000) : 0
  const signature = JSON.stringify([clientId, target, device?.id, operation, name.value.trim(), expiresAt])
  let requestKey = requestKeys.get(signature)
  if (!requestKey) { requestKey = Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join(''); requestKeys.set(signature, requestKey) }
  try {
    const success = await Data().save('clients', 'wireguard', { clientId, endpointId: target, deviceId: device?.id ?? '', operation, requestKey, name: name.value.trim(), expiresAt, deviceLimit: deviceLimit.value })
    if (current !== epoch || clientId !== props.clientId || !open.value) return
    if (!success) { failed.value = true; return }
    requestKeys.delete(signature)
    if (operation === 'create') { name.value = ''; expiry.value = '' }
    emit('changed')
    await refresh()
  } catch { if (current === epoch) failed.value = true }
  finally { if (current === epoch) busy.value = false }
}

async function confirmMutation(operation: 'rotate' | 'revoke' | 'delete', device: Device) {
  const current = epoch
  if (await confirm({ title: t('clientDevices.' + (operation === 'delete' ? 'delete' : operation)), message: t('clientDevices.confirm', { name: device.name }), confirmLabel: t('yes'), tone: 'error' }) && current === epoch) await mutate(operation, device, device.endpointId)
}
async function download(device: Device) {
  if (busy.value) return
  const current = epoch
  busy.value = true
  failed.value = false
  try {
    const response = await api.post('api/client-device-config', { clientId: props.clientId, deviceId: device.id, host: location.hostname.replace(/^\[|\]$/g, '') }, { headers: { 'Content-Type': 'application/json' }, timeout: 15000 })
    if (current !== epoch || !open.value) return
    if (!response.data.success || typeof response.data.obj !== 'string') throw new Error('unavailable')
    const element = document.createElement('a')
    element.href = 'data:text/plain;charset=utf-8,' + encodeURIComponent(response.data.obj)
    element.download = `wireguard_${props.clientId}_${device.id}.conf`
    document.body.appendChild(element); element.click(); element.remove()
  } catch { if (current === epoch) failed.value = true }
  finally { if (current === epoch) busy.value = false }
}
watch([open, () => props.clientId], () => {
  ++epoch; controller?.abort(); view.value = undefined; failed.value = false; busy.value = false; requestKeys.clear()
  if (open.value) void refresh()
}, { immediate: true })
onBeforeUnmount(() => { ++epoch; controller?.abort(); requestKeys.clear() })
</script>
