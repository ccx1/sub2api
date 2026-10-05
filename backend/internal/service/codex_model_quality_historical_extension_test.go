package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func historicalExtensionFixture(t *testing.T) (*OpenAIGatewayService, *qualityRuntimeRepo, *codexModelQualityJob) {
	t.Helper()
	s, repo, oldJob := qualityRuntimeFixture(t)
	cfg := s.cfg.Gateway.OpenAICodexTicket
	cfg.UsageMode, cfg.MinTicketAgeSeconds = config.CodexTicketUsageAged, 1
	cfg.HistoricalTicketValiditySeconds = 390
	cfg.HistoricalQualityEnabled = true
	cfg.HistoricalQualityCheckBeforeSeconds = 120
	cfg.HistoricalQualityCheckIntervalSeconds = 30
	cfg.HistoricalQualityExtendSeconds = 30
	s.cfg.Gateway.OpenAICodexTicket = cfg
	s.settingService.codexTicketSettingsCache.Store(nil)
	ticket := *oldJob.ticket
	ticket.CapturedAt = time.Now().Add(-5 * time.Minute)
	ticket.OriginCapturedAt = ticket.CapturedAt
	ticket.HistoricalUsedAt = ticket.CapturedAt
	s.openaiCodexTickets.Store(openAICodexTicketKey(ticket.AccountID, ticket.Model), &ticket)
	job, reason := s.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, oldJob.policy)
	require.NotNil(t, job, reason)
	return s, repo, job
}

func TestHistoricalQualityExtensionDueWindowAndInterval(t *testing.T) {
	_, _, job := historicalExtensionFixture(t)
	now := time.Now()
	require.True(t, historicalQualityExtensionDue(job.ticket, job.config, now))
	job.ticket.HistoricalQualityCheckedAt = now
	require.False(t, historicalQualityExtensionDue(job.ticket, job.config, now.Add(29*time.Second)))
	require.True(t, historicalQualityExtensionDue(job.ticket, job.config, now.Add(30*time.Second)))
	job.config.HistoricalQualityEnabled = false
	require.False(t, historicalQualityExtensionDue(job.ticket, job.config, now.Add(30*time.Second)))
	job.config.HistoricalQualityEnabled = true
	job.ticket.HistoricalExtendedExpiresAt = now.Add(10 * time.Minute)
	require.False(t, historicalQualityExtensionDue(job.ticket, job.config, now))
	require.False(t, historicalQualityExtensionDue(job.ticket, job.config, now.Add(11*time.Minute)))
}

func TestHistoricalQualityPassedExtendsFromPreviousDeadline(t *testing.T) {
	s, repo, job := historicalExtensionFixture(t)
	first := job.ticket.historicalExpires(job.config)
	require.True(t, s.persistHistoricalQualityExtension(job, first, true))
	key := openAICodexTicketKey(job.account.ID, job.ticket.Model)
	stored, _ := s.openaiCodexTickets.Load(key)
	updated := stored.(*openAICodexTicket)
	require.Equal(t, first.Add(30*time.Second), updated.historicalExpires(job.config))
	require.False(t, updated.HistoricalQualityCheckedAt.IsZero())
	require.False(t, updated.Revoked)
	require.Nil(t, repo.record)
	require.Equal(t, 1, repo.writes)
	require.False(t, s.persistHistoricalQualityExtension(job, first, true), "stale result cannot extend twice")
}

func TestHistoricalQualityCanExtendRepeatedly(t *testing.T) {
	s, _, job := historicalExtensionFixture(t)
	first := job.ticket.historicalExpires(job.config)
	require.True(t, s.persistHistoricalQualityExtension(job, first, true))
	key := openAICodexTicketKey(job.account.ID, job.ticket.Model)
	stored, _ := s.openaiCodexTickets.Load(key)
	ready := cloneCodexTicketInventory(stored.(*openAICodexTicket))
	ready.HistoricalQualityCheckedAt = time.Now().Add(-time.Minute)
	s.openaiCodexTickets.Store(key, ready)
	job.ticket = codexTicketLeaf(ready)
	require.True(t, s.persistHistoricalQualityExtension(job, first.Add(30*time.Second), true))
	stored, _ = s.openaiCodexTickets.Load(key)
	require.Equal(t, first.Add(time.Minute), stored.(*openAICodexTicket).historicalExpires(job.config))
}

func TestHistoricalQualityFailureOnlyRecordsCheckTime(t *testing.T) {
	s, repo, job := historicalExtensionFixture(t)
	expires := job.ticket.historicalExpires(job.config)
	require.True(t, s.persistHistoricalQualityExtension(job, expires, false))
	key := openAICodexTicketKey(job.account.ID, job.ticket.Model)
	stored, _ := s.openaiCodexTickets.Load(key)
	updated := stored.(*openAICodexTicket)
	require.Equal(t, expires, updated.historicalExpires(job.config))
	require.False(t, updated.Revoked)
	require.Nil(t, updated.Invalidation)
	require.Nil(t, repo.record)
	require.False(t, historicalQualityExtensionDue(updated, job.config, time.Now()))
}

func TestHistoricalQualityExtensionRejectsReplacementAndCASFailure(t *testing.T) {
	s, repo, job := historicalExtensionFixture(t)
	expires := job.ticket.historicalExpires(job.config)
	replacement := *job.ticket
	replacement.SessionID = "replacement-session"
	s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), &replacement)
	require.False(t, s.persistHistoricalQualityExtension(job, expires, true))
	require.Equal(t, 0, repo.writes)
	s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket)
	repo.casHook = func(context.Context) (bool, error) { return false, nil }
	require.False(t, s.persistHistoricalQualityExtension(job, expires, true))
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(job.account.ID, job.ticket.Model))
	require.Equal(t, expires, stored.(*openAICodexTicket).historicalExpires(job.config))
}

func TestHistoricalQualityExpiredTicketCannotExtend(t *testing.T) {
	s, repo, job := historicalExtensionFixture(t)
	job.ticket.HistoricalExtendedExpiresAt = time.Now().Add(-time.Second)
	job.ticket.OriginCapturedAt = time.Now().Add(-10 * time.Minute)
	job.ticket.CapturedAt = job.ticket.OriginCapturedAt
	s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket)
	require.False(t, s.persistHistoricalQualityExtension(job, job.ticket.historicalExpires(job.config), true))
	require.Equal(t, 0, repo.writes)
}

func TestHistoricalQualityChecksExpiredProtocolCookie(t *testing.T) {
	s, repo, job := historicalExtensionFixture(t)
	cfg := s.cfg.Gateway.OpenAICodexTicket
	cfg.CredentialMode = config.CodexTicketCredentialCookie
	s.cfg.Gateway.OpenAICodexTicket = cfg
	s.settingService.codexTicketSettingsCache.Store(nil)
	ticket := codexTicketLeaf(job.ticket)
	ticket.CredentialMode, ticket.State, ticket.Length = config.CodexTicketCredentialCookie, "", 0
	ticket.ExpiresAt = time.Now().Add(-time.Hour)
	ticket.Cookies = []*http.Cookie{{Name: "session", Value: "quality", Domain: "chatgpt.com", Path: "/backend-api", Secure: true,
		Expires: time.Now().Add(-time.Hour)}}
	s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), ticket)
	policy := job.policy
	policy.CanaryEnabled, policy.CanaryOnly = true, true
	policy.CanaryPrompt, policy.CanaryExpected = "Answer 42", []string{"42"}
	_, err := s.settingService.UpdateCodexModelQualityPolicy(context.Background(), policy)
	require.NoError(t, err)
	job, reason := s.prepareCodexModelQuality(context.Background(), repo.account, ticket.Model, policy)
	require.NotNil(t, job, reason)
	job.leaseExpiresAt = time.Now().Add(time.Minute)
	calls := 0
	s.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		require.Contains(t, req.Header.Get("Cookie"), "session=quality")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(qualityResponseBody(ticket.Model, "42")))}, nil
	}}
	before := job.ticket.historicalExpires(job.config)
	s.runHistoricalQualityExtension(context.Background(), job)
	require.Equal(t, 1, calls)
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(job.account.ID, job.ticket.Model))
	require.Equal(t, before.Add(30*time.Second), stored.(*openAICodexTicket).historicalExpires(job.config))
	require.Nil(t, repo.record)
}

func TestHistoricalQualityExtensionSurvivesSoftRevalidation(t *testing.T) {
	s, account, old := codexRevalidationFixture(t, "")
	cfg := s.cfg.Gateway.OpenAICodexTicket
	cfg.UsageMode, cfg.MinTicketAgeSeconds = config.CodexTicketUsageAged, 1
	cfg.HistoricalTicketValiditySeconds = 390
	s.cfg.Gateway.OpenAICodexTicket = cfg
	old.OriginCapturedAt = old.CapturedAt
	old.HistoricalExtendedExpiresAt = time.Now().Add(3 * time.Minute)
	old.HistoricalQualityCheckedAt = time.Now()
	key := openAICodexTicketKey(account.ID, old.Model)
	s.openaiCodexTickets.Store(key, cloneCodexTicketInventory(old))
	next := codexTicketLeaf(old)
	next.CapturedAt = time.Now()
	next.HistoricalExtendedExpiresAt = time.Time{}
	next.HistoricalQualityCheckedAt = time.Time{}
	require.True(t, s.replaceRevalidatedCodexTicket(context.Background(), account, old, next, nil))
	stored, _ := s.openaiCodexTickets.Load(key)
	updated := stored.(*openAICodexTicket)
	require.Equal(t, old.HistoricalExtendedExpiresAt, updated.HistoricalExtendedExpiresAt)
	require.Equal(t, old.HistoricalQualityCheckedAt, updated.HistoricalQualityCheckedAt)
}

func TestHistoricalQualityFailedProbeDoesNotQuarantine(t *testing.T) {
	s, repo, job := historicalExtensionFixture(t)
	policy := job.policy
	policy.CanaryEnabled, policy.CanaryOnly = true, true
	policy.CanaryPrompt, policy.CanaryExpected = "Answer 42", []string{"42"}
	_, err := s.settingService.UpdateCodexModelQualityPolicy(context.Background(), policy)
	require.NoError(t, err)
	job, reason := s.prepareCodexModelQuality(context.Background(), repo.account, job.ticket.Model, policy)
	require.NotNil(t, job, reason)
	job.leaseExpiresAt = time.Now().Add(time.Minute)
	s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(qualityResponseBody(job.ticket.Model, "41")))}, nil
	}}
	expires := job.ticket.historicalExpires(job.config)
	s.runHistoricalQualityExtension(context.Background(), job)
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(job.account.ID, job.ticket.Model))
	updated := stored.(*openAICodexTicket)
	require.Equal(t, expires, updated.historicalExpires(job.config))
	require.False(t, updated.HistoricalQualityCheckedAt.IsZero())
	require.False(t, updated.Revoked)
	require.Nil(t, updated.Invalidation)
	require.Nil(t, repo.record)
}
