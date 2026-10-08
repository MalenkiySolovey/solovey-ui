package validation

func ValidateConfig(sbConfig []byte) error {
	return NewDryChecker().ValidateConfig(sbConfig)
}

func ValidateConfigShape(sbConfig []byte) error {
	return NewDryChecker().ValidateConfigShape(sbConfig)
}
