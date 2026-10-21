package canonical

func SnapshotOutbounds(snapshot Snapshot) []map[string]any {
	outbounds := make([]map[string]any, 0, len(snapshot.Connections))
	for _, connection := range snapshot.Connections {
		outbound := ConnectionOutbound(connection)
		if len(outbound) > 0 {
			outbounds = append(outbounds, outbound)
		}
	}
	return outbounds
}

// ConnectionOutbound is a read projection for existing snapshot storage.
// Activation/export must use CheckedEndpointOutbound before owner validation.
func ConnectionOutbound(connection Connection) map[string]any {
	return EndpointOutbound(NormalizeEndpoint(connection))
}
