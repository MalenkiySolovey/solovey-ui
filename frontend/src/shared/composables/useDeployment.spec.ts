import { beforeEach, describe, expect, it, vi } from 'vitest'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/plugins/httputil', () => ({ default: http }))

import {
  confirmDeploymentMigration,
  deploymentMessage,
  getDeploymentBroker,
  getDeploymentRecovery,
  getDeploymentStatus,
  getDeploymentTimeline,
  previewDeployment,
  rollbackDeploymentMigration,
  startDeploymentMigration,
} from './useDeployment'

describe('deployment API contract', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses only fixed semantic routes and typed profile payloads', async () => {
    http.get.mockResolvedValue({ success: true, obj: {} })
    http.post.mockResolvedValue({ success: true, obj: {} })

    await getDeploymentStatus()
    await getDeploymentBroker()
    await getDeploymentRecovery()
    await previewDeployment('native-hardened', true)
    await startDeploymentMigration({ targetProfile: 'native-hardened' }, 'step-up')

    expect(http.get.mock.calls.map(call => call[0])).toEqual([
      'api/v1/operations/deployment/status',
      'api/v1/operations/deployment/broker',
      'api/v1/operations/deployment/recovery',
    ])
    expect(http.post).toHaveBeenCalledWith(
      'api/v1/operations/deployment/preview',
      { targetProfile: 'native-hardened', acknowledged: true },
      { headers: { 'Content-Type': 'application/json' } },
    )
    expect(http.post.mock.calls.flat().join(' ')).not.toContain('/etc/')
    expect(http.post.mock.calls.flat().join(' ')).not.toContain('docker.sock')
  })

  it('encodes operation identifiers and preserves safe unknown status values', async () => {
    http.get.mockResolvedValue({ success: true, obj: {} })
    http.post.mockResolvedValue({ success: true, obj: {} })
    const operation = 'deployment-operation:id/with?delimiters'
    await getDeploymentTimeline(operation)
    await confirmDeploymentMigration(operation, 7, 'grant')
    await rollbackDeploymentMigration(operation, 8, 'grant')

    const encoded = encodeURIComponent(operation)
    expect(http.get).toHaveBeenCalledWith(`api/v1/operations/deployment/migration/${encoded}/timeline`)
    expect(http.post.mock.calls[0][0]).toBe(`api/v1/operations/deployment/migration/${encoded}/confirm`)
    expect(http.post.mock.calls[1][0]).toBe(`api/v1/operations/deployment/migration/${encoded}/rollback`)
    expect(deploymentMessage<string>({ success: true, msg: '', obj: 'FUTURE_STATE' })).toBe('FUTURE_STATE')
    expect(deploymentMessage<string>({ success: false, msg: 'safe error', obj: 'leak' })).toBeNull()
  })
})
