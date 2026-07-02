import HttpUtils, { type Msg } from '@/plugins/httputil'

export const loadRemoteOutboundSubscriptions = (): Promise<Msg> => HttpUtils.get('api/remote-outbound-subscriptions')
