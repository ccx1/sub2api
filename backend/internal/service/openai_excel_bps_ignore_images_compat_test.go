package service

import (
	"context"
	"errors"
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

func TestExcelBPSIgnoreImagesRequiresExplicitOptIn(t *testing.T) {
	var missing *Account
	require.False(t, missing.IsExcelBPSIgnoreImagesEnabled())
	for _, raw := range []any{nil, false, "true", 1, true} {
		account := excelAccount()
		account.Extra[ExcelBPSIgnoreImagesKey] = raw
		require.Equal(t, raw == true, account.IsExcelBPSIgnoreImagesEnabled())
		account.Extra[excelBPSExtraKey] = false
		require.False(t, account.IsExcelBPSIgnoreImagesEnabled())
	}
}

func TestExcelBPSIgnoreImagesBulkSetting(t *testing.T) {
	for _, raw := range []any{"true", 1, nil} {
		_, err := normalizeBulkExcelBPSExtra(map[string]any{ExcelBPSIgnoreImagesKey: raw})
		require.Error(t, err)
	}
	extra := map[string]any{excelBPSExtraKey: true, ExcelBPSIgnoreImagesKey: true}
	changed, err := normalizeBulkExcelBPSExtra(extra)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, true, extra[ExcelBPSIgnoreImagesKey])
	extra[excelBPSExtraKey] = false
	_, err = normalizeBulkExcelBPSExtra(extra)
	require.NoError(t, err)
	require.Equal(t, false, extra[ExcelBPSIgnoreImagesKey])
	extra = map[string]any{excelBPSExtraKey: false}
	_, err = normalizeBulkExcelBPSExtra(extra)
	require.NoError(t, err)
	require.NotContains(t, extra, ExcelBPSIgnoreImagesKey)
}

func TestExcelBPSIgnoreImagesTextSkipsUnavailableSettings(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_text\",\"status\":\"completed\",\"output\":[]}}\n\n"))}}
	svc := openAIClientToolsTestService(upstream)
	svc.settingService = NewSettingService(&excelBPSImageSettingsRepo{err: errors.New("image settings unavailable")}, svc.cfg)
	account := excelAccount()
	account.Extra[ExcelBPSIgnoreImagesKey] = true
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":[{"type":"input_text","text":"input_image and data:image are literal text"}]}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, upstream.requests, 1)
	require.Contains(t, string(upstream.lastBody), "input_image and data:image are literal text")
}

func TestExcelBPSContentValidationParamWithoutPayload(t *testing.T) {
	svc := openAIClientToolsTestService(&httpUpstreamRecorder{})
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":[{"type":"encrypted_content","encrypted_content":"PRIVATE_CIPHERTEXT"}]}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := svc.Forward(context.Background(), c, excelAccount(), body)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "input[0].content[0]", gjson.Get(rec.Body.String(), "error.param").String())
	require.NotContains(t, rec.Body.String(), "PRIVATE_CIPHERTEXT")
}

func TestExcelBPSIgnoreImagesEnabledPreservesInlineImages(t *testing.T) {
	for _, mode := range []string{ExcelBPSImageModeNative, ExcelBPSImageModeRelay} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DATA_DIR", t.TempDir())
			svc := openAIClientToolsTestService(nil)
			t.Cleanup(func() { require.NoError(t, svc.CloseExcelBPSImages()) })
			svc.settingService = NewSettingService(&excelBPSImageSettingsRepo{values: map[string]string{
				SettingKeyExcelBPSImageRelayEnabled: "true", SettingKeyExcelBPSImageMode: mode, SettingKeyExcelBPSImageBaseURL: "https://images.example",
			}}, svc.cfg)
			uploads, responses := 0, 0
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				payload := `{"openai_file_id":"file-optinimage"}`
				if req.URL.String() == basispoints.AttachmentsURL {
					uploads++
				} else {
					responses++
					require.Equal(t, basispoints.ResponsesURL, req.URL.String())
					raw, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Contains(t, string(raw), `"type":"input_image"`)
					require.NotContains(t, string(raw), "Image input is unavailable")
					require.NotContains(t, string(raw), "data:image")
					payload = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_optin\",\"status\":\"completed\",\"output\":[]}}\n\n"
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}, nil
			}}
			account := excelAccount()
			account.Extra[ExcelBPSIgnoreImagesKey] = true
			body, _ := nativeGatewayBody(t)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, 1, responses)
			require.Equal(t, mode == ExcelBPSImageModeNative, uploads == 1)
		})
	}
}
