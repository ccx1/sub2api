package service

import (
	"context"
	"fmt"
	"time"
)

func (s *AccountUsageService) openAIUsageProbeAccount(ctx context.Context, account *Account) (*Account, error) {
	// 随机代理只绑定请求副本，不能回写到共享账号或调用方持有的缓存对象。
	requestAccount := *account
	if err := ResolveRandomProxyFromSource(ctx, &requestAccount, s.accountRepo); err != nil {
		if disableErr := DisableRandomProxyAccountOnUnavailable(ctx, &requestAccount, s.accountRepo, err); disableErr != nil {
			return nil, fmt.Errorf("resolve usage probe proxy: %w; disable account: %v", err, disableErr)
		}
		return nil, err
	}
	return &requestAccount, nil
}

func (s *AccountUsageService) persistOpenAIUsageProbeSnapshot(ctx context.Context, account *Account, updates map[string]any) error {
	if _, shared := account.Extra[SharedPoolOwnerKey]; !shared {
		s.persistOpenAICodexProbeSnapshot(account.ID, updates)
		return nil
	}
	if len(updates) == 0 {
		return nil
	}
	if s.accountRepo == nil {
		return fmt.Errorf("usage snapshot repository is unavailable")
	}
	// 共享账号操作锁必须覆盖快照保存；已取得的上游结果不因浏览器断开丢失。
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.accountRepo.UpdateExtra(writeCtx, account.ID, updates); err != nil {
		return fmt.Errorf("persist shared usage snapshot: %w", err)
	}
	notifyOpenAIAutoReset(account.ID)
	return nil
}
