<template>
  <v-card :subtitle="$t('types.hy.realm')" variant="outlined" class="my-2">
    <v-card-text>
      <v-switch :model-value="data.realm != null" :label="$t('types.hy.realmEnable')" color="primary" @update:model-value="toggle" />
      <template v-if="data.realm">
        <v-text-field v-model="data.realm.server_url" :label="$t('types.hy.realmServerUrl')" placeholder="https://realm.example:443" />
        <v-row>
          <v-col cols="12" sm="6"><v-text-field v-model="data.realm.realm_id" :label="$t('types.hy.realmId')" /></v-col>
          <v-col cols="12" sm="6"><SecretCredentialField v-model="data.realm.token" :available="available" :label="$t('types.hy.realmToken')" /></v-col>
        </v-row>
        <v-textarea :model-value="data.realm.stun_servers?.join('\n') ?? ''" :label="$t('types.hy.realmStun')" rows="2" @update:model-value="setStun" />
        <DomainResolver v-if="direction === 'in'" :data="data.realm" field="stun_domain_resolver" :label="$t('types.hy.realmStunResolver')" />
        <v-select v-model="data.realm.ip_version" :items="ipVersions" :label="$t('types.hy.realmIpVersion')" />
        <v-switch :model-value="data.realm.port_mapping?.enabled === true" :label="$t('types.hy.realmPortMapping')" color="primary" @update:model-value="setMapping" />
        <v-row v-if="data.realm.port_mapping?.enabled">
          <v-col cols="12" sm="6"><v-text-field v-model="data.realm.port_mapping.timeout" :label="$t('types.hy.realmMappingTimeout')" placeholder="5s" /></v-col>
          <v-col cols="12" sm="6"><v-text-field v-model="data.realm.port_mapping.lifetime" :label="$t('types.hy.realmMappingLifetime')" placeholder="1h" /></v-col>
        </v-row>
      </template>
    </v-card-text>
  </v-card>
  <v-dialog v-model="confirmDirect" max-width="520">
    <v-card :title="$t('types.hy.realm')" :text="$t('types.hy.realmReplaceDirect')">
      <v-card-actions><v-spacer /><v-btn @click="confirmDirect = false">{{ $t('cancel') }}</v-btn><v-btn color="primary" @click="enable">{{ $t('confirm') }}</v-btn></v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Hysteria2Realm } from '@/types/outbounds'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import DomainResolver from '@/components/fields/DomainResolver.vue'

const props = defineProps<{ data: { realm?: Hysteria2Realm & { stun_domain_resolver?: import('@/types/dial').DomainResolveOptions }; server?: string; server_port?: number; server_ports?: string[] }; available: boolean; direction?: string }>()
const { t } = useI18n()
const confirmDirect = ref(false)
const ipVersions = [{ title: t('types.hy.realmAuto'), value: 0 }, { title: 'IPv4', value: 4 }, { title: 'IPv6', value: 6 }]
function toggle(value: boolean | null) {
  if (!props.available) return
  if (!value) delete props.data.realm
  else if (props.data.server || props.data.server_port || props.data.server_ports?.length) confirmDirect.value = true
  else enable()
}
function enable() {
  if (!props.available) return
  // The explicit mode change replaces mutually exclusive native addressing.
  delete props.data.server
  delete props.data.server_port
  delete props.data.server_ports
  props.data.realm = { server_url: '', realm_id: '', stun_servers: [] }
  confirmDirect.value = false
}
function setStun(value: string | null) {
  if (props.data.realm) props.data.realm.stun_servers = (value ?? '').split(/[,\n]/).map(server => server.trim()).filter(Boolean)
}
function setMapping(enabled: boolean | null) {
  if (props.data.realm) props.data.realm.port_mapping = { ...props.data.realm.port_mapping, enabled: enabled === true }
}
</script>
