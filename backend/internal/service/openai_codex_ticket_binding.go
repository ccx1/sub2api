package service

import "encoding/json"

// 已发布票只绑定账号身份和实际固定出口；随机池策略和采集频率不使有效旧票失效。
func openAICodexTicketAccountBinding(account *Account) string {
	if account == nil {
		return ""
	}
	extra := make(map[string]any)
	for _, key := range []string{"enable_tls_fingerprint", "tls_fingerprint_builtin", "tls_fingerprint_profile_id",
		"codex_fingerprint_mode", AntiDegradeMarkerExtraKey, AntiDegradationExtraKey} {
		extra[key] = account.Extra[key]
	}
	identity := map[string]any{"account": account.ID, "platform": account.Platform, "type": account.Type,
		"chatgpt_account_id": account.Credentials["chatgpt_account_id"], "organization_id": account.Credentials["organization_id"],
		"chatgpt_organization_id": account.Credentials["chatgpt_organization_id"], "random": account.IsRandomProxy(), "extra": extra}
	if !account.IsRandomProxy() {
		identity["proxy_id"], identity["proxy_url"] = account.ProxyID, resolveAccountProxyURL(account)
	}
	encoded, _ := json.Marshal(identity)
	return "v2:" + openAICodexTicketEgress(string(encoded))
}

func (t *openAICodexTicket) accountCompatible(account *Account) bool {
	if t == nil || account == nil {
		return false
	}
	if (t.Verified || t.VerificationSkipped) && !account.IsRandomProxy() && t.Egress != "" {
		if account.ProxyID != nil && account.Proxy == nil || t.Egress != openAICodexTicketEgress(resolveAccountProxyURL(account)) {
			return false
		}
	}
	// 升级前未记录账号指纹的票沿用至到期，不能因新增元数据集中重采。
	if t.AccountBinding == "" {
		return true
	}
	if !account.IsRandomProxy() && account.ProxyID != nil && account.Proxy == nil {
		return false
	}
	return t.AccountBinding == openAICodexTicketAccountBinding(account)
}
