package service

import (
	"encoding/json"
)

// 保护专用更新只迁移原本匹配的票据绑定，开关、票值和有效期保持独立。
// 仓储在行锁下再次执行，确保使用最新持久票，不覆盖并发采集或撤票。
func preserveProtectionCodexTickets(current *Account, extra map[string]any) {
	if !isOpenAICodexTicketAccount(current) {
		return
	}
	next := *current
	next.Extra = extra
	if openAICodexTicketAccountBinding(current) == openAICodexTicketAccountBinding(&next) {
		return
	}
	binding, _ := json.Marshal(openAICodexTicketAccountBinding(&next))
	for key, value := range current.Extra {
		if !IsOpenAICodexTicketExtraKey(key) {
			continue
		}
		raw, err := json.Marshal(value)
		if err == nil {
			extra[key] = rebindProtectionCodexTicket(raw, current, binding)
		}
	}
}

func rebindProtectionCodexTicket(raw json.RawMessage, current *Account, binding json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	var ticket openAICodexTicket
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &ticket) != nil || fields == nil {
		return raw
	}
	if !ticket.Revoked && ticket.AccountBinding != "" && ticket.accountCompatible(current) {
		fields["account_binding"] = binding
	}
	if standby, ok := fields["standby"]; ok {
		fields["standby"] = rebindProtectionCodexTicket(standby, current, binding)
	}
	var reserve []json.RawMessage
	if json.Unmarshal(fields["reserve"], &reserve) == nil {
		for index, slot := range reserve {
			reserve[index] = rebindProtectionCodexTicket(slot, current, binding)
		}
		fields["reserve"], _ = json.Marshal(reserve)
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return encoded
}
