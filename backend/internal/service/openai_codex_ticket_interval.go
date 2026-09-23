package service

import (
	"context"
	"time"
)

func (s *OpenAIGatewayService) codexTicketHarvestInterval(ctx context.Context) time.Duration {
	cfg := s.openAICodexTicketConfigContext(ctx)
	seconds := cfg.HarvestProbeIntervalSeconds
	protection := cfg.TicketProtection()
	// 短拒收重试不能被较长的自动扫描周期盖住。
	seconds = min(seconds, protection.RejectionRetryIntervalSeconds, protection.RejectionRetryCooldownSeconds)
	return time.Duration(max(1, seconds)) * time.Second
}
