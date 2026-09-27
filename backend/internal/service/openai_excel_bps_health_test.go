package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExcelBPSRateLimitDoesNotAffectSchedulerHealth(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	svc := &OpenAIGatewayService{
		openaiAccountStats: stats,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
	}
	require.NotNil(t, svc.getOpenAIAccountScheduler(context.Background()))
	account := excelAccount()
	stats.report(account.ID, false, nil)
	before, _, _ := stats.snapshot(account.ID)
	for _, err := range []error{
		newExcelBPSRateLimitedFailoverError("30"),
		errExcelBPSRateLimitedAfterOutput,
		fmt.Errorf("wrapped: %w", errExcelBPSRateLimitedAfterOutput),
	} {
		require.False(t, svc.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", false, nil, err))
		require.False(t, svc.ObserveOpenAIAccountHealthFailure(context.Background(), account, err))
		after, _, _ := stats.snapshot(account.ID)
		require.Equal(t, before, after, "BPS throttling must not change the shared failure score")
	}
	svc.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", false, nil, errors.New("ordinary upstream failure"))
	after, _, _ := stats.snapshot(account.ID)
	require.Greater(t, after, before, "ordinary failures must still reach the scheduler")
}
