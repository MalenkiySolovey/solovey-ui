// Composables
import { createRouter, createWebHistory } from 'vue-router'
import { routes } from './routes'
import { LOCAL_HOST, routeHostID, hostPath, safeHostReturnTo } from './hostContext'
import Data from '@/store/modules/data'
import Ws from '@/store/ws'
import { getBaseUrl } from '@/plugins/base-url'
import { syncEnabledComponents } from '@/componentSystem/loader'
import { setComponentUIReadPermissions } from '@/componentSystem/registry'
import { getSecurityPosture, messageObject, type SecurityPosture } from '@/shared/composables/useSecurityOperations'
import { configureLoginNavigation } from '@/plugins/loginNavigation'
import { createPreloadRecovery, isPreloadError } from './preloadRecovery'


const router = createRouter({
  history: createWebHistory(getBaseUrl()),
  routes,
})

configureLoginNavigation(() => {
  const path = window.location.pathname.startsWith(getBaseUrl())
    ? '/' + window.location.pathname.slice(getBaseUrl().length) + window.location.search + window.location.hash
    : router.currentRoute.value.fullPath
  const returnTo = safeHostReturnTo(path)
  return router.push({ name: 'pages.login', query: returnTo ? { returnTo } : {} })
})

// After a panel upgrade, the browser tab still holds the previous
// index.html with hashed chunk names. The first dynamic import (e.g.
// navigating to /clients) requests a chunk that no longer exists in the
// embedded FS and the request fails. Without this guard the page just
// stops navigating and shows the broken Clients tab. Reloading once
// fetches the new index.html with the new hashes. We use sessionStorage
// to make sure we never enter an infinite reload loop.
const preloadRecovery = createPreloadRecovery(() => window.location.reload(), () => sessionStorage)
window.addEventListener('vite:preloadError', () => preloadRecovery.reloadOnce())
router.onError((err) => {
  if (isPreloadError(err)) preloadRecovery.reloadOnce()
})
router.afterEach((_to, _from, failure) => {
  if (!failure) preloadRecovery.navigationSucceeded()
})

// The session cookie is HttpOnly (set by api/session.go) so it cannot be
// observed from the client; auth is enforced server-side. Every API call
// returns `Invalid login` when the cookie is missing or expired, and
// httputil._handleMsg redirects the user to /login in that case. The router
// guard below only handles the UX detail of pulling fresh data on first
// navigation to a protected page and stopping the polling timer when we
// land on /login.
router.beforeEach(async (to) => {
  if (routeHostID(to) && routeHostID(to) !== LOCAL_HOST.id) {
    Ws().disconnect()
    return
  }
  const data = Data()
  const transitionOnly = to.path === '/security-transition'
  if (to.path !== '/login' && !transitionOnly) {
    const postureResponse = await getSecurityPosture()
    const posture = messageObject<SecurityPosture>(postureResponse)
    setComponentUIReadPermissions(posture?.authState === 'authenticated' ? posture.componentUIReadPermissions : [])
    if (posture?.authState === 'password_reset' || posture?.authState === 'mfa_pending' || posture?.authState === 'mfa_recovery') {
      return { name: 'pages.securityTransition', query: { returnTo: safeHostReturnTo(to.fullPath) } }
    }
    if (!data.componentsLoaded) {
      try {
        await data.loadData()
      } catch {
        // Keep navigation usable; API/auth errors are handled by HttpUtils.
      }
    }
    await syncEnabledComponents(router)
    void Ws().connect()
    const resolved = router.resolve(to.fullPath)
    const componentId = to.meta.componentId as string | undefined
    if ((to.matched.length === 0 || to.name === 'pages.unavailable') && resolved.name !== 'pages.unavailable' && resolved.matched.length > 0) {
      return to.fullPath
    }
    if (to.matched.length === 0) {
      return hostPath('/')
    }
    if (componentId && !resolved.matched.some(record => record.meta.componentId === componentId)) {
      return to.fullPath
    }
  } else {
    setComponentUIReadPermissions([])
    Ws().disconnect()
  }
})

export default router
