package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIQuotaRecoveryRepositoryConditionalUpdate(t *testing.T) {
	for _, mode := range []string{"matched", "new generation", "write failed", "outbox failed"} {
		t.Run(mode, func(t *testing.T) {
			var updateSQL string
			matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				if strings.HasPrefix(actual, `UPDATE "accounts"`) {
					updateSQL = actual
				}
				return sqlmock.QueryMatcherRegexp.Match(expected, actual)
			})
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newAccountRepositoryWithSQL(client, db, nil)
			limited, reset := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
			failure := errors.New("write unavailable")
			expect := mock.ExpectExec(`UPDATE "accounts" SET`).WithArgs(
				sqlmock.AnyArg(), int64(123), service.PlatformOpenAI, service.AccountTypeOAuth, limited, reset)
			switch mode {
			case "write failed":
				expect.WillReturnError(failure)
			case "new generation":
				expect.WillReturnResult(sqlmock.NewResult(0, 0))
			default:
				expect.WillReturnResult(sqlmock.NewResult(0, 1))
				outbox := mock.ExpectExec(`INSERT INTO scheduler_outbox`).WithArgs(
					service.SchedulerOutboxEventAccountChanged, int64(123), nil, nil, sqlmock.AnyArg())
				if mode == "outbox failed" {
					outbox.WillReturnError(failure)
				} else {
					outbox.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			changed, err := repo.ClearOpenAIRateLimitIfObserved(context.Background(), 123, limited, reset)
			if mode == "write failed" {
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, mode == "matched" || mode == "outbox failed", changed)
			require.NoError(t, mock.ExpectationsWereMet())
			assertQuotaRecoveryUpdateScope(t, updateSQL)
		})
	}
}

func assertQuotaRecoveryUpdateScope(t *testing.T, query string) {
	t.Helper()
	parts := strings.SplitN(query, " WHERE ", 2)
	require.Len(t, parts, 2)
	for _, condition := range []string{
		`"id" = $2`, `"deleted_at" IS NULL`, `"platform" = $3`, `"type" = $4`,
		`"parent_account_id" IS NULL`, `"rate_limited_at" = $5`, `"rate_limit_reset_at" = $6`,
	} {
		require.Contains(t, parts[1], condition)
	}
	require.Contains(t, parts[0], `"rate_limited_at" = NULL`)
	require.Contains(t, parts[0], `"rate_limit_reset_at" = NULL`)
	for _, untouched := range []string{"status", "schedulable", "overload_until", "temp_unschedulable_until", "extra", "credentials", "proxy_id"} {
		require.NotContains(t, parts[0], `"`+untouched+`"`, "独立保护状态、票据和代理不能随配额恢复改写")
	}
}
