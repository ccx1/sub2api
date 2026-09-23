package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

func codexTicketBusinessHoldKey(accountID int64) string {
	return "proxy:{pool}:codex:business-holds:" + strconv.FormatInt(accountID, 10)
}

var codexTicketBusinessHoldScript = redis.NewScript(`
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local action,token,ttl=ARGV[1],ARGV[2],tonumber(ARGV[3])
if action=='peek' then
  local first=redis.call('ZRANGEBYSCORE',KEYS[1],now+1,'+inf','WITHSCORES','LIMIT',0,1)
  return #first>0 and tonumber(first[2]) or 0
end
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
if action=='release' then
  redis.call('ZREM',KEYS[1],token)
elseif action=='renew' then
  if not redis.call('ZSCORE',KEYS[1],token) then return -1 end
  redis.call('ZADD',KEYS[1],'XX',now+ttl,token)
else
  redis.call('ZADD',KEYS[1],now+ttl,token)
end
local last=redis.call('ZREVRANGE',KEYS[1],0,0,'WITHSCORES')
if #last==0 then redis.call('DEL',KEYS[1])
else redis.call('PEXPIREAT',KEYS[1],tonumber(last[2])+1000) end
return 0
`)

func (a *ProxyPoolAllocator) runCodexTicketBusinessHold(ctx context.Context, action string, lease service.CodexTicketBusinessLease) (int64, error) {
	if a == nil || a.rdb == nil {
		return 0, errors.New("codex ticket shared business hold unavailable")
	}
	if lease.AccountID <= 0 || (action != "peek" && (lease.Token == "" || lease.TTL <= 0)) {
		return 0, errors.New("invalid codex ticket business lease")
	}
	result, err := codexTicketBusinessHoldScript.Run(ctx, a.rdb, []string{codexTicketBusinessHoldKey(lease.AccountID)}, action, lease.Token, lease.TTL.Milliseconds()).Int64()
	if err == nil && result < 0 {
		return 0, service.ErrCodexTicketBusinessHoldLost
	}
	return result, err
}

func (a *ProxyPoolAllocator) AcquireCodexTicketBusinessHold(ctx context.Context, lease service.CodexTicketBusinessLease) error {
	_, err := a.runCodexTicketBusinessHold(ctx, "acquire", lease)
	return err
}

func (a *ProxyPoolAllocator) RenewCodexTicketBusinessHold(ctx context.Context, lease service.CodexTicketBusinessLease) error {
	_, err := a.runCodexTicketBusinessHold(ctx, "renew", lease)
	return err
}

func (a *ProxyPoolAllocator) ReleaseCodexTicketBusinessHold(ctx context.Context, lease service.CodexTicketBusinessLease) error {
	_, err := a.runCodexTicketBusinessHold(ctx, "release", lease)
	return err
}

func (a *ProxyPoolAllocator) CodexTicketBusinessHoldUntil(ctx context.Context, accountID int64) (time.Time, error) {
	millis, err := a.runCodexTicketBusinessHold(ctx, "peek", service.CodexTicketBusinessLease{AccountID: accountID})
	if err != nil || millis == 0 {
		return time.Time{}, err
	}
	return time.UnixMilli(millis), nil
}

const codexSchedulerBusinessHold = `
local function businessholduntil()
  local key=string.gsub(KEYS[1],':account:',':business-holds:')
  local first=redis.call('ZRANGEBYSCORE',key,now+1,'+inf','WITHSCORES','LIMIT',0,1)
  return #first>0 and tonumber(first[2]) or 0
end
`
