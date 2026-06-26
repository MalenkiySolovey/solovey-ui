package canonical

func SnapshotOutbounds(snapshot Snapshot) []map[string]any {
	outbounds := make([]map[string]any, 0, len(snapshot.Connections))
	for _, connection := range snapshot.Connections {
		outbound := ConnectionOutbound(connection)
		if len(outbound) == 0 {
			continue
		}
		outbounds = append(outbounds, outbound)
	}
	return outbounds
}

func ConnectionOutbound(connection Connection) map[string]any {
	outbound := CleanOutbound(connection.BestOutbound)
	if len(outbound) == 0 {
		outbound = make(map[string]any)
	}
	if _, ok := outbound["type"]; !ok && connection.Protocol != "" {
		outbound["type"] = connection.Protocol
	}
	if _, ok := outbound["tag"]; !ok && connection.DisplayName != "" {
		outbound["tag"] = connection.DisplayName
	}
	if _, ok := outbound["server"]; !ok && connection.Endpoint.Server != "" {
		outbound["server"] = connection.Endpoint.Server
	}
	if _, ok := outbound["server_port"]; !ok && connection.Endpoint.Port != "" {
		outbound["server_port"] = connection.Endpoint.Port
	}
	return outbound
}
