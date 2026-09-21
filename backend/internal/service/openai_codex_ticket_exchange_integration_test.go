package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketExchangeExplainsDynamicModelMismatch(t *testing.T) {
	for _, phase := range []int{1, 2} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(call int) (*http.Response, error) {
				model := "gpt-6-astra"
				if call == phase {
					model = "gpt-6-astra-2026-09-01"
				}
				return codexTicketCompletedResponse(model, fakeCodexTicketState(356)), nil
			})
			require.False(t, item.Success, "诊断能力不能放宽原有模型验证")
			exchange := item.HarvestExchange
			if phase == 2 {
				exchange = item.BusinessExchange
				require.Equal(t, "business_model_mismatch", item.Reason)
			} else {
				require.Equal(t, "harvest_model_mismatch", item.Reason)
				require.Nil(t, item.BusinessExchange, "采集失败时业务复验没有发生")
			}
			require.NotNil(t, exchange)
			require.Equal(t, "gpt-6-astra", exchange.RequestedModel)
			require.Equal(t, []string{"gpt-6-astra-2026-09-01"}, exchange.ReportedModels)
			require.Equal(t, http.MethodPost, exchange.Request.Method)
			require.Contains(t, exchange.Request.Body, `"model":"gpt-6-astra"`)
			require.Contains(t, exchange.Request.Body, "ping")
			require.Equal(t, http.StatusOK, exchange.Response.StatusCode)
			require.Contains(t, exchange.Response.Body, "gpt-6-astra-2026-09-01")
			require.False(t, exchange.Response.BodyTruncated)
			encoded, err := json.Marshal(item)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), fakeCodexTicketState(356))
			require.NotContains(t, string(encoded), "Bearer tok")
		})
	}
}

func TestCodexTicketExchangeKeepsEveryCompletedModel(t *testing.T) {
	item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(int) (*http.Response, error) {
		response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(356))
		response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"wrong-model\"}}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n"))
		return response, nil
	})
	require.Equal(t, "harvest_model_mismatch", item.Reason)
	require.Equal(t, []string{"wrong-model", "gpt-6-astra"}, item.HarvestExchange.ReportedModels)
}

func TestCodexTicketExchangeRecordsRejectedResponseBody(t *testing.T) {
	item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{}, func(int) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"120"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"code":"rate_limit_exceeded","message":"Retry later","access_token":"private-access"}}`))}, nil
	})
	require.Equal(t, "harvest_http_rejected", item.Reason)
	require.Equal(t, 429, item.HarvestExchange.Response.StatusCode)
	require.Contains(t, item.HarvestExchange.Response.Body, "rate_limit_exceeded")
	require.NotContains(t, item.HarvestExchange.Response.Body, "private-access")
	require.Nil(t, item.BusinessExchange)
}

func TestCodexTicketExchangeHistoryLimitsBodiesButRetainsModels(t *testing.T) {
	history := CodexTicketHistory{}
	var firstExchange *CodexTicketExchange
	for i := 0; i < 105; i++ {
		exchange := &CodexTicketExchange{RequestedModel: "requested", ReportedModels: []string{"actual"},
			Request: &CodexTicketHTTPMessage{Body: "request"}, Response: &CodexTicketHTTPMessage{Body: "response"}}
		if i == 0 {
			firstExchange = exchange
		}
		history.Append(CodexTicketAttempt{ID: fmt.Sprint(i), StartedAt: time.Unix(int64(i), 0),
			HarvestExchange: exchange})
	}
	require.NotNil(t, firstExchange.Request, "历史清理不能修改调用者持有的原始记录")
	require.NotNil(t, firstExchange.Response)
	require.Len(t, history.Items, 100)
	for i, item := range history.Items {
		require.Equal(t, []string{"actual"}, item.HarvestExchange.ReportedModels)
		if i < 10 {
			require.NotNil(t, item.HarvestExchange.Request)
			require.NotNil(t, item.HarvestExchange.Response)
		} else {
			require.Nil(t, item.HarvestExchange.Request)
			require.Nil(t, item.HarvestExchange.Response)
		}
	}
	account := ticketTestAccount(41)
	account.Extra = map[string]any{OpenAICodexTicketHistoryKey: history}
	page, err := GetCodexTicketHistory(account, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 10, page.ExchangeRetainedLimit)
	require.Equal(t, 100, page.RetainedLimit)
}
