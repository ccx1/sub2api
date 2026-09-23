package service

import (
	"encoding/json"
	"errors"
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
	in := openAICodexTicketProbeInput{Token: "access-private", Model: "gpt-test", Attempt: &CodexTicketAttempt{}}
	c := startCodexTicketExchange(in, req)
	require.Same(t, c.exchange, in.Attempt.HarvestExchange)
	require.Nil(t, in.Attempt.BusinessExchange)
	return c, req
}

func TestCodexTicketExchangeRawRequestRetainsHeadersURLAndBodyWithoutConsuming(t *testing.T) {
	raw := "{\n  \"model\" : \"gpt-test\", \"token\" : \"access-private\", \"input\" : \"ping\"\n}\n"
	c, req := newExchangeTestCapture(t, raw)
	remaining, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, raw, string(remaining))
	require.Equal(t, "raw", c.exchange.CaptureMode)
	require.Equal(t, raw, c.exchange.Request.Body)
	require.Equal(t, int64(len(raw)), c.exchange.Request.BodyBytes)
	require.False(t, c.exchange.Request.BodyTruncated)
	require.Equal(t, req.URL.String(), c.exchange.Request.URL)
	require.Equal(t, map[string][]string(req.Header), c.exchange.Request.Headers)
	req.Header.Set("Authorization", "changed-after-capture")
	require.Equal(t, []string{"Bearer access-private"}, c.exchange.Request.Headers["Authorization"])
}

func TestCodexTicketExchangeRawResponsePreservesEveryByteAcrossChunks(t *testing.T) {
	for name, raw := range map[string]string{
		"JSON whitespace": " \n{ \"model\" : \"gpt-other\", \"access_token\" : \"new-private\" }\r\n",
		"SSE whitespace":  ": keepalive\r\n\r\nevent: response.completed\r\ndata: { \"response\" : { \"model\" : \"gpt-other\", \"state\" : \"raw-state\" } }\r\n\r\ndata: [DONE]\n\n",
		"HTML":            "<html>opaque-private &amp; raw error</html>\n",
		"incomplete JSON": "{\"password\":\"truncated-private",
		"incomplete SSE":  "data: {\"token\":\"private\"}\n",
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := newExchangeTestCapture(t, "{}")
			headers := http.Header{"Set-Cookie": {"sid=session-private; HttpOnly", "another=private"}, "X-Unknown": {"opaque-private"},
				http.CanonicalHeaderKey(openAICodexTurnStateHeader): {"gAAAAAraw-ticket"}}
			response := &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(raw))}
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
			require.Equal(t, raw, received.String())
			require.Equal(t, raw, c.exchange.Response.Body)
			require.Equal(t, int64(len(raw)), c.exchange.Response.BodyBytes)
			require.False(t, c.exchange.Response.BodyTruncated, "JSON/SSE语法不完整不应冒充采集截断")
			require.Equal(t, map[string][]string(headers), c.exchange.Response.Headers)
			require.False(t, c.exchange.Response.HeadersTruncated)
		})
	}
}

func TestCodexTicketExchangeRawBodyLimitRetainsPrefix(t *testing.T) {
	for _, size := range []int{codexTicketCaptureLimit, codexTicketCaptureLimit + 193} {
		c, _ := newExchangeTestCapture(t, "{}")
		raw := strings.Repeat("x", size)
		response := &http.Response{Body: io.NopCloser(strings.NewReader(raw))}
		c.captureResponse(response)
		reader := response.Body.(*codexTicketCaptureBody)
		_, err := io.Copy(io.Discard, reader)
		require.NoError(t, err)
		require.LessOrEqual(t, len(reader.body), codexTicketCaptureLimit)
		require.LessOrEqual(t, cap(reader.body), codexTicketCaptureLimit)
		require.NoError(t, reader.Close())
		require.Equal(t, int64(size), c.exchange.Response.BodyBytes)
		require.Equal(t, raw[:min(size, codexTicketCaptureLimit)], c.exchange.Response.Body)
		require.Equal(t, size > codexTicketCaptureLimit, c.exchange.Response.BodyTruncated)
		require.Nil(t, reader.body)
	}
	c, _ := newExchangeTestCapture(t, strings.Repeat("y", codexTicketCaptureLimit+100))
	require.Equal(t, strings.Repeat("y", codexTicketCaptureLimit), c.exchange.Request.Body)
	require.Equal(t, int64(codexTicketCaptureLimit+1), c.exchange.Request.BodyBytes)
	require.True(t, c.exchange.Request.BodyTruncated)
}

type codexTicketExchangeErrorBody struct{ readError, closeError error }

func (b *codexTicketExchangeErrorBody) Read(p []byte) (int, error) {
	return copy(p, "partial-private"), b.readError
}
func (b *codexTicketExchangeErrorBody) Close() error { return b.closeError }

func TestCodexTicketExchangeRawPartialReadPreservesBytesAndErrors(t *testing.T) {
	c, _ := newExchangeTestCapture(t, "{}")
	readError, closeError := errors.New("read-test"), errors.New("close-test")
	response := &http.Response{Body: &codexTicketExchangeErrorBody{readError: readError, closeError: closeError}}
	c.captureResponse(response)
	buffer := make([]byte, 100)
	n, err := response.Body.Read(buffer)
	require.ErrorIs(t, err, readError)
	require.Equal(t, "partial-private", string(buffer[:n]))
	require.ErrorIs(t, response.Body.Close(), closeError)
	require.Equal(t, "partial-private", c.exchange.Response.Body)
	require.True(t, c.exchange.Response.BodyTruncated)
	require.Equal(t, int64(n), c.exchange.Response.BodyBytes)
}

func TestCodexTicketExchangeRawHeadersHaveEncodedSizeBound(t *testing.T) {
	c, _ := newExchangeTestCapture(t, "{}")
	headers := http.Header{"Authorization": {"Bearer private", "second-private"}, "X-Large": {strings.Repeat("中文\"<&", 10000)}, "Z-Last": {"after-limit"}}
	c.captureResponse(&http.Response{Header: headers})
	require.True(t, c.exchange.Response.HeadersTruncated)
	require.Equal(t, []string{"Bearer private", "second-private"}, c.exchange.Response.Headers["Authorization"])
	prefix := c.exchange.Response.Headers["X-Large"][0]
	require.NotEmpty(t, prefix)
	require.True(t, strings.HasPrefix(headers["X-Large"][0], prefix))
	require.NotContains(t, c.exchange.Response.Headers, "Z-Last")
	encoded, err := json.Marshal(c.exchange.Response.Headers)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), codexTicketHeaderLimit)
}

func TestCodexTicketExchangeRawModelsAreBoundedWithoutRedaction(t *testing.T) {
	c, _ := newExchangeTestCapture(t, "{}")
	c.setReportedModels([]string{"gpt-other", "access-private", strings.Repeat("m", 1000)}, false)
	require.Equal(t, []string{"gpt-other", "access-private", strings.Repeat("m", 160)}, c.exchange.ReportedModels)
	require.True(t, c.exchange.ModelsTruncated)
	c.setReportedModels(make([]string, 20), false)
	require.Len(t, c.exchange.ReportedModels, 16)
	require.True(t, c.exchange.ModelsTruncated)
}

func TestCodexTicketExchangeRawNilSafetyAndBusinessStage(t *testing.T) {
	var c *codexTicketExchangeCapture
	c.captureResponse(nil)
	c.setReportedModels(nil, false)
	req, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	require.Nil(t, startCodexTicketExchange(openAICodexTicketProbeInput{}, req), "普通业务无Attempt不能生成报文")
	original := io.NopCloser(strings.NewReader("unchanged business body"))
	response := &http.Response{Body: original}
	c.captureResponse(response)
	require.Equal(t, original, response.Body)
	attempt := &CodexTicketAttempt{}
	c = startCodexTicketExchange(openAICodexTicketProbeInput{State: "old-private-state", Model: "old-private-state", Attempt: attempt}, req)
	require.Same(t, c.exchange, attempt.BusinessExchange)
	require.Nil(t, attempt.HarvestExchange)
	require.Equal(t, "old-private-state", c.exchange.RequestedModel)
	require.Equal(t, "raw", c.exchange.CaptureMode)
	c.captureResponse(nil)
}

func TestCodexTicketExchangeRawMissingRequestCloneDoesNotReadOriginal(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.test", io.NopCloser(strings.NewReader("untouched")))
	require.NoError(t, err)
	c := startCodexTicketExchange(openAICodexTicketProbeInput{Attempt: &CodexTicketAttempt{}}, req)
	require.True(t, c.exchange.Request.BodyTruncated)
	require.Empty(t, c.exchange.Request.Body)
	remaining, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, "untouched", string(remaining))
}

func TestCodexTicketExchangeLegacyCaptureModeRemainsUnspecified(t *testing.T) {
	var legacy CodexTicketExchange
	require.NoError(t, json.Unmarshal([]byte(`{"requested_model":"gpt-test","request":{"body":"[REDACTED]","body_bytes":10}}`), &legacy))
	require.Empty(t, legacy.CaptureMode)
}
