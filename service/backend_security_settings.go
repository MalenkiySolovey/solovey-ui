package service

// backendSecuritySettings adapts the existing Product settings authority to the
// narrow secret operations consumed by Backend security packages. It contains
// no policy or persistence logic of its own.
type backendSecuritySettings struct {
	service *SettingService
}

func (s backendSecuritySettings) EncryptString(key, value string) (string, error) {
	return s.service.encryptSettingValue(key, value)
}

func (s backendSecuritySettings) DecryptString(key, value string) (string, error) {
	return s.service.decryptSettingValue(key, value)
}

func (s backendSecuritySettings) MasterSecret() ([]byte, error) {
	return s.service.GetSecret()
}

func (s backendSecuritySettings) InstallSalt() ([]byte, error) {
	return s.service.GetInstallSalt()
}
