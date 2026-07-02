import { describe, expect, it } from 'vitest'
import {
  pickSecretAwareSettings,
  pickSettingsByDefaults,
  settingsPageDefaults,
} from './settingsPayload'

describe('settings payload defaults', () => {
  it('keeps Settings page defaults in one shared map', () => {
    expect(settingsPageDefaults.webPort).toBe('2095')
    expect(settingsPageDefaults.timeLocation).toBe('Europe/Moscow')
    expect(settingsPageDefaults.subJsonPath).toBe('/json/')
    expect(settingsPageDefaults.subClashPath).toBe('/clash/')
    expect(settingsPageDefaults.subXrayPath).toBe('/xray/')
    expect(settingsPageDefaults).not.toHaveProperty('subRemoteGroupAdaptation')
  })

  it('picks only keys from the selected defaults map', () => {
    const picked = pickSettingsByDefaults(settingsPageDefaults, {
      webPort: '3000',
      subPath: '/users/',
      unknown: 'value',
    })

    expect(picked.webPort).toBe('3000')
    expect(picked.subPath).toBe('/users/')
    expect(picked.webPath).toBe('/app/')
    expect(picked).not.toHaveProperty('unknown')
  })

  it('keeps secret markers only for declared secret fields', () => {
    const picked = pickSecretAwareSettings(['apiToken', 'cron'], ['apiToken'], {
      apiToken: '',
      apiTokenHasSecret: 'true',
      cron: '* * * * *',
      cronHasSecret: 'true',
    })

    expect(picked.apiTokenHasSecret).toBe('true')
    expect(picked.cron).toBe('* * * * *')
    expect(picked).not.toHaveProperty('cronHasSecret')
  })
})
