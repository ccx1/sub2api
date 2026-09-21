package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type AccountProtectionSyncFailure struct {
	AccountID int64  `json:"account_id"`
	Error     string `json:"error"`
}

type AccountProtectionSyncResult struct {
	Updated   int                            `json:"updated"`
	Unchanged int                            `json:"unchanged"`
	Failed    []AccountProtectionSyncFailure `json:"failed"`
	Error     string                         `json:"error,omitempty"`
}

func (s *AntiDegradeService) protectionSyncAccountIDs(ctx context.Context) ([]int64, error) {
	if s.accountRepo == nil {
		return nil, infraerrors.ServiceUnavailable("PROTECTION_SYNC_UNAVAILABLE", "账号保护同步服务不可用")
	}
	// 不继承账号管理页的共享账号过滤；固定 ID 排序避免策略写入改变分页顺序。
	ctx = WithSharedAccountFilter(ctx, "")
	params := pagination.PaginationParams{Page: 1, PageSize: 200, SortBy: "id", SortOrder: pagination.SortOrderAsc}
	ids := []int64{}
	seen := map[int64]bool{}
	for {
		accounts, page, err := s.accountRepo.ListWithFilters(ctx, params, PlatformOpenAI, "", "", "", 0, "")
		if err != nil {
			return nil, fmt.Errorf("list accounts for protection sync: %w", err)
		}
		for i := range accounts {
			a := &accounts[i]
			if isOpenAIOAuthLike(a) && !a.IsShadow() && !seen[a.ID] {
				ids = append(ids, a.ID)
				seen[a.ID] = true
			}
		}
		if len(accounts) == 0 || (page != nil && params.Page >= page.Pages) || (page == nil && len(accounts) < params.PageSize) {
			return ids, nil
		}
		params.Page++
	}
}

func protectionFollowsDefault(a *Account) bool {
	return isOpenAIOAuthLike(a) && !a.IsShadow() && a.AntiDegradationEnabled()
}

func (s *AntiDegradeService) syncProtectionDefaults(ctx context.Context, ids []int64, mode AntiDegradeMode) *AccountProtectionSyncResult {
	result := &AccountProtectionSyncResult{Failed: []AccountProtectionSyncFailure{}}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			result.Error = "默认策略已保存，同步中断；请重新保存以重试: " + err.Error()
			break
		}
		settings, err := s.GetProtectionSettings(ctx)
		if err != nil {
			result.Error = "默认策略已保存，核验当前策略失败；请重新保存以重试: " + err.Error()
			break
		}
		if settings.DefaultMode != mode {
			result.Error = "默认策略已被其他管理员更新，本次同步已停止；请刷新后重试"
			break
		}
		changed, err := s.syncAccountProtection(ctx, id, mode)
		if err != nil {
			result.Failed = append(result.Failed, AccountProtectionSyncFailure{AccountID: id, Error: err.Error()})
		} else if changed {
			result.Updated++
		} else {
			result.Unchanged++
		}
	}
	return result
}

func (s *AntiDegradeService) syncAccountProtection(ctx context.Context, id int64, mode AntiDegradeMode) (bool, error) {
	changed := false
	_, err := s.transition(ctx, id, func(planner *AntiDegradeService) error {
		a, err := planner.admin.GetAccount(ctx, id)
		if err != nil {
			return err
		}
		// 枚举与写入之间可能被管理员关闭，必须在最终草稿内重新判断。
		if !protectionFollowsDefault(a) {
			return nil
		}
		prepared, err := planner.applyAntiDegradeMode(ctx, id, mode)
		if err != nil {
			return err
		}
		changed = planner.admin.(*protectionDraftStore).changed
		return ValidateAccountProtectionConfiguration(prepared)
	})
	return changed && err == nil, err
}
