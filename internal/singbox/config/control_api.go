package singboxconfig

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

type controlAPIConfig struct {
	Experimental struct {
		ClashAPI *struct {
			ExternalController string `json:"external_controller"`
		} `json:"clash_api"`
		V2RayAPI *struct {
			Listen string `json:"listen"`
		} `json:"v2ray_api"`
	} `json:"experimental"`
}

func validateControlAPIListeners(config json.RawMessage) error {
	var doc controlAPIConfig
	if err := json.Unmarshal(config, &doc); err != nil {
		return fmt.Errorf("config must be a JSON object: %w", err)
	}
	if api := doc.Experimental.ClashAPI; api != nil {
		if err := validateLoopbackListener("config.experimental.clash_api.external_controller", api.ExternalController); err != nil {
			return err
		}
	}
	if api := doc.Experimental.V2RayAPI; api != nil {
		if err := validateLoopbackListener("config.experimental.v2ray_api.listen", api.Listen); err != nil {
			return err
		}
	}
	return nil
}

func validateLoopbackListener(field string, listen string) error {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("%s must be host:port on localhost", field)
	}
	if port == "" {
		return fmt.Errorf("%s must include a port", field)
	}
	num, err := strconv.Atoi(port)
	if err != nil || num <= 0 || num > 65535 {
		return fmt.Errorf("%s has invalid port", field)
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return fmt.Errorf("%s must not listen on all interfaces", field)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%s must use localhost, 127.0.0.1, or ::1", field)
	}
	if !addr.Unmap().IsLoopback() {
		return fmt.Errorf("%s must listen only on loopback", field)
	}
	return nil
}
