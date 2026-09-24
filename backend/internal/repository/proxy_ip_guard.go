package repository

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

func proxyIPGuardKey(ip string) string {
	return fmt.Sprintf("proxy:{pool}:codex:ip:%x", sha1.Sum([]byte(ip)))
}

func (a *ProxyPoolAllocator) proxyIPIdentity(ctx context.Context, proxy *service.Proxy) (string, error) {
	if a == nil || a.latencyCache == nil {
		return "", fmt.Errorf("proxy IP health cache unavailable")
	}
	info, err := a.latencyCache.GetProxyLatencies(ctx, []int64{proxy.ID})
	if err != nil {
		return "", fmt.Errorf("read proxy IP health: %w", err)
	}
	ips, err := a.proxyIPIdentities(ctx, []*service.Proxy{proxy}, info, false)
	return ips[proxy.ID], err
}

type proxyIPObservation struct {
	Key        string `json:"key"`
	IP         string `json:"ip"`
	Fallback   string `json:"fallback"`
	ObservedAt int64  `json:"observed_at"`
}

// 最近一次有效出口观察独立于健康缓存 TTL，配置变更后使用新身份键。
// remember 仅用于 Select/Reserve；只读检查不能生成或刷新映射。
func (a *ProxyPoolAllocator) proxyIPIdentities(ctx context.Context, proxies []*service.Proxy, health map[int64]*service.ProxyLatencyInfo, remember bool) (map[int64]string, error) {
	result := make(map[int64]string, len(proxies))
	if len(proxies) == 0 {
		return result, nil
	}
	if a == nil || a.rdb == nil {
		return nil, fmt.Errorf("proxy IP shared storage unavailable")
	}
	observations := make([]proxyIPObservation, 0, len(proxies))
	for _, proxy := range proxies {
		item := proxyIPObservation{Key: "proxy:{pool}:codex:ip_identity:" + strconv.FormatInt(proxy.ID, 10) + ":" + service.ProxyProbeIdentity(proxy),
			Fallback: codexProxyIPKey(proxy, nil)}
		if info := health[proxy.ID]; service.ProxyLatencyMatchesProxy(info, proxy) && net.ParseIP(strings.TrimSpace(info.IPAddress)) != nil {
			item.IP, item.ObservedAt = codexProxyIPKey(proxy, info), info.UpdatedAt.UnixMilli()
		}
		observations = append(observations, item)
	}
	payload, err := json.Marshal(observations)
	if err != nil {
		return nil, err
	}
	ips, err := proxyIPIdentityScript.Run(ctx, a.rdb, []string{observations[0].Key}, string(payload), remember).StringSlice()
	if err != nil {
		return nil, fmt.Errorf("resolve proxy IP identities: %w", err)
	}
	if len(ips) != len(proxies) {
		return nil, fmt.Errorf("invalid proxy IP identity response")
	}
	for i, proxy := range proxies {
		if ips[i] != "" && net.ParseIP(ips[i]) == nil {
			return nil, fmt.Errorf("invalid stored proxy IP identity")
		}
		result[proxy.ID] = ips[i]
	}
	return result, nil
}

var proxyIPIdentityScript = redis.NewScript(`
local observations=cjson.decode(ARGV[1])
local remember=ARGV[2]=='1'
local result={}
for _,item in ipairs(observations) do
  local stored=redis.call('HMGET',item.key,'ip','observed_at')
  local ip=stored[1] or ''
  local at=tonumber(stored[2] or 0)
  if not at then error('invalid proxy IP observation timestamp') end
  if item.ip~='' and item.observed_at>=at then
    ip=item.ip
    if remember then redis.call('HSET',item.key,'ip',ip,'observed_at',item.observed_at) end
  end
  if ip=='' then ip=item.fallback end
  table.insert(result,ip)
end
return result
`)

// IP 封禁独立于账号冷却；读取失败不能降级成可用出口。
const proxyIPGuardLua = `
local function proxyipblocked(ip,now)
  if not ip or ip=='' then return false end
  local raw=redis.call('GET','proxy:{pool}:codex:ip:'..redis.sha1hex(ip))
  if not raw then return false end
  local ok,state=pcall(cjson.decode,raw)
  if not ok or type(state)~='table' then error('invalid proxy IP guard state') end
  if state.disabled~=nil and type(state.disabled)~='boolean' then error('invalid proxy IP disabled state') end
  local until_at=tonumber(state.until_at or 0)
  if until_at==nil then error('invalid proxy IP cooldown state') end
  return state.disabled==true or until_at>now
end
`
