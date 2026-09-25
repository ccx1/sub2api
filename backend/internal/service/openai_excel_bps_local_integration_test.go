package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSAdminProbeRespectsModelSelection(t *testing.T) {
	cases := []struct {
		name, model, selected, host string
	}{
		{"selected", "gpt-6-astra", "gpt-6-astra", "bps.openai.com"},
		{"mapped alias", "alias", "gpt-6-astra", "bps.openai.com"},
		{"native model", "gpt-6-sol", "gpt-6-astra", "chatgpt.com"},
		{"default model", "", openai.DefaultTestModel, "bps.openai.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"probe\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
			account := excelAccount()
			account.Extra["openai_excel_bps_models"] = []string{tc.selected}
			account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra"}
			gateway := openAIClientToolsTestService(upstream)
			svc := &AccountTestService{openaiGatewayService: gateway, httpUpstream: upstream}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			require.NoError(t, svc.testOpenAIAccountConnection(c, account, tc.model, "probe", ""))
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, tc.host, upstream.lastReq.URL.Host)
		})
	}
}
