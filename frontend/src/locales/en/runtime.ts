export default {
  runtime: {
    sessions: 'Live sessions', flows: 'Flows', observed: 'Observed at', limited: 'Limited to',
    inboundOutbound: 'Inbound / outbound', addresses: 'Source / destination', trafficAge: 'Traffic / age',
    closeFlow: 'Close flow', closeObserved: 'Close observed flows',
    parentLimit: 'These are active flows. Authenticated parent transports may remain open; future reconnection is allowed.',
    unknownIdentity: 'Some runtime flows have no verified client identity and are excluded from this client view.',
    confirmClose: 'Close the currently observed flows? This does not block reconnection or guarantee termination of an authenticated parent transport.',
    states: { limited: 'Snapshot exceeds runtime limits; narrow the view or reduce active flows', starting: 'Core is starting', stopping: 'Core is stopping', failed: 'Core startup failed', loading: 'Loading sessions…', active: 'Core running — active flows', empty: 'Core running — no observed flows', stopped: 'Core stopped', maintenance: 'Core held in maintenance', unavailable: 'Runtime API unavailable', stale: 'Snapshot is stale — refresh before acting', unauthorized: 'Session access is unauthorized', identity_unavailable: 'Client identity unavailable for some runtime flows' },
    outcomes: { FLOW_CLOSED: 'Flow closed', PARTIAL_DISCONNECT: 'Observed flows processed; full parent disconnection is not guaranteed', ALREADY_GONE: 'No matching active flows observed', STALE_GENERATION: 'Core generation changed; action rejected', NOT_SUPPORTED: 'Operation is not supported within runtime limits', ERROR: 'Runtime action failed' },
  },
}
