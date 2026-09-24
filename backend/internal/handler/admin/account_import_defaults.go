package admin

func firstAccountImportOption(item, batch *bool) *bool {
	if item != nil {
		return item
	}
	return batch
}

func skipAccountImportDefaults(useDefaults *bool) bool {
	return useDefaults != nil && !*useDefaults
}
