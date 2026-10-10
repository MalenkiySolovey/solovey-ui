<template>
  <v-dialog v-model="open" max-width="1100" scrollable>
    <v-card>
      <v-card-title>{{ $t('runtime.sessions') }} — {{ clientName }}</v-card-title>
      <v-card-text>
        <v-alert :type="state === 'active' || state === 'empty' ? 'info' : 'warning'" variant="tonal" class="mb-3">
          {{ $t(`runtime.states.${state}`) }}
        </v-alert>
        <p class="mb-3">{{ $t('runtime.parentLimit') }}</p>
        <v-alert v-if="outcome" type="info" variant="tonal" class="mb-3">
          {{ $t(`runtime.outcomes.${outcome.outcome}`) }}
          ({{ outcome.parentControl === 'authenticated_quic' ? outcome.parentsClosed : outcome.closed }}/{{ outcome.parentControl === 'authenticated_quic' ? outcome.parentsMatched : outcome.matched }})
        </v-alert>
        <p v-if="view?.snapshot" class="text-caption mb-2">
          {{ $t('runtime.observed') }}: {{ new Date(view.snapshot.observedAt).toLocaleTimeString() }} ·
          {{ $t('runtime.flows') }}: {{ view.snapshot.total }}
          <span v-if="view.snapshot.truncated"> · {{ $t('runtime.limited') }}: {{ view.snapshot.limit }}</span>
        </p>
        <p v-if="view?.snapshot?.unassociated" class="text-caption mb-2">{{ $t('runtime.unknownIdentity') }}</p>
        <v-table v-if="view?.snapshot?.connections.length" density="compact">
          <thead><tr>
            <th>{{ $t('type') }}</th><th>{{ $t('runtime.inboundOutbound') }}</th>
            <th>{{ $t('runtime.addresses') }}</th><th>{{ $t('runtime.trafficAge') }}</th><th>{{ $t('actions.action') }}</th>
          </tr></thead>
          <tbody><tr v-for="flow in view.snapshot.connections" :key="flow.id">
            <td>{{ flow.network }} / {{ flow.protocol || flow.inboundType }}</td>
            <td>{{ flow.inbound }} → {{ flow.outbound }}</td>
            <td dir="ltr">{{ flow.source }}<br />{{ flow.destination }}</td>
            <td>{{ size(flow.upload) }} ↑ / {{ size(flow.download) }} ↓<br />{{ age(flow.createdAt) }} s</td>
            <td><v-btn size="small" variant="text" :disabled="!canClose" @click="closeFlows(flow.id)">{{ $t('runtime.closeFlow') }}</v-btn></td>
          </tr></tbody>
        </v-table>
        <div v-if="view?.snapshot?.parents?.length" class="mt-4">
          <p>{{ $t('runtime.quicParents') }}: {{ view.snapshot.parentTotal }}</p>
          <p class="text-caption mb-2">{{ $t('runtime.quicParentControl') }}</p>
          <p v-if="view.snapshot.parentsTruncated" class="text-caption">{{ $t('runtime.parentsLimited') }}</p>
          <v-table density="compact">
            <thead><tr><th>{{ $t('type') }}</th><th>{{ $t('runtime.inboundOutbound') }}</th><th>{{ $t('runtime.parentAge') }}</th></tr></thead>
            <tbody><tr v-for="parent in view.snapshot.parents" :key="`${parent.inbound}:${parent.epoch}:${parent.parentId}`">
              <td>{{ parent.inboundType }}</td><td>{{ parent.inbound }}</td><td>{{ age(parent.createdAt) }} s</td>
            </tr></tbody>
          </v-table>
        </div>
      </v-card-text>
      <v-card-actions class="flex-wrap">
        <v-btn :loading="busy" @click="refresh">{{ $t('refresh') }}</v-btn>
        <v-btn color="warning" :disabled="!canClose" @click="closeFlows()">{{ $t('runtime.closeObserved') }}</v-btn>
        <v-btn v-if="view?.capabilities.parentClose" color="warning" :disabled="!canCloseParents" @click="closeParents">{{ $t('runtime.closeQUICParents') }}</v-btn>
        <v-spacer /><v-btn @click="open = false">{{ $t('close') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { computed, toRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { useLiveSessions } from '@/features/useLiveSessions'
import { HumanReadable } from '@/plugins/utils'
import Ws from '@/store/ws'

const props = defineProps<{ clientId: number; clientName: string }>()
const open = defineModel<boolean>({ default: false })
const ws = Ws()
const revision = computed(() => `${ws.runtimeRevision}:${ws.state}`)
const { view, state, busy, outcome, canClose, canCloseParents, refresh, disconnect, disconnectParents } = useLiveSessions(open, toRef(props, 'clientId'), revision)
const { confirm } = useConfirm()
const { t } = useI18n()
const size = (value: number) => HumanReadable.sizeFormat(value)
const age = (createdAt: number) => Math.max(0, Math.floor((Date.now() - createdAt) / 1000))
const closeFlows = async (id?: string) => {
  const accepted = await confirm({ title: t('runtime.closeObserved'), message: t('runtime.confirmClose'), confirmLabel: t('runtime.closeObserved'), tone: 'error' })
  if (accepted) await disconnect(id)
}
const closeParents = async () => {
  const accepted = await confirm({ title: t('runtime.closeQUICParents'), message: t('runtime.confirmCloseQUIC'), confirmLabel: t('runtime.closeQUICParents'), tone: 'error' })
  if (accepted) await disconnectParents()
}
</script>
