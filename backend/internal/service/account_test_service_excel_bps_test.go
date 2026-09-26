package service

import (
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
