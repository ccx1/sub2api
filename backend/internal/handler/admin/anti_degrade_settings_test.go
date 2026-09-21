package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type protectionSettingsHandlerRepo struct {
	service.SettingRepository
	value  string
	err    error
	writes int
}

type protectionSettingsHandlerAccounts struct{ service.AccountRepository }

func (r *protectionSettingsHandlerAccounts) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]service.Account, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

func (r *protectionSettingsHandlerRepo) GetValue(context.Context, string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if r.value == "" {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}

func (r *protectionSettingsHandlerRepo) Set(_ context.Context, key, value string) error {
	if r.err != nil {
		return r.err
	}
	if key != service.SettingKeyAccountProtectionDefaultMode {
		return errors.New("unexpected key")
	}
	r.value = value
	r.writes++
	return nil
}

func protectionSettingsRequest(t *testing.T, h *AntiDegradeHandler, role, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, "/api/v1/admin/accounts/protection/settings", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if role != "" {
		c.Set(string(middleware.ContextKeyUserRole), role)
	}
	if method == http.MethodGet {
		h.GetProtectionSettings(c)
	} else {
		h.UpdateProtectionSettings(c)
	}
	return rec
}

func TestProtectionSettingsHandlerDefaultsAndPersists(t *testing.T) {
	repo := &protectionSettingsHandlerRepo{}
	h := NewAntiDegradeHandler(service.NewAntiDegradeServiceWithSettings(nil, repo, &protectionSettingsHandlerAccounts{}))
	rec := protectionSettingsRequest(t, h, service.RoleAdmin, http.MethodGet, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"default_mode":"mode2"`)
	rec = protectionSettingsRequest(t, h, service.RoleAdmin, http.MethodPut, `{"default_mode":"legacy"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "legacy", repo.value)
	rec = protectionSettingsRequest(t, h, service.RoleAdmin, http.MethodGet, "")
	require.Contains(t, rec.Body.String(), `"default_mode":"legacy"`)
}

func TestProtectionSettingsHandlerRequiresAdminAndValidMode(t *testing.T) {
	repo := &protectionSettingsHandlerRepo{}
	h := NewAntiDegradeHandler(service.NewAntiDegradeServiceWithSettings(nil, repo, &protectionSettingsHandlerAccounts{}))
	for _, role := range []string{"", service.RoleUser} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			rec := protectionSettingsRequest(t, h, role, method, `{"default_mode":"mode2"}`)
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		}
	}
	for _, body := range []string{`{}`, `{"default_mode":""}`, `{"default_mode":"unknown"}`, `{"default_mode":"native_baseline"}`, `{"default_mode":1}`, `not-json`} {
		rec := protectionSettingsRequest(t, h, service.RoleAdmin, http.MethodPut, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	require.Zero(t, repo.writes)
}

func TestProtectionSettingsHandlerReturnsStorageFailure(t *testing.T) {
	repo := &protectionSettingsHandlerRepo{err: errors.New("database unavailable")}
	h := NewAntiDegradeHandler(service.NewAntiDegradeServiceWithSettings(nil, repo, &protectionSettingsHandlerAccounts{}))
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		rec := protectionSettingsRequest(t, h, service.RoleAdmin, method, `{"default_mode":"mode2"}`)
		require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
	require.Zero(t, repo.writes)
}
