package repository

// 拒收按账号跨模型连续计数；定时重试仅越过本序列造成的代理静默。
const codexSchedulerRetry = `
local function rejectionmode() return a.rejection~=nil end
local function rejectionbypass(c,p)
  local r=a.rejection
  return rejectionmode() and r.proxies and r.proxies[c.id..':'..c.version]==n(p.g)
end
local function clearattempts()
  a.attempts,a.round,a.visited=0,1,{}
  a.retries,a.authuntil={},0
  if not a.drain then a.until_at=0 end
end
local function resetrejection()
  local r=a.rejection
  if r and r.exhausted and n(r.until_at)<=now and not a.token then
    clearattempts()
    r.failures,r.until_at,r.exhausted=0,0,false
    if a.reason=='rejection_cooldown' then a.state,a.reason,a.retry_at='idle','',0 end
  end
end
local function finishrejection()
  local r=a.rejection or {failures=0,proxies={}}
  r.failures=n(r.failures)+1
  r.exhausted=r.failures>=math.max(1,n(q.rejection_max))
  r.until_at=now+(r.exhausted and n(q.rejection_cooldown_ms) or n(q.rejection_interval_ms))
  r.proxies=r.proxies or {}
  if a.rulematched and a.proxy and a.proxy~='0' then
    r.proxies[a.proxy..':'..a.pversion]=n(readproxy(a.proxy,a.pversion).g)
  end
  a.rejection=r
  a.retries,a.authuntil={},0
  if not a.drain then a.until_at=0 end
end
`
