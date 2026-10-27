<template>
  <v-card subtitle="Call">
    <v-card-text>
      <v-alert type="info" variant="tonal" density="compact" class="mb-4">{{ $t('nativeInbound.callIdentityHint') }}</v-alert>
      <v-row>
        <v-col cols="12" sm="6"><v-select v-model="data.platform" :items="platforms" :label="$t('nativeInbound.callPlatform')" /></v-col>
        <v-col cols="12" sm="6"><v-select v-model="data.mode" :items="['dc', 'video']" clearable :label="$t('nativeInbound.callMode')" @click:clear="delete data.mode" /></v-col>
        <v-col cols="12"><v-text-field v-model.number="data.read_buffer" type="number" min="1" clearable :label="$t('nativeInbound.callReadBuffer')" @click:clear="delete data.read_buffer" /></v-col>
        <v-col cols="12"><SecretCredentialField v-model="data.join_link" :available="available" :label="$t('nativeInbound.callJoinLink')" /></v-col>
        <v-col cols="12"><div class="text-caption">{{ $t('nativeInbound.callJoinHint') }}</div></v-col>
        <v-col cols="12"><SecretCredentialField v-model="data.cookies" :available="available" :label="$t('nativeInbound.callCookies')" multiline /></v-col>
        <v-col cols="12"><div class="text-caption">{{ $t('nativeInbound.callCookiesHint') }}</div></v-col>
        <template v-if="data.platform === 'dion'">
          <v-col cols="12"><v-text-field v-model="data.email" :label="$t('nativeInbound.callEmail')" autocomplete="off" clearable @click:clear="delete data.email" /></v-col>
          <v-col cols="12"><SecretCredentialField v-model="data.password" :available="available" :label="$t('nativeInbound.callPassword')" /></v-col>
        </template>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { CallInbound } from '@/types/inbounds'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import { runtimeCapability } from '@/types/runtimeCapabilities'
import Data from '@/store/modules/data'

defineProps<{ data: CallInbound }>()
const platforms = ['dion', 'telemost', 'vk', 'wbstream']
const available = computed(() => runtimeCapability(Data().capabilities, 'inbounds', 'call')?.available === true)
</script>
