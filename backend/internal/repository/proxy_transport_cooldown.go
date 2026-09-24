package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	proxyTransportCooldownKey = "proxy:{pool}:transport_cooldowns"
	proxyTransportCooldownTTL = 5 * time.Minute
)

func proxyTransportVersion(proxy *service.Proxy) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(proxy.URL())))
}

func proxyTransportCooldownMember(proxy *service.Proxy) string {
	return strconv.FormatInt(proxy.ID, 10) + ":" + proxyTransportVersion(proxy)
}

func (a *ProxyPoolAllocator) ReportTransportFailure(ctx context.Context, accountID int64, proxy *service.Proxy) error {
	if proxy == nil || proxy.ID <= 0 {
		return nil
	}
	if a == nil || a.rdb == nil {
		return fmt.Errorf("proxy pool allocator dependencies unavailable")
	}
	member, id := strconv.FormatInt(accountID, 10), strconv.FormatInt(proxy.ID, 10)
	keys := []string{proxyTransportCooldownKey, proxyPoolAffinityKey(member), proxyPoolLeaseKey(id), proxyPoolBoundAccountsKey(id)}
	if err := proxyTransportFailureScript.Run(ctx, a.rdb, keys, member, id, proxyTransportVersion(proxy),
		proxyTransportCooldownTTL.Milliseconds()).Err(); err != nil {
		return fmt.Errorf("report proxy transport failure: %w", err)
	}
	return nil
}

func (a *ProxyPoolAllocator) TransportCoolingDown(ctx context.Context, proxy *service.Proxy) (bool, error) {
	if proxy == nil || proxy.ID <= 0 {
		return false, nil
	}
	if a == nil || a.rdb == nil {
		return false, fmt.Errorf("proxy pool allocator dependencies unavailable")
	}
	ip, err := a.proxyIPIdentity(ctx, proxy)
	if err != nil {
		return false, err
	}
	result, err := proxyTransportCoolingScript.Run(ctx, a.rdb, []string{proxyTransportCooldownKey}, proxyTransportCooldownMember(proxy), ip).Int()
	if err != nil {
		return false, fmt.Errorf("read proxy transport cooldown: %w", err)
	}
	return result == 1, nil
}

// 首个故障开启五分钟冷却；重复和迟到回调不能延长既有期限或清除新的出口关联。
var proxyTransportFailureScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local identity = ARGV[2] .. ':' .. ARGV[3]
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local added = redis.call('ZADD', KEYS[1], 'NX', now + tonumber(ARGV[4]), identity)
if added > 0 then redis.call('PEXPIRE', KEYS[1], ARGV[4]) end
if tonumber(ARGV[1]) > 0 and redis.call('HGET', KEYS[2], 'proxy_id') == ARGV[2]
    and redis.call('HGET', KEYS[2], 'version') == ARGV[3] then
  redis.call('DEL', KEYS[2])
  redis.call('ZREM', KEYS[3], ARGV[1])
  redis.call('SREM', KEYS[4], ARGV[1])
end
return added
`)

var proxyTransportCoolingScript = redis.NewScript(proxyIPGuardLua + `
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local until_at = tonumber(redis.call('ZSCORE', KEYS[1], ARGV[1])) or 0
return (until_at > now or proxyipblocked(ARGV[2],now)) and 1 or 0
`)
