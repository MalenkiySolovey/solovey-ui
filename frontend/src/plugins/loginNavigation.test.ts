import { describe, expect, it, vi } from 'vitest'

import { configureLoginNavigation, navigateToLogin } from './loginNavigation'

describe('login navigation adapter', () => {
  it('delegates authentication redirects without coupling the HTTP client to the router', async () => {
    const navigate = vi.fn()
    configureLoginNavigation(navigate)

    await navigateToLogin()

    expect(navigate).toHaveBeenCalledTimes(1)
  })
})
