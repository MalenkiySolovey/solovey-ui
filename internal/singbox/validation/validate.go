package validation

func ValidateConfig(sbConfig []byte) error {
	return NewDryChecker().ValidateConfig(sbConfig)
}
