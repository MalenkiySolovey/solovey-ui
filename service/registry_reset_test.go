package service

func resetConfigSaveObserversForTest() {
	configSaveObservers.Lock()
	configSaveObservers.entries = map[string]registeredConfigSaveObserver{}
	configSaveObservers.Unlock()
}

func ResetOutboundSaveHooksForTest() {
	outboundSaveHooks.Lock()
	outboundSaveHooks.entries = map[string]registeredOutboundSaveHook{}
	outboundSaveHooks.Unlock()
}

func ResetAPITokenScopeProvidersForTest() {
	apiTokenScopeProviders.Lock()
	apiTokenScopeProviders.list = nil
	apiTokenScopeProviders.Unlock()
}
