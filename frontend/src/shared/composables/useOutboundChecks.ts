import HttpUtils, { type Msg } from '@/plugins/httputil'

export const checkOutboundConnection = (tag: string): Promise<Msg> => HttpUtils.get('api/checkOutbound', { tag })
