import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/plugins/httputil', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

import HttpUtils from '@/plugins/httputil'
import { confirmSSHReconnect } from './useSSHManagement'
import type { DesiredSSHPolicy, SSHPreview } from './useSSHManagement'

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

describe('SSH policy and preview types', () => {
  it('keeps desired policy semantic while concrete preview is backend-labelled', () => {
    const policy: DesiredSSHPolicy = { schema: 'solovey-ui/ssh-managed-policy/v1', permitRootLogin: 'UNCHANGED' }
    const preview = {
      policy,
      concretePreview: {
        implementation: 'dropbear', format: 'uci_change_preview', label: 'Dropbear UCI selected-section changes',
        representation: "set dropbear.<selected>.PasswordAuth='0'\n", artifactDigest: 'a'.repeat(64),
      },
    } as SSHPreview

    expect(Object.keys(policy)).toEqual(['schema', 'permitRootLogin'])
    expect(preview.concretePreview?.implementation).toBe('dropbear')
    expect(preview.concretePreview?.format).toBe('uci_change_preview')
  })
})
