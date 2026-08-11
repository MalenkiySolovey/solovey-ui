import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/plugins/httputil', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

import HttpUtils from '@/plugins/httputil'
import { confirmSSHReconnect } from './useSSHManagement'

describe('SSH reconnect confirmation API', () => {
  beforeEach(() => {
    vi.mocked(HttpUtils.post).mockReset()
  })

  it('binds the one-time evidence reference and step-up token to the candidate revision', async () => {
    vi.mocked(HttpUtils.post).mockResolvedValue({ success: true, msg: '', obj: { state: 'COMMITTED' } })

    await confirmSSHReconnect('ssh-operation:test', 7, 'ssh-proof:evidence', 'step-up-value')

    expect(HttpUtils.post).toHaveBeenCalledWith(
      'api/v1/operations/ssh/candidate/ssh-operation%3Atest/reconnect/confirm',
      { expectedRevision: 7, providerEvidenceRef: 'ssh-proof:evidence' },
      {
        headers: {
          'Content-Type': 'application/json',
          'X-Step-Up-Token': 'step-up-value',
        },
      },
    )
  })
})
