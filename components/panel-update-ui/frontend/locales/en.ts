export default { panelUpdateComponent: {
  lifecycleResult: 'Start / stop result', lifecycle: { STARTED: 'Start completed', START_FAILED: 'Start failed', STOPPED: 'Stop completed', STOP_FAILED: 'Stop failed', NOT_OBSERVED: 'No result observed', BINARY_ABSENT: 'Code absent from this build', NOT_INSTALLED: 'Not installed' },
  panelVersion: 'Panel version', componentCatalog: 'Component catalog', binaryProfile: 'Binary profile', unknown: 'unknown', authority: 'Authority', runningBinary: 'running binary',
  installedComponents: 'Installed components', noneInstalled: 'No installed components.', enabled: 'Enabled', disabled: 'Disabled', locked: 'Locked', enable: 'Enable', disable: 'Disable', remove: 'Remove',
  availableForBuild: 'Available for this build', noneAvailable: 'No bundled components are waiting for install.', install: 'Install', unavailable: 'Unavailable', unavailableForProfile: 'Not available in this profile', noneUnavailable: 'No unavailable components detected.', notBundled: 'Not bundled', requiresNewerPanel: 'Requires newer panel',
  removeTitle: 'Remove component', removeWarning: 'The component footprint will be removed from this panel profile. Database data stays intact.', removeConfirm: 'Confirm removing {name}. Component-owned data remains available to the separate Drop Data workflow.', password: 'Password',
  current: 'current', latest: 'latest', requiresPanel: 'requires panel', incompatible: 'incompatible', releaseOnly: 'release-only',
} }
