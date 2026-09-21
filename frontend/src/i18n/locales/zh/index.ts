import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import sharedPool from './sharedPool'
import accountProtection from './accountProtection'
import codexTicketSettings from './codexTicketSettings'
import proxyGroups from './proxyGroups'
import accountProxyGroups from './accountProxyGroups'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
  sharedPool,
  accountProtection,
  codexTicketSettings,
  proxyGroups,
  accountProxyGroups,
}
