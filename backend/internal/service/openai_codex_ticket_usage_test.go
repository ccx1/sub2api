package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const usageTestModel = "gpt-6-astra"

// usageTestTicket 构造一张按状态长度可用的票；marker 区分票据身份，age 是距首次采集的时长。
func usageTestTicket(marker string, age time.Duration, now time.Time) *openAICodexTicket {
	return &openAICodexTicket{AccountID: 41, Model: usageTestModel, State: openAICodexTicketStatePrefix + strings.Repeat(marker, 286),
		Length: 292, CapturedAt: now.Add(-age), ExpiresAt: now.Add(time.Hour)}
}

// usageTestPool 按给定顺序组成主票、备用与储备票。
func usageTestPool(slots ...*openAICodexTicket) *openAICodexTicket {
	if len(slots) == 0 {
		return nil
	}
	root := codexTicketLeaf(slots[0])
	if len(slots) > 1 {
		root.Standby = codexTicketLeaf(slots[1])
	}
	for _, slot := range slots[min(2, len(slots)):] {
		root.Reserve = append(root.Reserve, codexTicketLeaf(slot))
	}
	return root
}

func usageTestService(t *testing.T, cfg config.OpenAICodexTicketConfig, slots ...*openAICodexTicket) *OpenAIGatewayService {
	t.Helper()
	cfg.Enabled = true
	if cfg.PoolCapacity == 0 {
		cfg.PoolCapacity = 5
	}
	svc := ticketTestService(t, cfg, nil)
	if len(slots) > 0 {
		svc.openaiCodexTickets.Store(openAICodexTicketKey(41, usageTestModel), usageTestPool(slots...))
	}
	return svc
}

func usageTestStates(tickets ...*openAICodexTicket) []string {
	states := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		states = append(states, ticket.State)
	}
	return states
}

func usageTestApply(ctx context.Context, svc *OpenAIGatewayService, account *Account) (*openAICodexTicketReceipt, string, error) {
	h := http.Header{}
	receipt, err := svc.applyOpenAICodexTicketSnapshot(ctx, account, usageTestModel, h)
	return receipt, h.Get(openAICodexTurnStateHeader), err
}

type codexTicketClaimStub struct {
	AccountRepository
	claim   func(fingerprint string) (bool, error)
	ids     []string
	limits  []int
	expires []time.Time
}

func (r *codexTicketClaimStub) ClaimCodexTicketConsumption(_ context.Context, _ int64, fingerprint string, expiresAt, _ time.Time, limit int) (bool, error) {
	r.ids = append(r.ids, fingerprint)
	r.limits = append(r.limits, limit)
	r.expires = append(r.expires, expiresAt)
	return r.claim(fingerprint)
}

func TestCodexTicketUsageCandidatesFollowUsageMode(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	fresh, mid := usageTestTicket("F", 10*time.Second, now), usageTestTicket("M", 400*time.Second, now)
	young, old := usageTestTicket("Y", 200*time.Second, now), usageTestTicket("O", 900*time.Second, now)
	pool := usageTestPool(fresh, mid, young, old)

	immediate := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true})
	require.Equal(t, usageTestStates(fresh, mid, young, old), usageTestStates(codexTicketUsageCandidates(pool, account, immediate, now)...))
	require.Equal(t, fresh.State, selectOpenAICodexTicket(pool, account, immediate, now).State, "即取即用保持票池首张可用票")

	aged := immediate
	aged.UsageMode, aged.MinTicketAgeSeconds = config.CodexTicketUsageAged, 300
	require.Equal(t, usageTestStates(old, mid), usageTestStates(codexTicketUsageCandidates(pool, account, aged, now)...), "只取满时长的票且最老优先")
	require.Equal(t, old.State, selectOpenAICodexTicket(pool, account, aged, now).State)

	// 软复验刷新 CapturedAt，但沉淀时长按谱系首次采集时间计算。
	revalidated := usageTestTicket("R", 5*time.Second, now)
	revalidated.OriginCapturedAt = now.Add(-2000 * time.Second)
	require.Equal(t, usageTestStates(revalidated, mid),
		usageTestStates(codexTicketUsageCandidates(usageTestPool(fresh, mid, revalidated), account, aged, now)...))

	// 沉淀模式不回退到未满时长的票。
	require.Empty(t, codexTicketUsageCandidates(usageTestPool(fresh, young), account, aged, now))
	require.Nil(t, selectOpenAICodexTicket(usageTestPool(fresh, young), account, aged, now))
}

func TestHistoricalTicketUseStartsAndPersistsValidityWindow(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	ticket := usageTestTicket("U", time.Hour, now)
	cfg := config.OpenAICodexTicketConfig{FailClosed: true, UsageMode: config.CodexTicketUsageAged,
		MinTicketAgeSeconds: 60, HistoricalTicketValiditySeconds: 300}
	svc := usageTestService(t, cfg, ticket)

	_, err := svc.applyOpenAICodexTicketSnapshot(context.Background(), account, usageTestModel, http.Header{})
	require.NoError(t, err)
	raw, ok := svc.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, usageTestModel))
	require.True(t, ok)
	stored := raw.(*openAICodexTicket)
	require.False(t, stored.HistoricalUsedAt.IsZero())
	require.WithinDuration(t, stored.HistoricalUsedAt.Add(300*time.Second), stored.historicalExpires(cfg), time.Second)
	status := OpenAICodexTicketStatuses(account, config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{usageTestModel}, UsageMode: config.CodexTicketUsageAged,
		MinTicketAgeSeconds: 60, HistoricalTicketValiditySeconds: 300}), time.Now())[0]
	require.NotNil(t, status.HistoricalUsedAt)
}

func TestCodexTicketLatestOnlySelectsNewestLineageWithoutFallback(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	old := usageTestTicket("O", 2*time.Hour, now)
	newest := usageTestTicket("N", time.Hour, now)
	refreshedOld := usageTestTicket("R", time.Minute, now)
	refreshedOld.OriginCapturedAt = old.CapturedAt
	pool := usageTestPool(old, newest, refreshedOld)
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{UsageMode: config.CodexTicketUsageLatestOnly})
	require.Equal(t, usageTestStates(newest), usageTestStates(codexTicketUsageCandidates(pool, account, cfg, now)...))
	newest.Revoked = true
	pool = usageTestPool(old, newest, refreshedOld)
	require.Empty(t, codexTicketUsageCandidates(pool, account, cfg, now), "最新谱系不可用时不使用旧票")
	require.Nil(t, selectOpenAICodexTicket(pool, account, cfg, now))
	status := codexTicketPoolStatus(usageTestModel, pool, account, cfg, now)
	require.Zero(t, status.AvailableCount)
	require.Zero(t, status.ReserveCount)
	require.Equal(t, "revoked", status.PrimaryReason)
}

func TestCodexTicketImmediateUsesPoolOrderAndFallsBack(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	old, newest, middle := usageTestTicket("O", 3*time.Hour, now), usageTestTicket("N", time.Hour, now), usageTestTicket("M", 2*time.Hour, now)
	pool := usageTestPool(old, middle, newest)
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{UsageMode: config.CodexTicketUsageImmediate})
	require.Equal(t, usageTestStates(old, middle, newest), usageTestStates(codexTicketUsageCandidates(pool, account, cfg, now)...))
	status := codexTicketPoolStatus(usageTestModel, pool, account, cfg, now)
	require.False(t, status.UsingStandby)
	require.True(t, status.StandbyReady)
	old.Revoked = true
	pool = usageTestPool(old, middle, newest)
	require.Equal(t, middle.State, selectOpenAICodexTicket(pool, account, cfg, now).State)
	status = codexTicketPoolStatus(usageTestModel, pool, account, cfg, now)
	require.True(t, status.UsingStandby)
	require.Equal(t, "revoked", status.PrimaryReason)
}

func TestCodexTicketUsageAgedInjectsMaturedTicket(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	fresh, old := usageTestTicket("F", 10*time.Second, now), usageTestTicket("O", 900*time.Second, now)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, UsageMode: config.CodexTicketUsageAged, MinTicketAgeSeconds: 300}, fresh, old)

	receipt, state, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.NotNil(t, receipt)
	require.Nil(t, receipt.claimed, "仅沉淀模式不领取票据")
	require.Equal(t, old.State, state)
	require.False(t, svc.openAICodexTicketBlocksAccount(account, usageTestModel))

	// 切回即取即用后恢复原行为：使用票池首张可用票。
	svc.cfg.Gateway.OpenAICodexTicket.UsageMode = config.CodexTicketUsageImmediate
	svc.cfg.Gateway.OpenAICodexTicket.MinTicketAgeSeconds = 0
	_, state, err = usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, fresh.State, state)
}

func TestCodexTicketUsageAgedWithoutMaturedTicket(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, UsageMode: config.CodexTicketUsageAged, MinTicketAgeSeconds: 300},
		usageTestTicket("F", 10*time.Second, now))

	receipt, state, err := usageTestApply(context.Background(), svc, account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Nil(t, receipt)
	require.Empty(t, state)
	require.True(t, svc.openAICodexTicketBlocksAccount(account, usageTestModel), "调度与注入使用同一口径")
	require.True(t, svc.openAICodexTicketBlocksAccountContext(context.Background(), account, usageTestModel))

	// 未开启 fail_closed 时与原行为一致：不注入，也不改动客户端请求头。
	svc.cfg.Gateway.OpenAICodexTicket.FailClosed = false
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, "client-state")
	receipt, err = svc.applyOpenAICodexTicketSnapshot(context.Background(), account, usageTestModel, h)
	require.NoError(t, err)
	require.Nil(t, receipt)
	require.Equal(t, "client-state", h.Get(openAICodexTurnStateHeader))
	require.False(t, svc.openAICodexTicketBlocksAccount(account, usageTestModel))
}

func TestCodexTicketConsumeAfterUseServesEachTicketOnce(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b, c := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now), usageTestTicket("C", 10*time.Second, now)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true, PoolCapacity: 3}, a, b, c)
	cfg := svc.openAICodexTicketConfig()
	require.False(t, svc.codexTicketInventoryNeedsRefresh(account, usageTestModel, cfg, time.Now()))
	require.False(t, svc.openAICodexTicketBlocksAccountContext(context.Background(), account, usageTestModel))

	for index, want := range []*openAICodexTicket{a, b, c} {
		receipt, state, err := usageTestApply(context.Background(), svc, account)
		require.NoError(t, err, index)
		require.Equal(t, want.State, state, index)
		require.NotNil(t, receipt.claimed, index)
		require.Equal(t, want.State, receipt.claimed.State, index)
		require.False(t, receipt.ticket.consumed, "回执中的发送票仍可通过发送前核验")
		if index == 0 {
			require.True(t, svc.codexTicketInventoryNeedsRefresh(account, usageTestModel, cfg, time.Now()), "领取后立即触发补票")
		}
	}

	receipt, state, err := usageTestApply(context.Background(), svc, account)
	require.Nil(t, receipt)
	require.Empty(t, state)
	require.True(t, IsOpenAITurnAdmissionError(err), "票被领完按准入失败处理")
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	// 逐轮准入交给实际领取判定；调度视图在票被领完后不再选中该账号。
	require.False(t, svc.openAICodexTicketBlocksAccount(account, usageTestModel))
	require.True(t, svc.openAICodexTicketBlocksAccountContext(context.Background(), account, usageTestModel))

	svc.cfg.Gateway.OpenAICodexTicket.FailClosed = false
	receipt, state, err = usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Nil(t, receipt)
	require.Empty(t, state)
}

func TestCodexTicketConsumeAfterUseReusesClaimWithinOneSend(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true}, a, b)

	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, usageTestModel, req))
	require.Equal(t, a.State, req.Header.Get(openAICodexTurnStateHeader))
	require.NoError(t, svc.validateOpenAICodexTicketSend(req, account), "已领取的票仍通过发送前核验")

	// 同一次发送的重复注入沿用已领取的票，不再消耗第二张。
	reuse := openAICodexTicketReuseContext(context.Background(), req)
	h := http.Header{}
	require.NoError(t, svc.applyOpenAICodexTicket(reuse, account, usageTestModel, h))
	require.Equal(t, a.State, h.Get(openAICodexTurnStateHeader))

	// 其他模型或其他账号不能借用这次领取。
	err = svc.applyOpenAICodexTicket(reuse, account, "gpt-5.6-sol", http.Header{})
	require.True(t, IsOpenAITurnAdmissionError(err))
	err = svc.applyOpenAICodexTicket(reuse, ticketTestAccount(42), usageTestModel, http.Header{})
	require.True(t, IsOpenAITurnAdmissionError(err))

	// 新的一次发送领取下一张票。
	_, state, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, b.State, state)
}

func TestCodexTicketConsumeAfterUseWebSocketReuseStopsAfterRevocation(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true}, a, b)

	receipt, state, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, a.State, state)
	ws := codexTicketWSReceiptFromSnapshot(receipt)
	require.NotNil(t, ws.claimed)
	require.NoError(t, ws.validate(time.Now()), "握手票已在库存中标记为已用，连接内核验仍应通过")

	_, state, err = usageTestApply(withOpenAICodexTicketWSReuse(context.Background(), ws), svc, account)
	require.NoError(t, err)
	require.Equal(t, a.State, state, "同一连接的后续轮次沿用握手票")

	svc.rememberCodexTicketRevocation(openAICodexTicketKey(account.ID, usageTestModel), &receipt.ticket)
	require.ErrorIs(t, ws.validate(time.Now()), ErrOpenAICodexTicketUnavailable)
	_, state, err = usageTestApply(withOpenAICodexTicketWSReuse(context.Background(), ws), svc, account)
	require.NoError(t, err)
	require.Equal(t, b.State, state, "被撤销的票不再沿用，改为领取新票")
}

func TestCodexTicketConsumeAfterUseHonorsSharedLedgerClaim(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b, c := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now), usageTestTicket("C", 10*time.Second, now)
	cfg := config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true}
	svc := usageTestService(t, cfg, a, b, c)
	stub := &codexTicketClaimStub{claim: func(id string) (bool, error) { return id != codexTicketConsumptionID(a), nil }}
	svc.accountRepo = stub

	for _, want := range []*openAICodexTicket{b, c} {
		_, state, err := usageTestApply(context.Background(), svc, account)
		require.NoError(t, err)
		require.Equal(t, want.State, state, "其他实例已在账本领取的票被跳过")
	}
	_, _, err := usageTestApply(context.Background(), svc, account)
	require.True(t, IsOpenAITurnAdmissionError(err))
	require.Equal(t, []string{codexTicketConsumptionID(a), codexTicketConsumptionID(b), codexTicketConsumptionID(c)}, stub.ids)
	for index := range stub.ids {
		require.Equal(t, OpenAICodexTicketConsumedLimit, stub.limits[index])
		require.True(t, stub.expires[index].After(now))
	}

	// 账本暂不可写时保留本机领取继续服务，同一张票在本机也不会再次使用。
	svc = usageTestService(t, cfg, a, b)
	calls := 0
	svc.accountRepo = &codexTicketClaimStub{claim: func(string) (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("ledger unavailable")
		}
		return true, nil
	}}
	for _, want := range []*openAICodexTicket{a, b} {
		_, state, err := usageTestApply(context.Background(), svc, account)
		require.NoError(t, err)
		require.Equal(t, want.State, state)
	}
}

func TestCodexTicketConsumptionLedgerAppliesAcrossInstancesAndModes(t *testing.T) {
	now := time.Now()
	a, b := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now)
	account := ticketTestAccount(41)
	account.Extra = map[string]any{OpenAICodexTicketConsumedKey: map[string]any{codexTicketConsumptionID(a): float64(now.Add(time.Hour).Unix())}}

	consume := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true}, a, b)
	_, state, err := usageTestApply(context.Background(), consume, account)
	require.NoError(t, err)
	require.Equal(t, b.State, state, "其他实例已领取的票不再使用")

	// 关闭用后即删后，账本中已领取、尚未删除的票仍不会被再次使用。
	immediate := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true}, a, b)
	for range 2 {
		_, state, err = usageTestApply(context.Background(), immediate, account)
		require.NoError(t, err)
		require.Equal(t, b.State, state)
	}

	account.Extra[openAICodexTicketExtraKey(usageTestModel)] = usageTestPool(a, b)
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true})
	statuses := OpenAICodexTicketStatuses(account, cfg, time.Now())
	index := -1
	for i := range statuses {
		if statuses[i].Model == usageTestModel {
			index = i
		}
	}
	require.GreaterOrEqual(t, index, 0)
	status := statuses[index]
	require.Equal(t, "consumed", status.PrimaryReason)
	require.False(t, status.PrimaryReady)
	require.Equal(t, 1, status.AvailableCount)
	require.True(t, status.Ready)
	require.True(t, status.UsingStandby)

	// 过期的账本条目不再生效。
	account.Extra[OpenAICodexTicketConsumedKey] = map[string]any{codexTicketConsumptionID(a): float64(now.Add(-time.Minute).Unix())}
	require.Empty(t, codexTicketConsumptionLedgerFromAccount(account, time.Now()))
}

func TestCodexTicketConsumedTicketsDroppedAtNextPublication(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true}, a, b)
	_, state, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, a.State, state)

	d := usageTestTicket("D", 0, time.Now())
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, d))
	raw, ok := svc.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, usageTestModel))
	require.True(t, ok)
	inventory, ok := raw.(*openAICodexTicket)
	require.True(t, ok)
	require.Equal(t, usageTestStates(b, d), usageTestStates(codexTicketSlots(inventory)...), "已领取的票在下一次发布时被物理删除")

	for _, want := range []*openAICodexTicket{b, d} {
		_, state, err = usageTestApply(context.Background(), svc, account)
		require.NoError(t, err)
		require.Equal(t, want.State, state)
	}
	_, _, err = usageTestApply(context.Background(), svc, account)
	require.True(t, IsOpenAITurnAdmissionError(err))
}

func TestCodexTicketImmediateUsageKeepsLegacyBehavior(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true}, a, b)

	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, usageTestModel, req))
	require.Equal(t, a.State, req.Header.Get(openAICodexTurnStateHeader))
	ctx := context.Background()
	require.Equal(t, ctx, openAICodexTicketReuseContext(ctx, req), "即取即用不产生领取记录")

	for range 2 {
		receipt, state, err := usageTestApply(ctx, svc, account)
		require.NoError(t, err)
		require.Nil(t, receipt.claimed)
		require.Equal(t, a.State, state, "同一张票可以重复使用")
	}
	require.Nil(t, svc.codexTicketConsumptionIndex(account.ID, false), "即取即用不建立领取索引")
	require.False(t, svc.openAICodexTicketBlocksAccount(account, usageTestModel))
}

func TestCodexTicketConsumptionIDIsStableAcrossSoftRevalidation(t *testing.T) {
	now := time.Now()
	a := usageTestTicket("A", time.Minute, now)
	id := codexTicketConsumptionID(a)
	require.Len(t, id, codexTicketConsumptionIDLength)
	require.True(t, validCodexTicketConsumptionID(id))
	require.NotContains(t, a.State, id, "指纹不暴露票据内容")

	revalidated := codexTicketLeaf(a)
	revalidated.OriginCapturedAt, revalidated.CapturedAt = a.CapturedAt, now
	revalidated.ExpiresAt, revalidated.RevalidatedAt = now.Add(2*time.Hour), now
	require.Equal(t, id, codexTicketConsumptionID(revalidated), "软复验后的同谱系票仍视为已使用")

	otherState := usageTestTicket("B", time.Minute, now)
	require.NotEqual(t, id, codexTicketConsumptionID(otherState))
	otherModel := codexTicketLeaf(a)
	otherModel.Model = "gpt-5.6-sol"
	require.NotEqual(t, id, codexTicketConsumptionID(otherModel))
	otherLineage := codexTicketLeaf(a)
	otherLineage.CapturedAt = a.CapturedAt.Add(time.Second)
	require.NotEqual(t, id, codexTicketConsumptionID(otherLineage))
	require.Empty(t, codexTicketConsumptionID(nil))

	for _, invalid := range []string{"", strings.Repeat("a", 23), strings.Repeat("a", 25), strings.Repeat("A", 24), strings.Repeat("g", 24)} {
		require.False(t, validCodexTicketConsumptionID(invalid), invalid)
	}
}

func TestParseCodexTicketConsumptionLedgerAcceptsStoredShapes(t *testing.T) {
	id1, id2, id3 := strings.Repeat("a", 24), strings.Repeat("0", 23)+"1", strings.Repeat("e", 24)
	parsed := parseCodexTicketConsumptionLedger(map[string]any{
		id1: float64(1700000000), id2: "1700000001", id3: json.Number("1.7e9"),
		"short": float64(1700000000), strings.Repeat("b", 24): float64(0), strings.Repeat("c", 24): true,
		strings.Repeat("d", 24): "not-a-number",
	})
	require.Equal(t, map[string]time.Time{id1: time.Unix(1700000000, 0), id2: time.Unix(1700000001, 0), id3: time.Unix(1700000000, 0)}, parsed)

	big := int64(1<<53) + 1
	parsed = parseCodexTicketConsumptionLedger(json.RawMessage(fmt.Sprintf(`{%q:%d}`, id1, big)))
	require.Equal(t, big, parsed[id1].Unix(), "整数精确解析，不经 float64 舍入")

	parsed = parseCodexTicketConsumptionLedger([]byte(fmt.Sprintf(`{%q:"1700000002"}`, id2)))
	require.Equal(t, map[string]time.Time{id2: time.Unix(1700000002, 0)}, parsed)

	parsed = parseCodexTicketConsumptionLedger(map[string]int64{id3: 1700000003})
	require.Equal(t, map[string]time.Time{id3: time.Unix(1700000003, 0)}, parsed)

	require.Nil(t, parseCodexTicketConsumptionLedger(nil))
	require.Nil(t, parseCodexTicketConsumptionLedger(json.RawMessage("{")))
	require.Nil(t, parseCodexTicketConsumptionLedger([]any{1, 2}))
}

func TestCodexTicketConsumptionUntilCoversTicketLifetime(t *testing.T) {
	now := time.Now()
	cfg := config.OpenAICodexTicketConfig{TTLSeconds: 3600}
	ticket := usageTestTicket("A", 0, now)

	ticket.ExpiresAt = now.Add(2 * time.Hour)
	until := codexTicketConsumptionUntil(ticket, cfg, now)
	require.Equal(t, now.Add(2*time.Hour+codexTicketConsumptionGrace).Unix(), until.Unix())
	require.Zero(t, until.Nanosecond())

	ticket.ExpiresAt = now.Add(48 * time.Hour)
	require.Equal(t, now.Add(48*time.Hour+codexTicketConsumptionGrace).Unix(), codexTicketConsumptionUntil(ticket, cfg, now).Unix())
	ticket.ExpiresAt = now.Add(120 * 24 * time.Hour)
	require.Equal(t, now.Add(codexTicketConsumptionMaxTTL).Unix(), codexTicketConsumptionUntil(ticket, cfg, now).Unix(), "不超过硬上限")

	// 票据早于配置有效期失效时，仍至少覆盖配置有效期。
	ticket.ExpiresAt = now.Add(time.Minute)
	require.Equal(t, now.Add(time.Hour+codexTicketConsumptionGrace).Unix(), codexTicketConsumptionUntil(ticket, cfg, now).Unix())
	require.Equal(t, now.Add(time.Hour+codexTicketConsumptionGrace).Unix(), codexTicketConsumptionUntil(nil, cfg, now).Unix())
}

func TestCodexTicketConsumptionIndexMarkReleaseMergeAndPrune(t *testing.T) {
	now := time.Now()
	index := &codexTicketConsumptionIndex{entries: make(map[string]time.Time)}
	id := strings.Repeat("a", 24)
	until := now.Add(time.Hour)
	require.True(t, index.mark(id, until, now))
	require.False(t, index.mark(id, until.Add(time.Minute), now), "同一张票只能领取一次")
	index.release(id, until.Add(time.Second))
	require.Contains(t, index.entries, id, "只撤回本次写入的条目")
	index.release(id, until)
	require.NotContains(t, index.entries, id)
	require.True(t, index.mark(id, until, now))

	// 合并其他实例的记录：忽略已过期条目，不缩短本机条目。
	other, expired := strings.Repeat("b", 24), strings.Repeat("c", 24)
	index.merge(map[string]time.Time{id: now.Add(time.Minute), other: now.Add(2 * time.Hour), expired: now.Add(-time.Second)}, now)
	require.Equal(t, until, index.entries[id])
	require.Equal(t, now.Add(2*time.Hour), index.entries[other])
	require.NotContains(t, index.entries, expired)
	// 合并进来的条目阻止本机重复领取，本机撤回也不会误删它。
	require.False(t, index.mark(other, now.Add(time.Hour), now))
	index.release(other, now.Add(time.Hour))
	require.Contains(t, index.entries, other)

	// 已过期的条目不阻止再次领取。
	stale := strings.Repeat("d", 24)
	index.entries[stale] = now.Add(-time.Minute)
	require.True(t, index.mark(stale, until, now))

	// 超过上限时淘汰最早失效的条目。
	capped := &codexTicketConsumptionIndex{entries: make(map[string]time.Time)}
	for i := range codexTicketConsumptionLocalCap + 1 {
		require.True(t, capped.mark(fmt.Sprintf("%024x", i), now.Add(time.Duration(i+1)*time.Minute), now))
	}
	require.Len(t, capped.entries, codexTicketConsumptionLocalCap)
	require.NotContains(t, capped.entries, fmt.Sprintf("%024x", 0))
	require.Contains(t, capped.entries, fmt.Sprintf("%024x", codexTicketConsumptionLocalCap))

	var missing *codexTicketConsumptionIndex
	require.False(t, missing.mark(id, until, now))
	missing.release(id, until)
	missing.merge(map[string]time.Time{id: until}, now)
}

func TestCodexTicketConsumedLedgerIsPrivateMetaKey(t *testing.T) {
	for _, key := range []string{OpenAICodexTicketConsumedKey, OpenAICodexTicketHistoryKey, OpenAICodexTicketInvalidationsKey} {
		require.True(t, IsOpenAICodexTicketMetaExtraKey(key), key)
		require.True(t, IsOpenAICodexTicketExtraKey(key), key)
		require.True(t, IsOpenAICodexTicketPrivateExtraKey(key), key)
	}
	require.False(t, IsOpenAICodexTicketMetaExtraKey(openAICodexTicketExtraKey(usageTestModel)))

	ledger := map[string]any{strings.Repeat("a", 24): float64(time.Now().Add(time.Hour).Unix())}
	require.Nil(t, parseOpenAICodexTicketFromAny(41, "consumed", ledger), "账本不会被解析成票池")
	require.Equal(t, map[string]any{"keep": 1}, RedactOpenAICodexTicketExtra(map[string]any{OpenAICodexTicketConsumedKey: ledger, "keep": 1}))
	merged := MergeOpenAICodexTicketExtra(map[string]any{OpenAICodexTicketConsumedKey: map[string]any{}}, map[string]any{OpenAICodexTicketConsumedKey: ledger})
	require.Equal(t, ledger, merged[OpenAICodexTicketConsumedKey], "账号编辑不能覆盖账本")
}

func TestCodexTicketRevalidationSkipsConsumedTickets(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now)
	for _, ticket := range []*openAICodexTicket{a, b} {
		ticket.Verified, ticket.RevalidateAt = true, now.Add(-time.Second)
	}
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true}, a, b)
	cfg := svc.openAICodexTicketConfig()

	target, _ := svc.codexTicketRevalidationTarget(account, usageTestModel, cfg)
	require.NotNil(t, target)
	require.Equal(t, a.State, target.State)

	_, state, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, a.State, state)
	target, _ = svc.codexTicketRevalidationTarget(account, usageTestModel, cfg)
	require.NotNil(t, target)
	require.Equal(t, b.State, target.State, "已领取的票不再复验续期")
}

func TestCodexTicketUsagePolicyDoesNotChangeBindingOrProbeConfig(t *testing.T) {
	account := ticketTestAccount(41)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true})
	base := svc.openAICodexTicketConfig()
	policy := base
	policy.UsageMode, policy.MinTicketAgeSeconds, policy.ConsumeAfterUse = config.CodexTicketUsageAged, 300, true
	require.Equal(t, svc.codexTicketBindingForConfig(account, base), svc.codexTicketBindingForConfig(account, policy), "切换取票机制不让已发布票失配")

	// 在途采集按旧取票机制捕获配置快照，切换取票机制不应中止它。
	input := openAICodexTicketProbeInput{Account: account, Config: &policy, SubscriptionTier: openAICodexTicketSubscriptionTier(account)}
	require.True(t, svc.openAICodexTicketProbeConfigCurrent(context.Background(), input))
}

func TestCodexTicketConsumeAfterUseReleasesClaimForUnusableTarget(t *testing.T) {
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true, CredentialMode: "cookie", CookieTTLSeconds: 60})
	account := ticketTestAccount(41)
	expires := time.Now().Add(time.Minute)
	ticket := &openAICodexTicket{AccountID: 41, Model: usageTestModel, CredentialMode: "cookie", Verified: true,
		CapturedAt: time.Now(), ExpiresAt: expires,
		Cookies: []*http.Cookie{{Name: "ticket_session", Value: "private", Path: "/backend-api", Secure: true, Expires: expires}}}
	svc.openaiCodexTickets.Store(openAICodexTicketKey(41, usageTestModel), usageTestPool(ticket))

	target, err := url.Parse("https://example.com/backend-api/codex/responses")
	require.NoError(t, err)
	h := http.Header{}
	receipt, err := svc.applyOpenAICodexTicketSnapshot(withOpenAICodexTicketTargetURL(context.Background(), target), account, usageTestModel, h)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Nil(t, receipt)
	require.Empty(t, h.Get("Cookie"))

	// 目标地址用不上该票时撤回领取，不消耗这张票。
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, usageTestModel, req))
	require.Equal(t, "ticket_session=private", req.Header.Get("Cookie"))
	require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))

	err = svc.applyOpenAICodexTicketRequest(account, usageTestModel, &http.Request{Method: http.MethodPost, URL: req.URL, Header: http.Header{}})
	require.True(t, IsOpenAITurnAdmissionError(err), "唯一的票已被领取")
}
