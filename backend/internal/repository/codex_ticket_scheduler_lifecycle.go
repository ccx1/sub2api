package repository

const codexSchedulerActions = `
if q.action == 'finish' and not owns() then save(); return reply('idle','stale_completion',false) end
if not owns() then return waiting('reservation_expired',0) end
if q.action == 'start' or q.action == 'stage' or q.action=='publish' then
  if q.policy ~= a.policy then return waiting('controls_changed',0) end
  if not q.manual and a.drain and n(a.until_at)>now then return waiting('protection_draining',a.until_at) end
end
if q.action=='publish' then
  if not a.started then return waiting('attempt_not_started',0) end
  return reply('running','',false)
end
if q.action == 'start' then
  local transportAt=transportuntil(a.harvest_selected)
  if transportAt>now then return waiting('proxy_silent',transportAt) end
  if a.started then return reply('running','',false) end
  if not q.manual and n(a.until_at)>now then return waiting(a.drain and 'protection_draining' or 'account_cooldown',a.until_at) end
  if a.proxy ~= '0' then
    local p=readproxy(a.proxy,a.pversion)
    if a.half then
      if p.owner~=a.token or n(p.g)~=n(a.pg) then return waiting('reservation_expired',0) end
    elseif p.state~='available' and not q.manual then return waiting('proxy_silent',n(p.until_at))
    elseif p.owner and n(p.lease)>now and p.owner~=a.token then return waiting('half_open_busy',n(p.lease)) end
  end
  if q.enabled and not q.manual and not rejectionmode() and n(a.attempts)>=n(a.maxattempts) then return waiting('account_cooldown',n(a.until_at)) end
  a.started,a.started_at=true,now
  a.learning_turns=a.learning_turns or {}
  a.learning_turns[a.model]=n(a.learning_turns[a.model])+1
  if a.pool then confirmpin(a.follow_business and 'business' or 'harvest',a.harvest_selected) end
  if a.follow_business then bindbusiness(a.harvest_selected) end
  if a.nextRound then a.round,a.visited=n(a.round)+1,{} end
  a.nextRound=false
  a.charged=q.enabled
  if a.charged then a.attempts=n(a.attempts)+1 end
  a.cursors[a.cursor],a.lastmodel=a.proxy,a.model
  a.cursors['last:' .. a.model]=a.proxy
  if a.pool and q.enabled and not a.harvestpinned then a.visited[a.proxy]=true end
  a.state,a.reason='running',''
  save();return reply('running','',false)
end
if q.action == 'stage' then
  if not a.started then return waiting('attempt_not_started',0) end
  local c=q.candidate
  if a.follow_business and (c.id~=a.proxy or c.version~=a.pversion) then return waiting('proxy_changed',0,c.id) end
  local ok,half,pg,why,at=guard(c,false)
  if not ok then
    a.deferredproxy,a.deferredversion,a.deferredcandidate,a.deferredcapacity=c.id,c.version,c,false
    a.deferreduntil=at
    return waiting(why,at,c.id)
  end
  if not capacity(c) then
    a.deferredproxy,a.deferredversion,a.deferredcandidate,a.deferredcapacity=c.id,c.version,c,true
    a.deferreduntil=now+n(q.interval_ms)
    return waiting('capacity',a.deferreduntil,c.id)
  end
  occupy(c.id)
  if not a.business or a.business.id~=c.id or a.business.version~=c.version then a.business_started_at=now end
  a.business=c
  confirmpin('business',c)
  save();return reply('running','',false,c.id)
end
if q.action == 'report' then
  report();save();return reply('running','',false)
end
if q.action == 'finish' then
  local neutral=q.outcome=='canceled' or q.outcome=='cancelled' or q.outcome=='controls_changed' or q.outcome=='verification_deferred'
  if not a.reported then q.neutral=neutral or not q.silence;report() end
  local success=q.outcome=='verified' or q.outcome=='published' or q.outcome=='success'
  local rejected=q.outcome=='ticket_rejected' and a.started
  finishpins(success,neutral)
  finishlearning(success,neutral)
  finishsession(success,neutral)
  if a.started and neutral and (a.charged or (a.charged==nil and a.protected)) then a.attempts=math.max(0,n(a.attempts)-1) end
  if a.started and not neutral and not rejected then a.rejection=nil end
  if a.started and success then clearattempts();a.rejection=nil
  elseif rejected then finishrejection()
  elseif a.started and q.enabled then
    if success then a.retries[a.model]=nil
    elseif not neutral then
      local r=a.retries[a.model] or {failures=0,until_at=0}
      if r.exhausted and n(r.until_at)<=now then r.failures=0 end
      if n(r.updated)>0 and n(r.updated)+86400000<=now then r.failures=0 end
      r.failures=n(r.failures)+1
      r.updated=now
      local delay=n(q.interval_ms)
      if #q.backoff>0 then delay=n(q.backoff[math.min(r.failures,#q.backoff)])*1000 end
      r.exhausted=n(q.retry_max)>0 and r.failures>=n(q.retry_max)
      if r.exhausted then delay=n(q.retry_exhausted_ms) end
      r.until_at=math.max(n(r.until_at),now+delay+jitter(delay))
      a.retries[a.model]=r
    end
    if q.retry_scope=='account' then a.authuntil=math.max(n(a.authuntil),n(q.retry_ms)) end
    if q.retry_scope=='model' then
      local r=a.retries[a.model] or {failures=0,until_at=0}
      r.harduntil=math.max(n(r.harduntil),n(q.retry_ms))
      a.retries[a.model]=r
    end
  end
  local roundsDone=roundsexhausted()
  if q.complete and not a.drain then a.active=false
  elseif not success and not rejected and not neutral and a.protected and (n(a.attempts)>=n(a.maxattempts) or roundsDone) then a.until_at=math.max(n(a.until_at),now+n(a.cooldowncfg)) end
  release()
  if q.outcome=='verification_deferred' then a.deferreduntil=math.max(n(a.deferreduntil),n(q.deferred_until)) end
  a.state,a.reason,a.retry_at='idle',q.outcome or '',0
  if n(a.until_at)>now then a.state,a.reason,a.retry_at='waiting',a.drain and 'protection_draining' or 'account_cooldown',a.until_at end
  if n(a.authuntil)>math.max(now,n(a.retry_at)) then a.state,a.reason,a.retry_at='waiting','account_retry',a.authuntil end
  local modelRetry=a.retries[a.model]
  local modelAt=modelRetry and math.max(n(modelRetry.until_at),n(modelRetry.harduntil)) or 0
  if modelAt>math.max(now,n(a.retry_at)) then
    a.state,a.reason,a.retry_at='waiting','model_backoff',modelAt
  end
  if q.outcome=='verification_deferred' and n(a.until_at)<=now then
    a.state,a.reason,a.retry_at='waiting','verification_deferred',n(q.deferred_until)
  end
  if rejected and not a.drain then
    a.state,a.reason,a.retry_at='waiting',a.rejection.exhausted and 'rejection_cooldown' or 'rejection_retry',a.rejection.until_at
  end
  save();return reply(a.state,a.reason,false,nil,n(a.retry_at))
end
return waiting('invalid_scheduler_action',0)
`
