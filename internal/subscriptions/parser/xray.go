package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type xrayTagAliases struct {
	Original  string
	Generated []string
}

func ParseXrayOutbounds(data string) ([]map[string]interface{}, error) {
	return ParseXrayOutboundsWithOptions(data, ParseOptions{})
}
func ParseXrayOutboundsWithOptions(data string, options ParseOptions) ([]map[string]interface{}, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, common.NewError("no result")
	}
	configs, err := parseXrayConfigs(data)
	if err != nil {
		return nil, err
	}
	topLevel := make([]map[string]interface{}, 0)
	dependencies := make([]map[string]interface{}, 0)
	multiConfig := len(configs) > 1
	for configIndex, config := range configs {
		for _, outbound := range parseXrayConfigOutbounds(config, options, configIndex, multiConfig) {
			if boolValue(outbound["xray_profile_member"]) {
				dependencies = append(dependencies, outbound)
				continue
			}
			topLevel = append(topLevel, outbound)
		}
	}
	outbounds := append(topLevel, dependencies...)
	if len(outbounds) == 0 {
		return nil, common.NewError("no result")
	}
	return outbounds, nil
}
func parseXrayConfigs(data string) ([]map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	configs := make([]map[string]interface{}, 0, 1)
	for {
		var value interface{}
		if err := decoder.Decode(&value); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, common.NewError("not an xray json subscription: ", err)
		}
		switch typed := value.(type) {
		case map[string]interface{}:
			configs = append(configs, typed)
		case []interface{}:
			for _, item := range typed {
				config, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				configs = append(configs, config)
			}
		default:
			return nil, common.NewError("not an xray json subscription")
		}
	}
	if len(configs) == 0 {
		return nil, common.NewError("not an xray json subscription")
	}
	return configs, nil
}
func parseXrayConfigOutbounds(config map[string]interface{}, options ParseOptions, configIndex int, multiConfig bool) []map[string]interface{} {
	rawOutbounds := xrayList(config["outbounds"])
	if len(rawOutbounds) == 0 {
		return nil
	}
	members, aliases := xrayProfileMemberOutbounds(rawOutbounds)
	if len(members) == 0 {
		return nil
	}
	if !xrayConfigHasBalancers(config) {
		profile := xrayConfigProfileOutbound(config, members, nil, options, configIndex, multiConfig)
		if len(profile) == 0 {
			return nil
		}
		return []map[string]interface{}{profile}
	}
	scopedMembers, scopedAliases := xrayScopedProfileMembers(xrayDependencyScope(config, configIndex, multiConfig), members, aliases)
	groups := xrayBalancerOutbounds(config, scopedAliases, scopedMembers, options, configIndex, multiConfig)
	if len(groups) == 0 {
		profile := xrayConfigProfileOutbound(config, members, nil, options, configIndex, multiConfig)
		if len(profile) == 0 {
			return nil
		}
		return []map[string]interface{}{profile}
	}
	profile := xrayBalancerProfileOutbound(config, groups, scopedAliases, options, configIndex, multiConfig)
	if len(profile) == 0 {
		return append(groups, scopedMembers...)
	}
	return append([]map[string]interface{}{profile}, scopedMembers...)
}
func xrayDependencyScope(config map[string]interface{}, configIndex int, multiConfig bool) string {
	if remarks := strings.TrimSpace(stringValue(config["remarks"])); remarks != "" {
		return remarks
	}
	if multiConfig {
		return fmt.Sprintf("xray-config-%d", configIndex+1)
	}
	return ""
}
func xrayConfigScope(config map[string]interface{}, configIndex int, multiConfig bool) string {
	if !multiConfig {
		return ""
	}
	if remarks := strings.TrimSpace(stringValue(config["remarks"])); remarks != "" {
		return remarks
	}
	return fmt.Sprintf("xray-config-%d", configIndex+1)
}
