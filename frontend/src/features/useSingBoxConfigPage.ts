import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { useUiMode } from '@/uiMode/useUiMode'
import { push } from 'notivue'
import { computed, onMounted, ref } from 'vue'

export const useSingBoxConfigPage = () => {
  const { mode } = useUiMode()
  const refreshing = ref(false)
  const config = ref<Record<string, unknown>>({})
  const nexus = computed(() => mode.value === 'nexus')
  const configText = computed(() => JSON.stringify(config.value ?? {}, null, 2))

  const refreshConfig = async () => {
    refreshing.value = true
    try {
      config.value = await HttpUtils.getRaw<Record<string, unknown>>('api/singbox-config', {}, {
        headers: { Accept: 'application/json' },
      })
    } catch (error: any) {
      push.error({
        message: error?.response?.data || error?.message || i18n.global.t('failed'),
        duration: 5000,
      })
    } finally {
      refreshing.value = false
    }
  }

  const copyConfig = async () => {
    try {
      await navigator.clipboard.writeText(configText.value)
      push.success({
        message: i18n.global.t('success') + ': ' + i18n.global.t('copyToClipboard'),
        duration: 5000,
      })
    } catch {
      push.error({
        message: i18n.global.t('failed') + ': ' + i18n.global.t('copyToClipboard'),
        duration: 5000,
      })
    }
  }

  onMounted(refreshConfig)

  return { configText, copyConfig, nexus, refreshConfig, refreshing }
}
