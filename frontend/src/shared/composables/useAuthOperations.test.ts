import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  clearCSRFToken: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/plugins/httputil', () => ({
  default: { post: mocks.post },
  logout: vi.fn(),
}))
vi.mock('@/store/csrf', () => ({ clearCSRFToken: mocks.clearCSRFToken }))

import { login } from './useAuthOperations'

describe('authentication operations', () => {
  beforeEach(() => {
    mocks.clearCSRFToken.mockReset()
    mocks.post.mockReset()
  })

  it('discards the pre-auth CSRF token after the server rotates a successful login session', async () => {
    mocks.post.mockResolvedValue({ success: true, msg: '', obj: { state: 'authenticated' } })

    await expect(login('operator', 'credential')).resolves.toMatchObject({ success: true })

    expect(mocks.post).toHaveBeenCalledWith('api/login', {
      user: 'operator',
      pass: 'credential',
      remember: false,
    })
    expect(mocks.clearCSRFToken).toHaveBeenCalledOnce()
  })

  it('keeps the pre-auth token available after a rejected login', async () => {
    mocks.post.mockResolvedValue({ success: false, msg: 'Invalid login', obj: null })

    await login('operator', 'wrong')

    expect(mocks.clearCSRFToken).not.toHaveBeenCalled()
  })
})
