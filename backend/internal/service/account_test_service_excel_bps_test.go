package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildExcelBPSAccountTestBodyUsesResponsesContract(t *testing.T) {
	raw, err := buildExcelBPSAccountTestBody("gpt-6-astra", "糖果题")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-6-astra" || body["stream"] != true || body["store"] != false {
		t.Fatalf("unexpected body: %#v", body)
	}
	input, _ := body["input"].([]any)
	item, _ := input[0].(map[string]any)
	contentItems, _ := item["content"].([]any)
	content, _ := contentItems[0].(map[string]any)
	if content["text"] != "糖果题" {
		t.Fatalf("prompt was not preserved: %#v", content)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "medium" {
		t.Fatalf("missing reasoning effort: %#v", body["reasoning"])
	}
}

func TestExcelBPSAccountOnlyUsesOAuth(t *testing.T) {
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_excel_bps": true}}
	apiKey := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"openai_excel_bps": true}}
	if !oauth.IsExcelBPSEnabled() || apiKey.IsExcelBPSEnabled() {
		t.Fatal("Excel BPS gate must be OAuth-only")
	}
}

// The admin probe must reach BPS even when the account is currently not
// schedulable (paused, cooling down, quota exhausted). Turn admission belongs
// to user traffic only; routing the probe through Forward reported
// "request admission denied: account_ineligible" instead of testing the account.
func TestExcelBPSAccountTestBypassesTurnAdmission(t *testing.T) {
	wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"21\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_excel\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"21\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
	account := excelAccount()
	account.Schedulable = false
	latest := *account
	gateway := openAIClientToolsTestService(upstream)
	gateway.accountRepo = &turnAdmissionRepo{account: &latest}
	gateway.requireLatestTurnAdmission = true
	svc := &AccountTestService{openaiGatewayService: gateway}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.NoError(t, svc.testOpenAIAccountConnection(c, account, "gpt-5.6-sol", "", ""))
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
	require.NotContains(t, rec.Body.String(), "admission denied")
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
}

func TestExcelBPSManualTestPreservesNativeProxyAndSessionIdentity(t *testing.T) {
	for _, random := range []bool{false, true} {
		for _, session := range []string{"", "explicit-manual-session"} {
			t.Run(fmt.Sprintf("random=%t/explicit_session=%t", random, session != ""), func(t *testing.T) {
				const wire = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_manual\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n"
				upstream := &httpUpstreamRecorder{}
				for range 2 {
					upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))})
				}
				svc := &AccountTestService{openaiGatewayService: openAIClientToolsTestService(upstream)}
				account := excelAccount()
				account.Proxy = &Proxy{ID: 71, Protocol: "http", Host: "selected-proxy.example", Port: 8080}
				account.ProxyID = &account.Proxy.ID
				if random {
					account.Extra[ProxyModeExtraKey] = ProxyModeRandom
				}
				for range 2 {
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil)
					if session != "" {
						c.Request.Header.Set("Session-Id", session)
					}
					require.NoError(t, svc.testExcelBPSAccountConnection(c, account, "gpt-6-astra", "Reply OK"))
					require.Equal(t, session, c.Request.Header.Get("Session-Id"), "manual probe must not mutate the inbound request")
					require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL, "manual tests retain the already resolved account proxy")
					require.Equal(t, basispoints.ResponsesURL, upstream.lastReq.URL.String())
					require.Contains(t, rec.Body.String(), "test_complete")
				}
				require.Len(t, upstream.bodies, 2)
				first := gjson.GetBytes(upstream.bodies[0], "prompt_cache_key").String()
				second := gjson.GetBytes(upstream.bodies[1], "prompt_cache_key").String()
				require.NotEmpty(t, first)
				if session == "" {
					require.NotEqual(t, first, second, "anonymous tests must not share a conversation")
				} else {
					require.Equal(t, first, second, "explicit load-test sessions remain stable")
				}
			})
		}
	}
}

func TestExcelBPSBackgroundTestHandlesNilHeader(t *testing.T) {
	const wire = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_background\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
	svc := &AccountTestService{openaiGatewayService: openAIClientToolsTestService(upstream)}
	account := excelAccount()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = &http.Request{}
	require.NotPanics(t, func() {
		err := svc.testExcelBPSAccountConnection(c, account, "gpt-6-astra", "Reply OK")
		require.NoError(t, err)
	})
	require.Nil(t, c.Request.Header, "the inbound request must remain unchanged")
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, basispoints.ResponsesURL, upstream.lastReq.URL.String())
}

func TestExcelBPSManualTestReportsRateLimit(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"30"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"PRIVATE_UPSTREAM"}}`))}}
	svc := &AccountTestService{openaiGatewayService: openAIClientToolsTestService(upstream)}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/api/v1/admin/accounts/300/test", nil)

	err := svc.testExcelBPSAccountConnection(c, excelAccount(), "gpt-6-astra", "Reply OK")

	// A single-account test shows the rate limit instead of a failover signal.
	require.EqualError(t, err, excelBPSRateLimitedClientMessage)
	require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
	require.Len(t, upstream.requests, 1)
}
