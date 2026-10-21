import type { RemoteOutboundConnection } from './types'

export const projectionBlocked = (connection: Pick<RemoteOutboundConnection, 'projectionStatus'>): boolean =>
  connection.projectionStatus === 'MANUAL_MAPPING_REQUIRED' || connection.projectionStatus === 'UNSUPPORTED'
