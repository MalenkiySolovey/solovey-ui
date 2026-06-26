import common from './common'
import settings from './settings'
import routing from './routing'
import dns from './dns'
import clients from './clients'
import subscription from './subscription'
import telegram from './telegram'
import paidsub from './paidsub'
import diagnostics from './diagnostics'
import network from './network'
import migration from './migration'

export default {
  ...common,
  ...settings,
  ...routing,
  ...dns,
  ...clients,
  ...subscription,
  ...telegram,
  ...paidsub,
  ...diagnostics,
  ...network,
  ...migration,
}
