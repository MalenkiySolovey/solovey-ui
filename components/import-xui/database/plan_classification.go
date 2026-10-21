//go:build !minimal

package importxui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/mapping"
	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
)

func classifyPlan(ctx context.Context, src *source.Database, plan *MigrationPlan) error {
	columns, err := src.MappingColumns()
	if err != nil {
		return err
	}
	known := map[string]string{
		"inbounds":          "id user_id up down total all_time remark enable expiry_time traffic_reset last_traffic_reset_time listen port protocol settings stream_settings tag sniffing allocate node_id created_at updated_at",
		"client_traffics":   "id inbound_id enable email up down all_time expiry_time total reset last_online node_id created_at updated_at",
		"outbound_traffics": "id tag up down total all_time",
		"settings":          "id key value",
		"users":             "id username password",
	}
	unknown := map[string]bool{}
	knownColumns := map[string]map[string]bool{}
	for table, names := range known {
		knownColumns[table] = map[string]bool{}
		for _, name := range strings.Fields(names) {
			knownColumns[table][name] = true
		}
	}
	for table, fields := range columns {
		for _, field := range fields {
			if !knownColumns[table][field] {
				unknown[table] = true
			}
		}
	}
	tableFields := func(table string) []mapping.FieldClassification {
		fields := []mapping.FieldClassification{}
		for _, column := range columns[table] {
			state, reason := mapping.ConvertWithDiagnostic, "canonical_owner_mapping"
			if !knownColumns[table][column] {
				state, reason = mapping.UnsupportedBlocked, "unmapped_source_column"
			}
			switch column {
			case "node_id", "created_at", "updated_at", "user_id", "sniffing":
				state, reason = mapping.IgnoredExplicitly, "outside_selected_import_scope"
			case "allocate":
				state, reason = mapping.ExactSupported, "default_allocation_only"
			}
			path := table + "." + column
			if state == mapping.UnsupportedBlocked {
				path = table + ".unmapped_column"
			}
			fields = append(fields, mapping.FieldClassification{Path: path, State: state, ReasonCode: reason})
		}
		return fields
	}
	byInbound := map[string][]mapping.FieldClassification{}
	unavailable := map[string]bool{}
	for _, item := range plan.Items {
		if (item.Kind == KindInbound || item.Kind == KindEndpoint) && len(item.Unsupported) > 0 {
			unavailable[fmt.Sprint(item.SrcID)] = true
		}
	}
	byTLS := map[string][]mapping.FieldClassification{}
	mergeFields := func(current, added []mapping.FieldClassification) []mapping.FieldClassification {
		seen := map[mapping.FieldClassification]bool{}
		result := make([]mapping.FieldClassification, 0, 256)
		for _, field := range append(append([]mapping.FieldClassification(nil), current...), added...) {
			if seen[field] {
				continue
			}
			if len(result) == 256 {
				return append(result, mapping.FieldClassification{Path: "tls.source", State: mapping.ManualMappingRequired, ReasonCode: "source_field_complexity_limit"})
			}
			seen[field] = true
			result = append(result, field)
		}
		return result
	}
	tlsForInbound := map[string]string{}
	blockedInbound := map[int64]bool{}
	inboundNames := map[int64][]string{}
	blockedClients := map[string]bool{}
	if err := src.EachInbound(func(row source.InboundRow) error {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if unavailable[fmt.Sprint(row.ID)] {
			byInbound[fmt.Sprint(row.ID)] = []mapping.FieldClassification{{Path: "inbound.capability", State: mapping.UnsupportedBlocked, ReasonCode: "destination_capability_unavailable"}}
			return nil
		}
		fields := append(tableFields("inbounds"), mapping.ClassifyInbound(row)...)
		if len(row.Allocate) > 0 && string(row.Allocate) != "null" && string(row.Allocate) != "{}" {
			fields = append(fields, mapping.FieldClassification{Path: "inbounds.allocate", State: mapping.ManualMappingRequired, ReasonCode: "source_allocation_requires_mapping"})
		}
		if unknown["inbounds"] {
			fields = append(fields, mapping.FieldClassification{Path: "inbounds.unmapped_column", State: mapping.UnsupportedBlocked, ReasonCode: "unmapped_source_column"})
		}
		key := fmt.Sprint(row.ID)
		byInbound[key] = fields
		inboundNames[row.ID] = mapping.InboundClientNames(row)
		var tlsKey string
		reality, _, err := mapping.ExtractReality(row)
		if err != nil {
			return err
		}
		if reality != nil {
			tlsKey = reality.Key
		} else {
			plain, _, err := mapping.ExtractPlainTLS(row)
			if err != nil {
				return err
			}
			if plain != nil {
				tlsKey = plain.Key
			}
		}
		if tlsKey != "" {
			tlsForInbound[key] = tlsKey
			byTLS[tlsKey] = mergeFields(byTLS[tlsKey], fields)
		}
		blockedInbound[row.ID] = mapping.Blocking(fields)
		return nil
	}); err != nil {
		return err
	}
	dependency := mapping.FieldClassification{Path: "source.dependencies", State: mapping.ManualMappingRequired, ReasonCode: "source_dependency_requires_mapping"}
	for id, names := range inboundNames {
		key := fmt.Sprint(id)
		if mapping.Blocking(byTLS[tlsForInbound[key]]) {
			blockedInbound[id] = true
			byInbound[key] = append(byInbound[key], dependency)
		}
		if blockedInbound[id] {
			for _, name := range names {
				blockedClients[name] = true
			}
		}
	}
	if err := src.EachClientTraffic(func(row source.ClientTraffic) error {
		if blockedInbound[row.InboundID] {
			blockedClients[row.Email] = true
		}
		return checkContext(ctx)
	}); err != nil {
		return err
	}
	for index := range plan.Items {
		item := &plan.Items[index]
		item.Classification = mapping.ConvertWithDiagnostic
		if item.Action == ActionSkip && string(item.PreviewJSON) == "null" {
			item.Classification = mapping.IgnoredExplicitly
		}
		if len(item.Unsupported) > 0 {
			item.Classification = mapping.UnsupportedBlocked
		}
		switch item.Kind {
		case KindTLS:
			item.Fields = append(item.Fields, byTLS[fmt.Sprint(item.SrcID)]...)
		case KindInbound, KindEndpoint:
			item.Fields = append(item.Fields, byInbound[fmt.Sprint(item.SrcID)]...)
		case KindClient:
			item.Fields = append(item.Fields, tableFields("client_traffics")...)
			if unknown["client_traffics"] || blockedClients[item.SrcTag] {
				item.Fields = append(item.Fields, mapping.FieldClassification{Path: "client.source", State: mapping.ManualMappingRequired, ReasonCode: "client_source_requires_mapping"})
			}
		case KindAdmin:
			item.Fields = append(item.Fields, tableFields("users")...)
			if unknown["users"] {
				item.Fields = append(item.Fields, mapping.FieldClassification{Path: "users.unmapped_column", State: mapping.UnsupportedBlocked, ReasonCode: "unmapped_source_column"})
			}
		case KindSetting:
			item.Fields = append(item.Fields, tableFields("settings")...)
			if unknown["settings"] {
				item.Fields = append(item.Fields, mapping.FieldClassification{Path: "settings.unmapped_column", State: mapping.UnsupportedBlocked, ReasonCode: "unmapped_source_column"})
			}
		case KindHistory:
			item.Fields = append(item.Fields, tableFields("client_traffics")...)
			item.Fields = append(item.Fields, tableFields("outbound_traffics")...)
			if unknown["client_traffics"] || unknown["outbound_traffics"] {
				item.Fields = append(item.Fields, mapping.FieldClassification{Path: "traffic.unmapped_column", State: mapping.UnsupportedBlocked, ReasonCode: "unmapped_source_column"})
			}
		case KindRouting:
			raw, err := src.XrayConfig()
			if err != nil {
				return err
			}
			item.Fields = mapping.ClassifyXrayConfig(raw)
			// Partial rule/transport/security mappings must not activate a
			// broadened policy. Existing exact mappings remain available.
			if len(item.Warnings) > 0 && item.Action != ActionSkip {
				item.Fields = append(item.Fields, mapping.FieldClassification{Path: "xrayConfig", State: mapping.ManualMappingRequired, ReasonCode: "partial_routing_mapping"})
			}
			for _, blocked := range blockedInbound {
				if blocked {
					item.Fields = append(item.Fields, dependency)
					break
				}
			}
		}
		if mapping.Blocking(item.Fields) {
			item.Classification = mapping.UnsupportedBlocked
			item.Action = ActionSkip
			item.PreviewJSON = json.RawMessage(`null`)
			item.Warnings = append(item.Warnings, "Unknown or unsupported source semantics require manual mapping; this item is blocked.")
		}
	}
	return nil
}
