package service

import (
	"context"
	"net/http"
	"testing"
)

func TestConfiguredOpenAIStreamRetryFailureRequiresConfiguredMatchAndNoUsage(t *testing.T) {
	policy := compileUpstreamErrorRetryPolicy(UpstreamErrorRetrySettings{
		Enabled:    true,
		MaxRetries: 2,
		DelayMS:    100,
		Errors:     "500\nserver_is_overloaded",
	})
	ctx := context.WithValue(context.Background(), upstreamErrorRetryContextKey{}, &upstreamErrorRetryState{
		clientCtx: context.Background(),
		policy:    policy,
	})

	failure := configuredOpenAIStreamRetryFailure(ctx,
		[]byte(`{"type":"response.failed","response":{"error":{"status_code":500,"code":"server_is_overloaded"}}}`),
		"server overloaded", &OpenAIUsage{})
	if failure == nil || failure.StatusCode != http.StatusInternalServerError || !failure.ConfiguredRetry {
		t.Fatalf("failure = %#v, want configured 500 retry", failure)
	}

	withUsage := &OpenAIUsage{InputTokens: 1}
	if configuredOpenAIStreamRetryFailure(ctx, []byte(`{"type":"response.failed","error":{"code":"server_is_overloaded"}}`), "server overloaded", withUsage) != nil {
		t.Fatal("configured retry must be rejected after upstream usage")
	}
}

func TestTryConfiguredUpstreamErrorRetryWithoutStateIsNoop(t *testing.T) {
	claimed, err := TryConfiguredUpstreamErrorRetry(context.Background(), &UpstreamFailoverError{
		StatusCode: http.StatusBadGateway,
	})
	if claimed || err != nil {
		t.Fatalf("claimed=%v err=%v, want no-op without request retry state", claimed, err)
	}
}
