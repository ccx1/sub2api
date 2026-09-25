//go:build unit

package service

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const pelicanRegressionPrompt = "生成一个鹈鹕骑自行车的完整 HTML 动画"

func pelicanRegressionAccount(platform, accountType string) *Account {
	return &Account{
		ID:          42,
		Name:        "pelican-regression",
		Platform:    platform,
		Type:        accountType,
		Status:      StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "test-api-key",
			"access_token": "test-oauth-token",
			"base_url":     "https://upstream.example",
		},
		Extra: map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
	}
}

func TestPelicanAccountConnectionOpenAIForwardsPromptAndReasoning(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			account := pelicanRegressionAccount(PlatformOpenAI, accountType)
			svc, upstream := adaptiveCNAccountTestService(account,
				adaptiveCNResponsesTestResponse(), adaptiveCNResponsesTestResponse())
			c, recorder := newTestContext()
			err := svc.TestPelicanAccountConnection(c, account.ID, "gpt-6-astra", pelicanRegressionPrompt, "high")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, pelicanRegressionPrompt, gjson.GetBytes(upstream.bodies[0], "input.0.content.0.text").String())
			require.Equal(t, "high", gjson.GetBytes(upstream.bodies[0], "reasoning.effort").String())
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Contains(t, recorder.Body.String(), "responses ok")
			require.Contains(t, recorder.Body.String(), `"success":true`)
			if accountType == AccountTypeOAuth {
				require.Equal(t, chatgptCodexAPIURL, upstream.requests[0].URL.String())
				require.Equal(t, "false", gjson.GetBytes(upstream.bodies[0], "store").Raw)
			} else {
				require.Equal(t, "https://upstream.example/v1/responses", upstream.requests[0].URL.String())
			}

			// 使用同一服务、独立请求验证鹈鹕选项不会污染普通连接探测。
			c, recorder = newTestContext()
			require.NoError(t, svc.TestAccountConnection(c, account.ID, "gpt-6-astra", pelicanRegressionPrompt, AccountTestModeDefault))
			require.Len(t, upstream.requests, 2)
			require.Equal(t, "hi", gjson.GetBytes(upstream.bodies[1], "input.0.content.0.text").String())
			require.False(t, gjson.GetBytes(upstream.bodies[1], "reasoning").Exists())
			require.Contains(t, recorder.Body.String(), `"success":true`)
		})
	}
}

func TestPelicanAccountConnectionClaudeForwardsPromptWithoutChangingOrdinaryProbe(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			account := pelicanRegressionAccount(PlatformAnthropic, accountType)
			svc, upstream := adaptiveCNAccountTestService(account,
				adaptiveCNAnthropicTestResponse(), adaptiveCNAnthropicTestResponse())
			c, recorder := newTestContext()
			err := svc.TestPelicanAccountConnection(c, account.ID, "claude-sonnet-4-5", pelicanRegressionPrompt, "high")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, pelicanRegressionPrompt, gjson.GetBytes(upstream.bodies[0], "messages.0.content.0.text").String())
			require.Contains(t, recorder.Body.String(), "anthropic ok")
			require.Contains(t, recorder.Body.String(), `"success":true`)
			if accountType == AccountTypeOAuth {
				require.Equal(t, testClaudeAPIURL, upstream.requests[0].URL.String())
			} else {
				require.Equal(t, "https://upstream.example/v1/messages?beta=true", upstream.requests[0].URL.String())
			}

			c, recorder = newTestContext()
			require.NoError(t, svc.TestAccountConnection(c, account.ID, "claude-sonnet-4-5", pelicanRegressionPrompt, AccountTestModeDefault))
			require.Len(t, upstream.requests, 2)
			require.Equal(t, "hi", gjson.GetBytes(upstream.bodies[1], "messages.0.content.0.text").String())
			require.False(t, gjson.GetBytes(upstream.bodies[1], "reasoning").Exists())
			require.Contains(t, recorder.Body.String(), `"success":true`)
		})
	}
}

func TestPelicanAccountConnectionReportsUpstreamFailureThroughSSE(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadGateway} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			account := pelicanRegressionAccount(PlatformOpenAI, AccountTypeOAuth)
			svc, upstream := adaptiveCNAccountTestService(account,
				newJSONResponse(status, `{"error":{"message":"upstream unavailable"}}`))
			c, recorder := newTestContext()

			err := svc.TestPelicanAccountConnection(c, account.ID, "gpt-6-astra", pelicanRegressionPrompt, "medium")

			require.ErrorContains(t, err, fmt.Sprintf("API returned %d", status))
			require.Len(t, upstream.requests, 1)
			require.Equal(t, pelicanRegressionPrompt, gjson.GetBytes(upstream.bodies[0], "input.0.content.0.text").String())
			require.Equal(t, "medium", gjson.GetBytes(upstream.bodies[0], "reasoning.effort").String())
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Contains(t, recorder.Body.String(), `"type":"error"`)
			require.Contains(t, recorder.Body.String(), "upstream unavailable")
			require.NotContains(t, recorder.Body.String(), `"success":true`)
		})
	}
}

func TestPelicanPayloadsDoNotChangeDefaultAccountTestPayloads(t *testing.T) {
	defaultClaude, err := createTestPayload("claude-sonnet-4-6")
	require.NoError(t, err)
	defaultMessages, ok := defaultClaude["messages"].([]map[string]any)
	require.True(t, ok)
	defaultContent, ok := defaultMessages[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "hi", defaultContent[0]["text"])

	defaultOpenAI := createOpenAITestPayload("gpt-6-astra", true)
	defaultInput, ok := defaultOpenAI["input"].([]map[string]any)
	require.True(t, ok)
	defaultOpenAIContent, ok := defaultInput[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "hi", defaultOpenAIContent[0]["text"])
	require.NotContains(t, defaultOpenAI, "reasoning")

	pelicanClaude, err := createPelicanClaudePayload("claude-sonnet-4-6", "draw the pelican animation")
	require.NoError(t, err)
	pelicanMessages, ok := pelicanClaude["messages"].([]map[string]any)
	require.True(t, ok)
	pelicanContent, ok := pelicanMessages[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "draw the pelican animation", pelicanContent[0]["text"])

	pelicanOpenAI := createPelicanOpenAIPayload("gpt-6-astra", true, "draw the pelican animation", "medium")
	pelicanInput, ok := pelicanOpenAI["input"].([]map[string]any)
	require.True(t, ok)
	pelicanOpenAIContent, ok := pelicanInput[0]["content"].([]map[string]any)
	require.True(t, ok)
	require.Equal(t, "draw the pelican animation", pelicanOpenAIContent[0]["text"])
	require.Equal(t, map[string]any{"effort": "medium"}, pelicanOpenAI["reasoning"])
}
