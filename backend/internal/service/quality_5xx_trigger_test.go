package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuality5xxSemanticRejectsBusinessAndUnknownErrors(t *testing.T) {
	for _, body := range []string{
		`{"type":"error","error":{"code":"invalid_prompt","message":"policy rejection"}}`,
		`{"type":"response.failed","response":{"error":{"type":"permission_error","status":500}}}`,
		`{"error":{"code":"content_policy_violation","status":502}}`,
		`{"error":{"code":"invalid_api_key","status":503}}`,
		`{"error":{"code":"unknown_error","message":"unexpected"}}`,
		`{"error":{"code":"request_cancelled","status":500}}`,
	} {
		require.Zero(t, QualitySemanticFailureStatus([]byte(body)), body)
	}
	require.Equal(t, 503, QualitySemanticFailureStatus([]byte(`{"type":"response.failed","response":{"error":{"code":"server_is_overloaded"}}}`)))
	require.Equal(t, 500, QualitySemanticFailureStatus([]byte(`{"error":{"status":500}}`)))
	require.Zero(t, QualityUpstreamFailureStatus(503, nil, context.Canceled))
	require.Zero(t, QualityUpstreamFailureStatus(502, nil, errors.New("transport error")))
	require.Zero(t, QualityUpstreamFailureStatus(401, nil, nil))
	require.Equal(t, 502, QualityUpstreamFailureStatus(502, nil, nil))
	require.Zero(t, QualitySemanticFailureStatus([]byte(`{"error":{"code":"server_error","status":400}}`)))
}

type qualityObserverAccountRepo struct {
	AccountRepository
	failures            int
	successes           int
	observedConcurrency int
}

func (r *qualityObserverAccountRepo) ApplyQuality5xx(context.Context, int64) error {
	r.failures++
	return nil
}
func (r *qualityObserverAccountRepo) RecordQualityRecoverySuccess(_ context.Context, _ int64, concurrency int) error {
	r.successes++
	r.observedConcurrency = concurrency
	return nil
}

func TestQuality5xxObserverFiltersProbesCancellationAndPreservesRequestSnapshot(t *testing.T) {
	repo := &qualityObserverAccountRepo{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{ID: 1, Type: AccountTypeOAuth, Concurrency: 4}
	svc.ObserveQualityUpstreamFailure(context.Background(), account, 503, nil, nil)
	require.Equal(t, 1, repo.failures, "protection must finish synchronously")
	svc.ObserveQualityUpstreamFailure(WithQualityProbe(context.Background()), account, 503, nil, nil)
	svc.ObserveQualityUpstreamFailure(context.Background(), account, 401, nil, nil)
	svc.ObserveQualityUpstreamFailure(context.Background(), account, 502, []byte(`{"error":{"code":"invalid_prompt"}}`), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.ObserveQualityUpstreamFailure(ctx, account, 503, nil, nil)
	require.Equal(t, 1, repo.failures)
	svc.ObserveQualityUpstreamSuccess(context.Background(), account)
	require.Equal(t, 1, repo.successes)
	require.Equal(t, 4, repo.observedConcurrency)
	svc.ObserveQualityUpstreamSuccess(WithQualityProbe(context.Background()), account)
	require.Equal(t, 1, repo.successes)
}
