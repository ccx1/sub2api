package repository

const codexSchedulerReserve = `
if q.action == 'reserve' then
  if a.token then return waiting('account_busy', n(a.lease)) end
  local businessUntil=businessholduntil()
  if businessUntil>now then return waiting('business_active',businessUntil) end
  if not q.manual and rejectionmode() and n(a.rejection.until_at)>now then
    return waiting(a.rejection.exhausted and 'rejection_cooldown' or 'rejection_retry',a.rejection.until_at)
  end
  if not q.manual and n(a.until_at) > now then return waiting(a.drain and 'protection_draining' or 'account_cooldown', a.until_at) end
  if not q.manual and n(a.authuntil) > now then return waiting('account_retry', a.authuntil) end
  if q.manual then a.deferredproxy,a.deferredversion,a.deferredcandidate,a.deferredcapacity,a.deferreduntil=nil,nil,nil,nil,nil end
  if n(a.deferreduntil)>now then return waiting('verification_deferred',a.deferreduntil,a.deferredproxy) end
  if a.deferredproxy and a.deferredproxy ~= '0' then
    if q.deferred_invalid then
      a.deferredproxy,a.deferredversion,a.deferredcandidate,a.deferredcapacity,a.deferreduntil=nil,nil,nil,nil,nil
      return waiting('business_proxy_unavailable',now+n(q.interval_ms))
    end
    if q.deferred_candidate and q.deferred_candidate.id==a.deferredproxy then
      a.deferredcandidate=q.deferred_candidate
      a.deferredversion=q.deferred_candidate.version
    end
    if n(a.deferreduntil)>now then return waiting('verification_deferred',a.deferreduntil,a.deferredproxy) end
    if a.deferredcapacity and a.deferredcandidate and not capacity(a.deferredcandidate) then
      a.deferreduntil=now+n(q.interval_ms)
      return waiting('verification_deferred',a.deferreduntil,a.deferredproxy)
    end
    local p = readproxy(a.deferredproxy, a.deferredversion)
    if p.state ~= 'available' and (q.enabled or n(p.until_at)>now or (p.owner and n(p.lease)>now)) then
      local same = false
      for _, c in ipairs(q.candidates) do if c.id == a.deferredproxy and c.version == a.deferredversion then same=true end end
      if not same or n(p.until_at)>now or (p.owner and n(p.lease)>now) then
        return waiting('verification_deferred', math.max(n(p.until_at), n(p.lease)), a.deferredproxy)
      end
    end
    a.deferredproxy,a.deferredversion,a.deferredcandidate,a.deferredcapacity,a.deferreduntil=nil,nil,nil,nil,nil
  end
  a.deferreduntil=nil
  local models, ready, earliest = q.models, {}, 0
  for _, model in ipairs(models) do
    local r = a.retries[model]
    local at = q.manual and 0 or (r and math.max(n(r.harduntil),n(r.until_at)) or 0)
    if at<=now then table.insert(ready, model)
    elseif earliest==0 or at<earliest then earliest=at end
  end
  if #ready == 0 then return waiting('model_backoff', earliest) end
  if q.manual then
    local allowed=false
    for _,model in ipairs(ready) do if model==q.model then allowed=true end end
    if not allowed then
      local r=a.retries[q.model]
      return waiting('model_backoff',r and n(r.harduntil) or earliest)
    end
  end
  local target = ready[1]
  for _, model in ipairs(ready) do if model > (a.lastmodel or '') then target=model; break end end
  if q.manual then for _,model in ipairs(ready) do if model==q.model then target=q.model;break end end end
  if q.model ~= target then a.model=target; return waiting('model_turn', 0) end
  local role=q.follow_business and 'business' or 'harvest'
  local harvestPin=q.pool and eligiblepin(role,q.candidates) or nil
  if harvestPin then
    local ok,half,_,why=guard(harvestPin,true)
    if (not ok and why=='proxy_silent') or (ok and half) then harvestPin=nil end
  end
  if #q.candidates == 0 then return waiting('pool_empty', 0) end
  if not a.active then
    a.g, a.active, a.protected, a.attempts, a.round = n(a.g)+1, true, q.enabled, 0, 1
    a.created_ms,a.observeddisabled=now,n(q.disabled_at_ms)
    a.maxattempts, a.maxrounds, a.cooldowncfg = q.max_attempts, q.max_rounds, q.cooldown_ms
    a.visited, a.snapshot, a.targets = {}, {}, models
    for _, c in ipairs(q.candidates) do table.insert(a.snapshot, c.id) end
  end
  if q.enabled and not q.manual and not rejectionmode() and a.protected and n(a.attempts)>=n(a.maxattempts) then
    a.until_at=now+n(a.cooldowncfg); return waiting('account_cooldown', a.until_at)
  end
  local candidates, nextRound = q.candidates, false
  if q.pool and not harvestPin then
    candidates=avoidfailed(role,candidates)
    if #candidates==0 then
      local ready,at=fallbackready(role)
      if not ready then return waiting('proxy_switch_waiting',at) end
      candidates=q.candidates
    end
  end
  if q.pool and q.enabled and not harvestPin then
    -- 保留本轮已访问记录，但以实时池更新快照，让新增代理立即参与。
    a.snapshot={}
    for _,c in ipairs(q.candidates) do table.insert(a.snapshot,c.id) end
    local remaining={}
    for _, c in ipairs(candidates) do if not a.visited[c.id] then table.insert(remaining,c) end end
    if #remaining==0 then
      if not q.manual and not rejectionmode() and n(a.round)>=n(a.maxrounds) then a.until_at=now+n(a.cooldowncfg); return waiting('rounds_exhausted',a.until_at) end
      nextRound=true
    else candidates=remaining end
  end
  local cursorKey=q.model .. ':' .. q.pool_version
  local last=a.cursors[cursorKey] or a.cursors['last:' .. q.model]
  if q.strategy~='round_robin' then
    local business=redis.call('HGET','proxy:{pool}:affinity:' .. q.account,'proxy_id')
    if business then last=business end
  end
  local ordered=randomorder(candidates,role)
  if (q.follow_business or q.strategy ~= 'round_robin') and not a[role..'avoid'] then
    local previous=redis.call('HGET','proxy:{pool}:affinity:'..q.account,'proxy_id') or last
    for i,c in ipairs(ordered) do if c.id==previous then table.remove(ordered,i);table.insert(ordered,1,c);break end end
  end
  local chosen, half, pg, retry, reason=nil,false,0,0,'proxy_unhealthy'
  local recovery,recoveryReason,recoveryAt,transportRecovery=poolrecovery()
  if recoveryReason then return waiting(recoveryReason,recoveryAt) end
  local function choose(list,allowHalf)
    for tier=0,1 do
      for _, c in ipairs(list) do
        if c.healthy and (c.degraded and 1 or 0)==tier then
          local ok,h,g,why,at=guard(c,true)
          if ok and (not h or allowHalf) and capacity(c) then chosen,half,pg=c,h,g;break end
          if ok then why,at=(h and not allowHalf) and 'recovery_waiting_for_harvest' or 'capacity',now+n(q.interval_ms) end
          if retry==0 or (at>now and at<retry) then reason,retry=why,at end
        end
      end
      if chosen then break end
    end
  end
  if recovery then
    chosen,half,pg=recovery,true,n(readproxy(recovery.id,recovery.version).g)
    if harvestPin and harvestPin.id~=chosen.id then harvestPin=nil end
  elseif harvestPin then
    local ok,h,g,why,at=pinready(harvestPin,true)
    if not ok then return waiting(why,at,harvestPin.id) end
    chosen,half,pg=harvestPin,h,g
  else
    choose(ordered)
    local halfCandidates=ordered
    if not chosen and a[role..'avoid'] then
      local ready,at=fallbackready(role)
      if not ready then return waiting('proxy_switch_waiting',at) end
      halfCandidates=randomorder(q.candidates,role)
      choose(halfCandidates)
    end
    if not chosen then choose(halfCandidates,true) end
  end
  if not chosen then return waiting(reason,retry) end
  a.token,a.lease,a.started,a.reported,a.harvest=q.token,now+n(q.lease_ms),false,false,false
  a.started_at,a.reported_at,a.business_started_at=nil,nil,nil
  a.charged=false
  a.nextRound=nextRound
  a.harvestpinned,a.business=harvestPin~=nil,nil
  a.harvest_selected,a.follow_business=chosen,q.follow_business
  a.session_mode,a.session_epoch=q.session_mode,sessionepoch(q.session_mode,q.model)
  a.model,a.proxy,a.pversion,a.pg,a.half=q.model,chosen.id,chosen.version,pg,half
  a.transporthalf=transportRecovery or false
  a.rulematched,a.silenceuntil,a.harvesthalf=false,0,half
  a.policy,a.cursor,a.pool,a.leases=q.policy,cursorKey,q.pool,{}
  if half then
    local p=readproxy(chosen.id,chosen.version)
    p.owner,p.lease=q.token,a.lease
    if transportRecovery then
      a.transportdeadline,a.transportmember=transportuntil(chosen),transportmember(chosen)
      redis.call('ZREM','proxy:{pool}:transport_cooldowns',a.transportmember)
    end
    writeproxy(chosen.id,chosen.version,p)
  end
  occupy(chosen.id)
  a.state,a.reason,a.retry_at='reserved','',0
  save()
  return reply('reserved','',false)
end
`
