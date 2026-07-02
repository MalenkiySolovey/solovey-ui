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
