<template>
  <v-card subtitle="SSH">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6"><v-text-field v-model="data.server_version" :label="$t('nativeInbound.sshServerVersion')" clearable @click:clear="delete data.server_version" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.max_auth_tries" type="number" :label="$t('nativeInbound.sshMaxAuthTries')" clearable @click:clear="delete data.max_auth_tries" /></v-col>
        <v-col cols="12"><SecretCredentialField v-model="data.host_key" :available="available" :label="$t('nativeInbound.sshHostKeys')" multiline /></v-col>
        <v-col cols="12"><v-combobox :model-value="paths" @update:model-value="setPaths" multiple chips clearable :label="$t('nativeInbound.sshHostKeyPaths')" /></v-col>
        <v-col cols="12"><div class="text-caption">{{ $t('nativeInbound.sshKeyDefault') }}</div></v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { SSHInbound } from '@/types/inbounds'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import { runtimeCapability } from '@/types/runtimeCapabilities'
import Data from '@/store/modules/data'

const props = defineProps<{ data: SSHInbound }>()
const available = computed(() => runtimeCapability(Data().capabilities, 'inbounds', 'ssh')?.available === true)
const paths = computed(() => typeof props.data.host_key_path === 'string' ? [props.data.host_key_path] : props.data.host_key_path ?? [])
function setPaths(value: string[]) {
  if (value?.length) props.data.host_key_path = value
  else delete props.data.host_key_path
}
</script>
