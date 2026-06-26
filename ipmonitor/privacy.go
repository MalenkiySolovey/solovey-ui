package ipmonitor

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func recordIPFields(ip string) (string, *string, bool) {
	ipHash, err := hashIP(ip)
	if err != nil {
		return "", nil, false
	}
	showRaw, err := getIPShowRaw(time.Now())
	if err != nil || !showRaw {
		return ipHash, nil, true
	}
	display := ip
	return ipHash, &display, true
}

func hashIP(ip string) (string, error) {
	salt, err := getInstallSalt()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write(salt)
	_, _ = hash.Write([]byte(ip))
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func prepareHistoryRows(rows []model.ClientIP) {
	showRaw, err := getIPShowRaw(time.Now())
	if err != nil {
		showRaw = false
	}
	for index := range rows {
		display := maskedIP(rows[index])
		if showRaw {
			if rows[index].IPDisplay != nil && *rows[index].IPDisplay != "" {
				display = *rows[index].IPDisplay
			} else if rows[index].IPHash == "" && !looksLikeSHA256Hex(rows[index].IP) {
				display = rows[index].IP
			}
		}
		rows[index].IP = display
		rows[index].IPHash = ""
		rows[index].IPDisplay = nil
	}
}

func maskedIP(row model.ClientIP) string {
	ipHash := row.IPHash
	if ipHash == "" {
		ipHash = hashLegacyIPValue(row.IP)
	}
	if len(ipHash) < ipMaskPrefix {
		return "masked"
	}
	return "masked:" + ipHash[:ipMaskPrefix]
}

func hashLegacyIPValue(ip string) string {
	if looksLikeSHA256Hex(ip) {
		return ip
	}
	ipHash, err := hashIP(ip)
	if err != nil {
		return ""
	}
	return ipHash
}

func looksLikeSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
