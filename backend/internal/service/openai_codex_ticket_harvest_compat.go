package service

import (
	"context"
	"net/http"
)

// 上游 ranxi2001 的 780/harvest 打票引擎会把业务请求钉到打票探针的会话身份上。
// 本仓库沿用 292 门票池 + CookieReceipt 方案，不存在 harvest 会话，这些钩子
// 恒为“未钉住”，调用点即回落到原有的账号身份/指纹收敛逻辑。保留同名钩子是
// 为了与上游代码结构对齐，降低后续合并冲突。

func (s *OpenAIGatewayService) harvestPinnedSessionForModel(context.Context, *Account, string) string {
	return ""
}

func (s *OpenAIGatewayService) harvestPinsCodexIdentity(context.Context, *Account, string) bool {
	return false
}

func (s *OpenAIGatewayService) pinHarvestIdentityMapsForModel(context.Context, *Account, string, map[string]any) bool {
	return false
}

func (s *OpenAIGatewayService) pinHarvestIdentityBodyForModel(_ context.Context, _ *Account, _ string, body []byte) ([]byte, error) {
	return body, nil
}

func (s *OpenAIGatewayService) pinBoundCodexTicketHarvestIdentity(*http.Request, *Account) {}

func (s *OpenAIGatewayService) applyCodexAccountIdentityOrHarvestPinMap(_ context.Context, _ *Account, identityAccount *Account, apiKeyID int64, _ string, body map[string]any) bool {
	return applyCodexAccountIdentityClientMetadataMap(body, identityAccount, apiKeyID)
}
