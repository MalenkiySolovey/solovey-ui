import HttpUtils, { type Msg } from '@/plugins/httputil'

export const convertOutboundLink = (link: string): Promise<Msg> => {
  return HttpUtils.post('api/linkConvert', { link })
}

export const convertSubscriptionLink = (link: string): Promise<Msg> => {
  return HttpUtils.post('api/subConvert', { link })
}
