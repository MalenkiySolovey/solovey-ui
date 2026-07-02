import {
  pickSettingsByDefaults,
  type SettingsMap,
} from '@/views/settingsPayload'

export type PaidSubSettingsMap = SettingsMap

export const paidSubSettingsDefaults: SettingsMap = {
  paidSubEnabled: 'false',
  paidSubBotToken: '',
  paidSubBotTokenHasSecret: 'false',
  paidSubBotPollSeconds: '25',
  paidSubTransportMode: 'proxy',
  paidSubProxyURL: '',
  paidSubProxyURLHasSecret: 'false',
  paidSubProxyUsername: '',
  paidSubProxyUsernameHasSecret: 'false',
  paidSubProxyPassword: '',
  paidSubProxyPasswordHasSecret: 'false',
  paidSubOutboundTag: '',
  paidSubAutoRegister: 'false',
  paidSubAutoInbounds: '[]',
  paidSubTrialDays: '3',
  paidSubTrialVolumeGB: '0',
  paidSubMaxClients: '5000',
  paidSubStartRateLimitPerMin: '3',
  paidSubCurrency: 'RUB',
  paidSubStarsEnabled: 'false',
  paidSubYooKassaEnabled: 'false',
  paidSubYooKassaToken: '',
  paidSubYooKassaTokenHasSecret: 'false',
  paidSubStripeEnabled: 'false',
  paidSubStripeToken: '',
  paidSubStripeTokenHasSecret: 'false',
  paidSubPayMasterEnabled: 'false',
  paidSubPayMasterToken: '',
  paidSubPayMasterTokenHasSecret: 'false',
  paidSubCryptoBotEnabled: 'false',
  paidSubCryptoBotToken: '',
  paidSubCryptoBotTokenHasSecret: 'false',
  paidSubExternalEnabled: 'false',
  paidSubExternalUrlTemplate: '',
  paidSubOrderTTLMinutes: '30',
  paidSubGreeting: '',
  paidSubRefundRevoke: 'true',
}

export const paidSubSettingKeys = Object.keys(paidSubSettingsDefaults).filter(key => !key.endsWith('HasSecret'))

export const paidSubSecretSettingKeys = [
  'paidSubBotToken',
  'paidSubProxyURL',
  'paidSubProxyUsername',
  'paidSubProxyPassword',
  'paidSubYooKassaToken',
  'paidSubStripeToken',
  'paidSubPayMasterToken',
  'paidSubCryptoBotToken',
]

export const pickPaidSubSettings = (source: SettingsMap): SettingsMap => {
  return pickSettingsByDefaults(paidSubSettingsDefaults, source)
}
