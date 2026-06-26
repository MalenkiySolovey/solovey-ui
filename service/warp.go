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
	"os"
	"strconv"
	"time"
)

func (s *WarpService) RegisterWarp(ep *model.Endpoint) error {
	tos := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	privateKey, _ := wgtypes.GenerateKey()
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
	if !ok {
		return common.NewError("missing warp device id")
	}
	token, ok := rspData["token"].(string)
	if !ok {
		return common.NewError("missing warp token")
	}
	account, ok := rspData["account"].(map[string]interface{})
	if !ok {
		return common.NewError("missing warp account")
	}
	license, ok := account["license"].(string)
	if !ok {
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
	warpConfig, _ := warpDetails["config"].(map[string]interface{})
	clientId, _ := warpConfig["client_id"].(string)
	reserved := s.getReserved(clientId)
	interfaceConfig, _ := warpConfig["interface"].(map[string]interface{})
	addresses, _ := interfaceConfig["addresses"].(map[string]interface{})
	v4, _ := addresses["v4"].(string)
	v6, _ := addresses["v6"].(string)
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
	peerPublicKey, _ := peer["public_key"].(string)
	peerPort, _ := strconv.Atoi(peerEpPort)
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
func (s *WarpService) getReserved(clientID string) []int {
	var reserved []int
	decoded, err := base64.StdEncoding.DecodeString(clientID)
	if err != nil {
		return nil
	}
	hexString := ""
	for _, char := range decoded {
		hex := fmt.Sprintf("%02x", char)
		hexString += hex
	}
	for i := 0; i < len(hexString); i += 2 {
		hexByte := hexString[i : i+2]
		decValue, err := strconv.ParseInt(hexByte, 16, 32)
		if err != nil {
			return nil
		}
		reserved = append(reserved, int(decValue))
	}
	return reserved
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
