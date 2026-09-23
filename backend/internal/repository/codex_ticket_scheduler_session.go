package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// 预览只读取代数，不创建状态或影响下一次真实打票。
func (a *ProxyPoolAllocator) GetCodexTicketSessionEpoch(ctx context.Context, scope service.CodexTicketSessionScope) (string, error) {
	if scope.AccountID <= 0 {
		return "", errors.New("invalid codex ticket session identity")
	}
	if scope.Mode == "" || scope.Mode == "random" {
		return "", nil
	}
	if scope.Mode != "account" && scope.Mode != "account_model" {
		return "", errors.New("invalid codex ticket session mode")
	}
	if scope.Mode == "account_model" && strings.TrimSpace(scope.Model) == "" {
		return "", errors.New("invalid codex ticket session model")
	}
	if a == nil || a.rdb == nil {
		return "", errors.New("codex ticket shared scheduler unavailable")
	}
	raw, err := a.rdb.Get(ctx, codexSchedulerAccountKey(scope.AccountID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("codex ticket shared scheduler unavailable")
	}
	var state struct {
		Sessions map[string]struct {
			Epoch string `json:"epoch"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return "", errors.New("invalid codex ticket shared state")
	}
	key := "account"
	if scope.Mode == "account_model" {
		key = "model:" + strings.TrimSpace(scope.Model)
	}
	return state.Sessions[key].Epoch, nil
}

const codexSchedulerSession = `
local function sessionkey(mode,model)
  if mode=='account' then return 'account' end
  if mode=='account_model' then return 'model:'..model end
  return nil
end
local function sessionepoch(mode,model)
  local key=sessionkey(mode,model)
  local item=key and a.sessions and a.sessions[key]
  return item and item.epoch or ''
end
local function finishsession(success,neutral)
  if not a.started or q.policy~=a.policy or a.drain or neutral then return end
  local key=sessionkey(a.session_mode,a.model)
  if not key then return end
  a.sessions=a.sessions or {}
  local item=a.sessions[key] or {epoch='',failure_version=1,model_failures={}}
  if item.epoch~=a.session_epoch then return end
  -- account 模式共享会话身份，但失败阈值仍按模型独立；旧聚合计数不迁给任一模型。
  if n(item.failure_version)~=1 or type(item.model_failures)~='table' then
    item.failure_version,item.model_failures,item.failures=1,{},nil
  end
  if success then item.model_failures[a.model]=nil
  elseif q.harvest_failed or q.business_failed then
    item.model_failures[a.model]=n(item.model_failures[a.model])+1
    if item.model_failures[a.model]>=n(q.pin_threshold) then item.epoch,item.model_failures=q.next_epoch,{} end
  end
  a.sessions[key]=item
end
`
