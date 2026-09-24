package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 复验选路保留独立成功出口，但仍占用原业务关联与同一账号容量。
func (a *ProxyPoolAllocator) SelectCodexTicketBusinessProxy(ctx context.Context, selection service.ProxyPoolSelection, reservation *service.CodexTicketReservation) (*service.Proxy, error) {
	if reservation == nil || selection.AccountID != reservation.AccountID {
		return nil, errors.New("invalid codex ticket business identity")
	}
	q, err := a.reservationInput(ctx, "business", reservation)
	if err != nil {
		return nil, err
	}
	candidates, proxies, err := a.codexSchedulerCandidates(ctx, service.CodexTicketReserveRequest{Selection: selection, PoolMode: true})
	if err != nil {
		return nil, err
	}
	limit, err := a.settings.GetProxyPoolMaxAccounts(ctx)
	if err != nil || limit < 0 || limit > 10000 {
		return nil, errors.New("read codex ticket proxy capacity failed")
	}
	q["candidates"], q["limit"] = candidates, limit
	q["business_lease_seconds"] = int64(proxyPoolLeaseTTL.Seconds())
	result, err := a.runCodexScheduler(ctx, reservation.AccountID, q)
	if err != nil {
		return nil, err
	}
	return proxies[result.ProxyID], nil
}

const codexSchedulerBusiness = `
if q.action=='business' then
  if not owns() then return waiting('reservation_expired',0) end
  if q.policy~=a.policy then return waiting('controls_changed',0) end
  if not q.manual and a.drain and n(a.until_at)>now then return waiting('protection_draining',a.until_at) end
  if not a.started then return waiting('attempt_not_started',0) end
  local candidates=q.candidates
  local pin=eligiblepin('business',candidates)
  if pin and ipwait(pin)~='' then pin=nil end
  if a.follow_business then
    if a.proxy=='0' and #candidates==0 then save();return reply('running','pool_empty',false,'0') end
    pin=nil
    for _,c in ipairs(candidates) do if c.id==a.proxy and c.version==a.pversion then pin=c;break end end
    if not pin then return waiting('proxy_changed',now+n(q.interval_ms),'0') end
  end
  local affinity='proxy:{pool}:affinity:'..q.account
  local previous=redis.call('HGET',affinity,'proxy_id')
  local previousVersion=redis.call('HGET',affinity,'version')
  local chosen,why,at=nil,'proxy_unhealthy',now+n(q.interval_ms)
  if pin then
    local ok,half,pg,reason,retry=pinready(pin,false)
    if not ok then
      a.deferredproxy,a.deferredversion,a.deferredcandidate=pin.id,pin.version,pin
      a.deferredcapacity,a.deferreduntil=reason=='capacity',retry
      return waiting(reason,retry,pin.id)
    end
    chosen=pin
  else
    candidates=avoidfailed('business',candidates)
    local bestTier,bestCount,bestQuality=math.huge,math.huge,math.huge
    local reusable=nil
    local haveWait=false
    local function choose(list)
      for _,c in ipairs(randomorder(list,'business')) do
        local ok,half,pg,reason,retry=pinready(c,false)
        local failed=redis.call('ZSCORE','proxy:{pool}:failures:'..q.account,c.id)
        if failed and tonumber(failed)<=math.floor(now/1000) then failed=false end
        if ok and not failed then
          if c.id==previous and c.affinity_version==previousVersion then reusable=c end
          local tier=c.degraded and 1 or 0
          local _,count=capacity(c)
          if tier<bestTier or (not a.businessavoid and tier==bestTier and (count<bestCount or (count==bestCount and n(c.quality)<bestQuality))) then
            chosen,bestTier,bestCount,bestQuality=c,tier,count,n(c.quality)
          end
        elseif not ok and (not haveWait or retry<at) then why,at,haveWait=reason,retry,true end
      end
    end
    choose(candidates)
    if not chosen and a.businessavoid and #q.candidates>0 then
      local ready,retry=fallbackready('business')
      if not ready then return waiting('proxy_switch_waiting',retry,'0') end
      choose(q.candidates)
    end
    if reusable and not a.businessavoid and (not reusable.degraded or bestTier~=0) then chosen=reusable end
  end
  if not chosen then
    if #q.candidates>0 then
      a.deferreduntil=at
      return waiting(why,at,'0')
    end
    if previous then
      redis.call('ZREM','proxy:{pool}:associations:'..previous,q.account)
      redis.call('SREM','proxy:{pool}:bound_accounts:'..previous,q.account)
      redis.call('DEL',affinity)
    end
    save();return reply('running','pool_empty',false,'0')
  end
  bindbusiness(chosen)
  occupy(chosen.id)
  a.deferredproxy,a.deferredversion,a.deferredcandidate,a.deferredcapacity,a.deferreduntil=nil,nil,nil,nil,nil
  save();return reply('running','',false,chosen.id)
end
`
