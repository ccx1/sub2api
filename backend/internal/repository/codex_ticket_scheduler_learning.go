package repository

// 节点学习复用调度 Redis，按账号、模型、角色和出口版本隔离，闲置 30 天后淘汰。
// 冷却仍由既有 guard/poolrecovery 决定；学习排序不能绕过 pin、容量或半开租约。
const codexSchedulerLearning = `
local function learningkey(role,c,model)
  return 'proxy:{pool}:codex:learning:'..q.account..':'..redis.sha1hex(model)..':'..role..':'..c.id..':'..c.version
end
local function readlearning(role,c,model)
  local raw=redis.call('GET',learningkey(role,c,model))
  if not raw then return {} end
  local ok,value=pcall(cjson.decode,raw)
  return ok and type(value)=='table' and value or {}
end
local function learnnode(role,c,accepted,failed,elapsed)
  if not c or c.id=='0' or (not accepted and not failed) then return end
  local s=readlearning(role,c,a.model)
  if failed then
    s.failures,s.consecutive_failures=n(s.failures)+1,n(s.consecutive_failures)+1
    s.last_result=q.quality_failed and role==(a.follow_business and 'business' or 'harvest') and 'quality_failed' or 'failed'
  else
    s.successes,s.consecutive_failures,s.last_success=n(s.successes)+1,0,now
    s.last_result='success'
  end
  if elapsed>0 then
    s.latency_ms=n(s.latency_samples)>0 and math.floor(n(s.latency_ms)*0.75+elapsed*0.25) or elapsed
    s.latency_samples=n(s.latency_samples)+1
  end
  local p=readproxy(c.id,c.version)
  s.cooldown_until=math.max(n(p.until_at),transportuntil(c),a[role..'avoid']==c.id and n(a[role..'avoiduntil']) or 0)
  s.updated_at=now
  redis.call('SET',learningkey(role,c,a.model),cjson.encode(s),'PX',2592000000)
end
local function finishlearning(success,neutral)
  if not a.started or neutral or q.policy~=a.policy or a.drain then return end
  local elapsed=n(a.started_at)>0 and math.max(0,(n(a.reported_at)>0 and n(a.reported_at) or now)-n(a.started_at)) or 0
  local harvestFailed=q.harvest_failed or q.quality_failed
  if a.follow_business then
    learnnode('business',a.harvest_selected,a.harvest or q.business_succeeded or success,harvestFailed or q.business_failed,elapsed)
  else
    learnnode('harvest',a.harvest_selected,a.harvest,harvestFailed,elapsed)
    local businessElapsed=n(a.business_started_at)>0 and math.max(0,now-n(a.business_started_at)) or 0
    learnnode('business',a.business,q.business_succeeded,q.business_failed,businessElapsed)
  end
end
local function learningorder(candidates,role)
  local model=q.model or a.model
  local stats={}
  for _,c in ipairs(candidates) do stats[c.id]=readlearning(role,c,model) end
  local turns=a.learning_turns or {}
  local explore=n(turns[model])%5==4
  table.sort(candidates,function(left,right)
    local l,r=stats[left.id],stats[right.id]
    local ln,rn=n(l.successes)+n(l.failures),n(r.successes)+n(r.failures)
    if explore and (ln==0)~=(rn==0) then return ln==0 end
    local lr,rr=(n(l.successes)+1)/(ln+2),(n(r.successes)+1)/(rn+2)
    if lr~=rr then return lr>rr end
    local ll=n(l.latency_samples)>0 and n(l.latency_ms) or math.huge
    local rl=n(r.latency_samples)>0 and n(r.latency_ms) or math.huge
    if ll~=rl then return ll<rl end
    return redis.sha1hex(q.random_seed..':'..left.id)<redis.sha1hex(q.random_seed..':'..right.id)
  end)
  return candidates
end
`
