<template>
  <v-dialog v-model="open" max-width="1000" scrollable>
    <v-card>
      <v-card-title>{{ $t('runtime.controls') }}</v-card-title>
      <v-card-text>
        <v-alert variant="tonal" :type="state === 'running' ? 'info' : 'warning'" class="mb-3">{{ $t(`runtime.states.${state}`) }}</v-alert>
        <p v-if="view" class="mb-3 text-caption">{{ $t('runtime.observed') }}: {{ new Date(view.runtime.status.observedAt).toLocaleTimeString() }} · {{ $t('runtime.actual') }}: {{ view.runtime.status.state }}</p>
        <p class="mb-3">{{ $t('runtime.temporarySelection') }}</p>
        <v-alert v-if="outcome" type="info" variant="tonal" class="mb-3">{{ message(outcome) }}</v-alert>
        <v-btn v-if="view?.canMaintenance" color="warning" class="mb-4" :disabled="!canMaintain" @click="maintenance">{{ $t(view.runtime.maintenance ? 'runtime.resume' : 'runtime.hold') }}</v-btn>
        <p v-if="view?.snapshot && !view.snapshot.groups.length">{{ $t('runtime.noGroups') }}</p>
        <div v-for="group in view?.snapshot?.groups || []" :key="group.tag" class="mb-4">
          <h3>{{ group.tag }} · {{ group.type }} · {{ group.selected }}</h3>
          <v-table density="compact">
            <tbody><tr v-for="item in group.items" :key="item.tag">
              <td>{{ item.tag }}</td><td>{{ item.type }}</td><td>{{ item.testedAt ? `${item.delay} ms` : '—' }}</td>
              <td><v-btn v-if="group.selectable" size="small" variant="text" :disabled="!canAct" @click="action('select', { group: group.tag, member: item.tag })">{{ $t('runtime.select') }}</v-btn>
                <v-btn size="small" variant="text" :disabled="!canAct" @click="action('probe', { group: group.tag, member: item.tag })">{{ $t('runtime.probe') }}</v-btn></td>
            </tr></tbody>
          </v-table>
        </div>
        <p class="mb-2">{{ $t('runtime.logLimit') }}</p>
        <v-btn size="small" :disabled="state !== 'running' || streamState === 'open' || streamState === 'loading'" @click="startLogs">{{ $t('runtime.watchLogs') }}</v-btn>
        <v-btn size="small" @click="stopLogs">{{ $t('runtime.stopLogs') }}</v-btn>
        <span class="ms-2">{{ $t(`runtime.stream.${streamState}`) }}</span>
        <pre class="runtime-log mt-3">{{ logs.map(line => line.message).join('\n') }}</pre>
      </v-card-text>
      <v-card-actions><v-btn :loading="busy" @click="refresh">{{ $t('refresh') }}</v-btn><v-spacer /><v-btn @click="open = false">{{ $t('close') }}</v-btn></v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { useRuntimeControls } from '@/features/useRuntimeControls'
import Ws from '@/store/ws'
const open = defineModel<boolean>({ default: false }), ws = Ws()
const revision = computed(() => `${ws.runtimeRevision}:${ws.state}`)
const { view, busy, state, canAct, canMaintain, outcome, logs, streamState, refresh, action, startLogs, stopLogs } = useRuntimeControls(open, revision)
const { t, te } = useI18n(), { confirm } = useConfirm()
const message = (code: string) => te(`runtime.controlResults.${code}`) ? t(`runtime.controlResults.${code}`) : t('runtime.controlResults.ERROR')
const maintenance = async () => {
  if (!view.value) return
  const enabled = !view.value.runtime.maintenance
  if (enabled && !await confirm({ title: t('runtime.hold'), message: t('runtime.confirmHold'), confirmLabel: t('runtime.hold'), tone: 'error' })) return
  await action('maintenance', { enabled })
}
</script>
<style scoped>
.runtime-log { max-height: 260px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
