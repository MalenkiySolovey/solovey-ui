package local

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	remotesub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/remote"
	suburi "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
	"gorm.io/gorm"
)

type OutboundSet struct {
	Outbounds []map[string]interface{}
	Tags      []string
}

func (s *OutboundSet) Append(outbound map[string]interface{}, tag string) {
	if outbound == nil || tag == "" {
		return
	}
	s.Outbounds = append(s.Outbounds, outbound)
	s.Tags = append(s.Tags, tag)
}

func (s *OutboundSet) AppendMany(outbounds []map[string]interface{}, tags []string) {
	s.Outbounds = append(s.Outbounds, outbounds...)
	s.Tags = append(s.Tags, tags...)
}

func BuildInboundOutbounds(clientConfig json.RawMessage, inbounds []*model.Inbound) (*OutboundSet, error) {
	var configs map[string]interface{}
	if err := json.Unmarshal(clientConfig, &configs); err != nil {
		return nil, err
	}

	set := &OutboundSet{}
	for _, inbound := range inbounds {
		if inbound == nil || len(inbound.OutJson) < 5 {
			continue
		}
		outbound, err := inboundOutboundConfig(configs, inbound)
		if err != nil {
			return nil, err
		}
		addrs, err := inboundAddresses(inbound)
		if err != nil {
			return nil, err
		}
		appendInboundOutbounds(set, outbound, addrs)
	}
	return set, nil
}

func AppendExternalLinkOutbounds(set *OutboundSet, links []string) {
	if set == nil {
		return
	}
	tagNumEnable := 0
	if len(links) > 1 {
		tagNumEnable = 1
	}
	for index, link := range links {
		outbound, tag, err := suburi.Parse(link, (index+1)*tagNumEnable)
		if err == nil && outbound != nil && tag != "" {
			set.Append(*outbound, tag)
		}
	}
}

func AppendRemoteGroupOutbounds(db *gorm.DB, set *OutboundSet, rawLinks json.RawMessage) error {
	return AppendRemoteGroupOutboundsWithOptions(db, set, rawLinks, remotesub.ClientConversionOptions{})
}

func AppendRemoteGroupOutboundsWithOptions(db *gorm.DB, set *OutboundSet, rawLinks json.RawMessage, options remotesub.ClientConversionOptions) error {
	if set == nil {
		return nil
	}
	remoteOutbounds, remoteTags, err := remotesub.OutboundsForClientLinksWithOptions(db, rawLinks, options)
	if err != nil {
		return err
	}
	set.AppendMany(remoteOutbounds, remoteTags)
	return nil
}

func PrependDefaultJSONOutbounds(set *OutboundSet) {
	if set == nil {
		return
	}
	tags := append([]string(nil), set.Tags...)
	defaultOutbounds := []map[string]interface{}{
		{
			"outbounds": append([]string{"auto", "direct"}, tags...),
			"tag":       "proxy",
			"type":      "selector",
		},
		{
			"tag":       "auto",
			"type":      "urltest",
			"outbounds": tags,
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  "10m",
			"tolerance": 50,
		},
		{
			"type": "direct",
			"tag":  "direct",
		},
	}
	set.Outbounds = append(defaultOutbounds, set.Outbounds...)
}

func inboundOutboundConfig(configs map[string]interface{}, inbound *model.Inbound) (map[string]interface{}, error) {
	var outbound map[string]interface{}
	if err := json.Unmarshal(inbound.OutJson, &outbound); err != nil {
		return nil, err
	}
	protocol, _ := outbound["type"].(string)
	if protocol == "shadowsocks" {
		return shadowsocksOutboundConfig(configs, inbound, outbound)
	}
	mergeClientProtocolConfig(configs, inbound, outbound, protocol)
	return outbound, nil
}

func shadowsocksOutboundConfig(configs map[string]interface{}, inbound *model.Inbound, outbound map[string]interface{}) (map[string]interface{}, error) {
	var userPass []string
	var inboundOptions map[string]interface{}
	if err := json.Unmarshal(inbound.Options, &inboundOptions); err != nil {
		return nil, err
	}
	method, _ := inboundOptions["method"].(string)
	if strings.HasPrefix(method, "2022") {
		inboundPass, _ := inboundOptions["password"].(string)
		userPass = append(userPass, inboundPass)
	}
	var pass string
	if method == "2022-blake3-aes-128-gcm" {
		if config, ok := configs["shadowsocks16"].(map[string]interface{}); ok {
			pass, _ = config["password"].(string)
		}
	} else if config, ok := configs["shadowsocks"].(map[string]interface{}); ok {
		pass, _ = config["password"].(string)
	}
	userPass = append(userPass, pass)
	outbound["password"] = strings.Join(userPass, ":")
	return outbound, nil
}

func mergeClientProtocolConfig(configs map[string]interface{}, inbound *model.Inbound, outbound map[string]interface{}, protocol string) {
	stripFlow := false
	if protocol == "vless" {
		if inbound.TlsId == 0 {
			stripFlow = true
		} else if transport, ok := outbound["transport"].(map[string]interface{}); ok {
			if transportType, _ := transport["type"].(string); transportType != "" && transportType != "tcp" {
				stripFlow = true
			}
		}
	}
	config, _ := configs[protocol].(map[string]interface{})
	for key, value := range config {
		if key == "name" || key == "alterId" || (key == "flow" && (inbound.TlsId == 0 || stripFlow)) {
			continue
		}
		outbound[key] = value
	}
}

func inboundAddresses(inbound *model.Inbound) ([]map[string]interface{}, error) {
	var addrs []map[string]interface{}
	if err := json.Unmarshal(inbound.Addrs, &addrs); err != nil {
		return nil, err
	}
	return addrs, nil
}

func appendInboundOutbounds(set *OutboundSet, outbound map[string]interface{}, addrs []map[string]interface{}) {
	protocol, _ := outbound["type"].(string)
	tag, _ := outbound["tag"].(string)
	if len(addrs) == 0 {
		if protocol == "mixed" {
			appendMixedOutbound(set, outbound)
			return
		}
		set.Append(outbound, tag)
		return
	}
	for index, addr := range addrs {
		newOut := cloneOutbound(outbound)
		newOut["server"], _ = addr["server"].(string)
		port, _ := addr["server_port"].(float64)
		newOut["server_port"] = int(port)
		if addrTLS, ok := addr["tls"].(map[string]interface{}); ok {
			outTLS, _ := newOut["tls"].(map[string]interface{})
			if outTLS == nil {
				outTLS = make(map[string]interface{})
			}
			for key, value := range addrTLS {
				outTLS[key] = value
			}
			newOut["tls"] = outTLS
		}
		remark, _ := addr["remark"].(string)
		newTag := fmt.Sprintf("%d.%s%s", index+1, tag, remark)
		newOut["tag"] = newTag
		if protocol == "mixed" {
			appendMixedOutbound(set, newOut)
			continue
		}
		set.Append(newOut, newTag)
	}
}

func appendMixedOutbound(set *OutboundSet, outbound map[string]interface{}) {
	socksOut := cloneOutbound(outbound)
	httpOut := cloneOutbound(outbound)
	socksTag := fmt.Sprintf("%s-socks", outbound["tag"])
	httpTag := fmt.Sprintf("%s-http", outbound["tag"])
	socksOut["type"] = "socks"
	httpOut["type"] = "http"
	socksOut["tag"] = socksTag
	httpOut["tag"] = httpTag
	set.Append(socksOut, socksTag)
	set.Append(httpOut, httpTag)
}

func cloneOutbound(outbound map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(outbound))
	for key, value := range outbound {
		clone[key] = value
	}
	return clone
}
