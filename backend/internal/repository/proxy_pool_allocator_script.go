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
local nowMillis = now * 1000 + math.floor(tonumber(clock[2]) / 1000)
local failureKey = KEYS[#KEYS - 1]
redis.call('ZREMRANGEBYSCORE', failureKey, '-inf', now)
local transportKey = 'proxy:{pool}:transport_cooldowns'
redis.call('ZREMRANGEBYSCORE', transportKey, '-inf', nowMillis)
local since = tonumber(previous.since) or now
local chosen, reusable, fallback = 0, 0, 0
local bestCount, bestQuality, bestTier = math.huge, math.huge, math.huge
local allFailed, recovery, earliest = #candidates > 0, 0, math.huge

for i, candidate in ipairs(candidates) do
  local key = KEYS[i]
  redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
  local count = redis.call('ZCARD', key)
  local existing = redis.call('ZSCORE', key, member) ~= false
  local harvestKey = 'proxy:{pool}:codex:leases:' .. candidate.id
  redis.call('ZREMRANGEBYSCORE', harvestKey, '-inf', nowMillis)
  local harvestMembers = {}
  for _, harvestID in ipairs(redis.call('ZRANGE', harvestKey, 0, -1)) do
    harvestMembers[harvestID] = true
    if redis.call('ZSCORE', key, harvestID) == false then count = count + 1 end
    if harvestID == member then existing = true end
  end
  for _, fixedID in ipairs(candidate.fixed) do
    if redis.call('ZSCORE', key, fixedID) == false and not harvestMembers[fixedID] then count = count + 1 end
    if fixedID == member then existing = true end
  end
  local projected = count + (existing and 0 or 1)
  local accountUntil = tonumber(redis.call('ZSCORE', failureKey, candidate.id)) or 0
  local transportUntil = tonumber(redis.call('ZSCORE', transportKey, candidate.id .. ':' .. candidate.version)) or 0
  local failedUntil = math.max(accountUntil * 1000, transportUntil)
  local available = limit == 0 or projected <= limit
  if failedUntil <= nowMillis then
    allFailed = false
  elseif available and failedUntil < earliest then
    recovery, earliest = i, failedUntil
  end
  if failedUntil <= nowMillis and available then
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
-- 只有全部有效候选都冷却时才逐个恢复；容量不足不能触发解冷却。
if chosen == 0 and allFailed and recovery > 0 then
  chosen = recovery
  redis.call('ZREM', failureKey, candidates[chosen].id)
  redis.call('ZREM', transportKey, candidates[chosen].id .. ':' .. candidates[chosen].version)
end
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
