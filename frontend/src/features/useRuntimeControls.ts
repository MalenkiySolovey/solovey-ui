import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import api from '@/plugins/api'
import HttpUtils, { type Msg } from '@/plugins/httputil'
import { getBaseUrl } from '@/plugins/base-url'
import { runtimeAvailability, type SessionView } from './runtimeSessions'

export interface RuntimeGroup {
  tag: string; type: string; selectable: boolean; selected: string
  items: { tag: string; type: string; testedAt: number; delay: number }[]
}
export interface ControlsView {
  runtime: SessionView
  canWrite: boolean
  canMaintenance: boolean
  snapshot: { generation: string; observedAt: number; groups: RuntimeGroup[] } | null
}
export interface RuntimeLog { generation: string; observedAt?: number; message?: string; level?: number; reset?: boolean; closed?: boolean; reason?: string }
interface ControlsDeps {
  load: (signal: AbortSignal) => Promise<ControlsView>
  action: (route: string, body: object, signal: AbortSignal) => Promise<Record<string, any>>
  stream: (generation: string, signal: AbortSignal, emit: (event: RuntimeLog) => void) => Promise<void>
}

export async function consumeRuntimeLogStream(response: Response, signal: AbortSignal, emit: (event: RuntimeLog) => void) {
  if (!response.ok || !response.body || !response.headers.get('Content-Type')?.includes('application/x-ndjson')) throw new Error('unavailable')
  const reader = response.body.getReader(), decoder = new TextDecoder()
  let pending = ''
  try {
    while (!signal.aborted) {
      const { value, done } = await reader.read()
      if (done) return
      pending += decoder.decode(value, { stream: true })
      if (pending.length > 131072) throw new Error('runtime_limit_exceeded')
      const lines = pending.split('\n')
      pending = lines.pop() || ''
      for (const line of lines) {
        if (line.length > 8192) throw new Error('runtime_limit_exceeded')
        if (line.trim() && !signal.aborted) emit(JSON.parse(line) as RuntimeLog)
      }
    }
  } finally { await reader.cancel(); reader.releaseLock() }
}

const defaults: ControlsDeps = {
  async load(signal) {
    const response = await HttpUtils.getRaw<Msg>('api/runtime/groups', {}, { signal, timeout: 7000 })
    if (!response.success || !response.obj) throw new Error('unavailable')
    return response.obj as ControlsView
  },
  async action(route, body, signal) {
    const response = await api.post<Msg>(`api/runtime/${route}`, body, { signal, timeout: 22000, headers: { 'Content-Type': 'application/json' } })
    if (!response.data.obj) throw new Error('unavailable')
    return response.data.obj
  },
  async stream(generation, signal, emit) {
    const url = `${getBaseUrl()}api/runtime/logs?generation=${encodeURIComponent(generation)}`
    const response = await fetch(url, { signal: AbortSignal.any([signal, AbortSignal.timeout(35000)]), credentials: 'same-origin', headers: { 'X-Requested-With': 'XMLHttpRequest' } })
    await consumeRuntimeLogStream(response, signal, emit)
  },
}

// Shared Classic/Nexus owner. Poll only an open visible dialog; actions and
// log frames are discarded when its revision/visibility epoch is retired.
export function useRuntimeControls(visible: Ref<boolean>, revision: Ref<string | number>, deps: ControlsDeps = defaults) {
  const view = ref<ControlsView | null>(null), busy = ref(false), phase = ref('loading'), now = ref(Date.now())
  const outcome = ref(''), logs = ref<RuntimeLog[]>([]), streamState = ref('closed')
  const pageVisible = ref(typeof document === 'undefined' || document.visibilityState !== 'hidden')
  const enabled = computed(() => visible.value && pageVisible.value)
  const stale = computed(() => !view.value || now.value - view.value.runtime.status.observedAt > 10000)
  const state = computed(() => view.value ? stale.value ? 'stale' : runtimeAvailability(view.value.runtime) || 'running' : phase.value)
  const canAct = computed(() => enabled.value && !busy.value && state.value === 'running' && view.value?.canWrite && view.value.snapshot?.generation === view.value.runtime.status.generation && now.value - view.value.snapshot.observedAt <= 10000)
  const canMaintain = computed(() => enabled.value && !busy.value && !stale.value && view.value?.canMaintenance && view.value.runtime.maintenanceAvailable && view.value.runtime.status.state !== 'unavailable')
  let epoch = 0, request: AbortController | null = null, stream: AbortController | null = null
  let timer: ReturnType<typeof setInterval> | null = null
  const retire = () => { epoch++; request?.abort(); stream?.abort(); request = null; stream = null; busy.value = false; streamState.value = 'closed' }
  const failure = (error: any) => [401, 403].includes(error?.response?.status) || error?.message === 'unauthorized' ? 'unauthorized' : 'unavailable'
  const refresh = async () => {
    if (!enabled.value || busy.value) return
    const current = ++epoch, controller = new AbortController()
    request = controller; busy.value = true; now.value = Date.now()
    try {
      const next = await deps.load(controller.signal)
      if (current !== epoch || !enabled.value) return
      if (view.value && next.runtime.status.generation !== view.value.runtime.status.generation) { stream?.abort(); stream = null; logs.value = []; streamState.value = 'closed' }
      view.value = next
    } catch (error) {
      if (current === epoch && !controller.signal.aborted) { view.value = null; phase.value = failure(error); stream?.abort(); streamState.value = 'closed' }
    } finally { if (current === epoch) { busy.value = false; request = null } }
  }
  const action = async (route: 'select' | 'probe' | 'maintenance', body: object) => {
    if (!view.value || (route === 'maintenance' ? !canMaintain.value : !canAct.value)) return
    const current = ++epoch, generation = view.value.runtime.status.generation, controller = new AbortController()
    request = controller; busy.value = true
    stream?.abort(); stream = null; streamState.value = 'closed'
    try {
      const result = await deps.action(route, { ...body, generation }, controller.signal)
      if (current !== epoch || !enabled.value || generation !== view.value?.runtime.status.generation) return
      outcome.value = result.reason || result.outcome || (result.maintenance ? 'maintenance_enabled' : 'maintenance_disabled')
    } catch (error) { if (current === epoch && !controller.signal.aborted) outcome.value = failure(error) }
    finally { if (current === epoch) { busy.value = false; request = null; await refresh() } }
  }
  const stopLogs = () => { stream?.abort(); stream = null; streamState.value = 'closed' }
  const startLogs = async () => {
    if (state.value !== 'running' || !enabled.value || stream) return
    const generation = view.value!.runtime.status.generation, controller = new AbortController()
    stream = controller; streamState.value = 'loading'; logs.value = []
    try {
      await deps.stream(generation, controller.signal, event => {
        if (stream !== controller || controller.signal.aborted || !enabled.value || event.generation !== generation || generation !== view.value?.runtime.status.generation) return
        streamState.value = event.closed ? 'closed' : 'open'
        if (event.reset) logs.value = []
        if (event.message) logs.value = [...logs.value.slice(-399), event]
      })
    } catch (error) { if (stream === controller && !controller.signal.aborted) streamState.value = failure(error) }
    finally { if (stream === controller) { stream = null; if (streamState.value !== 'unavailable') streamState.value = 'closed' } }
  }
  const restartView = () => {
    retire(); view.value = null; logs.value = []; outcome.value = ''; phase.value = 'loading'
    if (timer) clearInterval(timer)
    timer = null
    if (enabled.value) { void refresh(); timer = setInterval(() => { now.value = Date.now(); void refresh() }, 5000) }
  }
  watch([enabled, revision], restartView, { immediate: true })
  const visibilityChanged = () => { pageVisible.value = document.visibilityState !== 'hidden' }
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', visibilityChanged)
  onScopeDispose(() => { retire(); if (timer) clearInterval(timer); if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', visibilityChanged) })
  return { view, busy, state, canAct, canMaintain, outcome, logs, streamState, refresh, action, startLogs, stopLogs }
}
