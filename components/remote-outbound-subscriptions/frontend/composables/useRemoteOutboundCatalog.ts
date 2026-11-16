import { HttpUtils, type Msg } from '@solovey/component-ui/subscription-sources'

export const loadRemoteOutboundSubscriptions = (): Promise<Msg> => HttpUtils.get('api/remote-outbound-subscriptions')
