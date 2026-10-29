<template>
  <v-row>
    <v-col cols="12" sm="6"><v-select :model-value="data.mode || 'tls'" @update:model-value="changeMode" :items="['tls', 'static_key']" :label="$t('types.openvpn.mode')" /></v-col>
    <v-col cols="12" sm="6"><v-select v-model="data.network" :items="networks" :label="$t('types.openvpn.network')" /></v-col>
    <template v-if="!data.servers?.length">
      <v-col cols="12" sm="8"><v-text-field v-model="data.server" :label="$t('in.addr')" /></v-col>
      <v-col cols="12" sm="4"><v-text-field v-model.number="data.server_port" type="number" min="1" max="65535" :label="$t('in.port')" /></v-col>
    </template>
    <v-col v-for="(remote, index) in data.servers" :key="index" cols="12">
      <v-row>
        <v-col cols="12" sm="5"><v-text-field v-model="remote.server" :label="$t('in.addr')" /></v-col>
        <v-col cols="6" sm="2"><v-text-field v-model.number="remote.server_port" type="number" min="1" max="65535" :label="$t('in.port')" /></v-col>
        <v-col cols="6" sm="3"><v-select v-model="remote.network" :items="networks" clearable :label="$t('types.openvpn.network')" /></v-col>
        <v-col cols="12" sm="2"><v-btn icon="mdi-delete" :aria-label="$t('actions.del')" variant="text" @click="data.servers.splice(index, 1)" /></v-col>
      </v-row>
    </v-col>
    <v-col cols="12"><v-btn prepend-icon="mdi-plus" variant="tonal" @click="addRemote">{{ $t('types.openvpn.addRemote') }}</v-btn></v-col>
  </v-row>
  <v-expansion-panels multiple class="my-3">
    <v-expansion-panel :title="$t('types.openvpn.authentication')" v-if="data.mode !== 'static_key'">
      <v-expansion-panel-text><v-row>
        <v-col cols="12" sm="6"><v-text-field v-model="data.username" :label="$t('types.un')" autocomplete="off" /></v-col>
        <v-col cols="12" sm="6"><SecretCredentialField v-model="data.password" :available="available" :label="$t('types.pw')" /></v-col>
        <v-col cols="12" sm="6"><v-select v-model="data.auth_retry" :items="['none', 'nointeract', 'interact']" clearable :label="$t('types.openvpn.auth_retry')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.static_challenge" :label="$t('types.openvpn.static_challenge')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.static_challenge_echo" :label="$t('types.openvpn.static_challenge_echo')" /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openvpn.tls')" v-if="data.mode !== 'static_key'">
      <v-expansion-panel-text><v-row>
        <v-col v-for="key in tlsStrings" :key="key" cols="12" sm="6"><v-text-field :model-value="field('tls.' + key)" @update:model-value="setField('tls.' + key, $event)" :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col v-for="(items, key) in tlsEnums" :key="key" cols="12" sm="6"><v-select :model-value="field('tls.' + key)" @update:model-value="setField('tls.' + key, $event)" :items="items" clearable :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col v-for="key in tlsLists" :key="key" cols="12"><v-textarea :model-value="listValue('tls.' + key)" @update:model-value="setList('tls.' + key, $event)" rows="3" auto-grow :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col cols="12"><SecretCredentialField :model-value="field('tls.client_key')" @update:model-value="setField('tls.client_key', $event)" :available="available" multiline :label="$t('types.openvpn.client_key')" /></v-col>
        <v-col cols="12" sm="6"><v-select :model-value="field('tls.control_wrap.type')" @update:model-value="setField('tls.control_wrap.type', $event)" :items="['tls_auth', 'tls_crypt', 'tls_crypt_v2']" clearable :label="$t('types.openvpn.controlWrap')" /></v-col>
        <v-col cols="12" sm="6"><v-select :model-value="field('tls.control_wrap.direction')" @update:model-value="setField('tls.control_wrap.direction', $event)" :items="['client', 'server']" clearable :label="$t('types.openvpn.key_direction')" /></v-col>
        <v-col cols="12"><SecretCredentialField :model-value="field('tls.control_wrap.key')" @update:model-value="setField('tls.control_wrap.key', $event)" :available="available" multiline :label="$t('types.openvpn.controlKey')" /></v-col>
        <v-col cols="12"><v-text-field :model-value="field('tls.control_wrap.key_path')" @update:model-value="setField('tls.control_wrap.key_path', $event)" :label="$t('types.openvpn.controlKeyPath')" /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openvpn.staticKey')" v-else>
      <v-expansion-panel-text><v-row>
        <v-col cols="12"><SecretCredentialField v-model="data.static_key" :available="available" multiline :label="$t('types.openvpn.staticKey')" /></v-col>
        <v-col cols="12" sm="8"><v-text-field v-model="data.static_key_path" :label="$t('types.openvpn.static_key_path')" /></v-col>
        <v-col cols="12" sm="4"><v-select v-model="data.key_direction" :items="['client', 'server']" clearable :label="$t('types.openvpn.key_direction')" /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openvpn.connection')">
      <v-expansion-panel-text><v-row>
        <v-col v-for="key in modeFields(connectionStrings)" :key="key" cols="12" sm="6"><v-text-field :model-value="field(key)" @update:model-value="setField(key, $event)" :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col v-for="key in modeFields(connectionNumbers)" :key="key" cols="12" sm="6"><v-text-field :model-value="field(key)" @update:model-value="setNumber(key, $event)" type="number" min="0" :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col v-for="key in modeFields(connectionFlags)" :key="key" cols="12" sm="6"><v-switch :model-value="field(key)" @update:model-value="setField(key, $event)" :label="$t('types.openvpn.' + key)" hide-details /></v-col>
        <v-col v-for="(items, key) in connectionEnums" :key="key" cols="12" sm="6"><v-select :model-value="field(key)" @update:model-value="setField(key, $event)" :items="items" clearable :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col cols="12" v-if="data.mode !== 'static_key'"><v-combobox v-model="data.data_ciphers" multiple chips :label="$t('types.openvpn.data_ciphers')" /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openvpn.routes')">
      <v-expansion-panel-text><v-row>
        <v-col v-for="key in ['address', 'routes', 'redirect_gateway_flags']" :key="key" cols="12"><v-textarea :model-value="listValue(key)" @update:model-value="setList(key, $event)" rows="2" auto-grow :label="$t('types.openvpn.' + (key === 'routes' ? 'routeList' : key))" /></v-col>
        <v-col v-for="key in ['peer_address', 'peer_address_ipv6', 'route_gateway']" :key="key" cols="12" sm="6"><v-text-field :model-value="field(key)" @update:model-value="setField(key, $event)" :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.route_metric" type="number" :label="$t('types.openvpn.route_metric')" /></v-col>
        <v-col cols="12" sm="6"><v-select v-model="data.topology" :items="['net30', 'p2p', 'subnet']" clearable :label="$t('types.openvpn.topology')" /></v-col>
        <v-col v-for="key in ['redirect_gateway', 'redirect_private', 'block_ipv6']" :key="key" cols="12" sm="6"><v-switch :model-value="field(key)" @update:model-value="setField(key, $event)" :label="$t('types.openvpn.' + key)" /></v-col>
        <v-col v-if="data.mode !== 'static_key'" cols="12" sm="6"><v-switch v-model="data.route_no_pull" :label="$t('types.openvpn.route_no_pull')" /></v-col>
        <template v-if="data.mode !== 'static_key'">
          <v-col cols="12"><v-btn prepend-icon="mdi-plus" variant="tonal" @click="(data.pull_filters ??= []).push({ action: 'ignore', text: '' })">{{ $t('types.openvpn.addPullFilter') }}</v-btn></v-col>
          <v-col v-for="(filter, index) in data.pull_filters" :key="index" cols="12"><v-row>
            <v-col cols="12" sm="4"><v-select v-model="filter.action" :items="['accept', 'ignore', 'reject']" :label="$t('types.openvpn.pullFilterAction')" /></v-col>
            <v-col cols="12" sm="6"><v-text-field v-model="filter.text" :label="$t('types.openvpn.pullFilterText')" /></v-col>
            <v-col cols="12" sm="2"><v-btn icon="mdi-delete" :aria-label="$t('actions.del')" variant="text" @click="data.pull_filters.splice(index, 1)" /></v-col>
          </v-row></v-col>
        </template>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { convertOpenVPNModeDraft } from '@/features/endpointCredentialDraft'

const props = defineProps<{ data: Record<string, any>; available: boolean }>()
const { t } = useI18n()
const { confirm } = useConfirm()
const networks = ['udp', 'udp4', 'udp6', 'tcp', 'tcp4', 'tcp6']
const tlsStrings = ['server_name', 'certificate_path', 'client_certificate_path', 'client_key_path', 'crl_path', 'remote_certificate_eku', 'cipher', 'groups']
const tlsLists = ['certificate', 'client_certificate', 'peer_fingerprint', 'remote_certificate_ku']
const tlsEnums = { server_name_type: ['subject', 'name', 'name-prefix'], remote_certificate_tls: ['server', 'client', 'none'], certificate_profile: ['legacy', 'preferred', 'insecure', 'suiteb'], ns_certificate_type: ['server', 'client'], version_min: ['1.0', '1.1', '1.2', '1.3'], version_max: ['1.0', '1.1', '1.2', '1.3'] }
const connectionStrings = ['name', 'cipher', 'auth', 'udp_timeout', 'replay_window_time', 'ping_interval', 'ping_restart', 'renegotiate_interval', 'tls_timeout', 'handshake_window', 'data_ciphers_fallback']
const connectionNumbers = ['mtu', 'mss_fix', 'fragment', 'replay_window', 'udp_nat_max', 'explicit_exit_notify', 'renegotiate_bytes', 'renegotiate_packets']
const connectionFlags = ['system', 'remote_random', 'mss_fix_disabled', 'ping_restart_disabled', 'renegotiate_disabled']
const connectionEnums = { mss_fix_mode: ['mtu', 'fixed'], compression: ['none', 'no', 'lz4', 'lz4-v2', 'stub', 'stub-v2', 'disabled', 'off'], compression_lzo: ['none', 'no', 'yes', 'adaptive', 'asym', 'disabled', 'off'], allow_compression: ['no', 'asym', 'yes'], udp_mapping: ['endpoint_independent', 'address_dependent', 'address_and_port_dependent'], udp_filtering: ['endpoint_independent', 'address_dependent', 'address_and_port_dependent'] }
function field(path: string): any { return path.split('.').reduce((value, key) => value?.[key], props.data) }
function modeFields(keys: string[]): string[] { return keys.filter(key => props.data.mode === 'static_key' ? !key.startsWith('renegotiate_') && !['tls_timeout', 'handshake_window', 'data_ciphers_fallback'].includes(key) : key !== 'cipher') }
function listValue(path: string): string { const value = field(path); return Array.isArray(value) ? value.join('\n') : value ?? '' }
function setField(path: string, value: unknown) {
  if (!props.available) return
  const parts = path.split('.')
  let owner = props.data
  for (const part of parts.slice(0, -1)) owner = owner[part] ??= {}
  const key = parts[parts.length - 1]
  if (value === '' || value === undefined || value === null) delete owner[key]
  else owner[key] = value
}
function setList(path: string, value: string) { setField(path, value ? value.split('\n').map(line => line.trim()).filter(Boolean) : undefined) }
function setNumber(path: string, value: string | number) { if (value === '') setField(path, undefined); else if (Number.isSafeInteger(Number(value)) && Number(value) >= 0) setField(path, Number(value)) }
function addRemote() {
  if (!props.available) return
  const remotes = props.data.servers ??= []
  if (props.data.server && remotes.length === 0) remotes.push({ server: props.data.server, server_port: props.data.server_port ?? 1194 })
  delete props.data.server
  delete props.data.server_port
  remotes.push({ server: '', server_port: 1194 })
}
async function changeMode(mode: 'tls' | 'static_key') {
  if (!props.available || mode === (props.data.mode || 'tls')) return
  if (!await confirm({ title: t('types.openvpn.changeMode'), message: t('types.openvpn.modeConversion'), tone: 'error' })) return
  const result = convertOpenVPNModeDraft(props.data, mode)
  for (const key of Object.keys(props.data)) delete props.data[key]
  Object.assign(props.data, result)
}
</script>
