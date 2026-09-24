package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ticketHistoryAdminStub struct {
	service.AdminService
	account *service.Account
	err     error
	calls   []int64
}

func (s *ticketHistoryAdminStub) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	s.calls = append(s.calls, id)
	return s.account, s.err
}

func ticketHistoryRequest(stub *ticketHistoryAdminStub, target string) *httptest.ResponseRecorder {
	router := gin.New()
	handler := &AccountHandler{adminService: stub}
	router.GET("/accounts/:id/codex-ticket/history", handler.GetCodexTicketHistory)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func ticketHistoryAccount() *service.Account {
	return &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
}

func TestGetCodexTicketHistoryDefaultsAndDisabledAccount(t *testing.T) {
	account := ticketHistoryAccount()
	account.Type = service.AccountTypeSetupToken
	account.Extra = map[string]any{service.OpenAICodexTicketEnabledExtraKey: false}
	stub := &ticketHistoryAdminStub{account: account}
	result := ticketHistoryRequest(stub, "/accounts/41/codex-ticket/history")
	require.Equal(t, http.StatusOK, result.Code)
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, "success", envelope.Message)
	var data map[string]any
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.Equal(t, float64(1), data["page"])
	require.Equal(t, float64(20), data["page_size"])
	require.Equal(t, float64(100), data["retained_limit"])
	require.Equal(t, []any{}, data["items"])
	require.Equal(t, float64(0), data["total"])
	require.Equal(t, map[string]any{"total": float64(0), "success": float64(0), "failed": float64(0)}, data["summary"])
	require.Equal(t, []int64{41}, stub.calls)
}

func TestGetCodexTicketHistoryReturnsSelectedPageAndCumulativeSummary(t *testing.T) {
	history := service.CodexTicketHistory{}
	base := time.Now().UTC().Add(-2 * time.Hour)
	for i := range 105 {
		started := base.Add(time.Duration(i) * time.Minute)
		history.Append(service.CodexTicketAttempt{ID: strconv.Itoa(i), Model: "gpt-6-astra",
			StartedAt: started, FinishedAt: started.Add(time.Second), Success: i%2 == 0,
			HarvestProxy: &service.CodexTicketProxySnapshot{Address: "http://proxy.example.test:8080"}})
	}
	account := ticketHistoryAccount()
	account.Extra = map[string]any{service.OpenAICodexTicketHistoryKey: history}
	stub := &ticketHistoryAdminStub{account: account}
	result := ticketHistoryRequest(stub, "/accounts/41/codex-ticket/history?page=2&page_size=20")
	require.Equal(t, http.StatusOK, result.Code)
	var envelope struct {
		Code int                        `json:"code"`
		Data service.CodexTicketHistory `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Equal(t, int64(105), envelope.Data.Summary.Total)
	require.Equal(t, int64(53), envelope.Data.Summary.Success)
	require.Equal(t, int64(52), envelope.Data.Summary.Failed)
	require.Equal(t, 100, envelope.Data.Total)
	require.Equal(t, 2, envelope.Data.Page)
	require.Equal(t, 20, envelope.Data.PageSize)
	require.Len(t, envelope.Data.Items, 20)
	require.Equal(t, "84", envelope.Data.Items[0].ID)
	require.Equal(t, "http://proxy.example.test:8080", envelope.Data.Items[0].HarvestProxy.Address)
	require.Nil(t, envelope.Data.Items[0].BusinessProxy)
}

func TestGetCodexTicketHistoryRejectsInvalidParametersBeforeLoading(t *testing.T) {
	for _, target := range []string{
		"/accounts/nope/codex-ticket/history", "/accounts/0/codex-ticket/history",
		"/accounts/-1/codex-ticket/history", "/accounts/9223372036854775808/codex-ticket/history",
		"/accounts/41/codex-ticket/history?page=0", "/accounts/41/codex-ticket/history?page=oops",
		"/accounts/41/codex-ticket/history?page=9223372036854775808",
		"/accounts/41/codex-ticket/history?page_size=0", "/accounts/41/codex-ticket/history?page_size=101",
		"/accounts/41/codex-ticket/history?page_size=oops",
	} {
		t.Run(target, func(t *testing.T) {
			stub := &ticketHistoryAdminStub{account: ticketHistoryAccount()}
			result := ticketHistoryRequest(stub, target)
			require.Equal(t, http.StatusBadRequest, result.Code)
			require.Empty(t, stub.calls)
		})
	}
}

func TestGetCodexTicketHistoryRejectsUnsupportedAccountTypes(t *testing.T) {
	parentID := int64(1)
	for name, account := range map[string]*service.Account{
		"missing":        nil,
		"apikey":         {ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey},
		"other platform": {ID: 41, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth},
		"shadow":         {ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ParentAccountID: &parentID},
	} {
		t.Run(name, func(t *testing.T) {
			result := ticketHistoryRequest(&ticketHistoryAdminStub{account: account}, "/accounts/41/codex-ticket/history")
			require.Equal(t, http.StatusBadRequest, result.Code)
		})
	}
}

func TestGetCodexTicketHistoryPropagatesReadErrorsWithoutPartialData(t *testing.T) {
	for name, fixture := range map[string]struct {
		stub   *ticketHistoryAdminStub
		status int
	}{
		"missing account":  {&ticketHistoryAdminStub{err: service.ErrAccountNotFound}, http.StatusNotFound},
		"repository error": {&ticketHistoryAdminStub{err: errors.New("repository unavailable")}, http.StatusInternalServerError},
		"corrupt history": {&ticketHistoryAdminStub{account: &service.Account{ID: 41,
			Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Extra: map[string]any{service.OpenAICodexTicketHistoryKey: "malformed-private-history"}}}, http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			result := ticketHistoryRequest(fixture.stub, "/accounts/41/codex-ticket/history")
			require.Equal(t, fixture.status, result.Code)
			var envelope map[string]any
			require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope))
			require.Equal(t, float64(fixture.status), envelope["code"])
			require.NotContains(t, envelope, "data")
			require.NotContains(t, result.Body.String(), "malformed-private-history")
		})
	}
}
