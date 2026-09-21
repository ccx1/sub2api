package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketRevocationRetriesAfterPersistenceFailure(t *testing.T) {
	s, account, ticket, _ := ticketWatchdogFixture(t)
	account.Extra = map[string]any{openAICodexTicketExtraKey(ticket.Model): ticket}
	calls := 0
	s.accountRepo = &codexTicketCASStub{update: func(expected *Account, model string, replacement any) (bool, error) {
		calls++
		if calls <= 2 {
			return false, errors.New("temporary persistence failure")
		}
		current := parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
		used := parseOpenAICodexTicketFromAny(account.ID, model, expected.Extra[openAICodexTicketExtraKey(model)])
		if current.State != used.State || !current.CapturedAt.Equal(used.CapturedAt) {
			return false, nil
		}
		account.Extra[openAICodexTicketExtraKey(model)] = replacement
		return true, nil
	}}
	s.invalidateOpenAICodexTicket(context.Background(), account, ticket)
	require.Equal(t, 2, calls, "一次瞬时错误允许有界重试")
	require.Nil(t, s.lookupOpenAICodexTicket(account, ticket.Model), "写库失败仍立即阻断本机旧票")
	s.invalidateOpenAICodexTicket(context.Background(), account, ticket)
	require.Equal(t, 3, calls, "水位不能代替持久化完成状态")
	stored := parseOpenAICodexTicketFromAny(account.ID, ticket.Model, account.Extra[openAICodexTicketExtraKey(ticket.Model)])
	require.True(t, stored.Revoked)
	cold := ticketTestService(t, s.cfg.Gateway.OpenAICodexTicket, nil)
	require.Nil(t, cold.lookupOpenAICodexTicket(account, ticket.Model))
}

func TestCodexTicketWatchdogRetriesOldReceiptAfterOAuthRefresh(t *testing.T) {
	s, original, ticket, request := ticketWatchdogFixture(t)
	databaseAccount := cloneOpenAICodexTicketAccount(original)
	databaseAccount.Credentials["access_token"] = "refreshed-token"
	databaseAccount.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	done := make(chan struct{}, 1)
	calls := 0
	s.accountRepo = &codexTicketCASStub{update: func(expected *Account, model string, replacement any) (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("temporary persistence failure")
		}
		used := parseOpenAICodexTicketFromAny(expected.ID, model, expected.Extra[openAICodexTicketExtraKey(model)])
		current := parseOpenAICodexTicketFromAny(databaseAccount.ID, model, databaseAccount.Extra[openAICodexTicketExtraKey(model)])
		if used.State != current.State || !used.CapturedAt.Equal(current.CapturedAt) {
			return false, nil
		}
		databaseAccount.Extra[openAICodexTicketExtraKey(model)] = replacement
		done <- struct{}{}
		return true, nil
	}}
	response := codexTicketCompletedResponse("gpt-other", "")
	s.observeOpenAICodexTicketResponse(request, response)
	_, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("single watchdog callback did not retry the transient persistence failure")
	}
	require.Equal(t, 2, calls)
	require.NotEqual(t, original.Credentials["access_token"], databaseAccount.Credentials["access_token"])
	cold := ticketTestService(t, s.cfg.Gateway.OpenAICodexTicket, nil)
	require.Nil(t, cold.lookupOpenAICodexTicket(databaseAccount, ticket.Model))
}

func TestCodexTicketRevocationRetryProtectsNewTicket(t *testing.T) {
	s, account, ticket, _ := ticketWatchdogFixture(t)
	s.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) {
		return false, errors.New("temporary persistence failure")
	}}
	s.invalidateOpenAICodexTicket(context.Background(), account, ticket)
	s.accountRepo = nil
	next := *ticket
	next.State, next.CapturedAt = "gAAAAA"+strings.Repeat("N", 286), time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &next))
	s.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) {
		t.Fatal("旧票重试不能触碰本机已发布的新票")
		return false, nil
	}}
	s.invalidateOpenAICodexTicket(context.Background(), account, ticket)
	require.Equal(t, next.State, s.lookupOpenAICodexTicket(account, next.Model).State)
}

func TestCodexTicketRevocationConcurrentRetriesKeepWatermarkMonotonic(t *testing.T) {
	s, account, ticket, _ := ticketWatchdogFixture(t)
	var calls int
	s.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) { calls++; return true, nil }}
	s.invalidateOpenAICodexTicket(context.Background(), account, ticket)
	older := *ticket
	older.CapturedAt = ticket.CapturedAt.Add(-time.Minute)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			s.invalidateOpenAICodexTicket(context.Background(), account, ticket)
			s.invalidateOpenAICodexTicket(context.Background(), account, &older)
		})
	}
	group.Wait()
	cutoff, ok := s.openaiCodexTicketRevoked.Load(openAICodexTicketKey(account.ID, ticket.Model))
	require.True(t, ok)
	require.Equal(t, ticket.CapturedAt, cutoff)
	require.Greater(t, calls, 1, "同票重试应允许再次执行幂等持久化")
}
