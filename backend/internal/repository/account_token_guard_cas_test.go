package repository

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountTokenGuardCandidatesKeepDedicatedScope(t *testing.T) {
	for _, predicate := range []string{"a.deleted_at IS NULL", "a.platform='openai'", "a.type='oauth'", "a.parent_account_id IS NULL", "a.status IN ('active','error')", "ag.group_id=ANY($1::bigint[])", "g.deleted_at IS NULL", "a.expires_at>NOW()"} {
		require.Contains(t, tokenGuardCandidatesSQL, predicate)
	}
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := &accountRepository{sql: db}
	version := time.Now().Truncate(time.Microsecond)
	columns := []string{"id", "name", "platform", "type", "credentials", "extra", "proxy_id", "status", "error_message", "schedulable", "updated_at", "expires_at", "auto_pause_on_expired", "parent_account_id", "proxy_fallback_origin_id"}
	rows := sqlmock.NewRows(columns)
	for i, status := range []string{service.StatusActive, service.StatusError} {
		rows.AddRow(i+1, "guard", "openai", "oauth", `{"access_token":"old","nested":{"v":1}}`, `{}`, nil, status, "", false, version, nil, false, nil, nil)
	}
	mock.ExpectQuery(regexp.QuoteMeta(tokenGuardCandidatesSQL)).WithArgs("{4,8}").WillReturnRows(rows)
	got, err := repo.ListTokenGuardCandidates(context.Background(), []int64{4, 8})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, service.StatusError, got[1].Status)
	require.False(t, got[1].Schedulable)
	require.Equal(t, float64(1), got[1].Credentials["nested"].(map[string]any)["v"])
	_, err = repo.ListTokenGuardCandidates(context.Background(), []int64{0})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountTokenGuardRepairCASPreservesProtectionAndOutbox(t *testing.T) {
	for _, predicate := range []string{"a.updated_at=$3", "a.credentials=$4::jsonb", "a.status=$5", "a.schedulable=$6", "a.proxy_id IS NOT DISTINCT FROM $7", "a.deleted_at IS NULL", "a.parent_account_id IS NULL", "INSERT INTO scheduler_outbox", "FROM updated RETURNING account_id"} {
		require.Contains(t, tokenGuardRepairSQL, predicate)
	}
	setClause := strings.Split(strings.Split(tokenGuardRepairSQL, " SET ")[1], " WHERE ")[0]
	for _, protected := range []string{"schedulable", "rate_limit", "temp_unschedulable", "extra="} {
		require.NotContains(t, setClause, protected)
	}
	for _, test := range []string{"success", "stale", "query", "tail", "close", "scan"} {
		t.Run(test, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &accountRepository{sql: db}
			version := time.Now().Truncate(time.Microsecond)
			expected := &service.Account{ID: 7, UpdatedAt: version, Status: service.StatusError, Credentials: map[string]any{"access_token": "old"}}
			q := mock.ExpectQuery(regexp.QuoteMeta(tokenGuardRepairSQL)).WithArgs(`{"access_token":"new"}`, int64(7), version, `{"access_token":"old"}`, service.StatusError, false, nil, service.SchedulerOutboxEventAccountChanged)
			want := errors.New("database failure")
			rows := sqlmock.NewRows([]string{"updated_at"})
			switch test {
			case "success":
				rows.AddRow(version.Add(time.Microsecond))
			case "stale":
				want = service.ErrAccountTokenGuardStale
			case "query":
				q.WillReturnError(want)
			case "tail":
				rows.AddRow(version).AddRow(version).RowError(1, want)
			case "close":
				rows.AddRow(version).CloseError(want)
			case "scan":
				rows.AddRow("invalid timestamp")
			}
			if test != "query" {
				q.WillReturnRows(rows).RowsWillBeClosed()
			}
			got, err := repo.ApplyTokenGuardRepair(context.Background(), expected, map[string]any{"access_token": "new"})
			if test == "success" {
				require.NoError(t, err)
				require.Equal(t, version.Add(time.Microsecond), got)
			} else if test == "scan" {
				require.Error(t, err)
			} else {
				require.ErrorIs(t, err, want)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAccountTokenGuardStateCASLocksAccountAndComparesState(t *testing.T) {
	for _, predicate := range []string{"FOR UPDATE", "a.updated_at=$15", "a.credentials=$16::jsonb", "a.proxy_id IS NOT DISTINCT FROM $17", "a.status=$18", "a.schedulable=$19", "account_token_guard_states.updated_at=$14 AND $13::boolean", "EXISTS (SELECT 1 FROM account_token_guard_states WHERE account_id=$1)"} {
		require.Contains(t, tokenGuardStateCASSQL, predicate)
	}
	for _, test := range []string{"insert", "update", "stale", "query", "tail", "close"} {
		t.Run(test, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := &accountTokenGuardRepository{db: db}
			version := time.Now().Truncate(time.Microsecond)
			state := service.AccountTokenGuardState{AccountID: 7, AccountStatus: service.StatusError, AccountVersion: &service.Account{ID: 7, UpdatedAt: version, Credentials: map[string]any{"access_token": "old"}, Status: service.StatusError}}
			var expected any
			if test != "insert" {
				state.UpdatedAt = version
				expected = version
			}
			q := mock.ExpectQuery(regexp.QuoteMeta(tokenGuardStateCASSQL)).WithArgs(int64(7), "", service.StatusError, false, "", "", 0, 0, nil, nil, "", "", test != "insert", expected, version, `{"access_token":"old"}`, nil, service.StatusError, false)
			want := errors.New("state database failure")
			rows := sqlmock.NewRows([]string{"updated_at"})
			switch test {
			case "insert", "update":
				rows.AddRow(version.Add(time.Microsecond))
			case "stale":
				want = service.ErrAccountTokenGuardStale
			case "query":
				q.WillReturnError(want)
			case "tail":
				rows.AddRow(version).AddRow(version).RowError(1, want)
			case "close":
				rows.AddRow(version).CloseError(want)
			}
			if test != "query" {
				q.WillReturnRows(rows).RowsWillBeClosed()
			}
			got, err := repo.UpsertStateIfUnchanged(context.Background(), state)
			if test == "insert" || test == "update" {
				require.NoError(t, err)
				require.Equal(t, version.Add(time.Microsecond), got)
			} else {
				require.ErrorIs(t, err, want)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
