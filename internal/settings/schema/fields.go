package schema

func DefaultFieldMetadata() []Field {
	return mergeFields(
		webFieldMetadata(),
		sessionFieldMetadata(),
		runtimeFieldMetadata(),
		internalFieldMetadata(),
		subscriptionFieldMetadata(),
		telegramFieldMetadata(),
		paidSubFieldMetadata(),
		ipCertFieldMetadata(),
	)
}
