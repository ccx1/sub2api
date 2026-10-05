//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGrokForbiddenFailoverBudgetKeepsOneAlternateCeiling(t *testing.T) {
	var budget grokForbiddenFailoverBudget
	require.True(t, budget.canRetry(&service.UpstreamFailoverError{Reason: service.GrokUnknownForbiddenReason}, 2))
	require.False(t, budget.canRetry(&service.UpstreamFailoverError{StatusCode: 502}, 3))
	require.False(t, budget.canRetry(&service.UpstreamFailoverError{Reason: service.GrokUnknownForbiddenReason}, 3))
	require.False(t, budget.canRetry(&service.UpstreamFailoverError{NextAccountAction: service.NextAccountStop}, 2))
}
