package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"time"
)

func (s *WarpService) RegisterWarp(ep *model.Endpoint) error {
	tos := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	privateKey, err := wgtypes.GenerateKey()
	if err != nil {
		return fmt.Errorf("generate WARP private key: %w", err)
	}
	publicKey := privateKey.PublicKey().String()
	hostName, _ := os.Hostname()
	dataBytes, err := json.Marshal(map[string]string{
		"key":    publicKey,
		"tos":    tos,
		"type":   "PC",
		"model":  "s-ui",
		"name":   hostName,
		"locale": "en_US",
	})
	if err != nil {
		return err
	}
	resp, version, err := doWarpRequestVersions(func(version string) (*http.Request, []byte, error) {
		url := fmt.Sprintf("https://api.cloudflareclient.com/%s/reg", version)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
		if err != nil {
			return nil, nil, err
		}
		return req, dataBytes, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var rspData map[string]interface{}
	if err := json.Unmarshal(body, &rspData); err != nil {
		return err
	}
	deviceId, ok := rspData["id"].(string)
	if !ok || deviceId == "" {
		return common.NewError("missing warp device id")
	}
	token, ok := rspData["token"].(string)
	if !ok || token == "" {
		return common.NewError("missing warp token")
	}
	account, ok := rspData["account"].(map[string]interface{})
	if !ok {
		return common.NewError("missing warp account")
	}
	license, ok := account["license"].(string)
	if !ok || license == "" {
		logger.Debug("Error accessing license value.")
		return common.NewError("missing warp license")
	}
	warpInfo, err := s.getWarpInfo(version, deviceId, token)
	if err != nil {
		return err
	}
	var warpDetails map[string]interface{}
	if err := json.Unmarshal(warpInfo, &warpDetails); err != nil {
		return err
	}
	warpConfig, ok := warpDetails["config"].(map[string]interface{})
	if !ok {
		return common.NewError("missing warp configuration")
	}
	clientId, ok := warpConfig["client_id"].(string)
	if !ok {
		return common.NewError("missing warp client id")
	}
	reserved, err := s.getReserved(clientId)
	if err != nil {
		return err
	}
	interfaceConfig, ok := warpConfig["interface"].(map[string]interface{})
	if !ok {
		return common.NewError("missing warp interface configuration")
	}
	addresses, ok := interfaceConfig["addresses"].(map[string]interface{})
	if !ok {
		return common.NewError("missing warp interface addresses")
	}
	v4, v4OK := addresses["v4"].(string)
	v6, v6OK := addresses["v6"].(string)
	v4Address, v4Err := netip.ParseAddr(v4)
	v6Address, v6Err := netip.ParseAddr(v6)
	if !v4OK || !v6OK || v4Err != nil || v6Err != nil || !v4Address.Is4() || !v6Address.Is6() {
		return common.NewError("invalid warp interface addresses")
	}
	peers, ok := warpConfig["peers"].([]interface{})
	if !ok || len(peers) == 0 {
		return common.NewError("missing warp peers")
	}
	peer, ok := peers[0].(map[string]interface{})
	if !ok {
		return common.NewError("invalid warp peer")
	}
	peerEndpointObj, ok := peer["endpoint"].(map[string]interface{})
	if !ok {
		return common.NewError("missing warp peer endpoint")
	}
	peerEndpoint, ok := peerEndpointObj["host"].(string)
	if !ok {
		return common.NewError("missing warp peer endpoint host")
	}
	peerEpAddress, peerEpPort, err := net.SplitHostPort(peerEndpoint)
	if err != nil {
		return err
	}
	peerPublicKey, ok := peer["public_key"].(string)
	if !ok {
		return common.NewError("missing warp peer public key")
	}
	if _, err := wgtypes.ParseKey(peerPublicKey); err != nil {
		return common.NewError("invalid warp peer public key")
	}
	peerPort, err := strconv.Atoi(peerEpPort)
	if err != nil || peerPort < 1 || peerPort > 65535 {
		return common.NewError("invalid warp peer port")
	}
	peerConfigs := []map[string]interface{}{
		{
			"address":     peerEpAddress,
			"port":        peerPort,
			"public_key":  peerPublicKey,
			"allowed_ips": []string{"0.0.0.0/0", "::/0"},
			"reserved":    reserved,
		},
	}
	warpData := map[string]interface{}{
		"access_token": token,
		"device_id":    deviceId,
		"license_key":  license,
		"api_version":  version,
	}
	ep.Ext, err = json.MarshalIndent(warpData, "", "  ")
	if err != nil {
		return err
	}
	var epOptions map[string]interface{}
	if err := json.Unmarshal(ep.Options, &epOptions); err != nil {
		return err
	}
	epOptions["private_key"] = privateKey.String()
	epOptions["address"] = []string{fmt.Sprintf("%s/32", v4), fmt.Sprintf("%s/128", v6)}
	epOptions["listen_port"] = 0
	epOptions["peers"] = peerConfigs
	ep.Options, err = json.MarshalIndent(epOptions, "", "  ")
	return err
}
func (s *WarpService) getReserved(clientID string) ([]int, error) {
	decoded, err := base64.StdEncoding.DecodeString(clientID)
	if err != nil || len(decoded) != 3 {
		return nil, common.NewError("invalid warp client id")
	}
	reserved := make([]int, len(decoded))
	for index, value := range decoded {
		reserved[index] = int(value)
	}
	return reserved, nil
}
func uniqueWarpAPIVersions(preferred string) []string {
	versions := make([]string, 0, len(warpAPIVersions)+1)
	seen := make(map[string]struct{}, len(warpAPIVersions)+1)
	add := func(version string) {
		if version == "" {
			return
		}
		if _, ok := seen[version]; ok {
			return
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	add(preferred)
	for _, version := range warpAPIVersions {
		add(version)
	}
	return versions
}
func (s *WarpService) SetWarpLicense(old_license string, ep *model.Endpoint) error {
	var warpData map[string]string
	if err := json.Unmarshal(ep.Ext, &warpData); err != nil {
		return err
	}
	if warpData["license_key"] == old_license {
		return nil
	}
	dataBytes, err := json.Marshal(map[string]string{"license": warpData["license_key"]})
	if err != nil {
		return err
	}
	// Prefer the API version captured during registration; fall back to
	// trying every version if it is missing or stops working.
	versions := uniqueWarpAPIVersions(warpData["api_version"])
	var resp *http.Response
	var lastErr error
attempt:
	for _, version := range versions {
		url := fmt.Sprintf("https://api.cloudflareclient.com/%s/reg/%s/account", version, warpData["device_id"])
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url, nil)
		if err != nil {
			return err
		}
		setWarpAuthorizedHeaders(req, warpData["access_token"])
		r, err := doWarpAttempt(req, dataBytes)
		if err != nil {
			lastErr = err
			logger.Warningf("warp license update on %s failed: %v", version, err)
			continue
		}
		if r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices {
			resp = r
			break attempt
		}
		_ = r.Body.Close()
		lastErr = common.NewErrorf("cloudflare warp %s status: %d", version, r.StatusCode)
		logger.Warningf("warp license update on %s returned %d", version, r.StatusCode)
	}
	if resp == nil {
		if lastErr == nil {
			lastErr = errors.New("cloudflare warp: all attempts failed")
		}
		return lastErr
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	if success, ok := response["success"].(bool); ok && !success {
		errorArr, _ := response["errors"].([]interface{})
		if len(errorArr) == 0 {
			return common.NewError("warp license update failed")
		}
		errorObj, _ := errorArr[0].(map[string]interface{})
		return common.NewError(errorObj["code"], errorObj["message"])
	}
	return nil
}
