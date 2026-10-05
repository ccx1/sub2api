package service

import (
	"context"
	"time"
)

func (s *OpenAIGatewayService) codexTicketHarvestInterval(ctx context.Context) time.Duration {
	cfg := s.openAICodexTicketConfigContext(ctx)
	// 采集轮询间隔由管理员配置。规则拒收的重试节奏由 backoff/protection
	// 单独控制，不能把后台扫描周期静默压短，否则“采集间隔”不会按设置生效。
	return time.Duration(max(1, cfg.HarvestProbeIntervalSeconds)) * time.Second
}
