import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, createSSRApp, defineComponent, h, nextTick, reactive, ref, ssrContextKey } from 'vue'
import { renderToString } from '@vue/server-renderer'
import Stats from './Stats.vue'
import { useTrafficTimeZone } from '@/shared/composables/trafficTimeZone'

const load = vi.hoisted(() => vi.fn())
vi.mock('@/shared/composables/useOperationsData', () => ({ loadStats: load }))
vi.mock('@/locales', () => ({ dateLocale: () => 'en-US', i18n: { global: { t: (key: string) => key, n: (n: number) => String(n) } } }))
const renderer = createRenderer<any, any>({ createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {}, setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {} })
const apps: { unmount: () => void }[] = []
const now = Date.parse('2024-03-09T23:30:00Z')
const requests: { signal: AbortSignal, resolve: (response: any) => void }[] = []
let hidden = false
let page: EventTarget
let values: Map<string, string>
async function flush() { await Promise.resolve(); await nextTick(); await Promise.resolve() }

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(now)
  hidden = false
  page = new EventTarget()
  Object.defineProperty(page, 'hidden', { get: () => hidden })
  vi.stubGlobal('document', page)
  values = new Map()
  vi.stubGlobal('localStorage', { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value) })
  requests.splice(0)
  load.mockReset().mockImplementation((_resource: string, _tag: string, _hours: number, signal: AbortSignal) => new Promise(resolve => { requests.push({ signal, resolve }) }))
})
afterEach(() => { apps.splice(0).forEach(app => app.unmount()); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

function mount(visible = true) {
  const props = reactive({ visible, resource: 'client', tag: 'first' })
  const component = defineComponent({ ...(Stats as any), render: () => h('div') })
  const instance = ref<any>(null)
  const app = renderer.createApp(defineComponent({ setup: () => () => h(component, { ...props, ref: instance }) }))
  app.config.warnHandler = () => {}
  app.provide(ssrContextKey, { modules: new Set() })
  app.config.globalProperties.$t = (key: string) => key
  apps.push(app)
  app.mount({})
  const vm = instance.value
  vm.timeZone = 'UTC'
  return { vm, props, app }
}
function accepted(traffic = 7) { return { success: true, obj: [{ dateTime: now / 1000 - 10, direction: false, traffic }] } }

describe('shared Stats request and timer lifecycle', () => {
  it('keeps an in-flight timer request and persisted opt-in without overlapping loads', async () => {
    values.set('sui:stats:auto-refresh', 'true')
    const { vm } = mount()
    expect(vm.autoRefresh).toBe(true)
    expect(vi.getTimerCount()).toBe(1)
    await vi.advanceTimersByTimeAsync(30000)
    expect(load).toHaveBeenCalledTimes(1)
    expect(requests[0].signal.aborted).toBe(false)
    requests[0].resolve(accepted())
    await flush()
    vm.autoRefresh = false
    await flush()
    expect(values.get('sui:stats:auto-refresh')).toBe('false')
    expect(vi.getTimerCount()).toBe(0)
  })
  it('loads initially but defaults to manual refresh, without a core operation', async () => {
    const { vm } = mount()
    expect(load).toHaveBeenCalledTimes(1)
    requests[0].resolve(accepted())
    await flush()
    await vi.advanceTimersByTimeAsync(30000)
    expect(load).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
    void vm.loadData()
    expect(load).toHaveBeenCalledTimes(2)
    expect(load.mock.calls[1].slice(0, 3)).toEqual(['client', 'first', 1])
  })
  it('uses one opt-in timer, suspends hidden pages/dialogs and resumes without duplicates', async () => {
    const { vm, props, app } = mount(false)
    expect(load).not.toHaveBeenCalled()
    vm.autoRefresh = true
    await flush()
    expect(vi.getTimerCount()).toBe(0)
    props.visible = true
    await flush()
    expect(vi.getTimerCount()).toBe(1)
    requests[0].resolve(accepted())
    await flush()
    await vi.advanceTimersByTimeAsync(10000)
    expect(load).toHaveBeenCalledTimes(2)
    hidden = true
    page.dispatchEvent(new Event('visibilitychange'))
    expect(requests[1].signal.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
    hidden = false
    page.dispatchEvent(new Event('visibilitychange'))
    page.dispatchEvent(new Event('visibilitychange'))
    expect(vi.getTimerCount()).toBe(1)
    props.visible = false
    await flush()
    expect(vi.getTimerCount()).toBe(0)
    expect(requests.at(-1)!.signal.aborted).toBe(true)
    app.unmount()
    const count = load.mock.calls.length
    page.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(30000)
    expect(load).toHaveBeenCalledTimes(count)
  })
  it('aborts and discards obsolete loads on new parameters, manual refresh, hide and unmount', async () => {
    const { vm, props, app } = mount()
    props.tag = 'second'
    vm.limit = 6
    await flush()
    expect(requests[0].signal.aborted).toBe(true)
    expect(load.mock.calls.at(-1)!.slice(0, 3)).toEqual(['client', 'second', 6])
    requests[1].resolve(accepted(11))
    await flush()
    requests[0].resolve(accepted(999))
    await flush()
    expect(vm.total.download).toBe(11)
    void vm.loadData()
    const obsolete = requests.at(-1)!
    void vm.loadData()
    expect(obsolete.signal.aborted).toBe(true)
    const newest = requests.at(-1)!
    newest.resolve(accepted(17))
    await flush()
    obsolete.resolve(accepted(888))
    await flush()
    expect(vm.total.download).toBe(17)
    void vm.loadData()
    const hiddenLoad = requests.at(-1)!
    props.visible = false
    await flush()
    hiddenLoad.resolve(accepted(777))
    await flush()
    expect(hiddenLoad.signal.aborted).toBe(true)
    expect(vm.loaded).toBe(false)
    props.visible = true
    await flush()
    const last = requests.at(-1)!
    app.unmount()
    expect(last.signal.aborted).toBe(true)
    last.resolve(accepted(666))
    await flush()
    expect(vm.loaded).toBe(false)
  })
  it('sums exactly the chart samples and changes only labels across timezone/day boundaries', async () => {
    const { vm } = mount()
    const samples = [
      { dateTime: now / 1000 - 20, direction: true, traffic: 3 },
      { dateTime: now / 1000 - 10, direction: false, traffic: 7 },
      { dateTime: now / 1000 - 10, direction: false, traffic: 5 },
      null, {}, { dateTime: now / 1000 - 100, direction: true, traffic: -5 },
      { dateTime: now / 1000 + 10, direction: false, traffic: 999 },
      { dateTime: now / 1000 - 4000, direction: false, traffic: 999 },
      { dateTime: now / 1000 - 100, direction: 'false', traffic: 999 },
      { dateTime: 'invalid', direction: false, traffic: Infinity },
    ]
    const original = structuredClone(samples)
    requests[0].resolve({ success: true, obj: samples })
    await flush()
    expect(vm.total).toEqual({ upload: 3, download: 12 })
    expect(vm.chart.upload.filter((value: any) => value === null).length).toBeGreaterThan(350)
    const chart = structuredClone({ timestamps: [...vm.chart.timestamps], upload: [...vm.chart.upload], download: [...vm.chart.download] })
    const labels = [...vm.usage.labels]
    const otherView = useTrafficTimeZone()
    otherView.timeZone.value = 'Asia/Tokyo'
    await flush()
    expect(vm.timeZone).toBe('Asia/Tokyo')
    expect(vm.usage.labels).not.toEqual(labels)
    expect(vm.usage.labels.at(-1)).toContain('03/10/2024')
    expect(vm.chart).toEqual(chart)
    expect(vm.total).toEqual({ upload: 3, download: 12 })
    expect(samples).toEqual(original)
    expect(load).toHaveBeenCalledTimes(1)
  })
  it('keeps sparse/empty totals honest and remains usable without preference storage', async () => {
    vi.stubGlobal('localStorage', { getItem: () => { throw Error('disabled') }, setItem: () => { throw Error('disabled') } })
    const { vm } = mount()
    requests[0].resolve({ success: true, obj: [] })
    await flush()
    expect(vm.total).toEqual({ upload: 0, download: 0 })
    vm.autoRefresh = true
    vm.timeZone = 'Asia/Tokyo'
    await flush()
    expect(vi.getTimerCount()).toBe(1)
  })
  for (const mode of ['classic', 'nexus']) {
    it(`${mode} renders manual refresh, optional timer, timezone and graph-window total in the shared dialog`, async () => {
      const app = createSSRApp(defineComponent({ ...(Stats as any), created(this: any) { this.chart = { timestamps: [now / 1000], upload: [3], download: [7] }; this.loaded = true } }), { visible: true, modelValue: true, resource: 'client', tag: 'fixture' })
      app.config.globalProperties.$t = (key: string) => key
      app.config.warnHandler = () => {}
      for (const name of ['v-dialog', 'v-card', 'v-card-title', 'v-card-text', 'v-row', 'v-col', 'v-spacer', 'v-icon', 'v-divider', 'v-btn', 'v-switch', 'v-autocomplete', 'v-radio-group', 'v-radio', 'v-container', 'v-skeleton-loader', 'v-alert']) {
        app.component(name, defineComponent({ inheritAttrs: false, setup: (_, context) => () => h('div', context.attrs, context.slots.default?.()) }))
      }
      const html = await renderToString(app)
      expect(html).toContain('stats.refresh')
      expect(html).toContain('stats.autoRefresh')
      expect(html).toContain('stats.timeZone')
      expect(html).toContain('stats.windowTotal')
      expect(html).not.toContain('restart')
      expect(load).not.toHaveBeenCalled()
    })
  }
})
