package repository

import "github.com/redis/go-redis/v9"

// 账号关联长期保留，容量占位使用短租约；校验、复用和换绑在同一脚本内原子完成。
var proxyPoolReserveScript = redis.NewScript(`
local member = ARGV[1]
local ttl = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local candidates = cjson.decode(ARGV[4])
local sticky = ARGV[5] == '1'
local affinityKey = KEYS[#KEYS]
local bound = sticky and redis.call('HGETALL', affinityKey) or {}
local previous = {}
for i = 1, #bound, 2 do previous[bound[i]] = bound[i + 1] end
local clock = redis.call('TIME')
local now = tonumber(clock[1])
local failureKey = KEYS[#KEYS - 1]
redis.call('ZREMRANGEBYSCORE', failureKey, '-inf', now)
local since = tonumber(previous.since) or now
local chosen, reusable, fallback = 0, 0, 0
local bestCount, bestQuality, bestTier = math.huge, math.huge, math.huge

for i, candidate in ipairs(candidates) do
  local key = KEYS[i]
  redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
  local count = redis.call('ZCARD', key)
  local existing = redis.call('ZSCORE', key, member) ~= false
  for _, fixedID in ipairs(candidate.fixed) do
    if redis.call('ZSCORE', key, fixedID) == false then count = count + 1 end
    if fixedID == member then existing = true end
  end
  local projected = count + (existing and 0 or 1)
  local failed = redis.call('ZSCORE', failureKey, candidate.id) ~= false
  if not failed and (limit == 0 or projected <= limit) then
    local same = candidate.id == previous.proxy_id
    if same and candidate.version == previous.version then reusable = i end
    if same then fallback = i end
    local tier = candidate.degraded and 1 or 0
    local better = tier < bestTier
      or (tier == bestTier and count < bestCount)
      or (tier == bestTier and count == bestCount and candidate.quality < bestQuality)
    if not same and better then
      chosen, bestCount, bestQuality, bestTier = i, count, candidate.quality, tier
    end
  end
end

if reusable > 0 and (not candidates[reusable].degraded or bestTier ~= 0) then chosen = reusable end
if chosen == 0 then chosen = fallback end
local selectedID = chosen > 0 and candidates[chosen].id or ''
if sticky and previous.proxy_id and previous.proxy_id ~= selectedID then
  redis.call('ZREM', 'proxy:{pool}:associations:' .. previous.proxy_id, member)
  redis.call('SREM', 'proxy:{pool}:bound_accounts:' .. previous.proxy_id, member)
  redis.call('HDEL', affinityKey, 'failure_count', 'failure_since')
end
if chosen == 0 then
  if sticky then redis.call('DEL', affinityKey) end
  return ''
end
redis.call('ZADD', KEYS[chosen], now + ttl, member)
redis.call('EXPIRE', KEYS[chosen], ttl)
if sticky then
  if chosen ~= reusable then
    since = now
    redis.call('HDEL', affinityKey, 'failure_count', 'failure_since')
  end
  redis.call('HSET', affinityKey, 'proxy_id', selectedID, 'since', since, 'version', candidates[chosen].version)
  redis.call('SADD', 'proxy:{pool}:bound_accounts:' .. selectedID, member)
end
return selectedID
`)
