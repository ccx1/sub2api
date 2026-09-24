package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type transientCooldownAccountRepo struct {
	AccountRepository
}

func (transientCooldownAccountRepo) SetOverloaded(context.Context, int64, time.Time) error {
	return nil
}

type transientAccountBlockRepo struct {
	AccountRepository
	setTempCalls int
	lastUntil    time.Time
	lastReason   string
	setTempError error
}

func (r *transientAccountBlockRepo) SetOverloaded(context.Context, int64, time.Time) error {
	return nil
}

func (r *transientAccountBlockRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.setTempCalls++
	r.lastUntil = until
	r.lastReason = reason
	return r.setTempError
}

func TestHandleOpenAITransientError_BlocksOnlyRequestedModel(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       5105,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	firstShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadGateway, http.Header{}, []byte(`{"error":{"message":"Upstream request failed","type":"upstream_error"}}`), "gpt-5.5")
	secondShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadGateway, http.Header{}, []byte(`{"error":{"message":"Upstream request failed","type":"upstream_error"}}`), "gpt-5.5")

	require.False(t, firstShouldDisable)
	require.False(t, secondShouldDisable)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.6-terra"))
}

func TestHandleOpenAITransientError_TransientStatusesUseModelScope(t *testing.T) {
	for _, statusCode := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 520, 521, 522, 523, 524} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
			account := &Account{
				ID:       int64(5100 + statusCode),
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
			}

			firstShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, http.Header{}, []byte(`{"error":{"message":"temporary upstream failure"}}`), "gpt-5.5")
			secondShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, http.Header{}, []byte(`{"error":{"message":"temporary upstream failure"}}`), "gpt-5.5")

			require.False(t, firstShouldDisable)
			require.False(t, secondShouldDisable)
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "status %d must not block the whole account", statusCode)
			require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"), "status %d should block the failing model", statusCode)
		})
	}
}

func TestHandleOpenAITransient503_ThirdFailurePausesAccount(t *testing.T) {
	repo := &transientAccountBlockRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc.rateLimitService.SetAccountRuntimeBlocker(svc)
	account := &Account{
		ID:       5110,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}
	body := []byte(`{"error":{"message":"upstream unavailable"}}`)

	for i := 0; i < openAITransientAccountFailureThreshold; i++ {
		require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, body, "gpt-5.5"))
	}

	require.Equal(t, 1, repo.setTempCalls)
	require.True(t, repo.lastUntil.After(time.Now()))
	require.Contains(t, repo.lastReason, openAITransientAccountBlockReason)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

func TestHandleOpenAITransient503_OnlyConsecutive503Counts(t *testing.T) {
	repo := &transientAccountBlockRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 5111, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	body := []byte(`{"error":{"message":"upstream unavailable"}}`)

	for _, statusCode := range []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusServiceUnavailable} {
		require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, http.Header{}, body, "gpt-5.5"))
	}

	require.Equal(t, 0, repo.setTempCalls, "500/502 must break the consecutive 503 streak")
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandleOpenAITransient503_RequestCapacityDoesNotPauseAccount(t *testing.T) {
	repo := &transientAccountBlockRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 5114, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	ordinaryBody := []byte(`{"error":{"message":"upstream unavailable"}}`)
	capacityBody := []byte(`{"error":{"code":"server_is_overloaded","message":"server is overloaded"}}`)

	for range openAITransientAccountFailureThreshold - 1 {
		require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, ordinaryBody, "gpt-5.5"))
	}
	require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, capacityBody, "gpt-5.5"))
	require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, ordinaryBody, "gpt-5.5"))

	require.Equal(t, 0, repo.setTempCalls, "request capacity 503 must clear the account streak")
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandleOpenAITransient503_SuccessClearsStreak(t *testing.T) {
	repo := &transientAccountBlockRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 5112, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	body := []byte(`{"error":{"message":"upstream unavailable"}}`)

	for range openAITransientAccountFailureThreshold - 1 {
		require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, body, "gpt-5.5"))
	}
	svc.ReportOpenAIAccountScheduleResult(account, "gpt-5.5", true, nil)
	require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, body, "gpt-5.5"))

	require.Equal(t, 0, repo.setTempCalls, "a successful request must reset the 503 streak")
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandleOpenAITransient503_PersistFailureDoesNotBlockRuntime(t *testing.T) {
	repo := &transientAccountBlockRepo{setTempError: errors.New("db unavailable")}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc.rateLimitService.SetAccountRuntimeBlocker(svc)
	account := &Account{ID: 5113, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	body := []byte(`{"error":{"message":"upstream unavailable"}}`)

	for range openAITransientAccountFailureThreshold {
		require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, body, "gpt-5.5"))
	}

	require.Equal(t, 1, repo.setTempCalls)
	require.Nil(t, account.TempUnschedulableUntil, "failed persistence must not report a successful pause")
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

func TestHandleOpenAITransientError_529RemainsOverloadOnly(t *testing.T) {
	require.False(t, shouldCooldownOpenAITransientUpstreamError(529, []byte(`{"error":{"message":"overloaded"}}`)))
}

func TestHandleOpenAITransientError_CanonicalModelIsNotMappedTwice(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       5107,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"public-alias": "upstream-a",
				"upstream-a":   "upstream-b",
			},
		},
	}
	canonicalModel := account.GetMappedModel("public-alias")
	require.Equal(t, "upstream-a", canonicalModel)

	for range 2 {
		svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadGateway, http.Header{}, []byte(`{"error":{"message":"temporary upstream failure"}}`), canonicalModel)
	}

	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "public-alias"))
	svc.ReportOpenAIAccountScheduleResult(account, canonicalModel, true, nil)
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "public-alias"))
}

func TestHandleOpenAITransientError_DoesNotBlockParameter400(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       5103,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	shouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadRequest, http.Header{}, []byte(`{"error":{"message":"Invalid type for input[0].arguments"}}`), "gpt-5.5")

	require.False(t, shouldDisable)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"))
}

func TestHandleOpenAITransientError_HardDisableStillBlocksWholeAccount(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 5106, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	svc.BlockAccountScheduling(account, time.Now().Add(time.Minute), "upstream_disable")

	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.5", false))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.6-sol", false))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}
