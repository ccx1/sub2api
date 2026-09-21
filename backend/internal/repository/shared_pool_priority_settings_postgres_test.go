//go:build sharedpoolintegration

package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/accountgroup"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolSettingsPostgresDefaultPriority(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	settings, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 50, settings.DefaultPriority, "升级沿用原默认值")
	old := f.account("original-priority", f.a)
	require.NoError(t, f.repo.CreateSharedAccount(ctx, old, f.owner, "priority-original"))
	for _, priority := range []int{0, 17, 100} {
		settings.DefaultPriority = priority
		require.NoError(t, f.repo.SaveSharedSettings(ctx, settings))
		got, err := f.repo.SharedSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, priority, got.DefaultPriority)
	}
	existing, err := f.client.Account.Get(ctx, old.ID)
	require.NoError(t, err)
	require.Equal(t, 50, existing.Priority, "修改默认值不能覆盖已有单号设置")
	for _, invalid := range []int{-1, 101} {
		settings.DefaultPriority = invalid
		require.Error(t, f.repo.SaveSharedSettings(ctx, settings))
		got, err := f.repo.SharedSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, 100, got.DefaultPriority)
	}
	newAccount := f.account("custom-priority", f.a)
	newAccount.Priority = 17
	require.NoError(t, f.repo.CreateSharedAccount(ctx, newAccount, f.owner, "priority-new"))
	member, err := f.client.AccountGroup.Query().Where(accountgroup.AccountIDEQ(newAccount.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, 17, member.Priority, "默认分组关联使用同一账号优先级")
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "256_shared_pool_default_priority.sql"))
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	got, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 100, got.DefaultPriority, "重复运行迁移不重置配置")
}
