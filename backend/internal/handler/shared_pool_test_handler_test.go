//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sharedTestRepository struct {
	service.SharedPoolRepository
	service.AccountRepository
	account  *service.Account
	disabled bool
	reads    int
	updates  map[string]any
	proxy    *service.Proxy
}

func (r *sharedTestRepository) GetSharedAccount(_ context.Context, owner, id int64) (*service.SharedPoolAccountRecord, error) {
	if owner != 7 || id != r.account.ID {
		return nil, service.ErrSharedPoolAccountNotFound
	}
	return &service.SharedPoolAccountRecord{AccountID: id, OwnerUserID: owner, AdminDisabled: r.disabled}, nil
}

func (r *sharedTestRepository) GetByID(context.Context, int64) (*service.Account, error) {
	r.reads++
	copy := *r.account
	return &copy, nil
}

func (r *sharedTestRepository) UpdateExtra(_ context.Context, _ int64, extra map[string]any) error {
	r.updates = extra
	return nil
}

func (r *sharedTestRepository) SelectRandomActiveProxy(context.Context) (*service.Proxy, error) {
	return r.proxy, nil
}

type sharedTestUpstream struct {
	request  *http.Request
	body     []byte
	proxyURL string
	status   int
	response string
	calls    int
	deadline time.Time
}

func (u *sharedTestUpstream) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	u.request, u.proxyURL = req, proxyURL
	u.body, _ = io.ReadAll(req.Body)
	u.deadline, _ = req.Context().Deadline()
	status := u.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(u.response))}, nil
}

func (u *sharedTestUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func newSharedTestHandler() (*SharedPoolHandler, *sharedTestRepository, *sharedTestUpstream) {
	repo := &sharedTestRepository{account: &service.Account{ID: 11, Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Concurrency: 2, Credentials: map[string]any{"api_key": "test-secret", "base_url": "https://upstream.example"},
		Extra: map[string]any{}}}
	upstream := &sharedTestUpstream{response: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n"}
	pool := service.NewSharedPoolService(repo, repo, nil, nil, nil, nil, nil)
	tests := service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, &config.Config{}, nil)
	return NewSharedPoolHandler(pool, nil, nil, nil, tests, nil, nil, nil, nil, nil, nil), repo, upstream
}

func sharedTestContext(owner int64, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/shared-pool/accounts/11/test", strings.NewReader(body))
	c.Params = gin.Params{{Key: "id", Value: "11"}}
	if owner > 0 {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: owner})
	}
	return c, w
}

func TestSharedAccountTestingChecksOwnershipBeforeAccountAccess(t *testing.T) {
	for _, models := range []bool{false, true} {
		for _, tc := range []struct {
			owner    int64
			disabled bool
			status   int
		}{{0, false, 401}, {8, false, 404}, {7, true, 403}} {
			h, repo, upstream := newSharedTestHandler()
			repo.disabled = tc.disabled
			c, w := sharedTestContext(tc.owner, `{}`)
			if models {
				h.GetAvailableModels(c)
			} else {
				h.Test(c)
			}
			require.Equal(t, tc.status, w.Code)
			require.Zero(t, upstream.calls)
			require.Empty(t, h.lastTest)
			if tc.owner != 7 {
				require.Zero(t, repo.reads)
			}
		}
	}
}

func TestSharedAccountTestValidatesBodyBeforeConsumingQuota(t *testing.T) {
	for _, body := range []string{`{"mode":"video"}`, `{"model_id":1}`, `{"prompt":`, `{ } { }`,
		`{"image_data_url":"data:test"}`, strings.Repeat(" ", 128<<10) + `{}`} {
		t.Run(body[:min(len(body), 35)], func(t *testing.T) {
			h, _, upstream := newSharedTestHandler()
			c, w := sharedTestContext(7, body)
			h.Test(c)
			require.Equal(t, 400, w.Code)
			require.Empty(t, h.lastTest)
			require.Zero(t, upstream.calls)
		})
	}
}

func TestSharedAccountTestUsesChosenModelPromptAndProxy(t *testing.T) {
	h, repo, upstream := newSharedTestHandler()
	repo.account.Extra["openai_responses_mode"] = "force_chat_completions"
	repo.account.Extra["proxy_mode"] = "random"
	repo.proxy = &service.Proxy{ID: 4, Protocol: "http", Host: "proxy.example", Port: 8080, Status: service.StatusActive}
	upstream.response = "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
	c, w := sharedTestContext(7, `{"model_id":"chosen-model","prompt":"A short greeting","mode":"default"}`)
	h.Test(c)
	require.NotNil(t, upstream.request, w.Body.String())
	require.Equal(t, "chosen-model", gjson.GetBytes(upstream.body, "model").String())
	require.Contains(t, string(upstream.body), "A short greeting")
	require.Equal(t, "http://proxy.example:8080", upstream.proxyURL)
	require.Contains(t, w.Body.String(), `"success":true`)
	require.NotContains(t, w.Body.String(), "test-secret")
	c, limited := sharedTestContext(7, `{}`)
	h.Test(c)
	require.Equal(t, 429, limited.Code)
	require.Equal(t, 1, upstream.calls)
}

func TestSharedAccountTestRejectsInvalidAccountOrMode(t *testing.T) {
	h, repo, upstream := newSharedTestHandler()
	for _, id := range []string{"0", "invalid", "-1"} {
		c, w := sharedTestContext(7, `{}`)
		c.Params[0].Value = id
		h.Test(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	require.Zero(t, repo.reads)
	repo.account.Platform = service.PlatformGemini
	c, w := sharedTestContext(7, `{"mode":"compact"}`)
	h.Test(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, h.lastTest)
	require.Zero(t, upstream.calls)
}

func TestSharedAccountTestSupportsImagesAndLongRunningRequests(t *testing.T) {
	h, _, upstream := newSharedTestHandler()
	upstream.response = `{"data":[{"b64_json":"aGVsbG8="}]}`
	c, w := sharedTestContext(7, `{"model_id":"gpt-image-2","prompt":"Draw a small cat"}`)
	h.Test(c)
	require.NotNil(t, upstream.request, w.Body.String())
	require.Equal(t, "/v1/images/generations", upstream.request.URL.Path)
	require.Equal(t, "Draw a small cat", gjson.GetBytes(upstream.body, "prompt").String())
	require.Greater(t, time.Until(upstream.deadline), 9*time.Minute)
	require.Contains(t, w.Body.String(), "data:image/png;base64,aGVsbG8=")
	require.Contains(t, w.Body.String(), `"success":true`)
}

func TestSharedAccountTestSupportsNativeCompactProbe(t *testing.T) {
	h, repo, upstream := newSharedTestHandler()
	upstream.response = "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\",\"id\":\"cmp\",\"encrypted_content\":\"blob\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n"
	c, w := sharedTestContext(7, `{"model_id":"gpt-5.4","mode":"compact"}`)
	h.Test(c)
	require.NotNil(t, upstream.request, w.Body.String())
	require.Equal(t, "/v1/responses", upstream.request.URL.Path)
	require.Contains(t, string(upstream.body), "compaction_trigger")
	require.Equal(t, true, repo.updates["openai_compact_supported"])
	require.Contains(t, w.Body.String(), `"success":true`)
}

func TestSharedAccountTestRedactsUpstreamFailures(t *testing.T) {
	h, _, upstream := newSharedTestHandler()
	upstream.status, upstream.response = 500, `{"error":"upstream-secret-credential"}`
	c, w := sharedTestContext(7, `{"model_id":"gpt-image-2"}`)
	h.Test(c)
	require.Contains(t, w.Body.String(), `"type":"error"`)
	require.NotContains(t, w.Body.String(), "upstream-secret-credential")
}

func TestSharedAccountModelsUseCatalogWithoutCredentials(t *testing.T) {
	h, repo, upstream := newSharedTestHandler()
	repo.account.Credentials["model_mapping"] = map[string]any{"owner-visible-model": "internal-model"}
	c, w := sharedTestContext(7, "")
	h.GetAvailableModels(c)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "owner-visible-model", gjson.Get(w.Body.String(), "data.0.id").String())
	require.NotContains(t, w.Body.String(), "test-secret")
	require.NotContains(t, w.Body.String(), "internal-model")
	require.Zero(t, upstream.calls)
}

func TestSharedAccountTestAllowsLegacyEmptyBodyAndSeparateUsers(t *testing.T) {
	h, _, upstream := newSharedTestHandler()
	c, w := sharedTestContext(7, "")
	h.Test(c)
	require.NotEqual(t, 400, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	require.Equal(t, 1, upstream.calls)
	require.True(t, h.allowAccountTest(8))
	require.False(t, h.allowAccountTest(8))
	h.lastTest[7] = time.Now().Add(-2 * time.Minute)
	require.True(t, h.allowAccountTest(7))
}
