//go:build unit

package service

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

type ticketProxyWriteSpy struct {
	longContextBillingRepoStub
	beforeWrite                func()
	patch                      map[string]any
	writes                     int
	requestedMode, requestedID bool
}

func (r *ticketProxyWriteSpy) GetByID(context.Context, int64) (*Account, error) {
	return cloneOpenAICodexTicketAccount(r.account), nil
}

func (r *ticketProxyWriteSpy) Update(ctx context.Context, account *Account) error {
	r.requestedMode, r.requestedID = CodexTicketProxyWriteFields(ctx)
	r.patch = maps.Clone(account.Extra)
	if r.beforeWrite != nil {
		r.beforeWrite()
	}
	merged := MergeOpenAICodexTicketExtra(account.Extra, r.account.Extra)
	if err := ValidateCodexTicketProxyExtra(merged); err != nil {
		return err
	}
	account.Extra = merged
	r.account = account
	r.writes++
	return nil
}

func (r *ticketProxyWriteSpy) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.patch = maps.Clone(updates)
	if r.beforeWrite != nil {
		r.beforeWrite()
	}
	merged := MergeOpenAICodexTicketExtra(updates, r.account.Extra)
	if err := ValidateCodexTicketProxyExtra(merged); err != nil {
		return err
	}
	r.account.Extra = merged
	r.writes++
	return nil
}

func TestAdminAccountCodexTicketProxyUnrelatedUpdateKeepsFixedSetting(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 7}
	repo := &ticketProxyWriteSpy{longContextBillingRepoStub: longContextBillingRepoStub{account: account}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 41, &UpdateAccountInput{Extra: map[string]any{"custom": true}})
	require.NoError(t, err)
	require.Equal(t, "fixed", updated.Extra[CodexTicketProxyModeExtraKey])
	require.Equal(t, 7, updated.Extra[CodexTicketProxyIDExtraKey])
	require.Equal(t, true, updated.Extra["custom"])
	require.NotContains(t, repo.patch, CodexTicketProxyModeExtraKey)
	require.NotContains(t, repo.patch, CodexTicketProxyIDExtraKey)
	require.False(t, repo.requestedMode)
	require.False(t, repo.requestedID)
}

func TestAdminAccountCodexTicketProxyPartialSwitchToRandomClearsID(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 7}
	repo := &longContextBillingRepoStub{account: account}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 41, &UpdateAccountInput{Extra: map[string]any{CodexTicketProxyModeExtraKey: "random"}})
	require.NoError(t, err)
	require.Equal(t, "random", updated.Extra[CodexTicketProxyModeExtraKey])
	require.EqualValues(t, 0, updated.Extra[CodexTicketProxyIDExtraKey])
}

func TestAdminAccountCodexTicketProxyRejectsUnavailableFixedBeforeWrite(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 7}
	for _, operation := range []string{"create", "update", "extra", "bulk"} {
		t.Run(operation, func(t *testing.T) {
			repo := &longContextBillingRepoStub{account: cloneOpenAICodexTicketAccount(account)}
			svc := &adminServiceImpl{accountRepo: repo}
			extra := map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 8}
			var err error
			switch operation {
			case "create":
				_, err = svc.CreateAccount(context.Background(), &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, SkipDefaultGroupBind: true, Extra: extra})
			case "update":
				_, err = svc.UpdateAccount(context.Background(), 41, &UpdateAccountInput{Extra: extra})
			case "extra":
				err = svc.UpdateAccountExtra(context.Background(), 41, extra)
			case "bulk":
				_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{41}, Extra: extra})
			}
			requireApplicationErrorReason(t, err, "CODEX_TICKET_PROXY_UNAVAILABLE")
			require.Nil(t, repo.createdAccount)
			require.Zero(t, repo.updateExtraCalls)
			require.Zero(t, repo.bulkUpdateCalls)
			require.Equal(t, 7, repo.account.Extra[CodexTicketProxyIDExtraKey])
		})
	}
}

func TestAdminAccountCodexTicketProxyPartialIDChecksSelectedProxy(t *testing.T) {
	account := ticketTestAccount(41)
	account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 7}
	repo := &longContextBillingRepoStub{account: account}
	proxyRepo := &updatingProxyRepoStub{proxy: &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "selected.example", Port: 8080}}
	svc := &adminServiceImpl{accountRepo: repo, proxyRepo: proxyRepo}
	err := svc.UpdateAccountExtra(context.Background(), 41, map[string]any{CodexTicketProxyIDExtraKey: 8})
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateExtraCalls)
}

func TestAdminAccountCodexTicketProxyBulkShadowRejectsBeforeAnyWrite(t *testing.T) {
	regular, shadow := ticketTestAccount(41), ticketTestAccount(42)
	shadow.ParentAccountID = &regular.ID
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{regular, shadow}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{41, 42},
		Extra: map[string]any{CodexTicketProxyModeExtraKey: "random", CodexTicketProxyIDExtraKey: 0}})
	requireApplicationErrorReason(t, err, "CODEX_TICKET_PROXY_ACCOUNT_UNSUPPORTED")
	require.Zero(t, repo.bulkUpdateCalls)
	require.Empty(t, regular.Extra)
	require.Empty(t, shadow.Extra)
}

func TestAdminAccountCodexTicketProxyUnrelatedEditPreservesConcurrentModeChange(t *testing.T) {
	for _, extra := range []map[string]any{nil, {"custom": true}} {
		account := ticketTestAccount(41)
		account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 7}
		repo := &ticketProxyWriteSpy{longContextBillingRepoStub: longContextBillingRepoStub{account: account}}
		repo.beforeWrite = func() {
			repo.account.Extra[CodexTicketProxyModeExtraKey], repo.account.Extra[CodexTicketProxyIDExtraKey] = "random", 0
		}
		svc := &adminServiceImpl{accountRepo: repo}
		updated, err := svc.UpdateAccount(context.Background(), 41, &UpdateAccountInput{Name: "changed", Extra: extra})
		require.NoError(t, err)
		require.NotContains(t, repo.patch, CodexTicketProxyModeExtraKey)
		require.NotContains(t, repo.patch, CodexTicketProxyIDExtraKey)
		require.Equal(t, "random", updated.CodexTicketProxyMode())
		require.Zero(t, updated.CodexTicketProxyID())
	}
}

func TestAdminAccountCodexTicketProxyIDOnlyEditCannotRestoreConcurrentOldMode(t *testing.T) {
	for _, fullUpdate := range []bool{false, true} {
		account := ticketTestAccount(41)
		account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 7}
		repo := &ticketProxyWriteSpy{longContextBillingRepoStub: longContextBillingRepoStub{account: account}}
		repo.beforeWrite = func() {
			repo.account.Extra[CodexTicketProxyModeExtraKey], repo.account.Extra[CodexTicketProxyIDExtraKey] = "random", 0
		}
		proxyRepo := &updatingProxyRepoStub{proxy: &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "selected.example", Port: 8080}}
		svc := &adminServiceImpl{accountRepo: repo, proxyRepo: proxyRepo}
		extra := map[string]any{CodexTicketProxyIDExtraKey: 8}
		var err error
		if fullUpdate {
			_, err = svc.UpdateAccount(context.Background(), 41, &UpdateAccountInput{Extra: extra})
		} else {
			err = svc.UpdateAccountExtra(context.Background(), 41, extra)
		}
		requireApplicationErrorReason(t, err, "INVALID_CODEX_TICKET_PROXY_ID")
		require.NotContains(t, repo.patch, CodexTicketProxyModeExtraKey)
		require.Equal(t, 8, repo.patch[CodexTicketProxyIDExtraKey])
		require.Zero(t, repo.writes)
		if fullUpdate {
			require.False(t, repo.requestedMode)
			require.True(t, repo.requestedID)
		}
		require.Equal(t, "random", repo.account.CodexTicketProxyMode())
		require.Zero(t, repo.account.CodexTicketProxyID())
	}
}

func TestCodexTicketProxyWriteIntentCapturesOnlyOriginalFields(t *testing.T) {
	ctx := context.Background()
	mode, id := CodexTicketProxyWriteFields(ctx)
	require.False(t, mode)
	require.False(t, id)
	ctx = WithCodexTicketProxyWrite(ctx, map[string]any{CodexTicketProxyModeExtraKey: "random"})
	mode, id = CodexTicketProxyWriteFields(ctx)
	require.True(t, mode)
	require.False(t, id)
}
