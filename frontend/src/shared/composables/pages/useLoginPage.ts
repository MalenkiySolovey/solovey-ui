import { i18n, languages, setI18nLocale } from '@/locales'
import { resetInvalidLoginHandling } from '@/plugins/httputil'
import { login as authenticate } from '@/shared/composables/useAuthOperations'
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useLocale, useTheme } from 'vuetify'

export const useLoginPage = () => {
  const theme = useTheme()
  const locale = useLocale()
  const router = useRouter()
  const username = ref('')
  const password = ref('')
  const loading = ref(false)
  const errorMessage = ref('')

  const themes = [
    { value: 'light', icon: 'mdi-white-balance-sunny' },
    { value: 'dark', icon: 'mdi-moon-waning-crescent' },
    { value: 'system', icon: 'mdi-laptop' },
  ]

  const usernameRules = [(value: string) => value?.length > 0 || i18n.global.t('login.unRules')]
  const passwordRules = [(value: string) => value?.length > 0 || i18n.global.t('login.pwRules')]

  watch([username, password], () => {
    errorMessage.value = ''
  })

  const login = async () => {
    if (!username.value || !password.value) return

    errorMessage.value = ''
    loading.value = true
    try {
      const response = await authenticate(username.value, password.value)
      if (response.success) {
        resetInvalidLoginHandling()
        await router.push('/')
        return
      }
      errorMessage.value = response.msg || i18n.global.t('login.invalidCredentials')
    } finally {
      loading.value = false
    }
  }

  const changeLocale = async (value: string | null) => {
    locale.current.value = await setI18nLocale(value ?? 'en')
  }

  const changeTheme = (value: string) => {
    theme.change(value)
    localStorage.setItem('theme', value)
  }

  const isActiveTheme = (value: string) => (localStorage.getItem('theme') ?? 'system') === value

  return {
    changeLocale,
    changeTheme,
    errorMessage,
    isActiveTheme,
    languages,
    loading,
    login,
    password,
    passwordRules,
    themes,
    username,
    usernameRules,
  }
}
