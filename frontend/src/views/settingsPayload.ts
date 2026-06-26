export type SettingsMap = Record<string, string>

export const settingsPageDefaults: SettingsMap = {
  webListen: '',
  webDomain: '',
  webPort: '2095',
  webCertFile: '',
  webKeyFile: '',
  webPath: '/app/',
  webURI: '',
  sessionMaxAge: '0',
  trafficAge: '30',
  timeLocation: 'Europe/Moscow',
  subListen: '',
  subPort: '2096',
  subPath: '/sub/',
  subDomain: '',
  subCertFile: '',
  subKeyFile: '',
  subUpdates: '12',
  subEncode: 'true',
  subShowInfo: 'false',
  subSecretRequired: 'false',
  subRateLimitPerIP: '60',
  subLinkEnable: 'true',
  subJsonEnable: 'true',
  subClashEnable: 'true',
  subXrayEnable: 'true',
  subRemoteGroupAdaptation: 'urltest',
  subRemoteConversionPolicy: '{"outbound":{"mihomoFallback":"urltest","mihomoLoadBalance":"urltest","mihomoRelay":"selector","mihomoSmart":"urltest","mihomoSsid":"selector","xrayBalancer":"urltest"},"client":{"singBox":{"mihomoFallback":"urltest","mihomoLoadBalance":"urltest","mihomoRelay":"selector","mihomoSmart":"urltest","mihomoSsid":"selector","xrayBalancer":"urltest"},"xray":{"mihomoFallback":"balancer","mihomoLoadBalance":"balancer","mihomoRelay":"balancer","mihomoSmart":"balancer","mihomoSsid":"balancer","xrayBalancer":"original"},"mihomo":{"mihomoFallback":"original","mihomoLoadBalance":"original","mihomoRelay":"original","mihomoSmart":"original","mihomoSsid":"original","xrayBalancer":"url-test"}}}',
  subURI: '',
  subJsonPath: '/json/',
  subClashPath: '/clash/',
  subXrayPath: '/xray/',
  subJsonURI: '',
  subClashURI: '',
  subXrayURI: '',
  subTitle: '',
  subSupportUrl: '',
  subProfileUrl: '',
  subAnnounce: '',
  subNameInRemark: 'false',
  subJsonExt: '',
  subClashExt: '',
}

export const telegramSettingsDefaults: SettingsMap = {
  telegramEnabled: 'false',
  telegramBotToken: '',
  telegramBotTokenHasSecret: 'false',
  telegramChatID: '',
  telegramProxyURL: '',
  telegramProxyURLHasSecret: 'false',
  telegramProxyUsername: '',
  telegramProxyUsernameHasSecret: 'false',
  telegramProxyPassword: '',
  telegramProxyPasswordHasSecret: 'false',
  telegramTransportMode: 'proxy',
  telegramOutboundTag: '',
  telegramCpuThreshold: '90',
  telegramNotifyCpu: 'false',
  telegramReport: 'false',
  telegramReportCron: '',
  telegramBackupEnabled: 'false',
  telegramBackupPassphrase: '',
  telegramBackupPassphraseHasSecret: 'false',
  telegramBackupCron: '',
  telegramBackupExcludeTables: 'stats,client_ips,audit_events,changes',
  telegramBackupMaxSizeMB: '45',
}

export const telegramSettingKeys = [
  'telegramEnabled',
  'telegramBotToken',
  'telegramChatID',
  'telegramProxyURL',
  'telegramProxyUsername',
  'telegramProxyPassword',
  'telegramTransportMode',
  'telegramOutboundTag',
  'telegramCpuThreshold',
  'telegramNotifyCpu',
  'telegramReport',
  'telegramReportCron',
  'telegramBackupEnabled',
  'telegramBackupPassphrase',
  'telegramBackupCron',
  'telegramBackupExcludeTables',
  'telegramBackupMaxSizeMB',
]

export const telegramSecretSettingKeys = [
  'telegramBotToken',
  'telegramProxyURL',
  'telegramProxyUsername',
  'telegramProxyPassword',
  'telegramBackupPassphrase',
]

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

export const pickSettingsByDefaults = (defaults: SettingsMap, source: SettingsMap): SettingsMap => {
  const picked: SettingsMap = {}
  for (const key of Object.keys(defaults)) {
    picked[key] = source[key] !== undefined ? String(source[key]) : defaults[key]
  }
  return picked
}

export const pickSecretAwareSettings = (
  settingKeys: string[],
  secretKeys: string[],
  source: SettingsMap,
): SettingsMap => {
  const picked: SettingsMap = {}
  for (const key of settingKeys) {
    picked[key] = String(source[key] ?? '')
  }
  for (const key of secretKeys) {
    picked[key + 'HasSecret'] = String(source[key + 'HasSecret'] ?? 'false')
  }
  return picked
}

export const pickPaidSubSettings = (source: SettingsMap): SettingsMap => {
  return pickSettingsByDefaults(paidSubSettingsDefaults, source)
}
