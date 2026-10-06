import { InTypes } from '@/types/inbounds'
import { TrspTypes } from '@/types/transport'
import type { RuntimeCapability } from '@/types/runtimeCapabilities'

const hints: Readonly<Record<string, string>> = {
  [InTypes.VLESS]: 'inboundGuidance.vless',
  [InTypes.VMess]: 'inboundGuidance.vmess',
  [InTypes.Trojan]: 'inboundGuidance.trojan',
  [InTypes.Shadowsocks]: 'inboundGuidance.shadowsocks',
  [InTypes.SOCKS]: 'inboundGuidance.socks',
  [InTypes.HTTP]: 'inboundGuidance.http',
}
export function inboundGuidanceKeys(fact: RuntimeCapability | undefined, options: { hasTls: boolean; tlsSelected: boolean; transport?: string }): string[] {
  if (!fact?.available || !fact.known || !fact.contextSupported || fact.category !== 'inbounds' || !hints[fact.type]) return []
  const result = [hints[fact.type]]
  if (options.hasTls) result.push(options.tlsSelected ? 'inboundGuidance.tlsSelected' : 'inboundGuidance.tlsOptional')
  if (options.transport && Object.values(TrspTypes).includes(options.transport)) result.push('inboundGuidance.transport')
  return result
}
