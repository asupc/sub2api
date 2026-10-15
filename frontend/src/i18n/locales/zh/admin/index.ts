import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import modelManifest from './modelManifest'
import modelBenchmark from './modelBenchmark'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import audit from './audit'
import promptAudit from './promptAudit'
import plugins from './plugins'

export default {
  ...overview,
  ...channels,
  ...accounts,
  ...modelManifest,
  ...modelBenchmark,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
  ...promptAudit,
  ...plugins,
}
