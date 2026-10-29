<template>
  <v-card variant="outlined" class="my-3 pa-3">
    <v-alert type="info" variant="tonal" density="compact" class="mb-3">{{ $t('nativeRealm.identityHint') }}</v-alert>
    <v-btn prepend-icon="mdi-plus" variant="tonal" class="mb-3" @click="data.users.push({ name: '', token: { intent: 'replace', value: '' } })">{{ $t('nativeRealm.addIdentity') }}</v-btn>
    <v-card v-for="(user, index) in data.users" :key="index" variant="outlined" class="pa-3 mb-3">
      <v-row>
        <v-col cols="12" sm="6"><v-text-field v-model="user.name" :readonly="!!unchangedSecret(user.token)?.present" :label="$t('nativeRealm.name')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field :model-value="user.max_realms" type="number" min="0" :label="$t('nativeRealm.maxRealms')" :hint="$t('nativeRealm.unlimited')" persistent-hint @update:model-value="setLimit(user, $event)" /></v-col>
      </v-row>
      <SecretCredentialField v-model="user.token" :available="available" :label="$t('nativeRealm.token')" />
      <v-btn color="error" variant="text" prepend-icon="mdi-delete" @click="data.users.splice(index, 1)">{{ $t('nativeRealm.removeIdentity') }}</v-btn>
    </v-card>
    <v-divider class="my-3" />
    <v-row>
      <v-col v-for="field in http2Fields" :key="field" cols="12" sm="6">
        <v-text-field :model-value="data[field]" :label="$t('nativeRealm.' + field)" :type="field === 'max_concurrent_streams' ? 'number' : 'text'" min="0" clearable @update:model-value="setOption(field, $event)" />
      </v-col>
    </v-row>
  </v-card>
</template>

<script setup lang="ts">
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import { unchangedSecret } from '@/features/secretCredentialDraft'
import type { HysteriaRealm, RealmIdentity } from '@/types/services'

const props = defineProps<{ data: HysteriaRealm; available: boolean }>()
const http2Fields = ['idle_timeout', 'keep_alive_period', 'stream_receive_window', 'connection_receive_window', 'max_concurrent_streams'] as const
function setLimit(user: RealmIdentity, value: unknown) {
  if (value === '' || value === null || value === undefined) delete user.max_realms
  else user.max_realms = Number(value)
}
function setOption(field: typeof http2Fields[number], value: unknown) {
  if (value === '' || value === null || value === undefined) delete props.data[field]
  else if (field === 'max_concurrent_streams') props.data[field] = Number(value)
  else props.data[field] = String(value)
}
</script>
