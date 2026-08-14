package service

import (
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
)

var (
	cookieKeyHKDFInfo            = settingscrypto.CookieKeyHKDFInfo
	settingsSecretboxKeyHKDFInfo = settingscrypto.SettingsSecretboxKeyHKDFInfo
)

type secretboxCandidate struct {
	name string
	box  *secretbox.Box
}

func (s *SettingService) getSecretboxCandidates() ([]secretboxCandidate, error) {
	candidates, err := s.settingsSecretCodec().SecretboxCandidates()
	if err != nil {
		return nil, err
	}
	return fromSettingsCryptoCandidates(candidates), nil
}

func deriveHKDFKey(masterKey, salt, info []byte) ([]byte, error) {
	return settingscrypto.DeriveHKDFKey(masterKey, salt, info)
}

func decryptWithCandidate(candidates []secretboxCandidate, key, value string) (int, string, bool) {
	return settingscrypto.DecryptWithCandidate(toSettingsCryptoCandidates(candidates), key, value)
}

func fromSettingsCryptoCandidates(candidates []settingscrypto.Candidate) []secretboxCandidate {
	converted := make([]secretboxCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		converted = append(converted, secretboxCandidate{name: candidate.Name, box: candidate.Box})
	}
	return converted
}

func toSettingsCryptoCandidates(candidates []secretboxCandidate) []settingscrypto.Candidate {
	converted := make([]settingscrypto.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		converted = append(converted, settingscrypto.Candidate{Name: candidate.name, Box: candidate.box})
	}
	return converted
}
