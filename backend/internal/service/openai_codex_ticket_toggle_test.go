package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketToggleRepo struct {
	*accountTicketProxyRepo
	writes int
}

func (r *codexTicketToggleRepo) ListByPlatform(context.Context, string) ([]Account, error) {
	return []Account{*cloneOpenAICodexTicketAccount(r.account)}, nil
}

func (r *codexTicketToggleRepo) GetByID(context.Context, int64) (*Account, error) {
	return cloneOpenAICodexTicketAccount(r.account), nil
}

func (r *codexTicketToggleRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.writes++
	for key, value := range updates {
		r.account.Extra[key] = value
	}
	return nil
}

func codexTicketToggleFixture(t *testing.T, mode, kind string) (*OpenAIGatewayService, *codexTicketToggleRepo, *openAICodexTicket) {
	t.Helper()
	svc, base := accountTicketProxyFixture(t, mode)
	repo := &codexTicketToggleRepo{accountTicketProxyRepo: base}
	svc.accountRepo = repo
	svc.cfg.Gateway.OpenAICodexTicket.Models = []string{"gpt-6-astra"}
	svc.cfg.Gateway.OpenAICodexTicket.RefreshBeforeSeconds = 300
	if kind == "auto" {
		svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthAuto
	}
	now := time.Now().UTC()
	previous := &openAICodexTicket{AccountID: repo.account.ID, Model: "gpt-6-astra", State: fakeCodexTicketState(292),
		Length: 292, CapturedAt: now.Add(-20 * time.Minute), ExpiresAt: now.Add(40 * time.Minute)}
	if kind != "legacy" {
		previous.Verified, previous.Binding = true, svc.codexTicketBinding(repo.account)
		previous.AccountBinding = openAICodexTicketAccountBinding(repo.account)
		previous.Egress = openAICodexTicketEgress("")
	}
	repo.account.Extra[openAICodexTicketExtraKey(previous.Model)] = previous
	return svc, repo, previous
}

func TestCodexTicketToggleReusesPersistedFreshTicket(t *testing.T) {
	for _, mode := range []string{"inherit", "fixed", "random", "account"} {
		for _, kind := range []string{"legacy", "verified", "auto"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				svc, repo, previous := codexTicketToggleFixture(t, mode, kind)
				previous.Standby = inventoryTestTicket(previous, "D", time.Second)
				upstream := &ticketPoolUpstream{}
				svc.httpUpstream = upstream
				ctx := context.Background()
				// 没有内存票的进程也必须从持久化字段复用，关闭不能删除或延长旧票。
				repo.account.Extra[OpenAICodexTicketEnabledExtraKey] = false
				svc.refreshOpenAICodexTickets(ctx)
				svc.probeOnceOpenAICodexTicket(ctx, repo.account, previous.Model)
				require.Empty(t, upstream.proxies)
				repo.account.Extra[OpenAICodexTicketEnabledExtraKey] = true
				svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
				svc.refreshOpenAICodexTickets(ctx)
				require.Empty(t, upstream.proxies)
				svc.cfg.Gateway.OpenAICodexTicket.Enabled = true
				svc.refreshOpenAICodexTickets(ctx)
				require.Empty(t, upstream.proxies, "重新启用不能重采仍完整有效的持久化主备库存")
				// 已排队的采集任务也要重新检查，不能覆盖刚复用或并发发布的有效票。
				svc.probeOnceOpenAICodexTicket(ctx, repo.account, previous.Model)
				require.Empty(t, upstream.proxies)
				require.Zero(t, repo.writes)
				expected := codexTicketLeaf(previous)
				hydrateCodexTicketStateExpiry(expected)
				require.Equal(t, expected, svc.lookupOpenAICodexTicket(repo.account, previous.Model))
				require.Equal(t, previous, repo.account.Extra[openAICodexTicketExtraKey(previous.Model)])
				headers := http.Header{}
				require.NoError(t, svc.applyOpenAICodexTicket(ctx, repo.account, previous.Model, headers))
				require.Equal(t, previous.State, headers.Get(openAICodexTurnStateHeader))
			})
		}
	}
}

func TestCodexTicketRetryForcesPersistedLegacyTicket(t *testing.T) {
	for _, mode := range []string{"inherit", "fixed", "random", "account"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo, previous := codexTicketToggleFixture(t, mode, "legacy")
			result, err := svc.RetryOpenAICodexTicket(context.Background(), repo.account.ID, previous.Model)
			require.NoError(t, err)
			require.Equal(t, 1, result.Scheduled)
			require.Zero(t, result.Skipped)
			expected := codexTicketLeaf(previous)
			hydrateCodexTicketStateExpiry(expected)
			require.Equal(t, expected, svc.lookupOpenAICodexTicket(repo.account, previous.Model))
		})
	}
}

func TestCodexTicketToggleDoesNotReuseInvalidTicket(t *testing.T) {
	for _, invalid := range []string{"expired", "revoked", "identity", "auto_unverified"} {
		t.Run(invalid, func(t *testing.T) {
			svc, repo, previous := codexTicketToggleFixture(t, "inherit", "verified")
			switch invalid {
			case "expired":
				previous.State = codexTicketStateForExpiryTest(time.Now().Add(-2*time.Hour), 10)
				previous.ExpiresAt = time.Now().Add(-time.Second)
			case "revoked":
				previous.Revoked = true
			case "identity":
				repo.account.Credentials["chatgpt_account_id"] = "changed-account"
			case "auto_unverified":
				svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthAuto
				previous.Verified = false
			}
			upstream := &ticketPoolUpstream{}
			svc.httpUpstream = upstream
			repo.account.Extra[OpenAICodexTicketEnabledExtraKey] = false
			svc.refreshOpenAICodexTickets(context.Background())
			require.Empty(t, upstream.proxies)
			repo.account.Extra[OpenAICodexTicketEnabledExtraKey] = true
			svc.refreshOpenAICodexTickets(context.Background())
			require.Len(t, upstream.proxies, 2)
			next := svc.lookupOpenAICodexTicket(repo.account, previous.Model)
			require.NotNil(t, next)
			require.True(t, next.CapturedAt.After(previous.CapturedAt))
		})
	}
}

func TestCodexTicketFixedProxyStillRefreshesWithinConfiguredWindow(t *testing.T) {
	svc, repo, previous := codexTicketToggleFixture(t, "fixed", "verified")
	previous.State = "gAAAAA" + strings.Repeat("A", 286)
	previous.ExpiresAt = time.Now().UTC().Add(time.Minute)
	previous.Standby = inventoryTestTicket(previous, "D", time.Second)
	upstream := &ticketPoolUpstream{}
	svc.httpUpstream = upstream
	svc.refreshOpenAICodexTickets(context.Background())
	require.Len(t, upstream.proxies, 1, "复验原票只需要一次业务请求")
	next := svc.lookupOpenAICodexTicket(repo.account, previous.Model)
	require.NotNil(t, next)
	require.Equal(t, previous.State, next.State)
	require.True(t, next.RevalidatedAt.After(previous.CapturedAt))
	require.True(t, next.CapturedAt.After(previous.CapturedAt))
	require.True(t, next.ExpiresAt.After(previous.ExpiresAt))
	stored, ok := svc.openaiCodexTickets.Load(openAICodexTicketKey(repo.account.ID, previous.Model))
	require.True(t, ok)
	standby := stored.(*openAICodexTicket).Standby
	require.NotNil(t, standby)
	expectedStandby := codexTicketLeaf(previous.Standby)
	hydrateCodexTicketStateExpiry(expectedStandby)
	require.Equal(t, expectedStandby, standby, "主票同槽续用，不替换备用或增加库存")
	require.Len(t, codexTicketSlots(stored.(*openAICodexTicket)), 2)
}
