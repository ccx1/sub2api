package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

var _ service.CodexModelQualityStore = (*accountRepository)(nil)

var codexModelQualitySaveScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
return 1
`)

var codexModelQualityReleaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
return redis.call('DEL', KEYS[1])
`)

var codexModelQualityReadBindingScript = redis.NewScript(proxyIPGuardLua + `
local binding = redis.call('HMGET', KEYS[1], 'proxy_id', 'version')
if not binding[1] or not binding[2] then return {} end
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local accountUntil = tonumber(redis.call('ZSCORE', KEYS[2], binding[1])) or 0
local transportUntil = tonumber(redis.call('ZSCORE', KEYS[3], binding[1] .. ':' .. binding[2])) or 0
if accountUntil * 1000 > now or transportUntil > now or proxyipblocked(ARGV[1],now) then return {} end
return binding
`)

func (r *accountRepository) GetCodexModelQualityBoundProxy(ctx context.Context, accountID int64) (*service.Proxy, error) {
	if r == nil || r.proxyPool == nil || r.proxyPool.rdb == nil || r.client == nil || accountID <= 0 {
		return nil, errors.New("codex model quality proxy binding unavailable")
	}
	member := strconv.FormatInt(accountID, 10)
	keys := []string{proxyPoolAffinityKey(member), proxyPoolFailureKey(member), proxyTransportCooldownKey}
	binding, err := codexModelQualityReadBindingScript.Run(ctx, r.proxyPool.rdb, keys).StringSlice()
	if err != nil || len(binding) != 2 {
		return nil, err
	}
	id, err := strconv.ParseInt(binding[0], 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("invalid codex model quality proxy binding")
	}
	// 质量检查只能跟随调度器可见的公共代理；私有共享代理不属于随机池。
	entity, err := r.client.Proxy.Query().Where(dbproxy.IDEQ(id), dbproxy.DeletedAtIsNil(), excludePrivateSharedProxies).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	proxy := proxyEntityToService(entity)
	if !proxy.IsActive() || proxy.IsExpired(time.Now()) || binding[1] != fmt.Sprintf("%x", sha256.Sum256([]byte(proxy.URL()))) {
		return nil, nil
	}
	ip, err := r.proxyPool.proxyIPIdentity(ctx, proxy)
	if err != nil {
		return nil, err
	}
	current, err := codexModelQualityReadBindingScript.Run(ctx, r.proxyPool.rdb, keys, ip).StringSlice()
	if err != nil || len(current) != 2 || current[0] != binding[0] || current[1] != binding[1] {
		return nil, err
	}
	return proxy, nil
}

func (r *accountRepository) codexModelQualityRedis(accountID int64, model string) (*redis.Client, []string, error) {
	if r == nil || r.proxyPool == nil || r.proxyPool.rdb == nil {
		return nil, nil, errors.New("codex model quality shared storage unavailable")
	}
	model = strings.TrimSpace(model)
	if accountID <= 0 || model == "" {
		return nil, nil, errors.New("invalid codex model quality account or model")
	}
	// 同账号、模型共用租约；策略或出口变化也不能绕过去重。同槽支持 Redis Cluster。
	base := fmt.Sprintf("codex:model-quality:{%d:%x}:", accountID, sha256.Sum256([]byte(model)))
	return r.proxyPool.rdb, []string{base + "lease", base + "record"}, nil
}

func (r *accountRepository) LoadCodexModelQuality(ctx context.Context, accountID int64, model string) (*service.CodexModelQualityRecord, error) {
	client, keys, err := r.codexModelQualityRedis(accountID, model)
	if err != nil {
		return nil, err
	}
	payload, err := client.Get(ctx, keys[1]).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record *service.CodexModelQualityRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errors.New("invalid codex model quality record")
	}
	return record, nil
}

func (r *accountRepository) AcquireCodexModelQuality(ctx context.Context, accountID int64, model, token string, ttl time.Duration) (bool, error) {
	client, keys, err := r.codexModelQualityRedis(accountID, model)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(token) == "" || ttl < time.Millisecond {
		return false, errors.New("invalid codex model quality lease")
	}
	return client.SetNX(ctx, keys[0], token, ttl).Result()
}

func (r *accountRepository) SaveCodexModelQuality(ctx context.Context, accountID int64, model, token string, record *service.CodexModelQualityRecord, ttl time.Duration) (bool, error) {
	client, keys, err := r.codexModelQualityRedis(accountID, model)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(token) == "" || record == nil || ttl < time.Millisecond {
		return false, errors.New("invalid codex model quality result")
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	// 校验所有权与保存必须原子执行，超时任务不得覆盖新租约的结果。
	saved, err := codexModelQualitySaveScript.Run(ctx, client, keys, token, payload, ttl.Milliseconds()).Int64()
	return saved == 1 && err == nil, err
}

func (r *accountRepository) ReleaseCodexModelQuality(ctx context.Context, accountID int64, model, token string) error {
	client, keys, err := r.codexModelQualityRedis(accountID, model)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("invalid codex model quality lease")
	}
	return codexModelQualityReleaseScript.Run(ctx, client, keys[:1], token).Err()
}
