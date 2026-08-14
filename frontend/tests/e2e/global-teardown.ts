import {
  cleanupManagedServerRuntime,
  readManagedServerPid,
  stopManagedServer,
} from './server-lifecycle'

export default function globalTeardown() {
  stopManagedServer(readManagedServerPid())
  cleanupManagedServerRuntime(true)
}
