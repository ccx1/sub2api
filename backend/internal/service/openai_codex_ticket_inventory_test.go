package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func inventoryTestTicket(base *openAICodexTicket, marker string, offset time.Duration) *openAICodexTicket {
	ticket := *base
	ticket.State = "gAAAAA" + strings.Repeat(marker, 286)
	ticket.CapturedAt = base.CapturedAt.Add(offset)
	ticket.Standby = nil
	return &ticket
}

func TestCodexTicketInventoryKeepsPrimaryAndOneStandby(t *testing.T) {
	s, account, primary, request := ticketWatchdogFixture(t)
	backup := inventoryTestTicket(primary, "D", time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, backup))
	require.Equal(t, primary.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
	newer := inventoryTestTicket(primary, "C", 2*time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, newer))
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	inventory := stored.(*openAICodexTicket)
	require.Equal(t, primary.State, inventory.State)
	require.Equal(t, newer.State, inventory.Standby.State)
	require.Nil(t, inventory.Standby.Standby)
	receipt := request.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	require.Equal(t, primary.CapturedAt, receipt.ticket.CapturedAt)
	s.invalidateOpenAICodexTicket(context.Background(), account, primary)
	require.Equal(t, newer.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
}

func TestCodexTicketInventoryRejectsOnlyMatchingSlot(t *testing.T) {
	for _, olderBackup := range []bool{false, true} {
		t.Run(map[bool]string{false: "newer_backup", true: "older_backup"}[olderBackup], func(t *testing.T) {
			s, account, primary, _ := ticketWatchdogFixture(t)
			offset := time.Second
			if olderBackup {
				offset = -time.Second
			}
			backup := inventoryTestTicket(primary, "D", offset)
			root := *primary
			root.Standby = backup
			account.Extra = map[string]any{openAICodexTicketExtraKey(primary.Model): &root}
			s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, primary.Model), &root)
			s.invalidateOpenAICodexTicket(context.Background(), account, primary)
			selected := s.lookupOpenAICodexTicket(account, primary.Model)
			require.NotNil(t, selected)
			require.Equal(t, backup.State, selected.State)
			s.invalidateOpenAICodexTicket(context.Background(), account, primary)
			require.Equal(t, backup.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
			s.invalidateOpenAICodexTicket(context.Background(), account, backup)
			require.Nil(t, s.lookupOpenAICodexTicket(account, primary.Model))
		})
	}
}

func TestCodexTicketInventoryCASFailureDoesNotPublishStandby(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	s.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) {
		return false, errors.New("database unavailable")
	}}
	require.False(t, s.storeOpenAICodexTicket(context.Background(), account, inventoryTestTicket(primary, "D", time.Second)))
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.Nil(t, stored.(*openAICodexTicket).Standby)
	require.Equal(t, primary.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
}

func TestCodexTicketInventoryRefreshAndStaleSnapshots(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	now := time.Now()
	require.True(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, s.openAICodexTicketConfig(), now))
	backup := inventoryTestTicket(primary, "D", time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, backup))
	account.Extra = map[string]any{openAICodexTicketExtraKey(primary.Model): primary}
	require.False(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, s.openAICodexTicketConfig(), now))
	root := *primary
	root.Standby = backup
	root.Revoked = true
	account.Extra[openAICodexTicketExtraKey(primary.Model)] = &root
	require.Equal(t, backup.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
	account.Extra[openAICodexTicketExtraKey(primary.Model)] = primary
	require.Equal(t, backup.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
	require.True(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, s.openAICodexTicketConfig(), now))
}

func TestCodexTicketInventoryRefreshLoopFillsStandbyThenStops(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	upstream := &ticketPoolUpstream{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{"gpt-6-astra"}, HarvestProxyURL: "http://harvest.example:8080",
	}, upstream)
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	primary := &openAICodexTicket{Model: "gpt-6-astra", State: "gAAAAA" + strings.Repeat("A", 286), Length: 292,
		CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, primary))
	svc.refreshOpenAICodexTickets(context.Background())
	require.Len(t, upstream.proxies, 2, "只有主票时必须补齐备用并完成业务复验")
	stored, ok := svc.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.True(t, ok)
	inventory := stored.(*openAICodexTicket)
	require.NotNil(t, inventory.Standby)
	require.Equal(t, primary.State, inventory.State)
	require.NotEqual(t, primary.State, inventory.Standby.State)
	require.True(t, inventory.Standby.usable(time.Now(), account, svc.openAICodexTicketConfig()))
	svc.refreshOpenAICodexTickets(context.Background())
	require.Len(t, upstream.proxies, 2, "主备库存有效时不得继续请求上游")
	require.Equal(t, primary.State, svc.lookupOpenAICodexTicket(account, primary.Model).State)
}

func TestCodexTicketInventorySelectsCompatibleStandbyForHTTPAndWS(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	backup := inventoryTestTicket(primary, "D", time.Second)
	root := *primary
	root.ExpiresAt, root.Standby = time.Now().Add(-time.Second), backup
	account.Extra = map[string]any{openAICodexTicketExtraKey(primary.Model): &root}
	s.openaiCodexTickets.Delete(openAICodexTicketKey(account.ID, primary.Model))
	receipt, err := s.applyOpenAICodexTicketSnapshot(context.Background(), account, primary.Model, http.Header{})
	require.NoError(t, err)
	require.Equal(t, backup.State, receipt.ticket.State)
	require.Nil(t, receipt.ticket.Standby)
	ws := codexTicketWSReceiptFromSnapshot(receipt)
	require.True(t, backup.CapturedAt.Equal(ws.ticket.CapturedAt))
	backup.AccountBinding = "wrong-account-binding"
	cold := ticketTestService(t, s.openAICodexTicketConfig(), nil)
	_, err = cold.applyOpenAICodexTicketSnapshot(context.Background(), account, primary.Model, http.Header{})
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
}

func TestCodexTicketInventoryVerificationSkippedPolicy(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	skip := *primary
	skip.VerificationSkipped, skip.Verified = true, false
	skip.AccountBinding = openAICodexTicketAccountBinding(account)
	cfg := s.openAICodexTicketConfig()
	require.False(t, skip.usable(time.Now(), account, cfg))
	disabled := false
	cfg.VerifyBusiness = &disabled
	require.True(t, skip.usable(time.Now(), account, cfg))
	cfg.LengthMode = config.CodexTicketLengthAuto
	require.True(t, skip.usable(time.Now(), account, cfg))
	cfg.VerifyBusiness = nil
	require.False(t, skip.usable(time.Now(), account, cfg))
}

func TestCodexTicketInventoryOldStandbyReceiptCannotRevokeReplacement(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	old := inventoryTestTicket(primary, "D", time.Second)
	next := inventoryTestTicket(primary, "C", 2*time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, old))
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, next))
	s.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) {
		t.Fatal("旧备用回调不能触碰替换后的库存")
		return false, nil
	}}
	s.invalidateOpenAICodexTicket(context.Background(), account, old)
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.Equal(t, next.State, stored.(*openAICodexTicket).Standby.State)
	require.False(t, stored.(*openAICodexTicket).Standby.Revoked)
}

func TestCodexTicketInventoryRevocationFailureKeepsOtherSlot(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	backup := inventoryTestTicket(primary, "D", time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, backup))
	s.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) {
		return false, errors.New("database unavailable")
	}}
	s.invalidateOpenAICodexTicket(context.Background(), account, primary)
	require.Equal(t, backup.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.False(t, stored.(*openAICodexTicket).Revoked, "未持久化的撤票只记本机精确拒绝标记")
}

func TestCodexTicketInventorySkippedPublicationIsBoundAndPolicyIndependent(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	disabled := false
	s.cfg.Gateway.OpenAICodexTicket.VerifyBusiness = &disabled
	bound := s.codexTicketBinding(account)
	skipped := inventoryTestTicket(primary, "S", time.Second)
	skipped.VerificationSkipped, skipped.Binding = true, bound
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, skipped))
	s.cfg.Gateway.OpenAICodexTicket.VerifyBusiness = nil
	require.Equal(t, bound, s.codexTicketBinding(account), "复核开关不能改变已有已验证票绑定")
	newer := inventoryTestTicket(skipped, "T", time.Second)
	require.False(t, s.storeOpenAICodexTicket(context.Background(), account, newer))
	s.cfg.Gateway.OpenAICodexTicket.VerifyBusiness = &disabled
	newer.Binding = "stale-binding"
	require.False(t, s.storeOpenAICodexTicket(context.Background(), account, newer))
}

func TestCodexTicketInventoryDeduplicatesLiveStateWithoutChangingReceipt(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	duplicate := *primary
	duplicate.CapturedAt = primary.CapturedAt.Add(time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &duplicate))
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.Nil(t, stored.(*openAICodexTicket).Standby)
	require.True(t, primary.CapturedAt.Equal(stored.(*openAICodexTicket).CapturedAt))
	backup := inventoryTestTicket(primary, "D", 2*time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, backup))
	duplicate = *backup
	duplicate.CapturedAt = backup.CapturedAt.Add(time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &duplicate))
	stored, _ = s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.True(t, backup.CapturedAt.Equal(stored.(*openAICodexTicket).Standby.CapturedAt))
}

func TestCodexTicketInventoryAllowsReharvestedStateAfterRevocation(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	s.invalidateOpenAICodexTicket(context.Background(), account, primary)
	renewal := *primary
	renewal.CapturedAt = primary.CapturedAt.Add(time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &renewal))
	require.True(t, renewal.CapturedAt.Equal(s.lookupOpenAICodexTicket(account, primary.Model).CapturedAt))
	s.invalidateOpenAICodexTicket(context.Background(), account, primary)
	require.True(t, renewal.CapturedAt.Equal(s.lookupOpenAICodexTicket(account, primary.Model).CapturedAt))
}

func TestCodexTicketInventoryObservesOtherInstanceStandbyAdditionAndRevocation(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	root := *primary
	root.Standby = inventoryTestTicket(primary, "D", time.Second)
	account.Extra = map[string]any{openAICodexTicketExtraKey(primary.Model): &root}
	require.False(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, s.openAICodexTicketConfig(), time.Now()))
	root.Standby.Revoked = true
	require.True(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, s.openAICodexTicketConfig(), time.Now()))
	require.Equal(t, primary.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
	root.Standby.Revoked = false
	require.True(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, s.openAICodexTicketConfig(), time.Now()), "旧快照不得复活被其他实例撤销的备用")
}

func TestCodexTicketInventoryDuplicateStandbyPromotesWithoutPublishingRevocation(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	backup := inventoryTestTicket(primary, "D", time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, backup))
	s.invalidateOpenAICodexTicket(context.Background(), account, primary)
	duplicate := *backup
	duplicate.CapturedAt = backup.CapturedAt.Add(time.Second)
	s.accountRepo = &codexTicketCASStub{update: func(_ *Account, _ string, replacement any) (bool, error) {
		published := replacement.(*openAICodexTicket)
		require.False(t, published.Revoked, "发布库存不能误走仓储的根票撤销分支")
		require.True(t, sameCodexTicket(published, backup))
		return true, nil
	}}
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &duplicate))
}

func TestCodexTicketInventoryStatusSerializesUsingStandbyFalse(t *testing.T) {
	payload, err := json.Marshal(OpenAICodexTicketStatus{Model: "gpt-6-astra"})
	require.NoError(t, err)
	var status map[string]any
	require.NoError(t, json.Unmarshal(payload, &status))
	require.Contains(t, status, "using_standby")
	require.Equal(t, false, status["using_standby"])
}
