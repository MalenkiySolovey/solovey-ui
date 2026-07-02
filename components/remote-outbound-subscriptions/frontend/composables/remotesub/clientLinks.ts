import type { Link } from '@/types/clients'

export const isRemoteGroupLink = (link: Link) =>
  link.type === 'remoteGroup' && Boolean(link.groupId || link.remoteGroupId)

export const isRemoteSubscriptionLink = (link: Link) =>
  link.type === 'remoteSubscription' && Boolean(link.subscriptionId || link.remoteSubscriptionId)

export const isRemoteClientLink = (link: Link) => isRemoteGroupLink(link) || isRemoteSubscriptionLink(link)

export const remoteClientSelectionValues = (links: Link[]): string[] =>
  links
    .filter(isRemoteClientLink)
    .map(link => {
      if (link.type === 'remoteSubscription') {
        const id = Number(link.subscriptionId ?? link.remoteSubscriptionId ?? 0)
        return id ? `subscription:${id}` : ''
      }
      const id = Number(link.groupId ?? link.remoteGroupId ?? 0)
      return id ? `group:${id}` : ''
    })
    .filter(Boolean)
    .sort()

export const replaceRemoteClientLinks = (
  componentLinks: Link[],
  selectedIDs: string[],
  names: Map<string, string>,
): Link[] => {
  const otherComponentLinks = componentLinks.filter(link => !isRemoteClientLink(link))
  const remoteLinks = selectedIDs.map(rawID => {
    const value = String(rawID)
    const [kind, idText] = value.split(':')
    const id = Number(idText)
    if (kind === 'subscription' && id > 0) {
      return {
        type: 'remoteSubscription',
        subscriptionId: id,
        remark: names.get(value) ?? `Subscription ${id} / All`,
        uri: '',
      }
    }
    return {
      type: 'remoteGroup',
      groupId: id,
      remark: names.get(value) ?? String(id),
      uri: '',
    }
  }).filter(isRemoteClientLink)
  return [...otherComponentLinks, ...remoteLinks]
}
