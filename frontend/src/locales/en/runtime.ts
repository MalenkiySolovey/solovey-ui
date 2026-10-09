export default {
  runtime: {
    controls: 'Core runtime', actual: 'Actual state', maintenance: 'Maintenance', hold: 'Hold core for maintenance', resume: 'Resume core',
    confirmHold: 'Stop the core for all clients and keep it stopped across restarts and configuration saves? The panel stays available.',
    temporarySelection: 'Group choices affect the current runtime. Persistent configuration remains owned by the editor; the core may remember a choice in its own runtime cache.',
    noGroups: 'No runtime outbound groups', select: 'Select for runtime', probe: 'Test this member',
    logLimit: 'Current-generation logs: up to 30 seconds and 400 displayed lines. Start again explicitly after the stream closes.',
    watchLogs: 'Watch core logs', stopLogs: 'Stop stream', stream: { open: 'Streaming', loading: 'Connecting…', closed: 'Stream closed', unavailable: 'Stream unavailable', unauthorized: 'Unauthorized' },
    controlResults: { RUNTIME_SELECTED: 'Runtime selection applied', PROBE_COMPLETED: 'Member probe completed', maintenance_enabled: 'Maintenance hold saved', maintenance_disabled: 'Core resumed', stale_generation: 'Core generation changed; refresh before acting', maintenance_unavailable: 'Maintenance state unavailable; startup is blocked', runtime_api_unavailable: 'Runtime API unavailable', core_unavailable: 'Core unavailable', runtime_limit_exceeded: 'Runtime operation limit reached', member_not_in_group: 'Member is no longer in this group', outbound_check_timeout: 'Probe timed out', outbound_check_canceled: 'Probe canceled', outbound_check_network_failed: 'Probe network failure', outbound_check_failed: 'Probe failed', unauthorized: 'Unauthorized', unavailable: 'Runtime unavailable', ERROR: 'Runtime action failed' },
    sessions: 'Live sessions', flows: 'Flows', observed: 'Observed at', limited: 'Limited to',
    inboundOutbound: 'Inbound / outbound', addresses: 'Source / destination', trafficAge: 'Traffic / age',
    closeFlow: 'Close flow', closeObserved: 'Close observed flows',
    parentLimit: 'These are active flows. Authenticated parent transports may remain open; future reconnection is allowed.',
    unknownIdentity: 'Some runtime flows have no verified client identity and are excluded from this client view.',
    confirmClose: 'Close the currently observed flows? This does not block reconnection or guarantee termination of an authenticated parent transport.',
    states: { running: 'Core running', limited: 'Snapshot exceeds runtime limits; narrow the view or reduce active flows', starting: 'Core is starting', stopping: 'Core is stopping', failed: 'Core startup failed', loading: 'Loading sessions…', active: 'Core running — active flows', empty: 'Core running — no observed flows', stopped: 'Core stopped', maintenance: 'Core held in maintenance', unavailable: 'Runtime API unavailable', stale: 'Snapshot is stale — refresh before acting', unauthorized: 'Session access is unauthorized', identity_unavailable: 'Client identity unavailable for some runtime flows' },
    outcomes: { FLOW_CLOSED: 'Flow closed', PARTIAL_DISCONNECT: 'Observed flows processed; full parent disconnection is not guaranteed', ALREADY_GONE: 'No matching active flows observed', STALE_GENERATION: 'Core generation changed; action rejected', NOT_SUPPORTED: 'Operation is not supported within runtime limits', ERROR: 'Runtime action failed' },
  },
}
