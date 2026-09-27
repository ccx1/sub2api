package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetCodexTicketEnabledRejectsEnableWhileExcelBPSAllModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name    string
		extra   map[string]any
		body    string
		status  int
		updates int
	}{
		{"enable blocked by all-model BPS", map[string]any{"openai_excel_bps": true}, `{"enabled":true}`, http.StatusBadRequest, 0},
		{"disable allowed under BPS", map[string]any{"openai_excel_bps": true}, `{"enabled":false}`, http.StatusOK, 1},
		{"enable allowed with scoped BPS", map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []any{"gpt-6-astra"}}, `{"enabled":true}`, http.StatusOK, 1},
		{"enable allowed after BPS off", map[string]any{"openai_excel_bps": false}, `{"enabled":true}`, http.StatusOK, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStubAdminService()
			stub.getAccountResult = &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: tt.extra}
			handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.PUT("/accounts/:id/codex-ticket", handler.SetCodexTicketEnabled)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/accounts/1/codex-ticket", bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, tt.status, recorder.Code, recorder.Body.String())
			require.Equal(t, tt.updates, stub.updateAccountExtraCalls)
			if tt.status == http.StatusBadRequest {
				var responseBody struct {
					Reason string `json:"reason"`
				}
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseBody))
				require.Equal(t, "CODEX_TICKET_EXCEL_BPS_CONFLICT", responseBody.Reason)
			}
		})
	}
}
