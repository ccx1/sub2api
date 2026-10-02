package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountSchedulingIgnoresLocalEgressAndBPSInBandAuth(t *testing.T) {
	for _, existingFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_failure_%t", existingFailure), func(t *testing.T) {
			svc := &OpenAIGatewayService{openaiAccountStats: newOpenAIAccountRuntimeStats(), rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true")}
			account := excelAccount()
			ttft := 450
			svc.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", true, &ttft)
			if existingFailure {
				svc.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", false, nil, errors.New("upstream failure"))
			}
			beforeRate, beforeTTFT, beforeKnown := svc.openaiAccountStats.snapshot(account.ID)
			for _, err := range []error{
				&RandomProxyUnavailableError{Policy: RandomProxyEmptyPoolPolicyReject},
				ErrRandomProxyChanged,
				&basispoints.UpstreamFailure{Status: http.StatusUnauthorized},
				&basispoints.UpstreamFailure{Status: http.StatusForbidden},
				&basispoints.UpstreamFailure{Status: http.StatusTooManyRequests},
				errExcelBPSRateLimitedAfterOutput,
			} {
				for _, success := range []bool{false, true} {
					require.False(t, svc.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", success, &ttft, fmt.Errorf("forward: %w", err)))
				}
			}
			rate, latency, known := svc.openaiAccountStats.snapshot(account.ID)
			require.Equal(t, beforeRate, rate, "local and in-band auth failures must neither penalize nor heal account errors")
			require.Equal(t, beforeTTFT, latency)
			require.Equal(t, beforeKnown, known)
			svc.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", false, nil, errors.New(ErrRandomProxyUnavailable.Error()))
			svc.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", false, nil, &basispoints.UpstreamFailure{Status: http.StatusServiceUnavailable})
			rate, _, _ = svc.openaiAccountStats.snapshot(account.ID)
			require.InDelta(t, 0.2+0.8*(0.2+0.8*beforeRate), rate, 1e-12, "real failures and lookalike text still count")
		})
	}
}
