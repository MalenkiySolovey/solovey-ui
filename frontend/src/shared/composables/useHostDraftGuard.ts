import { getCurrentInstance, onMounted, onUnmounted, toValue, type MaybeRefOrGetter } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { i18n } from '@/locales'
import { hostChangeAllowed } from '@/router/hostContext'

// Shared canonical editors keep their existing draft. Only an explicit discard
// may leave its Host; a full-page navigation also receives the browser guard.
export const useHostDraftGuard = (dirty: MaybeRefOrGetter<boolean>) => {
  if (!getCurrentInstance()) return
  const guard = (to: Parameters<typeof hostChangeAllowed>[0], from: Parameters<typeof hostChangeAllowed>[1]) =>
    hostChangeAllowed(to, from, toValue(dirty), () => window.confirm(i18n.global.t('form.leaveConfirm')))
  onBeforeRouteLeave(guard)
  onBeforeRouteUpdate(guard)
  const beforeUnload = (event: BeforeUnloadEvent) => {
    if (!toValue(dirty)) return
    event.preventDefault()
    event.returnValue = ''
  }
  onMounted(() => window.addEventListener('beforeunload', beforeUnload))
  onUnmounted(() => window.removeEventListener('beforeunload', beforeUnload))
}
