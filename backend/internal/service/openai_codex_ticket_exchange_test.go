package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newExchangeTestCapture(t *testing.T, requestBody string) (*codexTicketExchangeCapture, *http.Request) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://user:pass@example.test/responses?token=query-private#fragment-private", strings.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer access-private")
	req.Header.Set("Cookie", "session=cookie-private")
	req.Header.Set("X-Unknown", "header-private")
	req.Header.Set("Chatgpt-Account-Id", "header-account-private")
	req.Header.Set("Content-Type", "application/json")
	in := openAICodexTicketProbeInput{Token: "access-private", Model: "gpt-test", ProxyURL: "socks5://proxy-user:proxy-p%40ss@127.0.0.1:1080", Attempt: &CodexTicketAttempt{},
		Account: &Account{Credentials: map[string]any{"refresh_token": "refresh-private", "chatgpt_account_id": "chat-account-private", "account_id": "account-private", "user_id": "user-private", "nested": map[string]any{"password": "password-private"}}}}
	c := startCodexTicketExchange(in, req)
	require.Same(t, c.exchange, in.Attempt.HarvestExchange)
	require.Nil(t, in.Attempt.BusinessExchange)
	return c, req
}

func TestCodexTicketExchangeJSONRedactsCredentialsWithoutConsumingRequest(t *testing.T) {
	raw := `{"model":"gpt-test","input":"ping","credential":{"token":"field-private"},"echo":"access-private refresh-private password-private cookie-private proxy-user proxy-p@ss proxy-user:proxy-p%40ss header-account-private chat-account-private account-private user-private"}`
	c, req := newExchangeTestCapture(t, raw)
	remaining, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, raw, string(remaining))
	require.Equal(t, int64(len(raw)), c.exchange.Request.BodyBytes)
	require.False(t, c.exchange.Request.BodyTruncated)
	require.Equal(t, "https://example.test/responses", c.exchange.Request.URL)
	for _, secret := range []string{"access-private", "refresh-private", "password-private", "cookie-private", "field-private", "header-private", "query-private", "fragment-private", "user:pass", "proxy-user", "proxy-p@ss", "proxy-p%40ss", "header-account-private", "chat-account-private", "account-private", "user-private"} {
		encoded, err := json.Marshal(c.exchange)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), secret)
	}
	require.Contains(t, c.exchange.Request.Body, `"model":"gpt-test"`)
	require.Equal(t, []string{codexTicketRedacted}, c.exchange.Request.Headers["Authorization"])
	require.Equal(t, []string{"application/json"}, c.exchange.Request.Headers["Content-Type"])
}

func TestCodexTicketExchangeSSEPreservesDiagnosticContentAcrossChunks(t *testing.T) {
	c, _ := newExchangeTestCapture(t, `{"input":"ping"}`)
	ticket := "gAAAAA" + strings.Repeat("x", 90)
	raw := "event: response.completed\r\ndata: {\"type\":\"response.completed\",\r\ndata: \"response\":{\"model\":\"gpt-other\",\"output\":\"pong\",\"state\":\"" + ticket + "\",\"note\":\"refresh-private session-private proxy-user proxy-p@ss response-account-private\",\"error\":null}}\r\n\r\ndata: [DONE]\r\n\r\n"
	response := &http.Response{StatusCode: 200, Header: http.Header{
		"Content-Type": {"text/event-stream"}, "X-Request-Id": {"request-visible"},
		"Set-Cookie": {"sid=session-private; HttpOnly"}, http.CanonicalHeaderKey(openAICodexTurnStateHeader): {ticket},
		"Chatgpt-Account-Id": {"response-account-private"},
	}, Body: io.NopCloser(strings.NewReader(raw))}
	c.captureResponse(response)
	var received strings.Builder
	buffer := make([]byte, 7)
	for {
		n, err := response.Body.Read(buffer)
		received.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	require.NoError(t, response.Body.Close())
	c.setReportedModels([]string{"gpt-other", ticket, "refresh-private", "proxy-p@ss", "chat-account-private", "response-account-private"}, false)
	require.Equal(t, raw, received.String())
	require.Equal(t, int64(len(raw)), c.exchange.Response.BodyBytes)
	require.False(t, c.exchange.Response.BodyTruncated)
	require.Contains(t, c.exchange.Response.Body, `"model":"gpt-other"`)
	require.Contains(t, c.exchange.Response.Body, `"output":"pong"`)
	require.Contains(t, c.exchange.Response.Body, "data: [DONE]")
	encoded, err := json.Marshal(c.exchange)
	require.NoError(t, err)
	for _, secret := range []string{ticket, "refresh-private", "session-private", "proxy-user", "proxy-p@ss", "chat-account-private", "response-account-private"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestCodexTicketExchangeShortIdentityPreservesActualModel(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.test/responses", strings.NewReader(`{"model":"gpt-5.1","message":"account 1 unavailable","user_id":"1"}`))
	require.NoError(t, err)
	req.Header.Set("Chatgpt-Account-Id", "1")
	c := startCodexTicketExchange(openAICodexTicketProbeInput{Model: "gpt-5.1", Attempt: &CodexTicketAttempt{},
		Account: &Account{Credentials: map[string]any{"user_id": "1"}}}, req)
	c.setReportedModels([]string{"gpt-5.1", "1"}, false)
	require.Equal(t, "gpt-5.1", c.exchange.RequestedModel)
	require.Equal(t, []string{"gpt-5.1", codexTicketRedacted}, c.exchange.ReportedModels)
	require.Contains(t, c.exchange.Request.Body, `"model":"gpt-5.1"`)
	require.Contains(t, c.exchange.Request.Body, `"message":"account [REDACTED] unavailable"`)
	require.NotContains(t, c.exchange.Request.Body, `"user_id":"1"`)
}

func TestCodexTicketExchangeLearnsResponseSecretsBeforePublishingAnyEcho(t *testing.T) {
	for name, raw := range map[string]string{
		"JSON":             `{"model":"gpt-other","message":"opaque-new fresh-user-id","access_token":"opaque-new","user_id":"fresh-user-id"}`,
		"numeric identity": `{"model":"gpt-other","user_id":123456789,"echo":123456789,"chatgpt_organization_id":"organization-private","message":"organization-private","status":200,"access_token":"opaque-new","nested":{"user_id":"fresh-user-id"}}`,
		"SSE later event":  "data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-other\",\"output\":\"opaque-new fresh-user-id\"}}\n\ndata: {\"access_token\":\"opaque-new\",\"user_id\":\"fresh-user-id\"}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := newExchangeTestCapture(t, `{"input":"echo opaque-new fresh-user-id"}`)
			response := &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": {"opaque-new"}}, Body: io.NopCloser(strings.NewReader(raw))}
			c.captureResponse(response)
			_, err := io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			c.setReportedModels([]string{"gpt-other", "opaque-new", "fresh-user-id"}, false)
			require.NoError(t, response.Body.Close())
			encoded, err := json.Marshal(c.exchange)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "opaque-new")
			require.NotContains(t, string(encoded), "fresh-user-id")
			require.NotContains(t, string(encoded), "123456789")
			require.NotContains(t, string(encoded), "organization-private")
			require.False(t, c.exchange.Response.BodyTruncated)
			require.Contains(t, c.exchange.Response.Body, `"model":"gpt-other"`)
			require.Equal(t, "gpt-other", c.exchange.ReportedModels[0])
		})
	}
}

func TestCodexTicketExchangeOmitsUnsafeAndOversizedBodies(t *testing.T) {
	for _, raw := range []string{
		`{"password":"truncated-private`,
		"data: {\"token\":\"truncated-private\"}\n",
		"<html>private unknown content</html>",
		`{"output":"` + strings.Repeat("x", codexTicketBodyLimit) + `"}`,
		`{"token":"` + strings.Repeat("s", codexTicketCaptureLimit) + `"}`,
	} {
		c, _ := newExchangeTestCapture(t, `{}`)
		response := &http.Response{Body: io.NopCloser(strings.NewReader(raw))}
		c.captureResponse(response)
		reader := response.Body.(*codexTicketCaptureBody)
		_, err := io.Copy(io.Discard, reader)
		require.NoError(t, err)
		require.LessOrEqual(t, len(reader.body), codexTicketCaptureLimit)
		require.LessOrEqual(t, cap(reader.body), codexTicketCaptureLimit)
		require.NoError(t, reader.Close())
		require.Equal(t, int64(len(raw)), c.exchange.Response.BodyBytes)
		require.True(t, c.exchange.Response.BodyTruncated)
		require.Equal(t, codexTicketBodyOmitted, c.exchange.Response.Body)
		require.Nil(t, reader.body)
	}
}

func TestCodexTicketExchangePartialReadAndMissingCloneDoNotLeak(t *testing.T) {
	c, _ := newExchangeTestCapture(t, `{}`)
	response := &http.Response{Body: io.NopCloser(strings.NewReader(`{"token":"partially-read-private"}`))}
	c.captureResponse(response)
	_, err := response.Body.Read(make([]byte, 15))
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, codexTicketBodyOmitted, c.exchange.Response.Body)
	require.True(t, c.exchange.Response.BodyTruncated)
	req, err := http.NewRequest(http.MethodPost, "https://example.test", io.NopCloser(strings.NewReader("untouched")))
	require.NoError(t, err)
	c = startCodexTicketExchange(openAICodexTicketProbeInput{State: "old-private-state", Attempt: &CodexTicketAttempt{}}, req)
	require.True(t, c.exchange.Request.BodyTruncated)
	remaining, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, "untouched", string(remaining))
}

func TestCodexTicketExchangeHeadersAndModelsAreBounded(t *testing.T) {
	c, _ := newExchangeTestCapture(t, `{}`)
	headers := http.Header{"X-Request-Id": {strings.Repeat("x", 6000)}, "Authorization": {"other-header-secret"}}
	for i := range 200 {
		headers.Set("X-Unknown-"+strings.Repeat("A", i), "never-visible")
	}
	response := &http.Response{Header: headers}
	c.captureResponse(response)
	c.setReportedModels([]string{strings.Repeat("m", 1000), "other-header-secret", "normal-model"}, false)
	require.True(t, c.exchange.Response.HeadersTruncated)
	require.True(t, c.exchange.ModelsTruncated)
	encoded, err := json.Marshal(c.exchange.Response.Headers)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), codexTicketHeaderLimit)
	encoded, err = json.Marshal(c.exchange)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "other-header-secret")
	require.NotContains(t, string(encoded), "never-visible")
	require.Contains(t, string(encoded), "normal-model")
}

func TestCodexTicketExchangeNilSafeAndBusinessStage(t *testing.T) {
	var c *codexTicketExchangeCapture
	c.captureResponse(nil)
	c.setReportedModels(nil, false)
	req, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	require.Nil(t, startCodexTicketExchange(openAICodexTicketProbeInput{}, req))
	attempt := &CodexTicketAttempt{}
	c = startCodexTicketExchange(openAICodexTicketProbeInput{State: "old-private-state", Model: "old-private-state", Attempt: attempt}, req)
	require.Same(t, c.exchange, attempt.BusinessExchange)
	require.Nil(t, attempt.HarvestExchange)
	require.Equal(t, codexTicketRedacted, c.exchange.RequestedModel)
	c.captureResponse(nil)
}
