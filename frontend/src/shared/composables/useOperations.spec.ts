import { beforeEach, describe, expect, it, vi } from 'vitest'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/plugins/httputil', () => ({ default: http }))

import {
  activateSignedUpdate,
  dropConfirmation,
  executeDropData,
  getResourcePressure,
  getUpdatePosture,
  getUpdateTimeline,
  isFresh,
  operationsMessage,
  prepareSignedUpdate,
  restoreConfirmation,
  rollbackSignedUpdate,
  safeOperationalValue,
  safeReasonCodes,
  updateConfirmation,
} from './useOperations'

describe('operations API contract', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    http.get.mockResolvedValue({ success: true, msg: '', obj: {} })
    http.post.mockResolvedValue({ success: true, msg: '', obj: {} })
  })

  it('uses fixed semantic routes and purpose-bound step-up headers', async () => {
    const payload = { operationId: 'update-operation:one', expectedRevision: 4, confirmation: 'ACTIVATE_UPDATE_17' }
    await getUpdatePosture('main')
    await getResourcePressure()
    await prepareSignedUpdate({ channel: 'main' }, 'prepare-grant')
    await activateSignedUpdate(payload, 'activate-grant')
    await rollbackSignedUpdate({ ...payload, confirmation: 'ROLLBACK_UPDATE_17' }, 'rollback-grant')
    await executeDropData({ ownerId: 'server-protection' }, 'drop-grant')

    expect(http.get).toHaveBeenNthCalledWith(1, 'api/v1/operations/update/posture', { channel: 'main' })
    expect(http.get).toHaveBeenNthCalledWith(2, 'api/v1/operations/resource-pressure')
    expect(http.post.mock.calls.map(call => call[0])).toEqual([
      'api/v1/operations/update/preflight',
      'api/v1/operations/update/activate',
      'api/v1/operations/update/rollback',
      'api/v1/operations/data/drop',
    ])
    expect(http.post.mock.calls.map(call => call[2]?.headers?.['X-Step-Up-Token'])).toEqual([
      'prepare-grant', 'activate-grant', 'rollback-grant', 'drop-grant',
    ])
    expect(JSON.stringify(http.post.mock.calls)).not.toMatch(/(?:url|path|command|argv|environment|publicKey)/i)
  })

  it('encodes operation identity and bounds the timeline request', async () => {
    await getUpdateTimeline('update-operation:id/with?delimiters', 9, 100)

    expect(http.get).toHaveBeenCalledWith(
      `api/v1/operations/update/operations/${encodeURIComponent('update-operation:id/with?delimiters')}/timeline`,
      { after: 9, limit: 100 },
    )
  })

  it('derives exact confirmations without retaining credentials or secrets', () => {
    expect(updateConfirmation('PREPARE', 17)).toBe('PREPARE_UPDATE_17')
    expect(updateConfirmation('ACTIVATE', 17)).toBe('ACTIVATE_UPDATE_17')
    expect(updateConfirmation('ROLLBACK', 17)).toBe('ROLLBACK_UPDATE_17')
    expect(dropConfirmation('server-protection')).toBe('DROP_DATA_SERVER_PROTECTION')
    expect(restoreConfirmation('abcdef0123456789')).toBe('RESTORE_DATABASE_ABCDEF012345')
    expect(restoreConfirmation('short')).toBe('')
  })

  it('renders future values safely and filters malformed reason text', () => {
    expect(safeOperationalValue('FUTURE_STATE')).toBe('FUTURE_STATE')
    expect(safeOperationalValue('')).toBe('UNKNOWN')
    expect(safeOperationalValue('x'.repeat(300))).toBe('UNKNOWN')
    expect(safeReasonCodes(['known_reason', '<script>', 'owner:component-1'])).toEqual([
      'known_reason', 'owner:component-1',
    ])
    expect(isFresh(101, 100)).toBe(true)
    expect(isFresh(100, 100)).toBe(false)
    expect(operationsMessage<string>({ success: true, msg: '', obj: 'FUTURE_STATE' })).toBe('FUTURE_STATE')
    expect(operationsMessage<string>({ success: false, msg: 'safe', obj: 'hidden' })).toBeNull()
  })
})
