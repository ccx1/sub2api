package admin

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type codexTicketVisibilityRepo struct {
	service.AccountRepository
	record *service.CodexModelQualityRecord
	reads  int
}

func (r *codexTicketVisibilityRepo) ListByPlatform(context.Context, string) ([]service.Account, error) {
	return nil, nil
}

func (r *codexTicketVisibilityRepo) LoadCodexModelQuality(context.Context, int64, string) (*service.CodexModelQualityRecord, error) {
	r.reads++
	return r.record, nil
}

func (r *codexTicketVisibilityRepo) AcquireCodexModelQuality(context.Context, int64, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (r *codexTicketVisibilityRepo) SaveCodexModelQuality(context.Context, int64, string, string, *service.CodexModelQualityRecord, time.Duration) (bool, error) {
	return false, nil
}

func (r *codexTicketVisibilityRepo) ReleaseCodexModelQuality(context.Context, int64, string, string) error {
	return nil
}

func TestAccountCodexTicketQualityVisibilityFollowsPolicy(t *testing.T) {
	const model = "gpt-6-astra"
	cfg := &config.Config{}
	settingsRepo := &settingHandlerRepoStub{}
	settings := service.NewSettingService(settingsRepo, cfg)
	qualityRepo := &codexTicketVisibilityRepo{record: &service.CodexModelQualityRecord{
		Status: service.CodexModelQualityStatus{AccountID: 41, Model: model, Status: "passed", Reason: "capability_passed"},
	}}
	gateway := service.NewOpenAIGatewayService(
		qualityRepo, nil, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings, nil,
	)
	gateway.StopOpenAICodexTicketHarvester()
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, Models: []string{model}}
	h := &AccountHandler{cfg: cfg}
	h.SetCodexTicketSettings(settings)
	h.SetCodexTicketRetryService(gateway)
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}

	for _, response := range []func(*service.Account) string{
		func(a *service.Account) string {
			return h.accountResponseFromService(a).CodexTurnTickets[0].QualityStatus
		},
		func(a *service.Account) string {
			return h.accountListResponseFromService(a).CodexTurnTickets[0].QualityStatus
		},
	} {
		require.Empty(t, response(account), "disabled quality must not appear in the usage window")
	}
	require.Zero(t, qualityRepo.reads, "disabled quality must not read persisted results")

	policy := service.DefaultCodexModelQualityPolicy()
	policy.Enabled = true
	_, err := settings.UpdateCodexModelQualityPolicy(context.Background(), policy)
	require.NoError(t, err)
	for _, response := range []func(*service.Account) string{
		func(a *service.Account) string {
			return h.accountResponseFromService(a).CodexTurnTickets[0].QualityStatus
		},
		func(a *service.Account) string {
			return h.accountListResponseFromService(a).CodexTurnTickets[0].QualityStatus
		},
	} {
		require.NotEmpty(t, response(account), "enabled quality should show the existing result")
	}
	require.Positive(t, qualityRepo.reads)
}
