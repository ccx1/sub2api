package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexTicketRetryCompletionRepo struct {
	codexTicketRetryRepo
	completed chan CodexTicketAttempt
}

func (*codexTicketRetryCompletionRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}

func (r *codexTicketRetryCompletionRepo) RecordCodexTicketAttempt(_ context.Context, _ int64, attempt CodexTicketAttempt) error {
	r.completed <- attempt
	return nil
}

type codexTicketRetryPersistFailureRepo struct {
	codexTicketRetryCompletionRepo
}

func (*codexTicketRetryPersistFailureRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	return errors.New("persist failed")
}

func codexTicketRetryCompletionFixture(t *testing.T, fail bool) (*OpenAIGatewayService, *codexTicketRetryCompletionRepo, *codexTicketVerificationUpstream) {
	t.Helper()
	upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
		model := "gpt-6-astra"
		if fail {
			model = "gpt-other"
		}
		return codexTicketCompletedResponse(model, fakeCodexTicketState(312))
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{"gpt-6-astra", "gpt-5.6-sol"}, LengthMode: config.CodexTicketLengthAuto,
		HarvestProxyURL: "http://harvest.example:8080", RefreshBeforeSeconds: 300, RetryBackoffSeconds: []int{60},
	}, upstream)
	account := ticketTestAccount(41)
	account.Status = StatusActive
	repo := &codexTicketRetryCompletionRepo{codexTicketRetryRepo: codexTicketRetryRepo{account: account}, completed: make(chan CodexTicketAttempt, 1)}
	svc.accountRepo = repo
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
		Model: "gpt-5.6-sol", State: fakeCodexTicketState(292), Length: 292, Verified: true,
		CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Binding: svc.codexTicketBinding(account),
	}))
	svc.openaiCodexTicketBackoff.Store(codexTicketBackoffKey(account, "tok", "gpt-6-astra"),
		&codexTicketBackoffState{Failures: 3, UpdatedAt: time.Now(), RetryAt: time.Now().Add(time.Hour)})
	return svc, repo, upstream
}

func TestCodexTicketRetryRunsOnlyMissingModelAndRestoresBackoffOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified", true: "model_mismatch"}[fail], func(t *testing.T) {
			svc, repo, upstream := codexTicketRetryCompletionFixture(t, fail)
			account := repo.account
			sol := *svc.lookupOpenAICodexTicket(account, "gpt-5.6-sol")
			result, err := svc.RetryOpenAICodexTicket(context.Background(), account.ID, "gpt-6-astra")
			require.NoError(t, err)
			require.Equal(t, CodexTicketRetryResult{Scheduled: 1, Skipped: 0, Models: []string{"gpt-6-astra"}}, result)
			select {
			case attempt := <-repo.completed:
				require.Equal(t, !fail, attempt.Success)
				require.Equal(t, "gpt-6-astra", attempt.Model)
				require.Equal(t, 312, *attempt.HarvestTicketLength)
				if fail {
					require.Equal(t, "harvest_model_mismatch", attempt.Reason)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("manual retry did not finish the asynchronous harvest")
			}
			require.Equal(t, sol, *svc.lookupOpenAICodexTicket(account, sol.Model))
			require.Equal(t, fail, svc.openAICodexTicketBackoffActive(account, "tok", "gpt-6-astra"))
			if fail {
				svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
				require.Len(t, upstream.requests, 1, "automatic probe must honor the renewed backoff")
				require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			} else {
				require.Len(t, upstream.requests, 2, "harvesting must complete business verification")
				require.True(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").usable(time.Now(), account, svc.openAICodexTicketConfig()))
			}
			for _, request := range upstream.requests {
				body, err := request.GetBody()
				require.NoError(t, err)
				payload, err := io.ReadAll(body)
				require.NoError(t, err)
				body.Close()
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(payload, "model").String())
			}
		})
	}
}

func TestCodexTicketManualRetryPersistenceFailureIsReportedAsFailure(t *testing.T) {
	upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292)), nil
	}}
	verificationDisabled := false
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{"gpt-6-astra"}, VerifyBusiness: &verificationDisabled,
		HarvestProxyURL: "http://harvest.example:8080",
	}, upstream)
	account := ticketTestAccount(41)
	account.Status = StatusActive
	repo := &codexTicketRetryPersistFailureRepo{codexTicketRetryCompletionRepo: codexTicketRetryCompletionRepo{
		codexTicketRetryRepo: codexTicketRetryRepo{account: account}, completed: make(chan CodexTicketAttempt, 1),
	}}
	svc.accountRepo = repo

	result, err := svc.RetryOpenAICodexTicket(context.Background(), account.ID, "gpt-6-astra")
	require.NoError(t, err)
	require.Equal(t, CodexTicketRetryResult{Scheduled: 1, Skipped: 0, Models: []string{"gpt-6-astra"}}, result)

	select {
	case attempt := <-repo.completed:
		require.False(t, attempt.Success)
		require.Equal(t, "publish_failed", attempt.Reason)
		require.Equal(t, "failed", attempt.Outcome)
	case <-time.After(3 * time.Second):
		t.Fatal("manual retry did not record the failed publication")
	}
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"), "failed persistence must not publish a local ticket")
}
