import { readManagedServerPid, stopManagedServer } from './server-lifecycle'

export default function globalTeardown() {
  stopManagedServer(readManagedServerPid())
}
