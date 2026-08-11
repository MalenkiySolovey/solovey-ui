import common from './common'
import settings from './settings'
import routing from './routing'
import dns from './dns'
import clients from './clients'
import network from './network'
import security from './security'
import sshManagement from './sshManagement'
import deployment from './deployment'
import operations from './operations'

export default {
  ...common,
  ...settings,
  ...routing,
  ...dns,
  ...clients,
  ...network,
  ...security,
  ...sshManagement,
  ...deployment,
  ...operations,
}
