package repository

import "github.com/redis/go-redis/v9"

var codexTicketSchedulerScript = redis.NewScript(codexSchedulerPrelude + codexSchedulerSession + codexSchedulerLearning + codexSchedulerPins + codexSchedulerPoolRecovery + codexSchedulerReserve + codexSchedulerBusiness + codexSchedulerActions)

// 所有键共享 {pool}，容量并集与准入在同一 Redis 原子执行中计算。
const codexSchedulerPrelude = `
local q = cjson.decode(ARGV[1])
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local raw = redis.call('GET', KEYS[1])
local a = raw and cjson.decode(raw) or {g=0, attempts=0, round=1, cursors={}, retries={}, visited={}, snapshot={}, lastmodel=''}
local function n(value) return tonumber(value) or 0 end
local function proxykey(id, version) return 'proxy:{pool}:codex:proxy:' .. id .. ':' .. version end
local function leasekey(id) return 'proxy:{pool}:codex:leases:' .. id end
local function readproxy(id, version)
  local value = redis.call('GET', proxykey(id, version))
  return value and cjson.decode(value) or {g=0, state='available', until_at=0}
end
local function writeproxy(id, version, p) redis.call('SET', proxykey(id, version), cjson.encode(p)) end
` + codexSchedulerIPProtection + `
local function save() redis.call('SET', KEYS[1], cjson.encode(a)) end
local function reply(state, reason, wait, proxy, retry)
  local result = {waiting=wait or false, token=a.token or '', session_epoch=a.session_epoch or '', proxy_id=proxy or a.proxy or '0',
    retry_ms=retry or 0, cooldown_ms=n(a.until_at), silence_ms=n(a.silenceuntil), status={state=state, reason=reason or '',
    model=a.model or '', proxy_id=n(proxy or a.proxy), attempts_used=n(a.attempts),
    max_attempts=n(a.maxattempts), round=n(a.round), max_rounds=n(a.maxrounds),
    half_open=a.half or false, generation=n(a.g), last_model=a.lastmodel or '',
    rule_matched=a.rulematched or false, policy_version=a.policy or '',
    harvest_half_open=a.harvesthalf or false, harvest_accepted=a.harvest or false}}
  return cjson.encode(result)
end
` + codexSchedulerRetry + codexSchedulerBusinessHold + `
local function roundsexhausted()
  if not a.pool or a.harvestpinned or n(a.round)<n(a.maxrounds) or #(a.snapshot or {})==0 then return false end
  for _,id in ipairs(a.snapshot) do if not a.visited[id] then return false end end
  return true
end
local function syncattemptlimit()
  if not a.active or n(q.max_attempts)<=0 then return end
  local previousMax=n(a.maxattempts)
  a.maxattempts=n(q.max_attempts)
  -- 上调预算只解除预算耗尽冷却，保留轮次耗尽和保护排空。
  if not a.drain and n(a.attempts)>=previousMax and n(a.attempts)<a.maxattempts and not roundsexhausted() then
    a.until_at=0
    if a.reason=='account_cooldown' then a.state,a.reason,a.retry_at='idle','',0 end
  end
end
if q.action == 'status' then
  syncattemptlimit()
  resetrejection()
  local state, reason, retry = a.state or 'idle', a.reason or '', n(a.retry_at)
  local businessUntil=businessholduntil()
  if not a.token and businessUntil>now then state,reason,retry='waiting','business_active',businessUntil
  elseif reason=='business_active' and businessUntil==0 then state,reason,retry='idle','',0 end
  if a.token and n(a.lease) > now then state, reason, retry = 'running', 'account_busy', n(a.lease)
  elseif n(a.until_at) > now then state, reason, retry = 'waiting', a.drain and 'protection_draining' or 'account_cooldown', n(a.until_at)
  elseif a.token then state, reason, retry = 'waiting', 'lease_expired', 0
  elseif a.rejection and n(a.rejection.until_at)>now then
    state,reason,retry='waiting',a.rejection.exhausted and 'rejection_cooldown' or 'rejection_retry',a.rejection.until_at
  end
  return reply(state, reason, false, nil, retry)
end
-- 迟到请求不得清理或改写新 owner 的状态，包括配置排空和过期回收。
if q.action~='reserve' and (a.token~=q.token or n(a.g)~=n(q.generation)) then
  return reply('idle','stale_completion',q.action~='finish')
end
syncattemptlimit()
local function waiting(reason, at, proxy)
  a.state, a.reason, a.retry_at = 'waiting', reason, at or 0
  save()
  return reply('waiting', reason, true, proxy, at)
end
local function release()
  if a.transporthalf and not a.harvest and n(a.transportdeadline)>now and a.transportmember then
    redis.call('ZADD','proxy:{pool}:transport_cooldowns','NX',a.transportdeadline,a.transportmember)
    redis.call('PEXPIRE','proxy:{pool}:transport_cooldowns',math.max(redis.call('PTTL','proxy:{pool}:transport_cooldowns'),n(a.transportdeadline)-now))
  end
  for _, id in ipairs(a.leases or {}) do redis.call('ZREM', leasekey(id), q.account) end
  if a.half and a.proxy and a.proxy ~= '0' then
    local p = readproxy(a.proxy, a.pversion)
    if p.owner == a.token and n(p.g) == n(a.pg) then
      p.owner, p.lease = nil, nil
      p.until_at = math.max(n(p.until_at), now + n(q.interval_ms))
      writeproxy(a.proxy, a.pversion, p)
    end
  end
  a.token, a.lease, a.leases, a.half = nil, nil, {}, false
  a.transporthalf,a.transportdeadline,a.transportmember=nil,nil,nil
end
if a.token and n(a.lease) <= now then release() end
resetrejection()
-- 关闭时刻来自持久化设置，off→on 发生在两次调度之间也必须排空旧周期。
local disabled=n(q.disabled_at_ms)
local newlydisabled=disabled>n(a.observeddisabled)
if a.active and a.protected and (newlydisabled or (not q.enabled and not a.drain)) then
  a.drain = true
  a.until_at = math.max(n(a.until_at), (disabled>0 and disabled or now) + n(a.cooldowncfg))
  a.observeddisabled=math.max(n(a.observeddisabled),disabled)
end
if not a.token and n(a.until_at) > 0 and n(a.until_at) <= now then
  a.active, a.drain, a.protected, a.until_at = false, false, false, 0
end
if q.enabled and a.active and not a.protected and not a.token then a.active=false end
local function owns()
  return a.token == q.token and n(a.g) == n(q.generation) and n(a.lease) > now
end
local function jitter(delay)
  local cap=math.min(2000,math.floor(math.max(0,delay)/4))
  if cap<=0 then return 0 end
  local value=tonumber(redis.sha1hex(tostring(a.g)..':'..tostring(a.model)..':'..tostring(now)..':'..tostring(a.attempts)):sub(1,7),16)
  return value % (cap+1)
end
local function capacity(c)
  if c.id == '0' then return true end
  local key = 'proxy:{pool}:associations:' .. c.id
  redis.call('ZREMRANGEBYSCORE', key, '-inf', math.floor(now / 1000))
  redis.call('ZREMRANGEBYSCORE', leasekey(c.id), '-inf', now)
  local members = {}
  for _, id in ipairs(redis.call('ZRANGE', key, 0, -1)) do members[id] = true end
  for _, id in ipairs(redis.call('ZRANGE', leasekey(c.id), 0, -1)) do members[id] = true end
  for _, id in ipairs(c.fixed or {}) do members[id] = true end
  local already=members[q.account]
  members[q.account] = true
  local count = 0
  for _ in pairs(members) do count = count + 1 end
  return n(q.limit) == 0 or count <= n(q.limit), count-(already and 0 or 1)
end
local function occupy(id)
  if id == '0' then return end
  redis.call('ZADD', leasekey(id), a.lease, q.account)
  redis.call('PEXPIRE', leasekey(id), math.max(redis.call('PTTL',leasekey(id)), a.lease-now+1000))
  for _, existing in ipairs(a.leases or {}) do if existing == id then return end end
  table.insert(a.leases, id)
end
local function transportmember(c) return c.id..':'..(c.affinity_version or '') end
local function transportuntil(c)
  if not c or c.id=='0' then return 0 end
  local at=n(redis.call('ZSCORE','proxy:{pool}:transport_cooldowns',transportmember(c)))
  return at>now and at or 0
end
local function guard(c, harvest)
  if c.id == '0' then return true, false, 0, '', 0 end
  local ipreason,ipuntil=ipwait(c)
  if ipreason~='' then return false,false,0,ipreason,ipuntil end
  local p = readproxy(c.id, c.version)
  if p.owner and n(p.lease) > now then return false, false, n(p.g), 'half_open_busy', n(p.lease) end
  local cooldown=transportuntil(c)
  if cooldown>now then return false,false,n(p.g),'proxy_silent',cooldown end
  if p.state == 'available' then return true, false, n(p.g), '', 0 end
  if not (q.action=='reserve' and q.pool and q.enabled) and (q.manual or rejectionbypass(c,p)) then return true,harvest,n(p.g),'',0 end
  if n(p.until_at) > now then return false, false, n(p.g), 'proxy_silent', n(p.until_at) end
  if not q.enabled then
    p.state, p.owner, p.lease, p.until_at, p.g = 'available', nil, nil, 0, n(p.g)+1
    writeproxy(c.id, c.version, p)
    return true, false, p.g, '', 0
  end
  if not harvest then return false, false, n(p.g), 'recovery_waiting_for_harvest', 0 end
  return true, true, n(p.g), '', 0
end
local function report()
  if a.reported or not a.started then return end
  a.reported, a.harvest, a.reported_at = true, q.accepted or false, now
  if not a.proxy or a.proxy == '0' then return end
  local p = readproxy(a.proxy, a.pversion)
  if n(p.g) ~= n(a.pg) or (p.owner and p.owner ~= a.token) then return end
  local current = q.policy == a.policy and q.enabled and not a.drain
  if a.half and p.owner == a.token then
    p.owner, p.lease, p.g = nil, nil, n(p.g)+1
    if q.accepted and (current or a.transporthalf) then p.state, p.until_at = 'available', 0
    else p.state, p.until_at = 'silent', now + ((q.neutral or not current) and n(q.interval_ms) or n(q.silence_ms)) end
    if p.state=='silent' then a.silenceuntil=p.until_at end
    a.rulematched=q.silence and current or false
    writeproxy(a.proxy, a.pversion, p)
  elseif q.silence and current then
    p.state, p.until_at, p.g = 'silent', now+n(q.silence_ms), n(p.g)+1
    p.policy = q.policy
    a.rulematched,a.silenceuntil=true,p.until_at
    writeproxy(a.proxy, a.pversion, p)
  end
end
`
