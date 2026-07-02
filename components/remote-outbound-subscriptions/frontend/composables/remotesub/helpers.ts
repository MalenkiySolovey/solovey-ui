import type { RemoteOutboundConnection, RemoteOutboundGroup, RemoteOutboundSubscription } from './types'

export const connectionGroupIds = (connection: RemoteOutboundConnection): number[] => {
  if (connection.groupIds && connection.groupIds.length > 0) return connection.groupIds
  return connection.groupId ? [connection.groupId] : []
}

export const connectionGroupNames = (subscription: RemoteOutboundSubscription, connection: RemoteOutboundConnection): string => {
  const ids = connectionGroupIds(connection)
  const names = (subscription.groups ?? [])
    .filter(group => ids.includes(group.id))
    .map(group => group.name)
  return names.length > 0 ? names.join(', ') : '-'
}

export function connectionSourceType(connection: RemoteOutboundConnection): string {
  return String(connection.sourceType || connection.type || '-')
}

export function connectionConvertedType(connection: RemoteOutboundConnection): string {
  return String(connection.convertedType || connection.type || '-')
}

export const isDefaultGroup = (group: RemoteOutboundGroup): boolean => {
  return group.name === 'Default'
}

export const formatTime = (value: number) => {
  if (!value) return '-'
  return new Date(value * 1000).toLocaleString()
}

export const formatInterval = (value: number) => {
  const seconds = Number(value) || 0
  if (seconds <= 0) return '-'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} h`
  return `${Math.round(hours / 24)} d`
}
