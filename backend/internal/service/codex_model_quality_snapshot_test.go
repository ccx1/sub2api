package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexQualitySnapshotRepo struct {
	AccountRepository
	account      *Account
	proxy, bound *Proxy
	groupIDs     []int64
	boundReads   int
	region       string
	regionReads  int
}

func (r *codexQualitySnapshotRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *codexQualitySnapshotRepo) GetCodexTicketProxy(context.Context, int64) (*Proxy, error) {
	return r.proxy, nil
}
func (r *codexQualitySnapshotRepo) GetCodexModelQualityBoundProxy(context.Context, int64) (*Proxy, error) {
	r.boundReads++
	return r.bound, nil
}
func (r *codexQualitySnapshotRepo) GetRandomProxyGroupIDs(context.Context, int64) ([]int64, error) {
	return r.groupIDs, nil
}
func (r *codexQualitySnapshotRepo) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	panic("quality checks must not allocate a proxy")
}
func (r *codexQualitySnapshotRepo) SelectBalancedProxy(context.Context, ProxyPoolSelection) (*Proxy, error) {
	panic("quality checks must not mutate shared proxy bindings")
}
func (r *codexQualitySnapshotRepo) MatchesProxyRegion(_ context.Context, _ *Proxy, country string) (bool, error) {
	r.regionReads++
	return r.region == country, nil
}

func codexQualitySnapshotFixture(t *testing.T, random bool) (*OpenAIGatewayService, *codexQualitySnapshotRepo, CodexModelQualityPolicy, *openAICodexTicket) {
	t.Helper()
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}, TargetLength: 292}, nil)
	proxy := &Proxy{ID: 9, Protocol: "http", Host: "snapshot.test", Port: 8080, Status: StatusActive}
	account := ticketTestAccount(41)
	account.Extra = map[string]any{}
	if random {
		account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	} else {
		account.ProxyID, account.Proxy = &proxy.ID, proxy
	}
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now().Add(-10 * time.Minute), ExpiresAt: time.Now().Add(time.Hour), Verified: true,
		Egress: openAICodexTicketEgress(proxy.URL()), SessionID: "ticket-session"}
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	repo := &codexQualitySnapshotRepo{account: account, proxy: proxy, bound: proxy}
	svc.accountRepo = repo
	policy := DefaultCodexModelQualityPolicy()
	policy.Enabled = true
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	svc.settingService = &SettingService{cfg: svc.cfg, settingRepo: &modelQualitySettingsRepo{values: map[string]string{SettingKeyCodexModelQuality: string(raw)}}}
	return svc, repo, policy, ticket
}

func TestCodexModelQualitySnapshotReadsBindingsWithoutAllocating(t *testing.T) {
	for _, random := range []bool{false, true} {
		svc, repo, policy, ticket := codexQualitySnapshotFixture(t, random)
		before, err := json.Marshal(repo.account)
		require.NoError(t, err)
		job, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
		require.NotNil(t, job, reason)
		require.Equal(t, repo.proxy.URL(), job.proxyURL)
		for range 3 {
			require.True(t, svc.codexModelQualityCurrent(context.Background(), job))
		}
		after, err := json.Marshal(repo.account)
		require.NoError(t, err)
		require.JSONEq(t, string(before), string(after), "polling must leave account snapshots untouched")
		if random {
			require.Equal(t, 4, repo.boundReads)
		} else {
			require.Zero(t, repo.boundReads)
		}
	}
}

func TestCodexModelQualitySnapshotRejectsMissingOrChangedRandomEgress(t *testing.T) {
	for _, change := range []func(*codexQualitySnapshotRepo){
		func(r *codexQualitySnapshotRepo) {
			r.bound = nil
			r.account.Extra[RandomProxyEmptyPoolPolicyExtraKey] = RandomProxyEmptyPoolPolicyDirect
		},
		func(r *codexQualitySnapshotRepo) {
			r.bound = &Proxy{ID: 10, Protocol: "http", Host: "different.test", Port: 8080, Status: StatusActive}
		},
		func(r *codexQualitySnapshotRepo) {
			r.account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
			r.account.Extra[RandomProxyPoolIDsExtraKey] = []any{int64(10)}
		},
		func(r *codexQualitySnapshotRepo) {
			r.account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolGroup
			r.account.Extra[RandomProxyGroupIDExtraKey] = int64(1)
			r.groupIDs = []int64{10}
		},
		func(r *codexQualitySnapshotRepo) { r.proxy.Status = StatusDisabled },
		func(r *codexQualitySnapshotRepo) {
			expired := time.Now().Add(-time.Second)
			r.proxy.ExpiresAt = &expired
		},
	} {
		svc, repo, policy, ticket := codexQualitySnapshotFixture(t, true)
		job, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
		require.NotNil(t, job, reason)
		change(repo)
		require.False(t, svc.codexModelQualityCurrent(context.Background(), job))
		next, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
		require.Nil(t, next)
		require.Contains(t, []string{"proxy_unavailable", "egress_changed"}, reason)
	}
}

func TestCodexModelQualitySnapshotRejectsRandomProxyOutsideRequiredRegion(t *testing.T) {
	svc, repo, policy, ticket := codexQualitySnapshotFixture(t, true)
	repo.account.Extra[ProxyRegionModeExtraKey] = "manual"
	repo.account.Extra[ProxyRegionCountryExtraKey] = "JP"
	repo.region = "PH"
	job, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
	require.Nil(t, job)
	require.Equal(t, "proxy_unavailable", reason)
	require.Equal(t, 1, repo.regionReads)
}

func TestCodexModelQualitySnapshotFencesTicketAccountAndPolicyChanges(t *testing.T) {
	for index, change := range []func(*OpenAIGatewayService, *codexQualitySnapshotRepo, *openAICodexTicket){
		func(_ *OpenAIGatewayService, r *codexQualitySnapshotRepo, _ *openAICodexTicket) {
			r.account.Status = StatusDisabled
		},
		func(_ *OpenAIGatewayService, r *codexQualitySnapshotRepo, _ *openAICodexTicket) {
			r.account.Credentials["access_token"] = "replacement-token"
		},
		func(_ *OpenAIGatewayService, r *codexQualitySnapshotRepo, _ *openAICodexTicket) {
			r.proxy.Host = "replacement.test"
		},
		func(_ *OpenAIGatewayService, r *codexQualitySnapshotRepo, ticket *openAICodexTicket) {
			next := *ticket
			next.CapturedAt = time.Now()
			r.account.Extra[openAICodexTicketExtraKey(ticket.Model)] = &next
		},
		func(s *OpenAIGatewayService, r *codexQualitySnapshotRepo, ticket *openAICodexTicket) {
			ticket.ExpiresAt = time.Now().Add(-time.Second)
			s.openaiCodexTickets.Delete(openAICodexTicketKey(r.account.ID, ticket.Model))
		},
		func(s *OpenAIGatewayService, _ *codexQualitySnapshotRepo, _ *openAICodexTicket) {
			s.settingService.settingRepo.(*modelQualitySettingsRepo).values[SettingKeyCodexModelQuality] = `{"enabled":false}`
		},
	} {
		svc, repo, policy, ticket := codexQualitySnapshotFixture(t, false)
		job, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
		require.NotNil(t, job, reason)
		change(svc, repo, ticket)
		if index == 1 {
			// OAuth refresh rotates transport credentials without replacing the
			// primary ticket being tested.
			require.True(t, svc.codexModelQualityCurrent(context.Background(), job), "case %d", index)
		} else {
			require.False(t, svc.codexModelQualityCurrent(context.Background(), job), "case %d", index)
		}
	}
}

func TestCodexModelQualitySnapshotIgnoresNewStandbyWhenPrimaryIsUnchanged(t *testing.T) {
	svc, repo, policy, primary := codexQualitySnapshotFixture(t, false)
	job, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, primary.Model, policy)
	require.NotNil(t, job, reason)

	standby := codexTicketLeaf(primary)
	standby.State = fakeCodexTicketState(292)[:len(primary.State)-1] + "B"
	standby.CapturedAt = primary.CapturedAt.Add(time.Second)
	standby.ExpiresAt = primary.ExpiresAt.Add(time.Second)
	repo.account.Extra[openAICodexTicketExtraKey(primary.Model)] = composeCodexTicketPool([]*openAICodexTicket{primary, standby}, job.config)

	current, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, primary.Model, policy)
	require.NotNil(t, current, reason)
	require.True(t, sameCodexTicket(current.ticket, primary), "the primary ticket must remain selected")
	require.Equal(t, job.scope, current.scope)
	require.True(t, svc.codexModelQualityCurrent(context.Background(), job))
}

func TestCodexModelQualitySnapshotSkipsLegacyTicketWithoutSession(t *testing.T) {
	svc, repo, policy, ticket := codexQualitySnapshotFixture(t, false)
	ticket.SessionID = ""
	job, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
	require.Nil(t, job)
	require.Equal(t, "request_unavailable", reason)
}

func TestCodexModelQualitySnapshotWaitsForNewTicketInsteadOfReusingResult(t *testing.T) {
	svc, repo, policy, ticket := codexQualitySnapshotFixture(t, false)
	job, reason := svc.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
	require.NotNil(t, job, reason)
	status := qualityStatusForJob(job)
	record := &CodexModelQualityRecord{Status: status, Scope: job.scope, Policy: job.policyHash}
	next := *ticket
	next.CapturedAt = time.Now()
	ready := next.CapturedAt.Add(time.Duration(policy.ReplacementCheckDelaySeconds) * time.Second)
	for _, previousStatus := range []string{"passed", "suspect", "quarantined", "inconclusive"} {
		record.Status.Status = previousStatus
		displayed := codexModelQualityDisplayStatus(record, repo.account, &next, job.config, policy)
		require.Equal(t, "pending", displayed.Status, previousStatus)
		require.Equal(t, "not_checked", displayed.Reason, previousStatus)
		require.False(t, displayed.BaselineReused)
		require.Equal(t, &ready, displayed.NextCheckAt)
	}
	backoff := ready.Add(time.Minute)
	record.Status.NextCheckAt = &backoff
	displayed := codexModelQualityDisplayStatus(record, repo.account, &next, job.config, policy)
	require.Equal(t, &backoff, displayed.NextCheckAt)
}
