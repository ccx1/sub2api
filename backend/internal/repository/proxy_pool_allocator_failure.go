package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const proxyPoolFailureTTL = time.Minute

func proxyPoolFailureKey(member string) string {
	return "proxy:{pool}:failures:" + member
}

func (a *ProxyPoolAllocator) ReportFailure(ctx context.Context, accountID, proxyID int64) error {
	if accountID <= 0 || proxyID <= 0 {
		return nil
	}
	if a == nil || a.rdb == nil {
		return fmt.Errorf("proxy pool allocator dependencies unavailable")
	}
	member, id := strconv.FormatInt(accountID, 10), strconv.FormatInt(proxyID, 10)
	keys := []string{proxyPoolFailureKey(member), proxyPoolAffinityKey(member), proxyPoolLeaseKey(id)}
	if err := proxyPoolFailureScript.Run(ctx, a.rdb, keys, member, id, int(proxyPoolFailureTTL.Seconds())).Err(); err != nil {
		return fmt.Errorf("report account proxy failure: %w", err)
	}
	return nil
}

// 迟到的连接失败只能解除自身出口，不能覆盖另一个请求已经换好的关联。
var proxyPoolFailureScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1])
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[3]), ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
if redis.call('HGET', KEYS[2], 'proxy_id') == ARGV[2] then
  redis.call('DEL', KEYS[2])
end
redis.call('ZREM', KEYS[3], ARGV[1])
return 1
`)
