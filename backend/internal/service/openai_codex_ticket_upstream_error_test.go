package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketUpstreamErrorScopesAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, body, scope string
		wire, effective   int
	}{
		{"capacity", `{"error":{"code":"model_at_capacity"}}`, "model", 200, 429},
		{"generic rate", `{"error":{"type":"rate_limit_error"}}`, "request", 200, 429},
		{"root error", `{"type":"error","code":"usage_limit_reached"}`, "credential", 200, 429},
		{"unauthorized", `{"error":{"code":"invalid_api_key"}}`, "credential", 200, 401},
		{"http", `not json`, "credential", 401, 401},
		{"message not classifier", `{"error":{"message":"usage_limit_reached"}}`, "request", 200, 502},
		{"invalid status", `{"error":{"status_code":429.5}}`, "request", 200, 502},
		{"control character", `{"error":{"type":"usage_limit_reached\u0000"}}`, "request", 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := classifyCodexTicketUpstreamError([]byte(tc.body), tc.wire, "")
			require.NotNil(t, d)
			require.Equal(t, tc.wire, d.WireHTTPStatus)
			require.Equal(t, tc.effective, d.EffectiveStatus)
			require.Equal(t, tc.scope, d.Scope)
		})
	}
	require.Nil(t, classifyCodexTicketUpstreamError([]byte(`{"error":{"type":"usage_limit_reached"}`), 200, ""))
	for _, body := range []string{
		`{"type":"error","code":"usage_limit_reached","resets_in_seconds":60}`,
		`{"error":{"code":"usage_limit_reached","resets_in_seconds":60}}`,
	} {
		d := classifyCodexTicketUpstreamError([]byte(body), 200, "120")
		require.NotNil(t, d.RetryAt)
		require.True(t, d.RetryAt.After(time.Now().Add(119*time.Second)))
	}
	for _, value := range []string{"-1", "1e99", "1.5", `"9999999999999999999999999999"`} {
		body := `{"error":{"type":"usage_limit_reached","resets_in_seconds":` + value + `}}`
		d := classifyCodexTicketUpstreamError([]byte(body), 200, "9999999999999999999999999999")
		require.Nil(t, d.RetryAt)
	}
}

func TestCodexTicketUpstreamNon200PreservesRejectionChain(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_api_key"}}`))}, nil
	}})
	attempt := &CodexTicketAttempt{}
	_, status, err := svc.probeOpenAICodexTicket(context.Background(), openAICodexTicketProbeInput{Account: ticketTestAccount(1), Token: "tok", Model: "gpt-6-astra", Timeout: time.Second, Attempt: attempt})
	require.Equal(t, 401, status)
	var rejected *openAICodexTicketProbeRejected
	require.True(t, errors.As(err, &rejected))
	require.Equal(t, "60", rejected.RetryAfter)
	require.Equal(t, "credential", codexTicketProbeUpstreamError(err).Scope)
	require.Equal(t, 401, attempt.HarvestExchange.UpstreamError.WireHTTPStatus)
}
