<template>
  <v-card subtitle="Call">
    <v-card-text>
      <v-alert type="info" variant="tonal" density="compact" class="mb-4">{{ $t(outbound ? 'nativeInbound.callOutboundHint' : 'nativeInbound.callIdentityHint') }}</v-alert>
      <v-row>
        <v-col cols="12" sm="6"><v-select v-model="data.platform" :items="platforms" :label="$t('nativeInbound.callPlatform')" /></v-col>
        <v-col v-if="data.platform === 'wbstream'" cols="12" sm="6"><v-select v-model="data.mode" :items="['dc', 'video']" clearable :label="$t('nativeInbound.callMode')" @click:clear="delete data.mode" /></v-col>
        <v-col v-else-if="data.mode !== undefined" cols="12" sm="6"><v-text-field :model-value="data.mode" readonly clearable :label="$t('nativeInbound.callModeInactive')" @click:clear="delete data.mode" /></v-col>
        <v-col cols="12"><v-text-field v-model.number="data.read_buffer" type="number" min="0" clearable :label="$t('nativeInbound.callReadBuffer')" @click:clear="delete data.read_buffer" /></v-col>
        <v-col cols="12"><SecretCredentialField v-model="data.join_link" :available="available" :label="$t('nativeInbound.callJoinLink')" /></v-col>
        <v-col cols="12"><div class="text-caption">{{ $t(outbound ? 'nativeInbound.callOutboundJoinHint' : 'nativeInbound.callJoinHint') }}</div></v-col>
        <v-col v-if="!outbound && Number(data.id) > 0" cols="12">
          <v-btn :disabled="!available || downloading" :loading="downloading" variant="tonal" @click="downloadRoom">{{ $t('nativeInbound.callDownloadRoom') }}</v-btn>
          <div class="text-caption mt-2">{{ $t('nativeInbound.callDownloadRoomHint') }}</div>
        </v-col>
        <v-col cols="12"><SecretCredentialField v-model="data.cookies" :available="available" :label="$t('nativeInbound.callCookies')" multiline /></v-col>
        <v-col cols="12"><div class="text-caption">{{ $t('nativeInbound.callCookiesHint') }}</div></v-col>
        <template v-if="!outbound && (data.platform === 'dion' || hasDionCredentials)">
          <v-col v-if="data.platform !== 'dion'" cols="12"><v-alert type="info" variant="tonal" density="compact">{{ $t('nativeInbound.callAccountInactive') }}</v-alert></v-col>
          <v-col cols="12"><v-text-field v-model="data.email" :label="$t('nativeInbound.callEmail')" autocomplete="off" clearable @click:clear="delete data.email" /></v-col>
          <v-col cols="12"><SecretCredentialField v-model="data.password" :available="available" :label="$t('nativeInbound.callPassword')" /></v-col>
        </template>
      </v-row>
    </v-card-text>
  </v-card>
  <Dial v-if="!outbound" :dial="data" />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'
import type { CallInbound } from '@/types/inbounds'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import Dial from '@/components/fields/Dial.vue'
import { runtimeCapability } from '@/types/runtimeCapabilities'
import Data from '@/store/modules/data'
import api from '@/plugins/api'
import HttpUtils from '@/plugins/httputil'

const props = defineProps<{ data: Partial<CallInbound>; outbound?: boolean }>()
const { t } = useI18n()
const downloading = ref(false)
const platforms = computed(() => props.outbound ? ['dion', 'telemost', 'vk', 'wbstream', 'bitrix'] : ['dion', 'telemost', 'vk', 'wbstream'])
const available = computed(() => runtimeCapability(Data().capabilities, props.outbound ? 'outbounds' : 'inbounds', 'call')?.available === true)
const hasDionCredentials = computed(() => {
  const password = props.data.password as { present?: boolean; intent?: string } | undefined
  return !!props.data.email || password?.present === true || password?.intent === 'replace'
})

async function downloadRoom() {
  if (downloading.value || !available.value || !props.data.id) return
  downloading.value = true
  let objectURL: string | undefined
  try {
    const status = await HttpUtils.get('api/runtime/status')
    const generation = status.success && status.obj?.status?.generation
    if (!generation) throw new Error('unavailable')
    const response = await api.post<Blob>('api/inbound-call-room', { id: props.data.id, generation }, {
      headers: { 'Content-Type': 'application/json' }, responseType: 'blob', timeout: 15_000,
    })
    const contentType = response.headers['content-type']
    if (typeof contentType !== 'string' || !contentType.startsWith('text/plain') || response.data.size === 0 || response.data.size > 4096) throw new Error('unavailable')
    objectURL = URL.createObjectURL(response.data)
    const download = document.createElement('a')
    download.href = objectURL
    download.download = `call-room-${props.data.id}.txt`
    download.click()
  } catch {
    push.error({ message: t('nativeInbound.callRoomUnavailable') })
  } finally {
    if (objectURL) {
      const completedURL = objectURL
      setTimeout(() => URL.revokeObjectURL(completedURL), 1000)
    }
    downloading.value = false
  }
}
</script>
