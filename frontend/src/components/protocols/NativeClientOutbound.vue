<template>
  <v-card>
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6"><v-text-field v-model="data.username" :label="$t('nativeOutbound.username')" autocomplete="off" /></v-col>
        <v-col cols="12" sm="6"><SecretCredentialField v-model="data.password" :available="available" :label="$t('nativeOutbound.password')" /></v-col>
      </v-row>
    </v-card-text>
  </v-card>
  <Mieru v-if="data.type === 'mieru'" :data="data" outbound />
  <TrustTunnel v-if="data.type === 'trusttunnel'" :data="data" outbound />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Mieru as MieruOptions, TrustTunnel as TrustTunnelOptions } from '@/types/outbounds'
import Mieru from '@/components/protocols/Mieru.vue'
import TrustTunnel from '@/components/protocols/TrustTunnel.vue'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import { runtimeCapability } from '@/types/runtimeCapabilities'
import Data from '@/store/modules/data'

const props = defineProps<{ data: Partial<MieruOptions & TrustTunnelOptions> }>()
const available = computed(() => runtimeCapability(Data().capabilities, 'outbounds', props.data.type ?? '')?.available === true)
</script>
