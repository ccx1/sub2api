package repository

// 首次出站确认出口；每个模型独立累计连续失败，任一模型达到阈值才换绑。
const codexSchedulerPins = `
local function eligiblepin(role,candidates)
  local pin=a[role..'pin'] or a[role..'candidate']
  if not pin then return nil end
  for _,c in ipairs(candidates) do
    if c.id==pin.id and c.version==pin.version then return c end
  end
  a[role..'pin'],a[role..'candidate']=nil,nil
  return nil
end
local function randomorder(candidates,role)
  local ordered={}
  for _,c in ipairs(candidates) do table.insert(ordered,c) end
  if role then return learningorder(ordered,role) end
  table.sort(ordered,function(left,right)
    return redis.sha1hex(q.random_seed..':'..left.id)<redis.sha1hex(q.random_seed..':'..right.id)
  end)
  return ordered
end
local function avoidfailed(role,candidates)
  local avoided=a[role..'avoid']
  if not avoided then return candidates end
  local result={}
  for _,c in ipairs(candidates) do if c.id~=avoided then table.insert(result,c) end end
  return result
end
local function fallbackready(role)
  if q.manual or rejectionmode() then return true,0 end
  local key=role..'avoiduntil'
  -- 旧排除状态首次重试时补期限，后续等待不得不断延长。
  if n(a[key])==0 then a[key]=now+n(q.interval_ms) end
  return n(a[key])<=now,n(a[key])
end
local function pinready(c,harvest)
  if not c.healthy then return false,false,0,'proxy_unhealthy',now+n(q.interval_ms) end
  local ok,half,pg,why,at=guard(c,harvest)
  if not ok then return false,half,pg,why,at>now and at or now+n(q.interval_ms) end
  if not capacity(c) then return false,false,pg,'capacity',now+n(q.interval_ms) end
  return true,half,pg,'',0
end
local function clearbusinessaffinity(candidate)
  local affinity='proxy:{pool}:affinity:'..q.account
  if redis.call('HGET',affinity,'proxy_id')~=candidate.id or redis.call('HGET',affinity,'version')~=candidate.affinity_version then return end
  redis.call('DEL',affinity)
  redis.call('ZREM','proxy:{pool}:associations:'..candidate.id,q.account)
  redis.call('SREM','proxy:{pool}:bound_accounts:'..candidate.id,q.account)
end
local function rejectpin(role,candidate)
  a[role..'pin'],a[role..'candidate'],a[role..'avoid']=nil,nil,candidate.id
  a[role..'avoiduntil']=now+n(q.interval_ms)
  if role=='business' then clearbusinessaffinity(candidate) end
end
local function confirmpin(role,candidate)
  if not candidate or candidate.id=='0' then return end
  local key=role..'pin'
  local pin=a[key] or a[role..'candidate']
  if not pin or pin.id~=candidate.id or pin.version~=candidate.version then
    pin={id=candidate.id,version=candidate.version,failure_version=1,model_failures={}}
  end
  a[key]=pin
  a[role..'avoid'],a[role..'avoiduntil'],a[role..'candidate']=nil,nil,nil
end
local function updatepin(role,candidate,accepted,failed)
  if not candidate or candidate.id=='0' then return end
  local pin=a[role..'pin']
  if not pin or pin.id~=candidate.id or pin.version~=candidate.version then return end
  -- 旧聚合次数无法可靠归属模型，升级后从各模型零次开始，避免误切。
  if n(pin.failure_version)~=1 or type(pin.model_failures)~='table' then
    pin.failure_version,pin.model_failures,pin.failures=1,{},nil
  end
  if accepted then pin.model_failures[a.model],pin.last_success=nil,now
  elseif failed then
    pin.model_failures[a.model]=n(pin.model_failures[a.model])+1
    if pin.model_failures[a.model]>=n(q.pin_threshold) then rejectpin(role,candidate) end
  end
end
local function bindbusiness(candidate)
  if candidate.id=='0' then return end
  local affinity='proxy:{pool}:affinity:'..q.account
  local previous=redis.call('HGET',affinity,'proxy_id')
  local previousVersion=redis.call('HGET',affinity,'version')
  if previous and previous~=candidate.id then
    redis.call('ZREM','proxy:{pool}:associations:'..previous,q.account)
    redis.call('SREM','proxy:{pool}:bound_accounts:'..previous,q.account)
  end
  local since=redis.call('HGET',affinity,'since')
  if previous~=candidate.id or previousVersion~=candidate.affinity_version then
    since=math.floor(now/1000)
    redis.call('HDEL',affinity,'failure_count','failure_since')
  end
  redis.call('HSET',affinity,'proxy_id',candidate.id,'version',candidate.affinity_version,'since',since or math.floor(now/1000))
  redis.call('SADD','proxy:{pool}:bound_accounts:'..candidate.id,q.account)
  local association='proxy:{pool}:associations:'..candidate.id
  redis.call('ZADD',association,math.floor(now/1000)+n(q.business_lease_seconds),q.account)
  redis.call('EXPIRE',association,n(q.business_lease_seconds))
end
local function finishpins(success,neutral)
  if not a.started or q.policy~=a.policy or a.drain then return end
  -- 连续质量复验失败淘汰实际采票出口，不能被此前的采票成功清零。
  if q.quality_failed and not neutral then
    if a.pool and a.harvest_selected and a.harvest_selected.id~='0' then
      rejectpin(a.follow_business and 'business' or 'harvest',a.harvest_selected)
    end
    return
  end
  if a.follow_business then
    updatepin('business',a.harvest_selected,q.business_succeeded or success,not neutral and (q.harvest_failed or q.business_failed))
  else
    if a.pool then updatepin('harvest',a.harvest_selected,a.harvest,not neutral and q.harvest_failed) end
    updatepin('business',a.business,q.business_succeeded,not neutral and q.business_failed)
  end
end
`
