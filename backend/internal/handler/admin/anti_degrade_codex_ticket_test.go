package admin

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type protectionTicketHandlerStore struct{ account *service.Account }

func (s *protectionTicketHandlerStore) GetAccount(context.Context, int64) (*service.Account, error) {
	account := *s.account
	account.Extra = maps.Clone(account.Extra)
	return &account, nil
}

func (s *protectionTicketHandlerStore) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	account := *s.account
	account.Extra = maps.Clone(input.Extra)
	if input.Concurrency != nil {
		account.Concurrency = *input.Concurrency
	}
	s.account = &account
	return s.GetAccount(ctx, id)
}

func protectionTicketHandlerRequest(t *testing.T, handler gin.HandlerFunc, body string) *dto.Account {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/accounts/1/protection", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
	handler(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var envelope struct {
		Data *dto.Account `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Data)
	return envelope.Data
}

func TestProtectionHandlerPreservesTicketSwitchAcrossToggleAndRepeatedEnable(t *testing.T) {
	for _, ticketEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ticket_off", true: "ticket_on"}[ticketEnabled], func(t *testing.T) {
			store := &protectionTicketHandlerStore{account: &service.Account{
				ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Concurrency: 4,
				Extra: map[string]any{service.OpenAICodexTicketEnabledExtraKey: ticketEnabled},
			}}
			h := NewAntiDegradeHandler(service.NewAntiDegradeService(store))
			for _, body := range []string{`{"enabled":true}`, `{"enabled":true}`, `{"enabled":false,"confirm_disable":true}`, `{"enabled":true}`} {
				account := protectionTicketHandlerRequest(t, h.SetProtection, body)
				require.Equal(t, ticketEnabled, store.account.Extra[service.OpenAICodexTicketEnabledExtraKey])
				require.Equal(t, &ticketEnabled, account.CodexTicketEnabled)
			}
		})
	}
}

func TestProtectionHandlerApplyAndDisableReturnActualTicketSwitch(t *testing.T) {
	for _, ticketEnabled := range []bool{false, true} {
		for _, action := range []string{"set_protection", "revert"} {
			t.Run(action+map[bool]string{false: "_ticket_off", true: "_ticket_on"}[ticketEnabled], func(t *testing.T) {
				store := &protectionTicketHandlerStore{account: &service.Account{
					ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Concurrency: 4,
					Extra: map[string]any{service.OpenAICodexTicketEnabledExtraKey: ticketEnabled},
				}}
				h := NewAntiDegradeHandler(service.NewAntiDegradeService(store))
				account := protectionTicketHandlerRequest(t, h.Apply, "")
				require.Equal(t, &ticketEnabled, account.CodexTicketEnabled)
				handler := h.SetProtection
				if action == "revert" {
					handler = h.Revert
				}
				account = protectionTicketHandlerRequest(t, handler, `{"enabled":false,"confirm_disable":true}`)
				require.Equal(t, &ticketEnabled, account.CodexTicketEnabled)
				require.Equal(t, ticketEnabled, store.account.Extra[service.OpenAICodexTicketEnabledExtraKey])
			})
		}
	}
}

func TestProtectionAccountResponseTicketEligibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*service.Account)
		want   *bool
	}{
		{"missing_defaults_on", func(*service.Account) {}, new(true)},
		{"explicit_off", func(a *service.Account) { a.Extra[service.OpenAICodexTicketEnabledExtraKey] = false }, new(false)},
		{"shared_pro_required", func(a *service.Account) {
			a.Extra[service.SharedPoolOwnerKey] = int64(7)
			a.Extra[service.SharedPoolSubscriptionTierKey] = "pro"
			a.Extra[service.OpenAICodexTicketEnabledExtraKey] = false
		}, new(true)},
		{"shadow", func(a *service.Account) { a.ParentAccountID = new(int64(9)) }, nil},
		{"api_key", func(a *service.Account) { a.Type = service.AccountTypeAPIKey }, nil},
		{"anthropic", func(a *service.Account) { a.Platform = service.PlatformAnthropic }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: map[string]any{}}
			tc.mutate(account)
			require.Equal(t, tc.want, protectionAccountResponse(account).CodexTicketEnabled)
		})
	}
	require.Nil(t, protectionAccountResponse(nil))
}
