package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// codexSafetyBufferingRejectReason 是共享的 Safety Buffering 否决判定。
//
// 账号被 cyber 风控标记后，服务端会在响应头里带上 Safety Buffering 标记
// （X-Codex-Safety-Buffering-Enabled=true），并可能声明一个降级模型
// （X-Codex-Safety-Buffering-Faster-Model 非空）。即便本次终态模型名恰好匹配，
// 这类链路随时会被 reroute 降级，因此按配置的严格度判为不合格。
//
// 打票探测与后台测智共用此函数，保证两条链路的验收口径一致。
// 返回非空字符串表示应否决，字符串即为失败原因；返回空表示放行。
func codexSafetyBufferingRejectReason(mode string, signals *CodexTicketSignals) string {
	if signals == nil {
		return ""
	}
	switch mode {
	case config.CodexTicketRejectSafetyBufferingAny:
		if signals.SafetyBufferingEnabled != nil && *signals.SafetyBufferingEnabled {
			return "safety_buffering_flagged"
		}
		if strings.TrimSpace(signals.FasterModel) != "" {
			return "safety_buffering_faster_model"
		}
	case config.CodexTicketRejectSafetyBufferingFasterModel:
		if strings.TrimSpace(signals.FasterModel) != "" {
			return "safety_buffering_faster_model"
		}
	}
	return ""
}

// codexSafetyBufferingReasonConfirmed 报告某个失败原因是否来自 Safety Buffering
// 否决闸，供测智准入/熔断把它当作确认失败一并处理。
func codexSafetyBufferingReasonConfirmed(reason string) bool {
	return reason == "safety_buffering_flagged" || reason == "safety_buffering_faster_model"
}
