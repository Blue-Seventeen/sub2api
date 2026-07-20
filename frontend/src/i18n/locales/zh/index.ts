import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import carryover from './carryover'
import { mergeLocaleMessages } from '../merge'

export default mergeLocaleMessages(carryover, {
  ...landing,
  ...common,
  ...dashboard,
  ...batchImage,
  admin,
  ...misc,
})
