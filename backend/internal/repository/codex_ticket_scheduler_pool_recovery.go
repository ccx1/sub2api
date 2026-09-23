package repository

// 全池冷却只恢复最早的一条；原子预留独占半开租约，验证结果决定恢复或重新冷却。
const codexSchedulerPoolRecovery = `
local function poolrecovery()
  if not q.pool then return nil,nil,0 end
  local oldest,deadline,busy,available=nil,math.huge,0,false
  local cooling=false
  local transportOldest,transportDeadline,allTransport,transportCooling=nil,math.huge,true,false
  for _,c in ipairs(q.candidates) do
    local p=readproxy(c.id,c.version)
    if p.owner and n(p.lease)>now and (busy==0 or n(p.lease)<busy) then busy=n(p.lease) end
    if c.healthy then
      local at=transportuntil(c)
      if at>now then
        transportCooling=true
        if not (p.owner and n(p.lease)>now) and capacity(c) and
            (not transportOldest or at<transportDeadline or (at==transportDeadline and n(c.id)<n(transportOldest.id))) then
          transportOldest,transportDeadline=c,at
        end
      elseif p.state=='available' then allTransport,available=false,true
      else
        allTransport=false
        cooling=true
        if not (p.owner and n(p.lease)>now) and capacity(c) and
            (not oldest or n(p.until_at)<deadline or (n(p.until_at)==deadline and n(c.id)<n(oldest.id))) then
          oldest,deadline=c,n(p.until_at)
        end
      end
    end
  end
  if allTransport and transportCooling then
    if busy>0 then return nil,'half_open_busy',busy end
    if not transportOldest then return nil,'capacity',now+n(q.interval_ms) end
    return transportOldest,nil,0,true
  end
  if not q.enabled then return nil,nil,0 end
  if available or not cooling then return nil,nil,0 end
  if busy>0 then return nil,'half_open_busy',busy end
  if not oldest then return nil,'capacity',now+n(q.interval_ms) end
  return oldest,nil,0
end
`
