package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var _ interface {
	ClaimCodexTicketConsumption(context.Context, int64, string, time.Time, time.Time, int) (bool, error)
} = (*accountRepository)(nil)

func expectCodexTicketConsumptionClaim(mock sqlmock.Sqlmock) *sqlmock.ExpectedExec {
	// 断言原子守护与有界保留存在，不绑定 SQL 排版。
	guards := []string{"UPDATE accounts", "jsonb_build_object($2::text", "jsonb_each(CASE WHEN jsonb_typeof(extra -> $2::text) = 'object'",
		"jsonb_typeof(entry.value) = 'number'", "> $5::bigint", "ORDER BY", "LIMIT $6", "jsonb_build_object($3::text, $4::bigint)",
		"WHERE id = $1 AND deleted_at IS NULL", "-> $3::text IS NULL"}
	pattern := "(?s)"
	for _, guard := range guards {
		pattern += regexp.QuoteMeta(guard) + ".*"
	}
	return mock.ExpectExec(pattern)
}

func TestClaimCodexTicketConsumptionIsAtomicAndBounded(t *testing.T) {
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	expires := now.Add(2 * time.Hour)
	for _, tc := range []struct {
		name     string
		affected int64
	}{{"first claim", 1}, {"already claimed elsewhere", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			expectCodexTicketConsumptionClaim(mock).
				WithArgs(int64(41), service.OpenAICodexTicketConsumedKey, "0123456789abcdef01234567", expires.Unix(), now.Unix(), 255).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			claimed, err := repo.ClaimCodexTicketConsumption(context.Background(), 41, "0123456789abcdef01234567", expires, now, 256)
			require.NoError(t, err)
			require.Equal(t, tc.affected == 1, claimed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestClaimCodexTicketConsumptionKeepsAtLeastOneRetainedEntry(t *testing.T) {
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	repo, mock := newCodexTicketCASRepo(t)
	expectCodexTicketConsumptionClaim(mock).
		WithArgs(int64(41), service.OpenAICodexTicketConsumedKey, "abcdef", now.Add(time.Hour).Unix(), now.Unix(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	claimed, err := repo.ClaimCodexTicketConsumption(context.Background(), 41, "abcdef", now.Add(time.Hour), now, 0)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimCodexTicketConsumptionRejectsUnsafeInputWithoutSQL(t *testing.T) {
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		accountID   int64
		fingerprint string
		expires     time.Time
	}{
		{"account", 0, "abcdef", now.Add(time.Hour)},
		{"empty", 41, "", now.Add(time.Hour)},
		{"not hex", 41, "private-ticket", now.Add(time.Hour)},
		{"upper case", 41, "ABCDEF", now.Add(time.Hour)},
		{"too long", 41, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0", now.Add(time.Hour)},
		{"expired", 41, "abcdef", now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			claimed, err := repo.ClaimCodexTicketConsumption(context.Background(), tc.accountID, tc.fingerprint, tc.expires, now, 256)
			require.Error(t, err)
			require.False(t, claimed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestClaimCodexTicketConsumptionPropagatesErrors(t *testing.T) {
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for _, rowsError := range []bool{false, true} {
		repo, mock := newCodexTicketCASRepo(t)
		want := errors.New("database unavailable")
		expectation := expectCodexTicketConsumptionClaim(mock)
		if rowsError {
			expectation.WillReturnResult(sqlmock.NewErrorResult(want))
		} else {
			expectation.WillReturnError(want)
		}
		claimed, err := repo.ClaimCodexTicketConsumption(context.Background(), 41, "abcdef", now.Add(time.Hour), now, 256)
		require.ErrorIs(t, err, want)
		require.False(t, claimed)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCodexTicketConsumptionLedgerIsSchedulerNeutral(t *testing.T) {
	require.True(t, isSchedulerNeutralExtraKey(service.OpenAICodexTicketConsumedKey))
	require.NotContains(t, filterSchedulerExtra(map[string]any{service.OpenAICodexTicketConsumedKey: map[string]any{"abcdef": 1}}),
		service.OpenAICodexTicketConsumedKey)
}
