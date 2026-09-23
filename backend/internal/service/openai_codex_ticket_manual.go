package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func (s *OpenAIGatewayService) submitManualCodexTicketProbes(ctx context.Context, account *Account, models []string) {
	if s == nil || account == nil || len(models) == 0 {
		return
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	budget := codexTicketManualBatchBudget(cfg, len(models))
	// 管理端请求结束不取消已接受的手动批次；批次自身仍有有限执行期限。
	manualCtx, cancel := context.WithTimeout(withCodexTicketManualRetry(context.WithoutCancel(ctx)), budget)
	tasks := s.submitCodexTicketProbes(manualCtx, account, models, true)
	go func() {
		defer cancel()
		waitManualCodexTicketBatch(manualCtx, s.codexTicketAccountQueue(account.ID), tasks)
	}()
}

func codexTicketManualBatchBudget(cfg config.OpenAICodexTicketConfig, models int) time.Duration {
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	requests := 1
	if config.CodexTicketBusinessVerificationEnabled(cfg) {
		requests++
		if cfg.BusinessVerificationRounds > 1 {
			// 多轮质量探测后，还可能需要独立业务出口验证。
			requests += cfg.BusinessVerificationRounds
		} else if config.CodexTicketUsesCookies(cfg) {
			// 单轮验证若收到新的 Cookie，还需要验证候选凭据。
			requests++
		}
	}
	return 5*time.Minute + time.Duration(models)*time.Duration(requests)*time.Duration(cfg.HarvestAttemptTimeoutSeconds)*time.Second
}

func waitManualCodexTicketBatch(ctx context.Context, queue *codexTicketAccountQueue, tasks []*codexTicketProbeTask) {
	for _, task := range tasks {
		select {
		case <-task.done:
		case <-ctx.Done():
			// 当前账号可能仍有别的长请求，及时撤掉等待项，释放全局手动优先占位。
			for _, pending := range tasks {
				queue.cancelPending(pending)
			}
			return
		}
	}
}
