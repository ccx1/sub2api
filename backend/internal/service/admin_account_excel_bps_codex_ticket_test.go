package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// 全模型 BPS 与打票互斥：开启 BPS 时关闭打票，关闭 BPS 后打票可手动重新开启。
func TestExcelBPSAllModelsDisablesCodexTicketAndAllowsManualReenable(t *testing.T) {
	for _, operation := range []string{"update", "extra"} {
		t.Run(operation, func(t *testing.T) {
			account := ticketTestAccount(61)
			account.Extra = map[string]any{OpenAICodexTicketEnabledExtraKey: true}
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
			svc := &adminServiceImpl{accountRepo: repo}
			ctx := context.Background()

			if operation == "update" {
				_, err := svc.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Extra: map[string]any{"openai_excel_bps": true}})
				require.NoError(t, err)
			} else {
				require.NoError(t, svc.UpdateAccountExtra(ctx, account.ID, map[string]any{"openai_excel_bps": true}))
			}
			current, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, false, current.Extra[OpenAICodexTicketEnabledExtraKey])
			require.True(t, ExcelBPSBlocksCodexTicket(current))

			// 关闭 BPS 不自动恢复打票。
			if operation == "update" {
				_, err = svc.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Extra: map[string]any{"custom": true}})
			} else {
				err = svc.UpdateAccountExtra(ctx, account.ID, map[string]any{"openai_excel_bps": false})
			}
			require.NoError(t, err)
			current, err = repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.False(t, ExcelBPSBlocksCodexTicket(current))
			require.False(t, OpenAICodexTicketAccountEnabled(current))

			require.NoError(t, svc.UpdateAccountExtra(ctx, account.ID, map[string]any{OpenAICodexTicketEnabledExtraKey: true}))
			current, err = repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.True(t, OpenAICodexTicketAccountEnabled(current))

			// 之后的普通编辑不会把手动开启的打票改回去。
			_, err = svc.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Extra: map[string]any{"custom": "again"}})
			require.NoError(t, err)
			current, err = repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.True(t, OpenAICodexTicketAccountEnabled(current))
		})
	}
}

func TestExcelBPSSelectedModelsKeepsCodexTicket(t *testing.T) {
	account := ticketTestAccount(62)
	account.Extra = map[string]any{OpenAICodexTicketEnabledExtraKey: true}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Extra: map[string]any{
		"openai_excel_bps": true, "openai_excel_bps_models": []any{"gpt-6-astra"},
	}})
	require.NoError(t, err)
	current, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, true, current.Extra[OpenAICodexTicketEnabledExtraKey])
	require.False(t, ExcelBPSBlocksCodexTicket(current))
	require.True(t, OpenAICodexTicketAccountEnabled(current))
}
