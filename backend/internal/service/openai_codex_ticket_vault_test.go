package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// vaultTestRepo 模拟数据库：每次读取都返回 JSON 往返后的副本，写入按键合并。
type vaultTestRepo struct {
	AccountRepository
	mu       sync.Mutex
	account  *Account
	writes   int
	failNext int
}

func vaultTestCopy(t *testing.T, value any, out any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, out))
}

func (r *vaultTestRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyAccount := *r.account
	copyAccount.Credentials = maps.Clone(r.account.Credentials)
	encoded, err := json.Marshal(r.account.Extra)
	if err != nil {
		return nil, err
	}
	copyAccount.Extra = map[string]any{}
	if err := json.Unmarshal(encoded, &copyAccount.Extra); err != nil {
		return nil, err
	}
	return &copyAccount, nil
}

func (r *vaultTestRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNext > 0 {
		r.failNext--
		return errors.New("db unavailable")
	}
	r.writes++
	encoded, err := json.Marshal(updates)
	if err != nil {
		return err
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return err
	}
	if r.account.Extra == nil {
		r.account.Extra = map[string]any{}
	}
	maps.Copy(r.account.Extra, decoded)
	return nil
}

func (r *vaultTestRepo) persisted(t *testing.T) []*openAICodexTicket {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	return codexTicketSlots(parseOpenAICodexTicketFromAny(41, usageTestModel, r.account.Extra[openAICodexTicketExtraKey(usageTestModel)]))
}

func vaultTestService(t *testing.T, cfg config.OpenAICodexTicketConfig, slots ...*openAICodexTicket) (*OpenAIGatewayService, *vaultTestRepo) {
	t.Helper()
	cfg.Enabled, cfg.Models = true, []string{usageTestModel}
	if cfg.PoolCapacity == 0 {
		cfg.PoolCapacity = 5
	}
	account := ticketTestAccount(41)
	account.Name = "vault-account"
	var extra any
	vaultTestCopy(t, usageTestPool(slots...), &extra)
	account.Extra = map[string]any{openAICodexTicketExtraKey(usageTestModel): extra}
	repo := &vaultTestRepo{account: account}
	svc := ticketTestService(t, cfg, nil)
	svc.accountRepo = repo
	return svc, repo
}

func vaultTestSlot(t *testing.T, vault CodexTicketVault, label string) CodexTicketVaultSlot {
	t.Helper()
	require.Len(t, vault.Models, 1)
	for _, slot := range vault.Models[0].Slots {
		if slot.Label == label {
			return slot
		}
	}
	t.Fatalf("slot %s not found", label)
	return CodexTicketVaultSlot{}
}

func TestCodexTicketVaultListsMetadataWithoutSecrets(t *testing.T) {
	now := time.Now()
	old, young, revoked := usageTestTicket("O", 15*time.Minute, now), usageTestTicket("Y", time.Minute, now), usageTestTicket("R", 20*time.Minute, now)
	old.SessionID, young.SessionID = "sess-private-old", "sess-private-young"
	old.Egress, old.AttemptID = "egress-private", "attempt-private"
	revoked.Revoked = true
	revoked.Invalidation = &CodexTicketInvalidation{Reason: "response_ticket_rejected", Source: "http", InvalidatedAt: now, AttemptID: "attempt-private"}
	svc, repo := vaultTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, UsageMode: config.CodexTicketUsageAged, MinTicketAgeSeconds: 300}, young, old, revoked)

	vault, err := svc.GetOpenAICodexTicketVault(context.Background(), 41)
	require.NoError(t, err)
	require.Equal(t, "vault-account", vault.AccountName)
	require.Equal(t, config.CodexTicketUsageAged, vault.Policy.UsageMode)
	require.Equal(t, 300, vault.Policy.MinTicketAgeSeconds)
	require.True(t, vault.Policy.FailClosed)
	require.Equal(t, 5, vault.PoolCapacity)
	model := vault.Models[0]
	require.Equal(t, usageTestModel, model.Model)
	require.True(t, model.Configured)
	require.Equal(t, 3, model.Total)
	require.Equal(t, 1, model.Available)
	require.Equal(t, 1, model.Maturing)

	primary, standby, reserve := vaultTestSlot(t, vault, "primary"), vaultTestSlot(t, vault, "standby"), vaultTestSlot(t, vault, "reserve-1")
	require.Equal(t, CodexTicketVaultStatusMaturing, primary.Status)
	require.False(t, primary.BusinessSelected)
	require.NotNil(t, primary.MatureAt)
	require.WithinDuration(t, young.CapturedAt.Add(5*time.Minute), *primary.MatureAt, time.Second)
	require.Equal(t, CodexTicketVaultStatusAvailable, standby.Status)
	require.True(t, standby.BusinessSelected, "沉淀模式业务使用已满时长的票")
	require.InDelta(t, 900, standby.AgeSeconds, 2)
	require.Equal(t, 292, standby.Length)
	require.Equal(t, "revoked", reserve.Status)
	require.Equal(t, &CodexTicketVaultInvalidation{Reason: "response_ticket_rejected", Source: "http", InvalidatedAt: reserve.Invalidation.InvalidatedAt}, reserve.Invalidation)
	for _, slot := range model.Slots {
		require.True(t, validCodexTicketVaultFingerprint(slot.Fingerprint), slot.Fingerprint)
	}
	require.NotEqual(t, primary.Fingerprint, standby.Fingerprint)

	encoded, err := json.Marshal(vault)
	require.NoError(t, err)
	body := string(encoded)
	for _, secret := range []string{old.State, young.State, revoked.State, openAICodexTicketStatePrefix, "sess-private", "egress-private", "attempt-private", `"tok"`, "acc-1"} {
		require.NotContains(t, body, secret)
	}
	require.Zero(t, repo.writes, "查看票库不写库")
}

func TestCodexTicketVaultCookieSlotsExposeOnlyNamesAndNode(t *testing.T) {
	now := time.Now()
	oailb := codexTicketNodeTestOAILB(t, 88)
	ticket := &openAICodexTicket{AccountID: 41, Model: usageTestModel, CredentialMode: config.CodexTicketCredentialCookie,
		SessionID: "sess-private", CapturedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Verified: true,
		Cookies: []*http.Cookie{{Name: codexOAILBCookieName, Value: oailb}, {Name: "cf_private", Value: "cookie-private-value"}, {Name: "cf_private", Value: "dup"}}}
	svc, _ := vaultTestService(t, config.OpenAICodexTicketConfig{CredentialMode: config.CodexTicketCredentialCookie}, ticket)

	vault, err := svc.GetOpenAICodexTicketVault(context.Background(), 41)
	require.NoError(t, err)
	slot := vaultTestSlot(t, vault, "primary")
	require.Equal(t, []string{codexOAILBCookieName, "cf_private"}, slot.CookieNames)
	require.NotNil(t, slot.RouteNode)
	require.Equal(t, "unified-88", slot.RouteNode.Name)
	encoded, err := json.Marshal(vault)
	require.NoError(t, err)
	for _, secret := range []string{oailb, "cookie-private-value", "sess-private", strings.Split(oailb, ".")[1]} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestCodexTicketVaultFingerprintStableAcrossSoftRevalidation(t *testing.T) {
	now := time.Now()
	original := usageTestTicket("A", 10*time.Minute, now)
	original.SessionID = "sess"
	revalidated := *original
	revalidated.OriginCapturedAt, revalidated.CapturedAt, revalidated.RevalidatedAt = original.CapturedAt, now, now
	require.Equal(t, codexTicketVaultFingerprint(41, usageTestModel, original), codexTicketVaultFingerprint(41, usageTestModel, &revalidated))
	other := *original
	other.State = openAICodexTicketStatePrefix + strings.Repeat("B", 286)
	require.NotEqual(t, codexTicketVaultFingerprint(41, usageTestModel, original), codexTicketVaultFingerprint(41, usageTestModel, &other))
	require.NotEqual(t, codexTicketVaultFingerprint(41, usageTestModel, original), codexTicketVaultFingerprint(42, usageTestModel, original))
}

func TestCodexTicketVaultRevokeByFingerprintTombstonesSlot(t *testing.T) {
	now := time.Now()
	a, b := usageTestTicket("A", 2*time.Minute, now), usageTestTicket("B", time.Minute, now)
	svc, repo := vaultTestService(t, config.OpenAICodexTicketConfig{FailClosed: true}, a, b)
	account, err := repo.GetByID(context.Background(), 41)
	require.NoError(t, err)
	cfg := svc.openAICodexTicketConfigForAccount(context.Background(), account)
	require.Equal(t, a.State, svc.lookupOpenAICodexTicketForUse(account, usageTestModel, cfg).State)

	fingerprint := codexTicketVaultFingerprint(41, usageTestModel, a)
	result, err := svc.RevokeOpenAICodexTicketVault(context.Background(), 41, CodexTicketVaultRevokeInput{Model: " " + usageTestModel, Fingerprint: strings.ToUpper(fingerprint)})
	require.NoError(t, err)
	require.Equal(t, CodexTicketVaultRevokeResult{AccountID: 41, Model: usageTestModel, Revoked: 1}, result)

	slots := repo.persisted(t)
	require.Len(t, slots, 2)
	require.True(t, slots[0].Revoked)
	require.NotNil(t, slots[0].Invalidation)
	require.Equal(t, CodexTicketVaultInvalidationReason, slots[0].Invalidation.Reason)
	require.Equal(t, CodexTicketVaultInvalidationSource, slots[0].Invalidation.Source)
	require.False(t, slots[1].Revoked)

	account, err = repo.GetByID(context.Background(), 41)
	require.NoError(t, err)
	require.Equal(t, b.State, svc.lookupOpenAICodexTicketForUse(account, usageTestModel, cfg).State, "作废后业务跳过该票")
	// 另一个实例只读数据库也不会再使用该票。
	other := ticketTestService(t, svc.cfg.Gateway.OpenAICodexTicket, nil)
	require.Equal(t, b.State, other.lookupOpenAICodexTicketForUse(account, usageTestModel, cfg).State)

	vault, err := svc.GetOpenAICodexTicketVault(context.Background(), 41)
	require.NoError(t, err)
	revoked := vaultTestSlot(t, vault, "primary")
	require.Equal(t, "revoked", revoked.Status)
	require.Equal(t, CodexTicketVaultInvalidationReason, revoked.Invalidation.Reason)

	// 重复作废同一张票视为已完成。
	writes := repo.writes
	result, err = svc.RevokeOpenAICodexTicketVault(context.Background(), 41, CodexTicketVaultRevokeInput{Model: usageTestModel, Fingerprint: fingerprint})
	require.NoError(t, err)
	require.Zero(t, result.Revoked)
	require.Zero(t, result.Remaining)
	require.Equal(t, writes, repo.writes)
}

func TestCodexTicketVaultRevokeAllRetriesFailedWrites(t *testing.T) {
	now := time.Now()
	a, b, c := usageTestTicket("A", 3*time.Minute, now), usageTestTicket("B", 2*time.Minute, now), usageTestTicket("C", time.Minute, now)
	svc, repo := vaultTestService(t, config.OpenAICodexTicketConfig{FailClosed: true}, a, b, c)
	repo.failNext = 1

	result, err := svc.RevokeOpenAICodexTicketVault(context.Background(), 41, CodexTicketVaultRevokeInput{Model: usageTestModel, All: true})
	require.NoError(t, err)
	require.Equal(t, 3, result.Revoked)
	require.Zero(t, result.Remaining)
	for _, slot := range repo.persisted(t) {
		require.True(t, slot.Revoked)
		require.Equal(t, CodexTicketVaultInvalidationReason, slot.Invalidation.Reason)
	}
	account, err := repo.GetByID(context.Background(), 41)
	require.NoError(t, err)
	require.Nil(t, svc.lookupOpenAICodexTicketForUse(account, usageTestModel, svc.openAICodexTicketConfigForAccount(context.Background(), account)))
}

func TestCodexTicketVaultRevokeReportsPersistentFailure(t *testing.T) {
	now := time.Now()
	a := usageTestTicket("A", time.Minute, now)
	svc, repo := vaultTestService(t, config.OpenAICodexTicketConfig{FailClosed: true}, a)
	repo.failNext = 100

	result, err := svc.RevokeOpenAICodexTicketVault(context.Background(), 41, CodexTicketVaultRevokeInput{Model: usageTestModel, All: true})
	require.Error(t, err)
	require.Zero(t, result.Revoked)
	require.Equal(t, 1, result.Remaining)
	require.False(t, repo.persisted(t)[0].Revoked, "写库失败时数据库仍未作废")
	account, err := repo.GetByID(context.Background(), 41)
	require.NoError(t, err)
	require.Nil(t, svc.lookupOpenAICodexTicketForUse(account, usageTestModel, svc.openAICodexTicketConfigForAccount(context.Background(), account)), "本机立即停用")
}

func TestCodexTicketVaultRevokeValidation(t *testing.T) {
	now := time.Now()
	svc, repo := vaultTestService(t, config.OpenAICodexTicketConfig{}, usageTestTicket("A", time.Minute, now))
	ctx := context.Background()
	invalid := []CodexTicketVaultRevokeInput{
		{Fingerprint: strings.Repeat("a", 24)},
		{Model: usageTestModel},
		{Model: usageTestModel, All: true, Fingerprint: strings.Repeat("a", 24)},
		{Model: usageTestModel, Fingerprint: "short"},
		{Model: usageTestModel, Fingerprint: strings.Repeat("g", 24)},
		{Model: strings.Repeat("m", codexTicketVaultMaxModelLength+1), All: true},
	}
	for _, input := range invalid {
		_, err := svc.RevokeOpenAICodexTicketVault(ctx, 41, input)
		require.ErrorIs(t, err, ErrCodexTicketVaultInvalidInput, input)
	}
	_, err := svc.RevokeOpenAICodexTicketVault(ctx, 41, CodexTicketVaultRevokeInput{Model: usageTestModel, Fingerprint: strings.Repeat("a", 24)})
	require.ErrorIs(t, err, ErrCodexTicketVaultSlotNotFound)
	_, err = svc.RevokeOpenAICodexTicketVault(ctx, 41, CodexTicketVaultRevokeInput{Model: "gpt-unknown", All: true})
	require.ErrorIs(t, err, ErrCodexTicketVaultSlotNotFound)
	require.Zero(t, repo.writes)

	repo.account.Platform = PlatformAnthropic
	_, err = svc.GetOpenAICodexTicketVault(ctx, 41)
	require.ErrorIs(t, err, ErrCodexTicketNodesUnavailable)
	_, err = svc.RevokeOpenAICodexTicketVault(ctx, 41, CodexTicketVaultRevokeInput{Model: usageTestModel, All: true})
	require.ErrorIs(t, err, ErrCodexTicketNodesUnavailable)
}

func TestCodexTicketNodeModelsSkipMetadataKeys(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = map[string]any{
		OpenAICodexTicketHistoryKey:           []any{},
		OpenAICodexTicketInvalidationsKey:     []any{},
		OpenAICodexTicketConsumedKey:          map[string]any{},
		openAICodexTicketExtraKey("gpt-left"): map[string]any{},
	}
	require.Equal(t, []string{usageTestModel, "gpt-left"}, codexTicketNodeModels(account, config.OpenAICodexTicketConfig{Models: []string{usageTestModel}}))
}
