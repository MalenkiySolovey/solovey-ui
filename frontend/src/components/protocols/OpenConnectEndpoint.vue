<template>
  <v-row>
    <v-col cols="12" md="8"><v-text-field :model-value="field('server')" @update:model-value="setField('server', $event)" :label="$t('types.openconnect.server')" placeholder="https://vpn.example.com" /></v-col>
    <v-col cols="12" md="4"><v-select :model-value="field('flavor') || 'anyconnect'" @update:model-value="setField('flavor', $event)" :items="['anyconnect', 'gp', 'fortinet', 'f5', 'pulse', 'nc']" :label="$t('types.openconnect.flavor')" /></v-col>
    <v-col cols="12" sm="6"><v-text-field :model-value="field('username')" @update:model-value="setField('username', $event)" :label="$t('types.un')" autocomplete="off" /></v-col>
    <v-col cols="12" sm="6"><v-text-field :model-value="field('auth_group')" @update:model-value="setField('auth_group', $event)" :label="$t('types.openconnect.auth_group')" /></v-col>
    <v-col cols="12" sm="6"><SecretCredentialField :model-value="field('password')" @update:model-value="setField('password', $event)" :available="available" :label="$t('types.pw')" /></v-col>
    <v-col cols="12" sm="6"><SecretCredentialField :model-value="field('cookie')" @update:model-value="setField('cookie', $event)" :available="available" :label="$t('types.openconnect.cookie')" /></v-col>
  </v-row>
  <v-expansion-panels multiple class="my-3">
    <v-expansion-panel :title="$t('types.openconnect.token')">
      <v-expansion-panel-text><v-row>
        <v-col cols="12" sm="6"><v-select :model-value="field('token.mode')" @update:model-value="setField('token.mode', $event)" :items="['totp', 'hotp', 'stoken', 'oidc']" clearable :label="$t('types.openconnect.tokenMode')" /></v-col>
        <v-col v-for="key in ['secret', 'pin', 'password']" :key="key" cols="12" sm="6"><SecretCredentialField :model-value="field('token.' + key)" @update:model-value="setField('token.' + key, $event)" :available="available" :label="$t('types.openconnect.token_' + key)" /></v-col>
        <v-col v-for="key in ['secret_path', 'device_id']" :key="key" cols="12" sm="6"><v-text-field :model-value="field('token.' + key)" @update:model-value="setField('token.' + key, $event)" :label="$t('types.openconnect.token_' + key)" /></v-col>
        <v-col cols="12" sm="6"><v-text-field :model-value="field('token.counter')" @update:model-value="setNumber('token.counter', $event)" type="number" min="0" :max="Number.MAX_SAFE_INTEGER" :label="$t('types.openconnect.token_counter')" /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openconnect.tls')">
      <v-expansion-panel-text><v-row>
        <v-col v-for="key in tlsStrings" :key="key" cols="12" sm="6"><v-text-field :model-value="field('tls.' + key)" @update:model-value="setField('tls.' + key, $event)" :label="$t('types.openconnect.' + key)" /></v-col>
        <v-col v-for="key in tlsLists" :key="key" cols="12"><v-textarea :model-value="listValue('tls.' + key)" @update:model-value="setField('tls.' + key, $event ? $event.split('\n') : undefined)" rows="3" auto-grow :label="$t('types.openconnect.' + key)" /></v-col>
        <v-col v-for="key in ['client_key', 'client_key_password', 'mca_key', 'mca_key_password']" :key="key" cols="12" sm="6"><SecretCredentialField :model-value="field('tls.' + key)" @update:model-value="setField('tls.' + key, $event)" :multiline="key.endsWith('_key')" :available="available" :label="$t('types.openconnect.' + key)" /></v-col>
        <v-col v-for="key in ['insecure', 'system_trust_disabled']" :key="key" cols="12" sm="6"><v-switch :model-value="field('tls.' + key)" @update:model-value="setField('tls.' + key, $event)" :label="$t('types.openconnect.' + key)" hide-details /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openconnect.network')">
      <v-expansion-panel-text><v-row>
        <v-col v-for="key in networkStrings" :key="key" cols="12" sm="6"><v-text-field :model-value="field(key)" @update:model-value="setField(key, $event)" :label="$t('types.openconnect.' + key)" /></v-col>
        <v-col v-for="key in networkNumbers" :key="key" cols="12" sm="6"><v-text-field :model-value="field(key)" @update:model-value="setNumber(key, $event)" type="number" min="0" :label="$t('types.openconnect.' + key)" /></v-col>
        <v-col v-for="key in ['udp_mapping', 'udp_filtering']" :key="key" cols="12" sm="6"><v-select :model-value="field(key)" @update:model-value="setField(key, $event)" :items="['endpoint_independent', 'address_dependent', 'address_and_port_dependent']" clearable :label="$t('types.openconnect.' + key)" /></v-col>
        <v-col cols="12" sm="6"><v-select :model-value="field('compression_mode')" @update:model-value="setField('compression_mode', $event)" :items="['stateless', 'all']" clearable :label="$t('types.openconnect.compression_mode')" /></v-col>
        <v-col v-for="key in networkFlags" :key="key" cols="12" sm="6"><v-switch :model-value="field(key)" @update:model-value="setField(key, $event)" :label="$t('types.openconnect.' + key)" hide-details /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openconnect.identity')">
      <v-expansion-panel-text><v-row>
        <v-col v-for="key in identityStrings" :key="key" cols="12" sm="6"><v-text-field :model-value="field(key)" @update:model-value="setField(key, $event)" :label="$t('types.openconnect.' + key.replace('.', '_'))" /></v-col>
        <v-col cols="12" sm="6"><v-switch :model-value="field('tncc.machine_identification_enabled')" @update:model-value="setField('tncc.machine_identification_enabled', $event)" :label="$t('types.openconnect.tncc_machine_identification_enabled')" /></v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('types.openconnect.formEntries')">
      <v-expansion-panel-text>
        <v-alert type="info" variant="tonal" class="mb-3">{{ $t('types.openconnect.formHint') }}</v-alert>
        <SecretCredentialField :model-value="field('form_entries')" @update:model-value="setField('form_entries', $event)" :available="available" multiline :label="$t('types.openconnect.formEntries')" />
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>

<script setup lang="ts">
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'

const props = defineProps<{ data: Record<string, any>; available: boolean }>()
const tlsStrings = ['server_name', 'certificate_authority_path', 'client_certificate_path', 'client_key_path', 'mca_certificate_path', 'mca_key_path']
const tlsLists = ['peer_fingerprint', 'certificate_authority', 'client_certificate', 'mca_certificate']
const networkStrings = ['name', 'udp_timeout', 'dpd_interval', 'reconnect_timeout', 'trojan_interval']
const networkNumbers = ['mtu', 'base_mtu', 'queue_length', 'dtls_local_port', 'udp_nat_max']
const networkFlags = ['system', 'no_udp', 'compression_disabled', 'ipv6_disabled', 'http_keepalive_disabled', 'xml_post_disabled', 'external_auth_disabled', 'password_authentication_disabled', 'tcp_keep_alive_enabled', 'pfs', 'allow_insecure_crypto']
const identityStrings = ['reported_os', 'user_agent', 'version', 'local_hostname', 'mobile.platform_version', 'mobile.device_type', 'mobile.device_unique_id', 'fortinet_host_check.hostcheck', 'fortinet_host_check.check_virtual_desktop', 'tncc.device_id', 'tncc.user_agent']
function field(path: string): any { return path.split('.').reduce((value, key) => value?.[key], props.data) }
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
function setNumber(path: string, value: string | number) {
  if (value === '') setField(path, undefined)
  else if (Number.isSafeInteger(Number(value)) && Number(value) >= 0) setField(path, Number(value))
}
</script>
