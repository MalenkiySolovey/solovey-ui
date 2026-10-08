import { defineStore } from 'pinia'
import HttpUtils from '@/plugins/httputil'
import Data from '@/store/modules/data'
import { clearCSRFToken } from '@/store/csrf'
import { getBaseUrl } from '@/plugins/base-url'
import { push } from 'notivue'
import { i18n } from '@/locales'

export type WsConnectionState = 'connected' | 'reconnecting' | 'degraded'

export interface WsLike {
  onopen: ((event?: any) => void) | null
  onmessage: ((event: any) => void) | null
  onclose: ((event?: any) => void) | null
  onerror: ((event?: any) => void) | null
  close: () => void
}

export interface WsRuntimeDeps {
  getToken: () => Promise<string | null>
  createSocket: (url: string, token: string) => WsLike
  loadData: () => void | Promise<void>
  onState?: (state: WsConnectionState) => void
  onEvent?: (event: any) => void
  onlineEvents?: Pick<Window, 'addEventListener' | 'removeEventListener'>
  setTimeout?: typeof setTimeout
  clearTimeout?: typeof clearTimeout
  setInterval?: typeof setInterval
  clearInterval?: typeof clearInterval
  location?: Pick<Location, 'protocol' | 'host'>
  baseUrl?: string
}

const noOpenFallbackMs = 5000
const reconnectBaseDelayMs = 250
const reconnectJitterMs = 250
const reconnectMaxDelayMs = 5000
const fallbackPollMs = 10000
const onlineRecoveryDelayMs = 100
const closeFallbackThreshold = 3

export const reconnectDelayForRetry = (retry: number) => {
  const safeRetry = Math.max(0, retry)
  const exponentialDelay = Math.pow(2, safeRetry) * reconnectBaseDelayMs
  const jitter = Math.random() * reconnectJitterMs
  return Math.min(exponentialDelay + jitter, reconnectMaxDelayMs)
}

export const wsProtocolsForToken = (token: string) => ['sui.realtime', `sui.token.${token}`]

export const requestWSToken = async (): Promise<string | null> => {
  const tokenResponse = await HttpUtils.post('api/realtime/ws-token', null)
  const token = tokenResponse.obj?.token
  return tokenResponse.success && typeof token === 'string' ? token : null
}

export class WsRuntime {
  state: WsConnectionState = 'degraded'
  private ws: WsLike | null = null
  private noOpenTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private fallbackTimer: ReturnType<typeof setInterval> | null = null
  private onlineRecoveryHandler: (() => void) | null = null
  private closeCount = 0
  private connectAttempt = 0
  private connectingAttempt: number | null = null
  private stopped = false
  private fallbackGeneration = 0

  constructor(private deps: WsRuntimeDeps) {}

  async connect() {
    if (this.ws || this.state === 'connected' || this.connectingAttempt !== null) return
    this.stopped = false
    this.clearReconnectTimer()
    const attempt = ++this.connectAttempt
    this.connectingAttempt = attempt
    this.setState('reconnecting')
    this.stopFallback()
    try {
      const token = await this.deps.getToken()
      if (attempt !== this.connectAttempt) return
      if (!token) {
        this.startFallback()
        return
      }
      const ws = this.deps.createSocket(this.wsURL(), token)
      this.ws = ws
      const current = () => !this.stopped && attempt === this.connectAttempt && this.ws === ws
      const openingTimer = this.setRuntimeTimeout(() => {
        if (!current() || this.noOpenTimer !== openingTimer) return
        this.noOpenTimer = null
        this.retireSocket(true)
        this.startFallback()
      }, noOpenFallbackMs)
      this.noOpenTimer = openingTimer
      ws.onopen = () => {
        if (!current()) return
        this.closeCount = 0
        this.clearNoOpenTimer()
        this.setState('connected')
        this.stopFallback()
      }
      ws.onmessage = (event) => {
        if (!current()) return
        let payload: unknown
        try {
          payload = JSON.parse(event.data)
        } catch {
          // Keep realtime open when a single event is malformed.
          return
        }
        this.deps.onEvent?.(payload)
      }
      ws.onclose = (event) => {
        if (!current()) return
        if (isSessionClose(event)) {
          clearCSRFToken()
        }
        this.clearNoOpenTimer()
        this.retireSocket(false)
        this.closeCount++
        if (this.closeCount >= closeFallbackThreshold) {
          this.startFallback()
          return
        }
        this.setState('reconnecting')
        const retry = this.closeCount - 1
        this.scheduleReconnect(reconnectDelayForRetry(retry))
      }
      ws.onerror = () => {
        if (!current()) return
        ws.close()
      }
    } catch {
      if (attempt === this.connectAttempt) this.startFallback()
    } finally {
      if (this.connectingAttempt === attempt) this.connectingAttempt = null
    }
  }

  disconnect() {
    this.stopped = true
    this.connectAttempt++
    this.connectingAttempt = null
    this.clearNoOpenTimer()
    this.clearReconnectTimer()
    this.retireSocket(true)
    this.stopFallback()
    this.setState('degraded')
  }

  private startFallback() {
    if (this.stopped) return
    this.clearNoOpenTimer()
    this.clearReconnectTimer()
    this.setState('degraded')
    this.startOnlineRecovery()
    if (this.fallbackTimer) return
    const generation = this.fallbackGeneration
    const fallbackTimer = this.setRuntimeInterval(() => {
      if (this.stopped || generation !== this.fallbackGeneration || this.fallbackTimer !== fallbackTimer) return
      void this.deps.loadData()
      if (this.reconnectTimer || this.ws) return
      void this.connect()
    }, fallbackPollMs)
    this.fallbackTimer = fallbackTimer
  }

  private stopFallback() {
    this.fallbackGeneration++
    this.stopOnlineRecovery()
    if (!this.fallbackTimer) return
    this.clearRuntimeInterval(this.fallbackTimer)
    this.fallbackTimer = null
  }

  private startOnlineRecovery() {
    if (this.onlineRecoveryHandler) return
    const events = this.deps.onlineEvents ?? (typeof window === 'undefined' ? undefined : window)
    if (!events?.addEventListener) return
    const generation = this.fallbackGeneration
    this.onlineRecoveryHandler = () => {
      if (this.stopped || generation !== this.fallbackGeneration) return
      if (this.reconnectTimer || this.ws) return
      this.scheduleReconnect(onlineRecoveryDelayMs)
    }
    events.addEventListener('online', this.onlineRecoveryHandler)
  }

  private stopOnlineRecovery() {
    if (!this.onlineRecoveryHandler) return
    const events = this.deps.onlineEvents ?? (typeof window === 'undefined' ? undefined : window)
    events?.removeEventListener?.('online', this.onlineRecoveryHandler)
    this.onlineRecoveryHandler = null
  }

  private clearNoOpenTimer() {
    if (this.noOpenTimer === null) return
    this.clearRuntimeTimeout(this.noOpenTimer)
    this.noOpenTimer = null
  }

  private clearReconnectTimer() {
    if (this.reconnectTimer === null) return
    this.clearRuntimeTimeout(this.reconnectTimer)
    this.reconnectTimer = null
  }

  private scheduleReconnect(delay: number) {
    this.clearReconnectTimer()
    const attempt = this.connectAttempt
    const timer = this.setRuntimeTimeout(() => {
      if (this.stopped || attempt !== this.connectAttempt || this.reconnectTimer !== timer) return
      this.reconnectTimer = null
      void this.connect()
    }, delay)
    this.reconnectTimer = timer
  }

  private retireSocket(close: boolean) {
    const socket = this.ws
    this.ws = null
    if (!socket) return
    socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null
    if (close) socket.close()
  }

  private setState(state: WsConnectionState) {
    this.state = state
    this.deps.onState?.(state)
  }

  private wsURL() {
    const loc = this.deps.location ?? window.location
    const scheme = loc.protocol === 'https:' ? 'wss' : 'ws'
    const base = this.deps.baseUrl ?? getBaseUrl()
    return `${scheme}://${loc.host}${base}api/realtime/ws`
  }

  private setRuntimeTimeout(callback: () => void, delay: number) {
    const timer = this.deps.setTimeout ?? globalThis.setTimeout.bind(globalThis)
    return timer(callback, delay)
  }

  private clearRuntimeTimeout(timerID: ReturnType<typeof setTimeout>) {
    const clear = this.deps.clearTimeout ?? globalThis.clearTimeout.bind(globalThis)
    clear(timerID)
  }

  private setRuntimeInterval(callback: () => void, delay: number) {
    const timer = this.deps.setInterval ?? globalThis.setInterval.bind(globalThis)
    return timer(callback, delay)
  }

  private clearRuntimeInterval(timerID: ReturnType<typeof setInterval>) {
    const clear = this.deps.clearInterval ?? globalThis.clearInterval.bind(globalThis)
    clear(timerID)
  }
}

const applyRealtimeEvent = (event: any) => {
  const data = Data()
  const ws = Ws()
  switch (event?.type) {
    case 'onlines':
      if (event.payload) data.onlines = event.payload
      break
    case 'component_progress':
      if (event.payload?.componentId) {
        const componentId = String(event.payload.componentId)
        const installedIDs = new Set(data.components.map(component => component.id))
        for (const id of Object.keys(ws.componentProgress)) {
          if (!installedIDs.has(id)) delete ws.componentProgress[id]
        }
        if (!installedIDs.has(componentId)) break
        if (event.payload.progress == null) delete ws.componentProgress[componentId]
        else ws.componentProgress[componentId] = event.payload.progress
      }
      break
    case 'failover_status':
      if (event.payload?.tag) {
        data.failoverStatus = {
          ...data.failoverStatus,
          [event.payload.tag]: event.payload,
        }
      }
      break
    case 'core_state':
      ws.runtimeRevision++
      if (event.payload?.warning) {
        const warning = String(event.payload.warning)
        const group = event.payload.group ? `: ${event.payload.group}` : ''
        push.warning({
          title: i18n.global.t('warning'),
          duration: 6000,
          message: `${warning}${group}`,
        })
      }
      break
    case 'config_invalidated':
    case 'reload':
      ws.runtimeRevision++
      void data.loadData()
      break
  }
}

const isSessionClose = (event?: any) => event?.code === 4401 || event?.reason === 'session_rotated'

const Ws = defineStore('Ws', {
  state: () => ({
    state: <WsConnectionState>'degraded',
    runtime: <WsRuntime | null>null,
    componentProgress: <Record<string, any>>{},
    runtimeRevision: 0,
  }),
  actions: {
    ensureRuntime() {
      if (!this.runtime) {
        this.runtime = new WsRuntime({
          getToken: requestWSToken,
          createSocket: (url, token) => new WebSocket(url, wsProtocolsForToken(token)),
          loadData: () => Data().loadData(),
          onState: (state) => {
            this.state = state
          },
          onEvent: applyRealtimeEvent,
        })
      }
      return this.runtime
    },
    connect() {
      return this.ensureRuntime().connect()
    },
    disconnect() {
      this.runtime?.disconnect()
      this.runtime = null
      this.state = 'degraded'
    },
  },
})

export default Ws
