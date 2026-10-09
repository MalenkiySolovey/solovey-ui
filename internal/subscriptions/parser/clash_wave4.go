package parser

import (
	"errors"
	"math"
	"strconv"
	"time"

	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
)

func adaptClashWave4(outbound, proxy map[string]any) error {
	if outbound["type"] == "shadowsocks" && stringValue(proxy["plugin"]) != "" {
		return errors.New("Clash SIP002 plugin metadata requires a URI or JSON profile to preserve its ordered options")
	}
	if server, ok := outbound["server"].(string); ok && server != "" {
		host, err := codec.NormalizeHost(server)
		if err != nil {
			return err
		}
		outbound["server"] = host
	}
	if transport, ok := outbound["transport"].(map[string]any); ok && transport["type"] == "ws" {
		opts, _ := proxy["ws-opts"].(map[string]any)
		if value, present := opts["max-early-data"]; present {
			max, err := codec.WebSocketEarlyData(map[string]any{"max_early_data": value})
			if err != nil {
				return err
			}
			transport["max_early_data"] = max
		}
	}
	if ech, ok := proxy["ech-opts"].(map[string]any); ok && boolValue(ech["enable"]) {
		config, err := entitytls.PublicECHConfig(ech["config"])
		if err != nil {
			return err
		}
		tls, _ := outbound["tls"].(map[string]any)
		if tls == nil {
			tls = map[string]any{}
			outbound["tls"] = tls
		}
		tls["enabled"] = true
		tls["ech"] = map[string]any{"enabled": true, "config": []string{config}}
	}
	if typ, _ := outbound["type"].(string); typ == "hysteria" || typ == "hysteria2" {
		if value, present := proxy["ports"]; present {
			text, ok := value.(string)
			if !ok {
				if port, ok := value.(int); ok {
					text = strconv.Itoa(port)
				} else {
					return errors.New("invalid Clash Hysteria ports")
				}
			}
			ports, err := canonical.ParseHysteriaSharePorts(text)
			if err != nil {
				return err
			}
			if len(ports) > 0 {
				outbound["server_ports"] = ports
			}
		}
		if value, present := proxy["hop-interval"]; present {
			var seconds int64
			switch number := value.(type) {
			case int:
				seconds = int64(number)
			case float64:
				if math.Trunc(number) != number || number < 0 || number >= float64(math.MaxInt64/int64(time.Second)) {
					return errors.New("Clash Hysteria hop-interval cannot be represented as a fixed runtime duration")
				}
				seconds = int64(number)
			default:
				return errors.New("Clash Hysteria hop-interval cannot be represented as a fixed runtime duration")
			}
			if seconds < 0 || seconds > math.MaxInt64/int64(time.Second) {
				return errors.New("invalid Clash Hysteria hop-interval")
			}
			outbound["hop_interval"] = (time.Duration(seconds) * time.Second).String()
		}
	}
	return nil
}
