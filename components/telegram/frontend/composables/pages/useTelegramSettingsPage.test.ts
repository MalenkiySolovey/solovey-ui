import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, defineComponent, h } from 'vue'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { useTelegramSettingsPage } from './useTelegramSettingsPage'
import { telegramSettingsDefaults } from '../../views/telegramSettingsPayload'
import { STORED_SECRET_PLACEHOLDER } from '@/components/settings/settingsSecretField'
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
const notifications = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock('@/plugins/httputil', () => ({ default: http }))
vi.mock('notivue', () => ({ push: notifications }))
vi.mock('@/locales', () => ({ i18n: { global: { t: (key: string) => key } } }))
const renderer = createRenderer<any, any>({
  createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {},
  setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {},
})
const apps: Array<{ unmount: () => void }> = []
const deferred = () => { let resolve!: (value: any) => void; return { promise: new Promise<any>(done => { resolve = done }), resolve: (value: any) => resolve(value) } }
const saved = { ...telegramSettingsDefaults, telegramEnabled: 'true', telegramChatID: '100' }
const saveResponse = (settings: Record<string, string>) => ({ success: true, obj: { settings } })
const mount = async () => {
  let owner!: ReturnType<typeof useTelegramSettingsPage>
  const app = renderer.createApp(defineComponent({ setup: () => { owner = useTelegramSettingsPage(); return () => h('div') } }))
  app.mount({}); apps.push(app)
  await Promise.resolve(); await Promise.resolve()
  return { owner, app }
}
beforeEach(() => {
  vi.clearAllMocks()
  http.get.mockImplementation(async (url: string) => url === 'api/settings' ? { success: true, obj: { ...saved } } : { success: true, obj: { outbounds: [] } })
  http.post.mockResolvedValue({ success: true, obj: { success: true } })
})
afterEach(() => { apps.splice(0).forEach(app => app.unmount()) })

describe('Telegram settings action owner', () => {
  for (const response of [{ success: false }, { success: true }, { success: true, obj: null }, { success: true, obj: [] }]) {
    it('does not persist defaults without a confirmed settings snapshot: ' + JSON.stringify(response), async () => {
      http.get.mockImplementation(async (url: string) => url === 'api/settings' ? response : { success: true, obj: { outbounds: [] } })
      const { owner } = await mount()
      owner.settings.value.telegramBotToken = '123456:unsaved-test-token'
      expect(owner.settingsReady.value).toBe(false)
      await owner.testTelegram(); await owner.detectTelegramChat()
      expect(await owner.save()).toBe(false)
      expect(http.post).not.toHaveBeenCalled()
      expect(owner.busy.value).toBe(false)
    })
  }

  it('retries the existing read path before enabling settings actions', async () => {
    http.get.mockImplementation(async () => ({ success: false }))
    const { owner } = await mount()
    expect(owner.settingsReady.value).toBe(false)
    const retry = deferred(); http.get.mockReturnValueOnce(retry.promise)
    const pending = owner.reloadSettings()
    expect(owner.loading.value).toBe(true)
    await owner.testTelegram(); expect(http.post).not.toHaveBeenCalled()
    retry.resolve({ success: true, obj: saved }); await pending
    expect(owner.settingsReady.value).toBe(true)
    expect(owner.stateChange.value).toBe(false)
    await owner.testTelegram()
    expect(http.post.mock.calls[0][0]).toBe('api/telegram/test')
  })

  it('awaits the existing validated Save before Test and uses canonical saved settings', async () => {
    const { owner } = await mount()
    owner.settings.value.telegramChatID = '200'
    const save = deferred(); const test = deferred()
    http.post.mockReturnValueOnce(save.promise).mockReturnValueOnce(test.promise)
    const pending = owner.testTelegram()
    expect(owner.busy.value).toBe(true)
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post.mock.calls[0][0]).toBe('api/save')
    expect(http.post.mock.calls[0][1].object).toBe('settings')
    expect(http.post.mock.calls[0][1].action).toBe('set')
    expect(JSON.parse(http.post.mock.calls[0][1].data).telegramChatID).toBe('200')
    await owner.testTelegram(); await owner.detectTelegramChat()
    expect(await owner.save()).toBe(false)
    expect(http.post).toHaveBeenCalledTimes(1)
    save.resolve(saveResponse({ ...saved, telegramChatID: '201' }))
    await Promise.resolve(); await Promise.resolve(); await Promise.resolve()
    expect(owner.settings.value.telegramChatID).toBe('201')
    expect(owner.stateChange.value).toBe(false)
    expect(http.post.mock.calls[1][0]).toBe('api/telegram/test')
    expect(http.post.mock.calls[1][1]).toEqual({})
    expect(owner.testResult.value).toBeNull()
    test.resolve({ success: true, obj: { success: true } }); await pending
    expect(owner.testResult.value).toEqual({ success: true })
    expect(owner.busy.value).toBe(false)
  })

  for (const response of [{ success: false, msg: 'backend validation' }, { success: true }, { success: true, obj: { settings: [] } }]) {
    it('does not Test after a failed or non-authoritative Save response ' + JSON.stringify(response), async () => {
      const { owner } = await mount(); owner.settings.value.telegramChatID = '200'
      http.post.mockResolvedValueOnce(response)
      await owner.testTelegram()
      expect(http.post).toHaveBeenCalledTimes(1)
      expect(owner.settings.value.telegramChatID).toBe('200')
      expect(owner.stateChange.value).toBe(true)
      expect(owner.testResult.value).toBeNull()
      expect(owner.busy.value).toBe(false)
    })
  }

  it('does not create a persistence path for a clean Test', async () => {
    const { owner } = await mount(); await owner.testTelegram()
    expect(http.post).toHaveBeenCalledTimes(1)
    expect(http.post.mock.calls[0][0]).toBe('api/telegram/test')
  })

  it('keeps a newer programmatic draft when an older Save completes and skips Test', async () => {
    const { owner } = await mount(); owner.settings.value.telegramChatID = '200'
    const save = deferred(); http.post.mockReturnValueOnce(save.promise)
    const pending = owner.testTelegram()
    owner.settings.value.telegramChatID = '300'
    save.resolve(saveResponse({ ...saved, telegramChatID: '200' })); await pending
    expect(owner.settings.value.telegramChatID).toBe('300')
    expect(owner.stateChange.value).toBe(true)
    expect(http.post).toHaveBeenCalledTimes(1)
    owner.settings.value.telegramChatID = '200'
    expect(owner.stateChange.value).toBe(false)
  })

  it('aborts and fences old Test result on a draft change, then permits a current Test', async () => {
    const { owner } = await mount(); const old = deferred()
    http.post.mockReturnValueOnce(old.promise)
    const pending = owner.testTelegram(); const signal = http.post.mock.calls[0][2].signal
    owner.settings.value.telegramChatID = '200'
    expect(signal.aborted).toBe(true)
    old.resolve({ success: true, obj: { success: true } }); await pending
    expect(owner.testResult.value).toBeNull()
    http.post.mockResolvedValueOnce(saveResponse({ ...saved, telegramChatID: '200' })).mockResolvedValueOnce({ success: true, obj: { success: false, errorClass: 'unauthorized' } })
    await owner.testTelegram()
    expect(owner.testResult.value).toEqual({ success: false, errorClass: 'unauthorized' })
    expect(owner.busy.value).toBe(false)
  })

  it('uses existing backup validation and placeholder stripping on the sole Save path', async () => {
    const { owner } = await mount()
    owner.settings.value.telegramBackupPassphrase = 'short'
    await owner.testTelegram()
    expect(http.post).not.toHaveBeenCalled()
    owner.settings.value.telegramBackupPassphrase = ''
    owner.settings.value.telegramBotTokenHasSecret = 'true'
    owner.settings.value.telegramBotToken = STORED_SECRET_PLACEHOLDER
    http.post.mockResolvedValueOnce(saveResponse({ ...saved, telegramBotTokenHasSecret: 'true' }))
    await owner.save()
    expect(JSON.parse(http.post.mock.calls[0][1].data).telegramBotToken).toBe('')
  })

  for (const phase of ['save', 'test', 'detect'] as const) {
    it('teardown aborts and fences pending ' + phase, async () => {
      const { owner, app } = await mount(); const old = deferred()
      if (phase === 'save') owner.settings.value.telegramChatID = '200'
      http.post.mockReturnValueOnce(old.promise)
      const pending = phase === 'detect' ? owner.detectTelegramChat() : owner.testTelegram()
      const signal = http.post.mock.calls[0][2].signal
      app.unmount(); expect(signal.aborted).toBe(true)
      old.resolve(phase === 'save' ? saveResponse({ ...saved, telegramChatID: '201' }) : { success: true, obj: { success: true, chatId: '999' } }); await pending
      expect(owner.testResult.value).toBeNull(); expect(owner.discoveryResult.value).toBeNull()
      expect(owner.settings.value.telegramChatID).toBe(phase === 'save' ? '200' : '100')
      expect(owner.busy.value).toBe(false)
      expect(http.post).toHaveBeenCalledTimes(1)
    })
  }

  for (const action of ['save', 'test', 'detect'] as const) {
    it('propagates a current programming failure and releases pending ' + action, async () => {
      const { owner } = await mount(); if (action !== 'detect') owner.settings.value.telegramChatID = '200'
      http.post.mockRejectedValueOnce(new Error('programming failure'))
      await expect(action === 'save' ? owner.save() : action === 'test' ? owner.testTelegram() : owner.detectTelegramChat()).rejects.toThrow('programming failure')
      expect(owner.busy.value).toBe(false)
      expect(http.post).toHaveBeenCalledTimes(1)
    })
  }

  it('shows disabled inputs/actions and an accessible bounded discovery result', () => {
    const source = readFileSync(fileURLToPath(new URL('../../views/TelegramSettings.vue', import.meta.url)), 'utf8')
    expect(source).toContain('<fieldset :disabled="busy || !settingsReady"')
    expect(source).toContain(':disabled="busy || !settingsReady || !stateChange ||')
    expect(source).toContain(':loading="testLoading" :disabled="busy || !settingsReady"')
    expect(source).toContain('@click="reloadSettings"')
    expect(source).toContain(`:aria-label="$t('telegram.detectChat')"`)
    expect(source).toContain('role="status" aria-live="polite"')
  })
})

describe('Telegram discovery draft action', () => {
  for (const token of ['', STORED_SECRET_PLACEHOLDER, '123456:request-only-test-token']) {
    it('chooses request-local or stored token without persisting it: ' + (token ? 'provided' : 'empty'), async () => {
      const { owner } = await mount(); owner.settings.value.telegramBotToken = token
      http.post.mockResolvedValueOnce({ success: true, obj: { success: true, chatId: '-1001234567890123456' } })
      await owner.detectTelegramChat()
      expect(http.post).toHaveBeenCalledTimes(1)
      expect(http.post.mock.calls[0][0]).toBe('api/telegram/detect-chat')
      expect(http.post.mock.calls[0][1]).toEqual(token && token !== STORED_SECRET_PLACEHOLDER ? { token } : {})
      expect(owner.settings.value.telegramChatID).toBe('-1001234567890123456')
      expect(owner.settings.value.telegramBotToken).toBe(token)
      expect(owner.discoveryResult.value?.success).toBe(true)
      expect(owner.stateChange.value).toBe(true)
      expect(owner.busy.value).toBe(false)
    })
  }

  it('does not use old discovery after edit; a later action owns its result', async () => {
    const { owner } = await mount(); const old = deferred(); http.post.mockReturnValueOnce(old.promise)
    const pending = owner.detectTelegramChat(); const signal = http.post.mock.calls[0][2].signal
    await owner.testTelegram(); expect(await owner.save()).toBe(false)
    expect(http.post).toHaveBeenCalledTimes(1)
    owner.settings.value.telegramBotToken = '123456:next-test-token'
    expect(signal.aborted).toBe(true)
    old.resolve({ success: true, obj: { success: true, chatId: '999' } }); await pending
    expect(owner.settings.value.telegramChatID).toBe('100'); expect(owner.discoveryResult.value).toBeNull()
    http.post.mockResolvedValueOnce({ success: true, obj: { success: true, chatId: '777' } }); await owner.detectTelegramChat()
    expect(owner.settings.value.telegramChatID).toBe('777')
  })

  it('preserves the draft on provider failure or invalid chat identity', async () => {
    const { owner } = await mount()
    for (const result of [{ success: false, errorClass: 'no_chat' }, { success: true, chatId: '0' }, { success: true, chatId: 123 }, { success: true, chatId: 'bad' }]) {
      http.post.mockResolvedValueOnce({ success: true, obj: result }); await owner.detectTelegramChat()
      expect(owner.settings.value.telegramChatID).toBe('100')
      expect(owner.discoveryResult.value?.success).toBe(false)
      expect(owner.busy.value).toBe(false)
    }
  })
})
