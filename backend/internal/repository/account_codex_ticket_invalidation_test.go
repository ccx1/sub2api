package repository

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func codexTicketInvalidationFixture() (*service.Account, map[string]any, service.CodexTicketInvalidation) {
	account := codexTicketCASAccount()
	captured := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	event := service.CodexTicketInvalidation{AttemptID: "attempt-1", Model: "model", CapturedAt: captured,
		InvalidatedAt: captured.Add(time.Minute), Reason: "response_model_mismatch", Source: "http", ReportedModels: []string{"fallback-model"}}
	account.Extra["codex_turn_ticket:model"] = map[string]any{"state": "private-ticket", "captured_at": captured,
		"attempt_id": event.AttemptID, "model": event.Model}
	replacement := map[string]any{"state": "private-ticket", "captured_at": captured, "attempt_id": event.AttemptID,
		"model": event.Model, "revoked": true, "invalidation": event}
	return account, replacement, event
}

func expectCodexTicketInvalidation(mock sqlmock.Sqlmock) *sqlmock.ExpectedExec {
	guards := []string{"UPDATE accounts", "CASE WHEN", "THEN extra -> $1", "->> 'revoked') = 'true' THEN extra",
		"jsonb_build_object('revoked', true, 'invalidation', $7::jsonb)", "jsonb_agg(item ORDER BY position)",
		"jsonb_build_array($7::jsonb)", "jsonb_typeof(extra -> $8) = 'array'", "THEN extra -> $8 ELSE '[]'::jsonb END",
		"WITH ORDINALITY", "ORDER BY position LIMIT $9", "id = $2 AND platform = $3 AND type = $4",
		"(extra -> $1 ->> 'state') = $5", "(extra -> $1 ->> 'captured_at') = $6",
		"(extra -> $1 -> 'standby' ->> 'state') = $5", "(extra -> $1 -> 'standby' ->> 'captured_at') = $6",
		"jsonb_array_elements", "(ticket ->> 'state') = $5 AND (ticket ->> 'captured_at') = $6", "deleted_at IS NULL"}
	pattern := "(?s)"
	for _, guard := range guards {
		pattern += regexp.QuoteMeta(guard) + ".*"
	}
	return mock.ExpectExec(pattern)
}

func TestCompareAndSwapCodexTicketInvalidationAtomicAndIdempotent(t *testing.T) {
	for _, name := range []string{"first revocation", "repeat", "stale callback"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			account, replacement, event := codexTicketInvalidationFixture()
			affected := int64(1)
			if name == "repeat" {
				account.Extra["codex_turn_ticket:model"].(map[string]any)["revoked"] = true
			} else if name == "stale callback" {
				affected = 0
			}
			payload, err := json.Marshal(event)
			require.NoError(t, err)
			// Only one SQL statement writes both keys; repeated persisted revocation is an unchanged success.
			expectCodexTicketInvalidation(mock).WithArgs("codex_turn_ticket:model", int64(41), service.PlatformOpenAI,
				service.AccountTypeOAuth, "private-ticket", "2026-09-20T01:00:00Z", codexTicketCASJSON(payload),
				service.OpenAICodexTicketInvalidationsKey, service.OpenAICodexTicketInvalidationsLimit).
				WillReturnResult(sqlmock.NewResult(0, affected))
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", replacement)
			require.NoError(t, err)
			require.Equal(t, affected > 0, changed)
			require.NotContains(t, account.Extra, service.OpenAICodexTicketInvalidationsKey, "caller snapshot remains immutable")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCompareAndSwapCodexTicketInvalidationPropagatesErrors(t *testing.T) {
	for _, rowsError := range []bool{false, true} {
		repo, mock := newCodexTicketCASRepo(t)
		account, replacement, _ := codexTicketInvalidationFixture()
		want := errors.New("database unavailable")
		expectation := expectCodexTicketInvalidation(mock)
		if rowsError {
			expectation.WillReturnResult(sqlmock.NewErrorResult(want))
		} else {
			expectation.WillReturnError(want)
		}
		changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", replacement)
		require.ErrorIs(t, err, want)
		require.False(t, changed)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestPrepareCodexTicketInvalidationDiscardsUnrelatedMetadataWithoutBlockingRevocation(t *testing.T) {
	for _, field := range []string{"model", "attempt", "ticket attempt", "capture", "invalidation time", "expected model"} {
		t.Run(field, func(t *testing.T) {
			account, replacement, event := codexTicketInvalidationFixture()
			switch field {
			case "model":
				event.Model = "other-model"
			case "attempt":
				event.AttemptID = "other-attempt"
			case "ticket attempt":
				replacement["attempt_id"] = "other-attempt"
			case "capture":
				event.CapturedAt = event.CapturedAt.Add(time.Second)
			case "invalidation time":
				event.InvalidatedAt = time.Time{}
			case "expected model":
				account.Extra["codex_turn_ticket:model"].(map[string]any)["model"] = "other-model"
			}
			replacement["invalidation"] = event
			request, err := prepareCodexTicketCAS(account, "model", replacement)
			require.NoError(t, err)
			require.True(t, request.revoke)
			require.False(t, request.withInvalidation)
			require.Len(t, request.args, 6, "valid ticket identity must still be revoked using the legacy statement")
		})
	}
}

func TestCompareAndSwapCodexTicketMalformedInvalidationKeepsRevocationAvailable(t *testing.T) {
	for _, invalid := range []any{"bad event", []string{"bad event"}, 42, map[string]any{"invalidated_at": "bad time"}} {
		repo, mock := newCodexTicketCASRepo(t)
		account, replacement, _ := codexTicketInvalidationFixture()
		replacement["invalidation"], replacement["attempt_id"] = invalid, 42
		mock.ExpectExec(regexp.QuoteMeta(codexTicketRevocationSQL)).
			WithArgs("codex_turn_ticket:model", int64(41), service.PlatformOpenAI, service.AccountTypeOAuth,
				"private-ticket", "2026-09-20T01:00:00Z").WillReturnResult(sqlmock.NewResult(0, 1))
		changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", replacement)
		require.NoError(t, err)
		require.True(t, changed)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestPrepareCodexTicketInvalidationSanitizesUntrustedDiagnostics(t *testing.T) {
	account, replacement, event := codexTicketInvalidationFixture()
	negative := -1
	event.Reason, event.Source, event.ReturnedTicketLength = "private-error-detail", "private-path", &negative
	event.ReportedModels = []string{" model ", "model", "", strings.Repeat("界", 100), "a", "b", "c", "d", "e", "f", "g"}
	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	var untrusted map[string]any
	require.NoError(t, json.Unmarshal(encoded, &untrusted))
	untrusted["state"], untrusted["authorization"], untrusted["body"] = "private-ticket", "private-auth", "private-body"
	replacement["invalidation"] = untrusted
	request, err := prepareCodexTicketCAS(account, "model", replacement)
	require.NoError(t, err)
	require.True(t, request.withInvalidation)
	payload := request.args[6].(string)
	for _, secret := range []string{"private-ticket", "private-auth", "private-body", "private-error-detail", "private-path"} {
		require.NotContains(t, payload, secret)
	}
	var got service.CodexTicketInvalidation
	require.NoError(t, json.Unmarshal([]byte(payload), &got))
	require.Equal(t, "unknown", got.Reason)
	require.Equal(t, "unknown", got.Source)
	require.Nil(t, got.ReturnedTicketLength)
	require.Len(t, got.ReportedModels, 8)
	require.Equal(t, "model", got.ReportedModels[0])
	require.Equal(t, strings.Repeat("界", 85), got.ReportedModels[1])
}

func TestPrepareCodexTicketInvalidationSupportsLegacyTicketAndZeroLength(t *testing.T) {
	account, replacement, event := codexTicketInvalidationFixture()
	delete(account.Extra["codex_turn_ticket:model"].(map[string]any), "attempt_id")
	delete(replacement, "attempt_id")
	zero := 0
	event.AttemptID, event.ReturnedTicketLength = "", &zero
	replacement["invalidation"] = event
	request, err := prepareCodexTicketCAS(account, "model", replacement)
	require.NoError(t, err)
	var got service.CodexTicketInvalidation
	require.NoError(t, json.Unmarshal([]byte(request.args[6].(string)), &got))
	require.Empty(t, got.AttemptID)
	require.NotNil(t, got.ReturnedTicketLength)
	require.Zero(t, *got.ReturnedTicketLength)
}

func TestCodexTicketInvalidationExtraIsSchedulerNeutral(t *testing.T) {
	require.True(t, isSchedulerNeutralExtraKey(service.OpenAICodexTicketInvalidationsKey))
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		service.OpenAICodexTicketInvalidationsKey: []service.CodexTicketInvalidation{},
	}))
}

func TestCodexTicketInvalidationPreservesCookieChangeReason(t *testing.T) {
	account, replacement, event := codexTicketInvalidationFixture()
	event.Reason = "cookie_changed"
	replacement["invalidation"] = event
	request, err := prepareCodexTicketCAS(account, "model", replacement)
	require.NoError(t, err)
	require.True(t, request.withInvalidation)
	var got service.CodexTicketInvalidation
	require.NoError(t, json.Unmarshal([]byte(request.args[6].(string)), &got))
	require.Equal(t, "cookie_changed", got.Reason)
}
