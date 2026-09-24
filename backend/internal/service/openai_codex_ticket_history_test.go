package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketHistoryRepo struct {
	AccountRepository
	history CodexTicketHistory
}

func (r *codexTicketHistoryRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}
func (r *codexTicketHistoryRepo) RecordCodexTicketAttempt(_ context.Context, _ int64, attempt CodexTicketAttempt) error {
	r.history.Append(attempt)
	return nil
}

func TestCodexTicketHistoryCountsWholeRoundAndRedactsProxy(t *testing.T) {
	for _, fail := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
				model := "gpt-6-astra"
				if fail == call {
					model = "wrong-model"
				}
				return codexTicketCompletedResponse(model, fakeCodexTicketState(292))
			}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://private-user:private-password@harvest.example:8080"}, upstream)
			repo := &codexTicketHistoryRepo{}
			svc.accountRepo = repo
			account := ticketTestAccount(41)
			proxyID := int64(7)
			account.ProxyID, account.Proxy = &proxyID, &Proxy{ID: 7, Name: "出口7", Protocol: "http", Host: "business.example", Port: 8080, Username: "private-user", Password: "private-password"}
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.EqualValues(t, 1, repo.history.Summary.Total)
			require.Len(t, repo.history.Items, 1)
			item := repo.history.Items[0]
			require.Equal(t, fail == 0, item.Success)
			require.Equal(t, "http://harvest.example:8080", item.HarvestProxy.Address)
			if fail != 1 {
				require.Equal(t, "http://business.example:8080", item.BusinessProxy.Address)
				require.EqualValues(t, 7, item.BusinessProxy.ID)
			} else {
				require.Nil(t, item.BusinessProxy)
			}
			require.False(t, item.FinishedAt.Before(item.StartedAt))
			encoded, err := json.Marshal(repo.history)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private-")
			require.Contains(t, string(encoded), fakeCodexTicketState(292))
			require.Equal(t, "raw", item.HarvestExchange.CaptureMode)
			if fail != 1 {
				require.Equal(t, "raw", item.BusinessExchange.CaptureMode)
			}
		})
	}
}

func TestCodexTicketHistoryRetainsCumulativeTotalsAndPaginates(t *testing.T) {
	history := CodexTicketHistory{}
	base := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 123; i++ {
		history.Append(CodexTicketAttempt{ID: fmt.Sprint(i), StartedAt: base.Add(time.Duration(i) * time.Minute), Success: i%2 == 0})
	}
	account := ticketTestAccount(41)
	account.Extra = map[string]any{OpenAICodexTicketHistoryKey: history}
	page, err := GetCodexTicketHistory(account, 5, 20)
	require.NoError(t, err)
	require.EqualValues(t, 123, page.Summary.Total)
	require.EqualValues(t, 62, page.Summary.Success)
	require.EqualValues(t, 61, page.Summary.Failed)
	require.Equal(t, 100, page.Total)
	require.Len(t, page.Items, 20)
	require.Equal(t, "42", page.Items[0].ID)
	page, err = GetCodexTicketHistory(account, int(^uint(0)>>1), 100)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.NotContains(t, RedactOpenAICodexTicketExtra(account.Extra), OpenAICodexTicketHistoryKey)
	merged := MergeOpenAICodexTicketExtra(map[string]any{OpenAICodexTicketHistoryKey: "forged"}, account.Extra)
	require.Equal(t, history, merged[OpenAICodexTicketHistoryKey])
}

func TestCodexTicketRandomFullInventorySkipsHarvest(t *testing.T) {
	for _, verified := range []bool{false, true} {
		calls := 0
		svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
			calls++
			return nil, fmt.Errorf("full ticket inventory must not trigger HTTP")
		}})
		repo := &codexTicketHistoryRepo{}
		svc.accountRepo = repo
		account := ticketTestAccount(41)
		account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
		old := &openAICodexTicket{Model: "gpt-6-astra", State: "gAAAAA" + strings.Repeat("A", 286), Length: 292,
			CapturedAt: time.Now().Add(-20 * time.Minute), ExpiresAt: time.Now().Add(40 * time.Minute), Verified: verified, Binding: svc.codexTicketBinding(account)}
		require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, old))
		standby := inventoryTestTicket(old, "D", time.Second)
		standby.ExpiresAt = time.Now().Add(time.Hour)
		require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, standby))
		account.Extra[RandomProxyPoolIDsExtraKey] = []int64{8, 9}
		svc.probeOnceOpenAICodexTicket(context.Background(), account, old.Model)
		require.Zero(t, calls)
		require.Zero(t, repo.history.Summary.Total)
		require.Equal(t, old.State, svc.lookupOpenAICodexTicket(account, old.Model).State)
	}
}
