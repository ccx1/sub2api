package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketPreviewHandlerRejectsMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, id, body string
		unavailable    bool
		status         int
	}{
		{"invalid ID", "bad", `{}`, false, 400},
		{"negative ID", "-1", `{}`, false, 400},
		{"unavailable", "1", `{}`, true, 503},
		{"unknown field", "1", `{"model":"gpt-6-astra","url":"https://other.invalid"}`, false, 400},
		{"unknown nested field", "1", `{"model":"gpt-6-astra","set":[{"name":"Originator","value":"x","other":1}]}`, false, 400},
		{"trailing JSON", "1", `{} {}`, false, 400},
		{"too large", "1", `{"model":"` + strings.Repeat("x", 33<<10) + `"}`, false, 400},
		{"empty body", "1", "", false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			h := &AccountHandler{}
			if !tc.unavailable {
				h.codexTicketRetry = &service.OpenAIGatewayService{}
			}
			router := gin.New()
			router.POST("/accounts/:id/preview", h.PreviewCodexTicketRequest)
			request := httptest.NewRequest(http.MethodPost, "/accounts/"+tc.id+"/preview", strings.NewReader(tc.body))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
		})
	}
}
