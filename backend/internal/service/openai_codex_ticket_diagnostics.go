package service

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func captureCodexTicketAttemptPolicy(attempt *CodexTicketAttempt, account *Account, cfg config.OpenAICodexTicketConfig) {
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	attempt.LengthMode = cfg.LengthMode
	if cfg.LengthMode == config.CodexTicketLengthStrict {
		target := openAICodexTicketTargetLength(account, cfg)
		attempt.TargetLength, attempt.RejectedLengths = &target, slices.Clone(cfg.RejectedLengths)
	}
}

// 收到响应即保存数值观测；完整响应读取失败也不能把已返回票据误显示成“无票”。
func recordCodexTicketProbeResponse(input openAICodexTicketProbeInput, response *http.Response) {
	if input.Attempt == nil || response == nil {
		return
	}
	length, status := len(extractOpenAICodexTurnState(response.Header)), response.StatusCode
	if !codexTicketProbeBusiness(input) {
		input.Attempt.HarvestTicketLength, input.Attempt.HarvestHTTPStatus = &length, &status
	} else {
		input.Attempt.BusinessTicketLength, input.Attempt.BusinessHTTPStatus = &length, &status
	}
}

func codexTicketCandidateFailureReason(state string, account *Account, cfg config.OpenAICodexTicketConfig) string {
	if state == "" {
		return "ticket_missing"
	}
	if !strings.HasPrefix(state, openAICodexTicketStatePrefix) || cfg.LengthMode == config.CodexTicketLengthAuto {
		return "ticket_format_invalid"
	}
	if len(state) != openAICodexTicketTargetLength(account, cfg) {
		return "ticket_length_mismatch"
	}
	if codexTicketStateRejected(state, cfg) {
		return "ticket_length_rejected"
	}
	return "ticket_format_invalid"
}

type codexTicketProbeResponseError struct{ reason string }

func (e *codexTicketProbeResponseError) Error() string { return "codex ticket probe " + e.reason }

func codexTicketProbeResponseResult(observer *openAICodexTicketResponseObserver) error {
	if observer.failed {
		return &codexTicketProbeResponseError{reason: "response_failed"}
	}
	completed, matches := observer.Result()
	if !completed {
		return &codexTicketProbeResponseError{reason: "response_incomplete"}
	}
	if !matches {
		return &codexTicketProbeResponseError{reason: "model_mismatch"}
	}
	return nil
}
