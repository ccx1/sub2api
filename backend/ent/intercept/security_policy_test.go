package intercept_test

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/securitypolicykeyword"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSecurityPolicyKeywordQueryUsesSoftDeleteInterceptor(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	mock.ExpectQuery(`SELECT .* FROM "security_policy_keywords" WHERE "security_policy_keywords"\."deleted_at" IS NULL`).
		WillReturnRows(sqlmock.NewRows(securitypolicykeyword.Columns))

	words, err := client.SecurityPolicyKeyword.Query().All(context.Background())
	require.NoError(t, err)
	require.Empty(t, words)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSecurityPolicyKeywordSoftDeleteWithRuntimeInterceptors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	word, err := client.SecurityPolicyKeyword.Create().SetKeyword("test-keyword").Save(ctx)
	require.NoError(t, err)
	words, err := client.SecurityPolicyKeyword.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, words, 1)
	require.Equal(t, word.ID, words[0].ID)

	require.NoError(t, client.SecurityPolicyKeyword.DeleteOneID(word.ID).Exec(ctx))
	words, err = client.SecurityPolicyKeyword.Query().All(ctx)
	require.NoError(t, err)
	require.Empty(t, words)
	stored, err := client.SecurityPolicyKeyword.Get(mixins.SkipSoftDelete(ctx), word.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.DeletedAt)
}
