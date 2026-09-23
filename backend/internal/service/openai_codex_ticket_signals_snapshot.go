package service

// 撤销记录异步落库，冻结快照不能继续共享响应观察器的可变切片/指针。
func cloneCodexTicketSignals(value *CodexTicketSignals) *CodexTicketSignals {
	if value == nil {
		return nil
	}
	copy := *value
	copy.SafetyBufferingEnabled = cloneCodexTicketSignalValue(value.SafetyBufferingEnabled)
	copy.Quota = make([]CodexTicketQuotaSignal, len(value.Quota))
	for i, q := range value.Quota {
		q.UsedPercent = cloneCodexTicketSignalValue(q.UsedPercent)
		q.ResetAt = cloneCodexTicketSignalValue(q.ResetAt)
		q.ResetAfterSeconds = cloneCodexTicketSignalValue(q.ResetAfterSeconds)
		q.WindowMinutes = cloneCodexTicketSignalValue(q.WindowMinutes)
		q.LimitReached = cloneCodexTicketSignalValue(q.LimitReached)
		copy.Quota[i] = q
	}
	return &copy
}

func cloneCodexTicketSignalValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
