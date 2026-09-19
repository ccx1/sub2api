package service

// 身份保护与运行时出口分开校验，随机代理通过账号关联保持稳定。
func validateAccountRoutingConfiguration(extra map[string]any) error {
	if err := ValidateRandomProxyPoolExtra(extra); err != nil {
		return err
	}
	if err := ValidateRandomProxyReuseExtra(extra); err != nil {
		return err
	}
	return ValidateDailyCooldownExtra(extra)
}
