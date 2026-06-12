package service

import (
	"encoding/json"
	"strconv"
)

// Exported accessors for the experimental "Paid Subscriptions" module (paidsub
// package). They live in the service package so they can reuse the unexported,
// decryption-aware getString/getBool/getInt helpers; the encrypted token keys
// are decrypted transparently here and must never be logged.

func (s *SettingService) GetPaidSubEnabled() (bool, error) {
	return s.getBool(settingKeyPaidSubEnabled)
}

func (s *SettingService) GetPaidSubBotToken() (string, error) {
	return s.getString(settingKeyPaidSubBotToken)
}

func (s *SettingService) GetPaidSubBotPollSeconds() (int, error) {
	v, err := s.getInt(settingKeyPaidSubBotPollSeconds)
	if err != nil {
		return 25, err
	}
	if v < 1 {
		v = 1
	}
	if v > 50 {
		v = 50
	}
	return v, nil
}

func (s *SettingService) GetPaidSubUpdateOffset() (int64, error) {
	str, err := s.getString(settingKeyPaidSubUpdateOffset)
	if err != nil {
		return 0, err
	}
	if str == "" {
		return 0, nil
	}
	return strconv.ParseInt(str, 10, 64)
}

func (s *SettingService) SetPaidSubUpdateOffset(offset int64) error {
	return s.setString(settingKeyPaidSubUpdateOffset, strconv.FormatInt(offset, 10))
}

func (s *SettingService) GetPaidSubAutoRegister() (bool, error) {
	return s.getBool(settingKeyPaidSubAutoRegister)
}

// GetPaidSubAutoInbounds returns the admin-selected inbound ids that newly
// auto-registered clients are assigned to. Invalid JSON yields an empty list
// (auto-registration then has nothing to assign and is effectively disabled).
func (s *SettingService) GetPaidSubAutoInbounds() ([]uint, error) {
	str, err := s.getString(settingKeyPaidSubAutoInbounds)
	if err != nil {
		return nil, err
	}
	if str == "" {
		return []uint{}, nil
	}
	var ids []uint
	if err := json.Unmarshal([]byte(str), &ids); err != nil {
		return []uint{}, nil
	}
	return ids, nil
}

func (s *SettingService) GetPaidSubTrialDays() (int, error) {
	return s.getInt(settingKeyPaidSubTrialDays)
}

func (s *SettingService) GetPaidSubTrialVolumeGB() (int, error) {
	return s.getInt(settingKeyPaidSubTrialVolumeGB)
}

func (s *SettingService) GetPaidSubMaxClients() (int, error) {
	return s.getInt(settingKeyPaidSubMaxClients)
}

func (s *SettingService) GetPaidSubStartRateLimitPerMin() (int, error) {
	return s.getInt(settingKeyPaidSubStartRateLimitPerMin)
}

func (s *SettingService) GetPaidSubCurrency() (string, error) {
	return s.getString(settingKeyPaidSubCurrency)
}

func (s *SettingService) GetPaidSubStarsEnabled() (bool, error) {
	return s.getBool(settingKeyPaidSubStarsEnabled)
}

func (s *SettingService) GetPaidSubYooKassaEnabled() (bool, error) {
	return s.getBool(settingKeyPaidSubYooKassaEnabled)
}

func (s *SettingService) GetPaidSubYooKassaToken() (string, error) {
	return s.getString(settingKeyPaidSubYooKassaToken)
}

func (s *SettingService) GetPaidSubStripeEnabled() (bool, error) {
	return s.getBool(settingKeyPaidSubStripeEnabled)
}

func (s *SettingService) GetPaidSubStripeToken() (string, error) {
	return s.getString(settingKeyPaidSubStripeToken)
}

func (s *SettingService) GetPaidSubPayMasterEnabled() (bool, error) {
	return s.getBool(settingKeyPaidSubPayMasterEnabled)
}

func (s *SettingService) GetPaidSubPayMasterToken() (string, error) {
	return s.getString(settingKeyPaidSubPayMasterToken)
}

func (s *SettingService) GetPaidSubCryptoBotEnabled() (bool, error) {
	return s.getBool(settingKeyPaidSubCryptoBotEnabled)
}

func (s *SettingService) GetPaidSubCryptoBotToken() (string, error) {
	return s.getString(settingKeyPaidSubCryptoBotToken)
}

func (s *SettingService) GetPaidSubExternalEnabled() (bool, error) {
	return s.getBool(settingKeyPaidSubExternalEnabled)
}

func (s *SettingService) GetPaidSubExternalUrlTemplate() (string, error) {
	return s.getString(settingKeyPaidSubExternalURLTemplate)
}

func (s *SettingService) GetPaidSubOrderTTLMinutes() (int, error) {
	return s.getInt(settingKeyPaidSubOrderTTLMinutes)
}

func (s *SettingService) GetPaidSubGreeting() (string, error) {
	return s.getString(settingKeyPaidSubGreeting)
}

// GetPaidSubRefundRevoke reports the admin policy for the bot's user-initiated
// Stars auto-refund: when true (default), a successful refund also rolls back
// the days/traffic that order granted (anti-abuse: buy → refund → keep using).
// The user never chooses this; the panel refund button has its own per-refund
// toggle.
func (s *SettingService) GetPaidSubRefundRevoke() (bool, error) {
	return s.getBool(settingKeyPaidSubRefundRevoke)
}

func (s *SettingService) GetPaidSubTransportMode() (string, error) {
	return s.getString(settingKeyPaidSubTransportMode)
}

func (s *SettingService) GetPaidSubOutboundTag() (string, error) {
	return s.getString(settingKeyPaidSubOutboundTag)
}
