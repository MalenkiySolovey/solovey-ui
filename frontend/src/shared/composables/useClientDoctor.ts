import HttpUtils, { type Msg } from '@/plugins/httputil'

export const diagnoseClient = (clientId: number, target: string): Promise<Msg> => HttpUtils.post('api/doctor/client', {
  data: JSON.stringify({ clientId, target }),
})
