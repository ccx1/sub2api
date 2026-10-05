package service

import (
	"errors"
	"net/http"
)

const GrokUnknownForbiddenReason GatewayFailureReason = "grok_unknown_forbidden"

func (a *Account) SkipGrokForbiddenPause() bool {
	if a == nil || !a.IsGrok() {
		return false
	}
	enabled, _ := a.Extra["grok_skip_forbidden_pause"].(bool)
	return enabled
}

func GrokRealtimeFailoverError(account *Account, status int, headers http.Header, body []byte) *UpstreamFailoverError {
	err := (&UpstreamFailoverError{StatusCode: status, ResponseHeaders: headers, ResponseBody: body}).WithGrokForbiddenPolicy(account)
	if isGrokContentPolicyRejection(status, body) {
		err.NextAccountAction = NextAccountStop
		err.Scope = GatewayFailureScopeRequest
	}
	return err
}

// 未知 403 的重试预算由 handler 持有，不能改变账号持久状态。
func (e *UpstreamFailoverError) WithGrokForbiddenPolicy(account *Account) *UpstreamFailoverError {
	if e == nil || e.IsCredentialFailure() || !grokSkipsForbiddenHealth(account, e.StatusCode, e.ResponseBody) {
		return e
	}
	e.Stage = GatewayFailureStageInference
	e.Scope = GatewayFailureScopeRequest
	e.Reason = GrokUnknownForbiddenReason
	e.RetryableOnSameAccount = false
	e.RequestScopedTransient = true
	return e
}

func grokSkipsForbiddenHealth(account *Account, status int, body []byte) bool {
	return account != nil && account.SkipGrokForbiddenPause() && isGrokUnknownForbidden(status, body) &&
		!(account.IsCustomErrorCodesEnabled() && account.ShouldHandleErrorCode(status)) &&
		len(matchTempUnschedulableRules(account, status, body)) == 0
}

type grokContentPolicyError struct{ message string }

func (e *grokContentPolicyError) Error() string {
	return "grok content policy rejection: " + e.message
}

func grokRequestScopedResponseError(account *Account, status int, body []byte, message string) error {
	if account == nil || !account.IsGrok() {
		return nil
	}
	if isGrokContentPolicyRejection(status, body) {
		return &grokContentPolicyError{message: message}
	}
	err := (&UpstreamFailoverError{StatusCode: status, ResponseBody: body}).WithGrokForbiddenPolicy(account)
	if err.Reason == GrokUnknownForbiddenReason {
		err.NextAccountAction = NextAccountStop
		return err
	}
	return nil
}

func isGrokRequestScopedFailure(err error) bool {
	var contentErr *grokContentPolicyError
	if errors.As(err, &contentErr) {
		return true
	}
	var failoverErr *UpstreamFailoverError
	return errors.As(err, &failoverErr) && failoverErr.Reason == GrokUnknownForbiddenReason
}
