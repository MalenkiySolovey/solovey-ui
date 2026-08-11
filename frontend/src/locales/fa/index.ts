import common from './common'
import settings from './settings'
import routing from './routing'
import dns from './dns'
import clients from './clients'
import network from './network'
import security from './security'

export default {
  ...common,
  ...settings,
  ...routing,
  ...dns,
  ...clients,
  ...network,
  ...security,
}
