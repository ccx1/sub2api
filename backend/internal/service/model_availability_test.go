package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type modelAvailabilityAccountRepo struct {
	AccountRepository
	mu sync.Mutex

	account        *Account
	modelLimitCall []modelAvailabilityLimitCall

	listIDs     []int64
	clearCalls  []string
	removeCalls [][]string
	removeOK    bool
	removeErr   error
}

type modelAvailabilityLimitCall struct {
	accountID int64
	scope     string
	resetAt   time.Time
	reason    string
}

func (r *modelAvailabilityAccountRepo) SetModelRateLimit(_ context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	call := modelAvailabilityLimitCall{accountID: id, scope: scope, resetAt: resetAt}
	if len(reason) > 0 {
		call.reason = reason[0]
	}
	r.modelLimitCall = append(r.modelLimitCall, call)
	return nil
}

func (r *modelAvailabilityAccountRepo) SetOverloaded(context.Context, int64, time.Time) error {
	return nil
}

func (r *modelAvailabilityAccountRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	return nil
}

func (r *modelAvailabilityAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, errors.New("not found")
	}
	return r.account, nil
}

func (r *modelAvailabilityAccountRepo) ListModelRateLimitedAccountIDsByReason(context.Context, string, int) ([]int64, error) {
	return r.listIDs, nil
}

func (r *modelAvailabilityAccountRepo) ClearModelRateLimitIfReason(_ context.Context, _ int64, scope, _ string) (bool, error) {
	r.clearCalls = append(r.clearCalls, scope)
	return true, nil
}

func (r *modelAvailabilityAccountRepo) RemoveModelMappingKeys(_ context.Context, _ int64, keys []string) (bool, error) {
	r.removeCalls = append(r.removeCalls, append([]string(nil), keys...))
	return r.removeOK, r.removeErr
}

type modelAvailabilityTesterStub struct {
	models []string
	result *ScheduledTestResult
	err    error
}

func (t *modelAvailabilityTesterStub) RunTestBackground(_ context.Context, _ int64, modelID string) (*ScheduledTestResult, error) {
	t.models = append(t.models, modelID)
	return t.result, t.err
}

const modelAvailabilityUnavailableBody = `{"error":{"message":"Service temporarily unavailable","type":"server_error"}}`

func newModelAvailabilityAPIKeyAccount(mapping map[string]any) *Account {
	credentials := map[string]any{"api_key": "sk-test"}
	if mapping != nil {
		credentials["model_mapping"] = mapping
	}
	return &Account{
		ID:          9101,
		Name:        "apikey-503",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: credentials,
	}
}

func TestHandleUpstreamModelUnavailable_CooldownsOnlyRequestedModel(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "gpt-5.5", "gpt-5.4": "gpt-5.4"})

	require.True(t, svc.HandleUpstreamModelUnavailable(context.Background(), account, "gpt-5.5", http.StatusServiceUnavailable, []byte(modelAvailabilityUnavailableBody)))
	require.Len(t, repo.modelLimitCall, 1)
	call := repo.modelLimitCall[0]
	require.Equal(t, "gpt-5.5", call.scope)
	require.Equal(t, upstreamModelUnavailableReason, call.reason)
	require.WithinDuration(t, time.Now().Add(upstreamModelUnavailableCooldown), call.resetAt, 5*time.Second)

	require.False(t, account.IsSchedulableForModelWithContext(context.Background(), "gpt-5.5"))
	require.True(t, account.IsSchedulableForModelWithContext(context.Background(), "gpt-5.4"))
}

func TestHandleUpstreamModelUnavailable_UsesMappedUpstreamModel(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "vendor-gpt-5.5"})

	require.True(t, svc.HandleUpstreamModelUnavailable(context.Background(), account, "gpt-5.5", http.StatusServiceUnavailable, []byte(modelAvailabilityUnavailableBody)))
	require.Len(t, repo.modelLimitCall, 1)
	require.Equal(t, "vendor-gpt-5.5", repo.modelLimitCall[0].scope)
	require.False(t, account.IsSchedulableForModelWithContext(context.Background(), "gpt-5.5"))
}

func TestHandleUpstreamModelUnavailable_Skips(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*Account)
		statusCode int
		body       string
	}{
		{name: "non_503", statusCode: http.StatusBadGateway, body: modelAvailabilityUnavailableBody},
		{name: "oauth", statusCode: http.StatusServiceUnavailable, body: modelAvailabilityUnavailableBody, mutate: func(a *Account) { a.Type = AccountTypeOAuth }},
		{name: "pool_mode", statusCode: http.StatusServiceUnavailable, body: modelAvailabilityUnavailableBody, mutate: func(a *Account) { a.Credentials["pool_mode"] = true }},
		{name: "capacity_shed", statusCode: http.StatusServiceUnavailable, body: `{"error":{"message":"Our servers are currently overloaded. Please try again later."}}`},
		{name: "custom_codes_exclude_503", statusCode: http.StatusServiceUnavailable, body: modelAvailabilityUnavailableBody, mutate: func(a *Account) {
			a.Credentials["custom_error_codes_enabled"] = true
			a.Credentials["custom_error_codes"] = []any{float64(401)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &modelAvailabilityAccountRepo{}
			svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := newModelAvailabilityAPIKeyAccount(nil)
			if tc.mutate != nil {
				tc.mutate(account)
			}
			require.False(t, svc.HandleUpstreamModelUnavailable(context.Background(), account, "gpt-5.5", tc.statusCode, []byte(tc.body)))
			require.Empty(t, repo.modelLimitCall)
		})
	}
}

func TestHandleUpstreamError_503WithModelCooldownsModel(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := newModelAvailabilityAPIKeyAccount(nil)
	account.Platform = PlatformAnthropic

	require.False(t, svc.HandleUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, []byte(modelAvailabilityUnavailableBody), "claude-sonnet-4-5"))
	require.Len(t, repo.modelLimitCall, 1)
	require.Equal(t, "claude-sonnet-4-5", repo.modelLimitCall[0].scope)
}

func TestHandleOpenAIAccountUpstreamError_503CooldownsModelWithoutChangingResult(t *testing.T) {
	repo := &modelAvailabilityAccountRepo{}
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := newModelAvailabilityAPIKeyAccount(nil)

	require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, http.Header{}, []byte(modelAvailabilityUnavailableBody), "gpt-5.5"))
	require.Len(t, repo.modelLimitCall, 1)
	require.Equal(t, "gpt-5.5", repo.modelLimitCall[0].scope)
	require.Equal(t, upstreamModelUnavailableReason, repo.modelLimitCall[0].reason)
}

func newModelAvailabilityRecheckFixture(account *Account, tester *modelAvailabilityTesterStub) (*ModelAvailabilityRecheckService, *modelAvailabilityAccountRepo) {
	repo := &modelAvailabilityAccountRepo{account: account, listIDs: []int64{account.ID}, removeOK: true}
	svc := NewModelAvailabilityRecheckService(repo, repo, tester, nil, nil)
	return svc, repo
}

func expiredModelAvailabilityLimit(account *Account, scope string) {
	past := time.Now().Add(-time.Minute)
	setAccountModelRateLimitSnapshot(account, scope, past, upstreamModelUnavailableReason, past.Add(-upstreamModelUnavailableCooldown))
}

func TestModelAvailabilityRecheck_SuccessClearsCooldown(t *testing.T) {
	account := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "gpt-5.5", "gpt-5.4": "gpt-5.4"})
	expiredModelAvailabilityLimit(account, "gpt-5.5")
	tester := &modelAvailabilityTesterStub{result: &ScheduledTestResult{Status: "success"}}
	svc, repo := newModelAvailabilityRecheckFixture(account, tester)

	require.Equal(t, 1, svc.runOnce(context.Background()))
	require.Equal(t, []string{"gpt-5.5"}, tester.models)
	require.Equal(t, []string{"gpt-5.5"}, repo.clearCalls)
	require.Empty(t, repo.removeCalls)
	require.Empty(t, repo.modelLimitCall)
}

func TestModelAvailabilityRecheck_StillUnavailableRemovesModel(t *testing.T) {
	account := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "vendor-gpt-5.5", "gpt-5.4": "gpt-5.4"})
	expiredModelAvailabilityLimit(account, "vendor-gpt-5.5")
	tester := &modelAvailabilityTesterStub{result: &ScheduledTestResult{Status: "failed", ErrorMessage: "API returned 503: " + modelAvailabilityUnavailableBody}}
	svc, repo := newModelAvailabilityRecheckFixture(account, tester)

	require.Equal(t, 1, svc.runOnce(context.Background()))
	require.Equal(t, []string{"gpt-5.5"}, tester.models, "probe must use the mapping key so the test re-maps to the upstream model")
	require.Equal(t, [][]string{{"gpt-5.5"}}, repo.removeCalls)
	require.Equal(t, []string{"vendor-gpt-5.5"}, repo.clearCalls)
	require.Empty(t, repo.modelLimitCall)
}

func TestModelAvailabilityRecheck_InconclusiveFailureKeepsModelPaused(t *testing.T) {
	for _, errMsg := range []string{
		"Request failed: dial tcp: i/o timeout",
		"API returned 429: rate limited",
		`API returned 503: {"error":{"message":"Our servers are currently overloaded."}}`,
	} {
		t.Run(errMsg, func(t *testing.T) {
			account := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "gpt-5.5", "gpt-5.4": "gpt-5.4"})
			expiredModelAvailabilityLimit(account, "gpt-5.5")
			tester := &modelAvailabilityTesterStub{result: &ScheduledTestResult{Status: "failed", ErrorMessage: errMsg}}
			svc, repo := newModelAvailabilityRecheckFixture(account, tester)

			svc.runOnce(context.Background())
			require.Empty(t, repo.removeCalls)
			require.Empty(t, repo.clearCalls)
			require.Len(t, repo.modelLimitCall, 1)
			require.Equal(t, upstreamModelUnavailableReason, repo.modelLimitCall[0].reason)
			require.WithinDuration(t, time.Now().Add(modelAvailabilityRecheckRetryCooldown), repo.modelLimitCall[0].resetAt, 5*time.Second)
		})
	}
}

func TestModelAvailabilityRecheck_EmptyMappingKeepsModelPaused(t *testing.T) {
	account := newModelAvailabilityAPIKeyAccount(nil)
	expiredModelAvailabilityLimit(account, "gpt-5.5")
	tester := &modelAvailabilityTesterStub{result: &ScheduledTestResult{Status: "failed", ErrorMessage: "API returned 503: " + modelAvailabilityUnavailableBody}}
	svc, repo := newModelAvailabilityRecheckFixture(account, tester)

	svc.runOnce(context.Background())
	require.Empty(t, repo.removeCalls)
	require.Len(t, repo.modelLimitCall, 1)
}

func TestModelAvailabilityRecheck_RemoveRefusedKeepsModelPaused(t *testing.T) {
	account := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "gpt-5.5"})
	expiredModelAvailabilityLimit(account, "gpt-5.5")
	tester := &modelAvailabilityTesterStub{result: &ScheduledTestResult{Status: "failed", ErrorMessage: "API returned 503: " + modelAvailabilityUnavailableBody}}
	svc, repo := newModelAvailabilityRecheckFixture(account, tester)
	repo.removeOK = false

	svc.runOnce(context.Background())
	require.Len(t, repo.removeCalls, 1)
	require.Empty(t, repo.clearCalls)
	require.Len(t, repo.modelLimitCall, 1)
}

func TestModelAvailabilityRecheck_SkipsPausedAndNotYetDue(t *testing.T) {
	paused := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "gpt-5.5", "gpt-5.4": "gpt-5.4"})
	paused.Schedulable = false
	expiredModelAvailabilityLimit(paused, "gpt-5.5")
	tester := &modelAvailabilityTesterStub{result: &ScheduledTestResult{Status: "success"}}
	svc, _ := newModelAvailabilityRecheckFixture(paused, tester)
	require.Equal(t, 0, svc.runOnce(context.Background()))

	pending := newModelAvailabilityAPIKeyAccount(map[string]any{"gpt-5.5": "gpt-5.5", "gpt-5.4": "gpt-5.4"})
	future := time.Now().Add(time.Minute)
	setAccountModelRateLimitSnapshot(pending, "gpt-5.5", future, upstreamModelUnavailableReason, time.Now())
	// 其它 reason 的到期条目不属于本服务。
	setAccountModelRateLimitSnapshot(pending, "gpt-5.4", time.Now().Add(-time.Minute), "upstream_model_not_found", time.Now().Add(-time.Hour))
	svc, _ = newModelAvailabilityRecheckFixture(pending, tester)
	require.Equal(t, 0, svc.runOnce(context.Background()))
	require.Empty(t, tester.models)
}

func TestModelAvailabilityProbeStatusCode(t *testing.T) {
	require.Equal(t, 503, modelAvailabilityProbeStatusCode("API returned 503: x"))
	require.Equal(t, 404, modelAvailabilityProbeStatusCode("Chat Completions API (/v1/chat/completions) returned 404: x"))
	require.Equal(t, 0, modelAvailabilityProbeStatusCode("Request failed: EOF"))
}
