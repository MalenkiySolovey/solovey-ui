import { computed, onScopeDispose, ref, watch, type Ref } from 'vue'
import api from '@/plugins/api'
import HttpUtils, { type Msg } from '@/plugins/httputil'
import { sessionState, type DisconnectResult, type QUICParentTarget, type SessionView } from './runtimeSessions'

interface SessionDeps {
  load: (clientId: number, signal: AbortSignal) => Promise<SessionView>
  close: (generation: string, clientId: number, flowId: string | undefined, signal: AbortSignal, parents?: QUICParentTarget[]) => Promise<DisconnectResult>
}
const defaults: SessionDeps = {
  async load(clientId, signal) {
    const response = await HttpUtils.getRaw<Msg>('api/runtime/sessions', { clientId, limit: 100 }, { signal, timeout: 7000 })
    if (!response.success || !response.obj) throw new Error(response.msg === 'Invalid login' ? 'unauthorized' : 'unavailable')
    return response.obj as SessionView
  },
  async close(generation, clientId, flowId, signal, parents) {
    const response = await api.post<Msg>('api/runtime/disconnect', { generation, clientId, flowId, parents }, {
      signal, timeout: 7000, headers: { 'Content-Type': 'application/json' },
    })
    if (!response.data.obj) throw new Error('unavailable')
    return response.data.obj as DisconnectResult
  },
}

// One visible-view projection, with no flow registry or second WebSocket owner.
export function useLiveSessions(visible: Ref<boolean>, clientId: Ref<number>, revision: Ref<string | number>, deps: SessionDeps = defaults) {
  const view = ref<SessionView | null>(null)
  const phase = ref('loading')
  const busy = ref(false)
  const outcome = ref<DisconnectResult | null>(null)
  const now = ref(Date.now())
  const pageVisible = ref(typeof document === 'undefined' || document.visibilityState !== 'hidden')
  const enabled = computed(() => visible.value && pageVisible.value)
  const state = computed(() => view.value ? sessionState(view.value, now.value) : phase.value)
  const canClose = computed(() => !busy.value && state.value === 'active' && Boolean(view.value?.capabilities.flowClose) && Boolean(view.value?.snapshot?.connections.length))
  const canCloseParents = computed(() => {
    const snapshot = view.value?.snapshot
    const parents = snapshot?.parents ?? []
    return !busy.value && state.value === 'active' && Boolean(view.value?.capabilities.parentClose)
      && !snapshot?.parentsTruncated && parents.length > 0 && parents.length <= 128
      && parents.every(parent => parent.clientId === clientId.value && parent.parentControl === 'authenticated_quic')
  })
  let epoch = 0
  let request: AbortController | null = null
  let timer: ReturnType<typeof setInterval> | null = null

  const retire = () => {
    epoch++
    request?.abort()
    request = null
    busy.value = false
  }
  const stopTimer = () => {
    if (timer) clearInterval(timer)
    timer = null
  }
  const failure = (error: any) => error?.response?.status === 401 || error?.response?.status === 403 || error?.message === 'unauthorized' ? 'unauthorized' : 'unavailable'

  const refresh = async () => {
    if (!enabled.value || busy.value || !clientId.value) return
    const current = ++epoch
    const controller = new AbortController()
    request = controller
    busy.value = true
    now.value = Date.now()
    try {
      const next = await deps.load(clientId.value, controller.signal)
      if (current !== epoch || !enabled.value) return
      view.value = next
      phase.value = ''
    } catch (error) {
      if (current !== epoch || controller.signal.aborted) return
      view.value = null
      phase.value = failure(error)
    } finally {
      if (current === epoch) { busy.value = false; request = null }
    }
  }
  const performDisconnect = async (flowId?: string, parents?: QUICParentTarget[]) => {
    if (!(parents ? canCloseParents.value : canClose.value) || !view.value?.snapshot) return
    const current = ++epoch
    const generation = view.value.snapshot.generation
    const controller = new AbortController()
    request = controller
    busy.value = true
    try {
      const result = parents
        ? await deps.close(generation, clientId.value, undefined, controller.signal, parents)
        : await deps.close(generation, clientId.value, flowId, controller.signal)
      if (current !== epoch || !enabled.value || generation !== view.value?.snapshot?.generation) return
      outcome.value = result
    } catch (error) {
      if (current !== epoch || controller.signal.aborted) return
      phase.value = failure(error)
      view.value = null
    } finally {
      if (current === epoch) {
        busy.value = false; request = null
        // An acknowledgement cannot make a flow disappear from the UI.
        await refresh()
      }
    }
  }
  const disconnect = (flowId?: string) => performDisconnect(flowId)
  const disconnectParents = () => {
    const parents = view.value?.snapshot?.parents?.map(({ parentId, inbound, epoch }) => ({ parentId, inbound, epoch }))
    if (!parents?.length) return
    return performDisconnect(undefined, parents)
  }
  const restartView = () => {
    retire()
    stopTimer()
    view.value = null
    outcome.value = null
    phase.value = 'loading'
    if (!enabled.value) return
    void refresh()
    timer = setInterval(() => { now.value = Date.now(); void refresh() }, 5000)
  }
  watch([enabled, clientId, revision], restartView, { immediate: true })
  const visibilityChanged = () => { pageVisible.value = document.visibilityState !== 'hidden' }
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', visibilityChanged)
  onScopeDispose(() => {
    retire(); stopTimer()
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', visibilityChanged)
  })
  return { view, state, busy, outcome, canClose, canCloseParents, refresh, disconnect, disconnectParents }
}
