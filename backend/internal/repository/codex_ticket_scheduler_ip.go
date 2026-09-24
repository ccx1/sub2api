package repository

// 同一出口 IP 跨账号共享失败轮次；代次隔离冷却前及成功前的在途回调。
// 禁用记录不设过期时间，手动打票和全池提前恢复也不能解除它。
const codexSchedulerIPProtection = `
local function ipkey(ip)
  if not ip or ip=='' then return nil end
  return 'proxy:{pool}:codex:ip:'..redis.sha1hex(ip)
end
local function readip(ip)
  local key=ipkey(ip)
  local raw=key and redis.call('GET',key)
  if not raw then return {g=0,failed={},rounds=0,until_at=0,disabled=false} end
  local state=cjson.decode(raw)
  if type(state)~='table' then error('invalid proxy IP protection state') end
  if state.failed==nil then state.failed={} elseif type(state.failed)~='table' then error('invalid proxy IP protection state') end
  return state
end
local function writeip(ip,state)
  local key=ipkey(ip)
  if key then
    state.ip=ip
    redis.call('SET',key,cjson.encode(state))
  end
end
local function ipwait(c)
  if not c or not c.ip or c.ip=='' then return '',0 end
  local state=readip(c.ip)
  if state.disabled then return 'ip_disabled',0 end
  if n(state.until_at)>now then return 'ip_cooling',n(state.until_at) end
  return '',0
end
local function finiship(neutral)
  local c=a.harvest_selected
  if not q.ip_enabled or not a.started or neutral or q.policy~=a.policy or not c or not c.ip or c.ip=='' then return end
  local state=readip(c.ip)
  if state.disabled or n(state.until_at)>now or a.ip_generation==nil or n(a.ip_generation)~=n(state.g) then return end
  -- 独立业务出口的失败不能惩罚采票 IP；采票出口的质量复验失败才算本轮失败。
  local failed=q.harvest_failed or q.quality_failed
  local accepted=a.harvest and not failed
  if accepted then
    state.failed,state.failed_accounts,state.rounds,state.until_at,state.g={},0,0,0,n(state.g)+1
  elseif failed then
    local cutoff=now-math.max(1,n(q.ip_window_ms))
    for id,at in pairs(state.failed) do if n(at)<=cutoff then state.failed[id]=nil end end
    state.failed[q.account]=now
    state.last_failure_at=now
    local count=0
    for _ in pairs(state.failed) do count=count+1 end
    state.failed_accounts=count
    if count>=math.max(1,n(q.ip_failure_threshold)) then
      state.failed,state.rounds,state.g={},n(state.rounds)+1,n(state.g)+1
      state.disabled=state.rounds>=math.max(1,n(q.ip_max_rounds))
      state.until_at=state.disabled and 0 or now+math.max(1,n(q.ip_cooldown_ms))
    end
  else return end
  writeip(c.ip,state)
end
`
