//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type sharedTicketViewRepo struct{ *sharedTestRepository }

func (r sharedTicketViewRepo) SharedSettings(context.Context) (*service.SharedPoolSettings, error) {
	return &service.SharedPoolSettings{}, nil
}

func (r sharedTicketViewRepo) SharedUserRates(context.Context) ([]service.SharedPoolUserRate, error) {
	return nil, nil
}

type sharedTicketAdmin struct {
	service.AdminService
	repo   *sharedTestRepository
	writes int
}

func (s *sharedTicketAdmin) UpdateAccountExtra(_ context.Context, _ int64, updates map[string]any) error {
	s.writes++
	s.repo.updates = updates
	for key, value := range updates {
		s.repo.account.Extra[key] = value
	}
	return nil
}

func TestSharedCodexTicketHandlerRequiresExplicitBooleanAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		owner      int64
		status     int
	}{
		{"anonymous", `{"enabled":false}`, 0, http.StatusUnauthorized},
		{"other owner", `{"enabled":false}`, 8, http.StatusNotFound},
		{"missing", `{}`, 7, http.StatusBadRequest},
		{"null", `{"enabled":null}`, 7, http.StatusBadRequest},
		{"wrong type", `{"enabled":"false"}`, 7, http.StatusBadRequest},
		{"extra fields", `{"enabled":false,"extra":{"codex_turn_ticket:x":"secret"}}`, 7, http.StatusBadRequest},
		{"multiple values", `{"enabled":false} {}`, 7, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo, upstream := newSharedTestHandler()
			c, w := sharedTestContext(tc.owner, tc.body)
			h.SetCodexTicketEnabled(c)
			require.Equal(t, tc.status, w.Code)
			require.Zero(t, repo.reads)
			require.Zero(t, upstream.calls)
		})
	}
	h, _, _ := newSharedTestHandler()
	c, w := sharedTestContext(7, `{"enabled":false}`)
	c.Params[0].Value = "invalid"
	h.SetCodexTicketEnabled(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSharedCodexTicketHandlerRejectsOwnerChangesWithoutExposingSecrets(t *testing.T) {
	h, repo, upstream := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	repo.account.Extra["codex_turn_ticket:model"] = map[string]any{"state": "private-ticket"}
	repo.account.Extra["codex_harvest_proxy_url"] = "https://user:private-password@example.invalid"
	admin := &sharedTicketAdmin{repo: repo}
	h.pool = service.NewSharedPoolService(sharedTicketViewRepo{repo}, repo, nil, admin, nil, sharedImportEarningsStub{}, nil)
	for _, enabled := range []bool{false, true} {
		body := `{"enabled":false}`
		if enabled {
			body = `{"enabled":true}`
		}
		c, w := sharedTestContext(7, body)
		h.SetCodexTicketEnabled(c)
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Contains(t, w.Body.String(), "SHARED_CODEX_TICKET_ADMIN_ONLY")
		require.True(t, service.OpenAICodexTicketAccountEnabled(repo.account))
		require.Empty(t, repo.updates)
		for _, private := range []string{"private-ticket", "private-password", "test-secret", "codex_turn_ticket", "credentials", "codex_harvest_proxy_url"} {
			require.NotContains(t, w.Body.String(), private)
		}
	}
	require.Zero(t, admin.writes)
	require.Zero(t, upstream.calls)
}

func TestSharedCodexTicketHandlerRejectsUnsupportedAndShadow(t *testing.T) {
	parentID := int64(2)
	for _, account := range []*service.Account{
		{Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey},
		{Platform: service.PlatformGemini, Type: service.AccountTypeOAuth},
		{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ParentAccountID: &parentID},
	} {
		h, repo, upstream := newSharedTestHandler()
		account.ID = repo.account.ID
		repo.account = account
		c, w := sharedTestContext(7, `{"enabled":true}`)
		h.SetCodexTicketEnabled(c)
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Empty(t, repo.updates)
		require.Zero(t, upstream.calls)
	}
}
