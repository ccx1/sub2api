package handler

import "github.com/Wei-Shaw/sub2api/internal/service"

type grokForbiddenFailoverBudget struct {
	active      bool
	switchLimit int
}

// 备用账号即使返回另一种错误，也不能扩大最初未知 403 的预算。
func (b *grokForbiddenFailoverBudget) canRetry(err *service.UpstreamFailoverError, switchCount int) bool {
	if err == nil || !err.ShouldRetryNextAccount() {
		return false
	}
	if err.Reason == service.GrokUnknownForbiddenReason && !b.active {
		b.active = true
		b.switchLimit = switchCount + 1
	}
	return !b.active || switchCount < b.switchLimit
}
